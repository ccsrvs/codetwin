package similarity

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
