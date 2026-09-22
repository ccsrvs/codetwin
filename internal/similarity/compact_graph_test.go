package similarity

import (
	"math/rand"
	"reflect"
	"testing"
)

type testEdge struct {
	a, b  int
	score float64
}

func randomEdges(size int, density float64, seed int64) []testEdge {
	rng := rand.New(rand.NewSource(seed))
	var edges []testEdge
	for a := 0; a < size; a++ {
		for b := a + 1; b < size; b++ {
			if rng.Float64() < density {
				edges = append(edges, testEdge{a, b, 0.01 + rng.Float64()*0.99})
			}
		}
	}
	return edges
}

type visited struct {
	a, b  int
	score float64
}

func traverse(g Graph, min float64) (neighbors [][]visited, edges []visited) {
	neighbors = make([][]visited, g.Len())
	for i := 0; i < g.Len(); i++ {
		g.ForEachNeighbor(i, min, func(j int, s float64) { neighbors[i] = append(neighbors[i], visited{i, j, s}) })
	}
	g.ForEachEdge(min, func(a, b int, s float64) { edges = append(edges, visited{a, b, s}) })
	return neighbors, edges
}

// The frozen graph must answer every query exactly like a dense matrix
// holding the same edges, whichever layout Freeze picks.
func TestCompactGraphMatchesDenseReference(t *testing.T) {
	for _, tc := range []struct {
		name    string
		density float64
		layout  compactLayout
	}{
		{"sparse edges, auto", 0.05, layoutAuto},
		{"dense edges, auto", 0.9, layoutAuto},
		{"forced rows", 0.5, layoutRows},
		{"forced matrix", 0.5, layoutMatrix},
		{"no edges", 0, layoutAuto},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const size = 60
			edges := randomEdges(size, tc.density, 7)
			reference := NewDenseGraph(size)
			builder := newCompactGraphBuilder(size)
			for _, e := range edges {
				reference.SetScore(e.a, e.b, e.score)
				builder.SetScore(e.a, e.b, e.score)
			}
			graph := builder.freeze(tc.layout)

			if graph.Len() != size {
				t.Fatalf("Len = %d", graph.Len())
			}
			for a := 0; a < size; a++ {
				for b := 0; b < size; b++ {
					if got, want := graph.Score(a, b), reference.Score(a, b); got != want {
						t.Fatalf("Score(%d,%d) = %v, want %v", a, b, got, want)
					}
				}
			}
			for _, min := range []float64{0, 0.5} {
				gotN, gotE := traverse(graph, min)
				wantN, wantE := traverse(reference, min)
				if !reflect.DeepEqual(gotN, wantN) || !reflect.DeepEqual(gotE, wantE) {
					t.Fatalf("traversal at min %.1f differs from dense reference", min)
				}
			}

			// Pair ignores zero existing edges after the build.
			if len(edges) > 0 {
				e := edges[len(edges)/2]
				graph.SetScore(e.b, e.a, 0)
				reference.SetScore(e.a, e.b, 0)
				if graph.Score(e.a, e.b) != 0 || graph.Score(e.b, e.a) != 0 {
					t.Fatal("zeroed edge still scores")
				}
				gotN, gotE := traverse(graph, 0)
				wantN, wantE := traverse(reference, 0)
				if !reflect.DeepEqual(gotN, wantN) || !reflect.DeepEqual(gotE, wantE) {
					t.Fatal("traversal after zeroing differs from dense reference")
				}
			}
		})
	}
}

func TestCompactGraphAutoLayoutPicksSmallerStorage(t *testing.T) {
	const size = 200
	for _, tc := range []struct {
		density float64
		want    string
	}{{0.02, "*similarity.rowGraph"}, {0.9, "*similarity.DenseGraph"}} {
		builder := newCompactGraphBuilder(size)
		for _, e := range randomEdges(size, tc.density, 3) {
			builder.SetScore(e.a, e.b, e.score)
		}
		if got := reflect.TypeOf(builder.freeze(layoutAuto)).String(); got != tc.want {
			t.Errorf("density %.2f: layout %s, want %s", tc.density, got, tc.want)
		}
	}
}
