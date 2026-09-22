package similarity

import "sort"

// compactGraphBuilder collects scored edges during BuildGraph without
// locks or maps. Every edge is stored once, in the row of its smaller
// endpoint. That row must have a single writer that appends columns in
// ascending order, which BuildGraph's stripe partition guarantees: the
// worker that owns row i scores pairs (i, j) for j = i+1, i+2, ...
type compactGraphBuilder struct {
	upper [][]rowEdge
}

type rowEdge struct {
	col   int32
	score float64
}

func newCompactGraphBuilder(size int) *compactGraphBuilder {
	return &compactGraphBuilder{upper: make([][]rowEdge, size)}
}

// SetScore records a positive edge. Zero scores are absent edges.
func (b *compactGraphBuilder) SetScore(i, j int, score float64) {
	if i == j || score == 0 {
		return
	}
	if i > j {
		i, j = j, i
	}
	b.upper[i] = append(b.upper[i], rowEdge{col: int32(j), score: score})
}

type compactLayout int

const (
	layoutAuto compactLayout = iota
	layoutRows
	layoutMatrix
)

// freeze converts the collected edges into a read-mostly graph. With
// layoutAuto it picks whichever storage is smaller: a dense matrix costs
// 8 bytes per ordered pair, compressed rows 12 bytes per stored edge
// direction. Real repositories give most comparable pairs a nonzero
// score, so the dense matrix usually wins.
func (b *compactGraphBuilder) freeze(layout compactLayout) MutableGraph {
	n := len(b.upper)
	edges := 0
	for _, row := range b.upper {
		edges += len(row)
	}
	if layout == layoutAuto {
		denseBytes := int64(n) * int64(n) * 8
		rowBytes := int64(edges)*2*12 + int64(n+1)*8
		layout = layoutRows
		if denseBytes <= rowBytes {
			layout = layoutMatrix
		}
	}
	if layout == layoutMatrix {
		graph := NewDenseGraph(n)
		for i, row := range b.upper {
			for _, e := range row {
				graph.SetScore(i, int(e.col), e.score)
			}
			b.upper[i] = nil // release as we go to bound peak memory
		}
		return graph
	}

	// Compressed sparse rows, symmetric. Row r holds its lower neighbors
	// (appended while earlier rows are processed, in ascending order)
	// followed by its upper neighbors (ascending by construction).
	offsets := make([]int, n+1)
	for i, row := range b.upper {
		offsets[i+1] += len(row)
		for _, e := range row {
			offsets[int(e.col)+1]++
		}
	}
	for i := 0; i < n; i++ {
		offsets[i+1] += offsets[i]
	}
	cols := make([]int32, edges*2)
	scores := make([]float64, edges*2)
	fill := append([]int(nil), offsets[:n]...)
	for i, row := range b.upper {
		for _, e := range row {
			j := int(e.col)
			cols[fill[i]], scores[fill[i]] = e.col, e.score
			fill[i]++
			cols[fill[j]], scores[fill[j]] = int32(i), e.score
			fill[j]++
		}
		b.upper[i] = nil
	}
	return &rowGraph{offsets: offsets, cols: cols, scores: scores}
}

// rowGraph is a symmetric compressed-sparse-row similarity graph: row r's
// neighbors are cols[offsets[r]:offsets[r+1]], ascending, with matching
// scores. Absent edges score zero and the diagonal scores one.
type rowGraph struct {
	offsets []int
	cols    []int32
	scores  []float64
}

func (g *rowGraph) Len() int { return len(g.offsets) - 1 }

// find returns the position of col in row, or -1.
func (g *rowGraph) find(row, col int) int {
	lo, hi := g.offsets[row], g.offsets[row+1]
	k := lo + sort.Search(hi-lo, func(i int) bool { return int(g.cols[lo+i]) >= col })
	if k < hi && int(g.cols[k]) == col {
		return k
	}
	return -1
}

func (g *rowGraph) Score(a, b int) float64 {
	if a == b {
		return 1
	}
	if k := g.find(a, b); k >= 0 {
		return g.scores[k]
	}
	return 0
}

// SetScore updates an existing edge in both directions; it exists for
// pair ignores, which only ever zero edges. Adding an edge after the
// build would break the packed layout, so it panics.
func (g *rowGraph) SetScore(a, b int, score float64) {
	if a == b {
		return
	}
	ka, kb := g.find(a, b), g.find(b, a)
	if ka < 0 {
		if score == 0 {
			return
		}
		panic("similarity: cannot add an edge to a frozen graph")
	}
	g.scores[ka], g.scores[kb] = score, score
}

func (g *rowGraph) ForEachNeighbor(index int, minScore float64, visit func(index int, score float64)) {
	for k := g.offsets[index]; k < g.offsets[index+1]; k++ {
		if s := g.scores[k]; s > 0 && s >= minScore {
			visit(int(g.cols[k]), s)
		}
	}
}

func (g *rowGraph) ForEachEdge(minScore float64, visit func(a, b int, score float64)) {
	for a := 0; a < g.Len(); a++ {
		for k := g.offsets[a]; k < g.offsets[a+1]; k++ {
			if b := int(g.cols[k]); b > a {
				if s := g.scores[k]; s > 0 && s >= minScore {
					visit(a, b, s)
				}
			}
		}
	}
}
