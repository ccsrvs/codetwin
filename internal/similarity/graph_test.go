package similarity

import (
	"reflect"
	"testing"
)

type graphFactory struct {
	name string
	new  func(int) MutableGraph
}

var graphFactories = []graphFactory{
	{name: "dense", new: func(size int) MutableGraph { return NewDenseGraph(size) }},
	{name: "sparse", new: func(size int) MutableGraph { return NewSparseGraph(size) }},
}

func TestDenseGraphScoreContract(t *testing.T) {
	graph := NewDenseGraph(3)

	if got := graph.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}
	for i := 0; i < graph.Len(); i++ {
		if got := graph.Score(i, i); got != 1 {
			t.Errorf("Score(%d, %d) = %v, want 1", i, i, got)
		}
	}

	graph.SetScore(0, 2, 0.75)
	if got := graph.Score(0, 2); got != 0.75 {
		t.Errorf("Score(0, 2) = %v, want 0.75", got)
	}
	if got := graph.Score(2, 0); got != 0.75 {
		t.Errorf("Score(2, 0) = %v, want symmetric score 0.75", got)
	}
}

func TestDenseGraphTraversalIsDeterministicAndSkipsAbsentEdges(t *testing.T) {
	graph := NewDenseGraph(4)
	graph.SetScore(0, 3, 0.80)
	graph.SetScore(0, 1, 0.90)
	graph.SetScore(1, 2, 0.40)

	type neighbor struct {
		index int
		score float64
	}
	var neighbors []neighbor
	graph.ForEachNeighbor(0, 0.50, func(index int, score float64) {
		neighbors = append(neighbors, neighbor{index: index, score: score})
	})
	wantNeighbors := []neighbor{{index: 1, score: 0.90}, {index: 3, score: 0.80}}
	if !reflect.DeepEqual(neighbors, wantNeighbors) {
		t.Errorf("neighbors = %#v, want %#v", neighbors, wantNeighbors)
	}

	type edge struct {
		a, b  int
		score float64
	}
	var edges []edge
	graph.ForEachEdge(0.40, func(a, b int, score float64) {
		edges = append(edges, edge{a: a, b: b, score: score})
	})
	wantEdges := []edge{
		{a: 0, b: 1, score: 0.90},
		{a: 0, b: 3, score: 0.80},
		{a: 1, b: 2, score: 0.40},
	}
	if !reflect.DeepEqual(edges, wantEdges) {
		t.Errorf("edges = %#v, want %#v", edges, wantEdges)
	}

	var absent []edge
	graph.ForEachEdge(0, func(a, b int, score float64) {
		absent = append(absent, edge{a: a, b: b, score: score})
	})
	if !reflect.DeepEqual(absent, wantEdges) {
		t.Errorf("zero-threshold edges = %#v, want only materialized edges %#v", absent, wantEdges)
	}
}

func TestGraphImplementationsHaveEquivalentScoreAndTraversalSemantics(t *testing.T) {
	type neighbor struct {
		index int
		score float64
	}
	type edge struct {
		a, b  int
		score float64
	}

	for _, factory := range graphFactories {
		t.Run(factory.name, func(t *testing.T) {
			graph := factory.new(5)
			graph.SetScore(3, 0, 0.80) // reversed endpoints must remain symmetric
			graph.SetScore(0, 1, 0.90)
			graph.SetScore(1, 2, 0.40)
			graph.SetScore(2, 4, 0) // zero is an absent edge
			graph.SetScore(2, 2, 0) // diagonal identity is immutable

			if graph.Len() != 5 || graph.Score(0, 0) != 1 || graph.Score(2, 2) != 1 {
				t.Fatalf("invalid graph identity: len=%d diagonal=%v/%v", graph.Len(), graph.Score(0, 0), graph.Score(2, 2))
			}
			if graph.Score(0, 3) != 0.80 || graph.Score(3, 0) != 0.80 {
				t.Fatalf("asymmetric score: %v / %v", graph.Score(0, 3), graph.Score(3, 0))
			}

			var neighbors []neighbor
			graph.ForEachNeighbor(0, 0.50, func(index int, score float64) {
				neighbors = append(neighbors, neighbor{index: index, score: score})
			})
			wantNeighbors := []neighbor{{index: 1, score: 0.90}, {index: 3, score: 0.80}}
			if !reflect.DeepEqual(neighbors, wantNeighbors) {
				t.Errorf("neighbors = %#v, want %#v", neighbors, wantNeighbors)
			}

			var edges []edge
			graph.ForEachEdge(0, func(a, b int, score float64) {
				edges = append(edges, edge{a: a, b: b, score: score})
			})
			wantEdges := []edge{
				{a: 0, b: 1, score: 0.90},
				{a: 0, b: 3, score: 0.80},
				{a: 1, b: 2, score: 0.40},
			}
			if !reflect.DeepEqual(edges, wantEdges) {
				t.Errorf("edges = %#v, want %#v", edges, wantEdges)
			}

			graph.SetScore(0, 3, 0)
			if graph.Score(0, 3) != 0 || graph.Score(3, 0) != 0 {
				t.Error("zero score must remove an edge in both directions")
			}
		})
	}
}

func TestSparseGraphStoresOnlyEdges(t *testing.T) {
	const size = 10_000
	graph := NewSparseGraph(size)
	graph.SetScore(1, 2, 0.75)
	graph.SetScore(50, 9_999, 0.60)

	if got := graph.storedEntries(); got != 4 {
		t.Fatalf("stored entries = %d, want 4 (two directions per edge), not O(n²)", got)
	}
}

func BenchmarkGraphStorage(b *testing.B) {
	const (
		size        = 2_000
		edgesPerRow = 4
	)
	for _, factory := range graphFactories {
		b.Run(factory.name, func(b *testing.B) {
			b.ReportAllocs()
			for iteration := 0; iteration < b.N; iteration++ {
				graph := factory.new(size)
				for i := 0; i < size; i++ {
					for offset := 1; offset <= edgesPerRow && i+offset < size; offset++ {
						graph.SetScore(i, i+offset, 0.75)
					}
				}
			}
		})
	}
}
