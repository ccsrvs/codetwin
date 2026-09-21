package similarity

import (
	"sort"
	"sync"
)

// Graph is the read-only similarity view consumed by clustering and report
// construction. Implementations must visit neighbors and edges in ascending
// index order so results stay deterministic across storage strategies.
type Graph interface {
	Len() int
	Score(a, b int) float64
	ForEachNeighbor(index int, minScore float64, visit func(index int, score float64))
	ForEachEdge(minScore float64, visit func(a, b int, score float64))
}

// MutableGraph adds the score update needed while building a graph and while
// applying configured pair ignores.
type MutableGraph interface {
	Graph
	SetScore(a, b int, score float64)
}

// DenseGraph preserves the current dense storage implementation behind the
// Graph contract. It is intentionally unsynchronized: BuildGraph assigns each
// unordered pair to exactly one worker, so concurrent writes touch disjoint
// cells.
type DenseGraph struct {
	scores [][]float64
}

// NewDenseGraph returns a graph with size vertices, identity scores on the
// diagonal, and no edges between distinct vertices.
func NewDenseGraph(size int) *DenseGraph {
	scores := make([][]float64, size)
	for i := range scores {
		scores[i] = make([]float64, size)
		scores[i][i] = 1
	}
	return &DenseGraph{scores: scores}
}

func (g *DenseGraph) Len() int { return len(g.scores) }

func (g *DenseGraph) Score(a, b int) float64 { return g.scores[a][b] }

// SetScore stores an undirected score in both directions.
func (g *DenseGraph) SetScore(a, b int, score float64) {
	if a == b {
		return
	}
	g.scores[a][b] = score
	g.scores[b][a] = score
}

// ForEachNeighbor visits positive, non-self scores meeting minScore in
// ascending vertex order. Zero represents an absent edge, including when
// minScore is zero.
func (g *DenseGraph) ForEachNeighbor(index int, minScore float64, visit func(index int, score float64)) {
	for other, score := range g.scores[index] {
		if other != index && score > 0 && score >= minScore {
			visit(other, score)
		}
	}
}

// ForEachEdge visits each positive undirected edge once, ordered first by its
// lower endpoint and then by its upper endpoint.
func (g *DenseGraph) ForEachEdge(minScore float64, visit func(a, b int, score float64)) {
	for a := 0; a < len(g.scores); a++ {
		for b := a + 1; b < len(g.scores[a]); b++ {
			score := g.scores[a][b]
			if score > 0 && score >= minScore {
				visit(a, b, score)
			}
		}
	}
}

// Matrix exposes the dense backing storage only for the legacy BuildMatrix
// compatibility API. New consumers should depend on Graph instead.
func (g *DenseGraph) Matrix() [][]float64 { return g.scores }

type sparseRow struct {
	mu     sync.RWMutex
	scores map[int]float64
}

// SparseGraph stores only nonzero edges. Each undirected edge is indexed in
// both endpoint rows so score lookup and neighbor traversal remain efficient;
// absent edges read as score zero. Per-row locks allow BuildGraph workers to
// populate disjoint and shared endpoint rows safely.
type SparseGraph struct {
	rows []sparseRow
}

// NewSparseGraph returns a graph with size vertices, implicit identity scores
// on the diagonal, and no allocated edge maps.
func NewSparseGraph(size int) *SparseGraph {
	return &SparseGraph{rows: make([]sparseRow, size)}
}

func (g *SparseGraph) Len() int { return len(g.rows) }

func (g *SparseGraph) Score(a, b int) float64 {
	if a == b {
		return 1
	}
	return g.rows[a].scores[b]
}

// SetScore stores an undirected score. Setting zero removes the edge, keeping
// absent edges allocation-free after configured pair ignores are applied.
func (g *SparseGraph) SetScore(a, b int, score float64) {
	if a == b {
		return
	}
	if a > b {
		a, b = b, a
	}
	first, second := &g.rows[a], &g.rows[b]
	first.mu.Lock()
	second.mu.Lock()
	if score == 0 {
		delete(first.scores, b)
		delete(second.scores, a)
	} else {
		if first.scores == nil {
			first.scores = make(map[int]float64)
		}
		if second.scores == nil {
			second.scores = make(map[int]float64)
		}
		first.scores[b] = score
		second.scores[a] = score
	}
	second.mu.Unlock()
	first.mu.Unlock()
}

type indexedScore struct {
	index int
	score float64
}

// ForEachNeighbor visits positive edges meeting minScore in ascending vertex
// order. The row is snapshotted so callbacks may safely update the graph.
func (g *SparseGraph) ForEachNeighbor(index int, minScore float64, visit func(index int, score float64)) {
	row := &g.rows[index]
	row.mu.RLock()
	neighbors := make([]indexedScore, 0, len(row.scores))
	for other, score := range row.scores {
		if score > 0 && score >= minScore {
			neighbors = append(neighbors, indexedScore{index: other, score: score})
		}
	}
	row.mu.RUnlock()
	sort.Slice(neighbors, func(i, j int) bool { return neighbors[i].index < neighbors[j].index })
	for _, neighbor := range neighbors {
		visit(neighbor.index, neighbor.score)
	}
}

// ForEachEdge visits each positive undirected edge once in ascending endpoint
// order. Only the upper-triangle copy is emitted.
func (g *SparseGraph) ForEachEdge(minScore float64, visit func(a, b int, score float64)) {
	for a := range g.rows {
		row := &g.rows[a]
		row.mu.RLock()
		edges := make([]indexedScore, 0, len(row.scores))
		for b, score := range row.scores {
			if b > a && score > 0 && score >= minScore {
				edges = append(edges, indexedScore{index: b, score: score})
			}
		}
		row.mu.RUnlock()
		sort.Slice(edges, func(i, j int) bool { return edges[i].index < edges[j].index })
		for _, edge := range edges {
			visit(a, edge.index, edge.score)
		}
	}
}

func (g *SparseGraph) storedEntries() int {
	total := 0
	for i := range g.rows {
		row := &g.rows[i]
		row.mu.RLock()
		total += len(row.scores)
		row.mu.RUnlock()
	}
	return total
}
