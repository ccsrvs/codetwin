package similarity

import (
	"reflect"
	"testing"
)

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
