package cluster

import (
	"testing"
	"time"
)

func TestDBSCAN_AllNoiseWhenSparse(t *testing.T) {
	// All point pairs far apart → no clusters form
	distFn := func(i, j int) float64 { return 0.9 }
	res := DBSCAN(4, 0.5, 2, distFn)
	if res.NumClusters != 0 {
		t.Errorf("expected 0 clusters for sparse points, got %d", res.NumClusters)
	}
	for i, l := range res.Labels {
		if l != -1 {
			t.Errorf("point %d: label = %d; want -1 (noise)", i, l)
		}
	}
}

func TestDBSCAN_OneCluster(t *testing.T) {
	// All point pairs close → one cluster of everyone
	distFn := func(i, j int) float64 { return 0.1 }
	res := DBSCAN(4, 0.5, 2, distFn)
	if res.NumClusters != 1 {
		t.Errorf("expected 1 cluster for dense points, got %d", res.NumClusters)
	}
	for i, l := range res.Labels {
		if l != 0 {
			t.Errorf("point %d: label = %d; want 0", i, l)
		}
	}
}

func TestDBSCAN_TwoSeparateClusters(t *testing.T) {
	// Points {0,1} close, {2,3} close, two halves far from each other
	distFn := func(i, j int) float64 {
		sameHalf := (i < 2 && j < 2) || (i >= 2 && j >= 2)
		if sameHalf {
			return 0.1
		}
		return 0.9
	}
	res := DBSCAN(4, 0.5, 2, distFn)
	if res.NumClusters != 2 {
		t.Errorf("expected 2 clusters, got %d (labels=%v)", res.NumClusters, res.Labels)
	}
	if res.Labels[0] != res.Labels[1] {
		t.Errorf("points 0,1 should be in same cluster, got labels %d,%d", res.Labels[0], res.Labels[1])
	}
	if res.Labels[2] != res.Labels[3] {
		t.Errorf("points 2,3 should be in same cluster, got labels %d,%d", res.Labels[2], res.Labels[3])
	}
	if res.Labels[0] == res.Labels[2] {
		t.Errorf("points across halves should NOT share cluster label, got %d for both", res.Labels[0])
	}
}

func TestGroups_ExcludesNoise(t *testing.T) {
	res := Result{Labels: []int{0, 0, -1, 1, 1, -1}, NumClusters: 2}
	g := Groups(res)
	if len(g) != 2 {
		t.Errorf("expected 2 groups, got %d", len(g))
	}
	if len(g[0]) != 2 || len(g[1]) != 2 {
		t.Errorf("each cluster should have 2 members; got g[0]=%v g[1]=%v", g[0], g[1])
	}
}

func TestDBSCAN_FullyConnectedDoesNotExplode(t *testing.T) {
	// Regression: on a fully-connected graph, the previous implementation
	// enqueued every point O(n) times and recomputed neighbors O(n³)
	// total — practically a hang for n ≥ a few thousand. With the
	// inSeeds dedupe, total work is O(n²) and 2000 points run in well
	// under a second.
	const n = 2000
	distFn := func(i, j int) float64 { return 0.0 } // everyone neighbors everyone

	done := make(chan Result, 1)
	go func() {
		done <- DBSCAN(n, 0.5, 2, distFn)
	}()

	select {
	case res := <-done:
		if res.NumClusters != 1 {
			t.Errorf("fully-connected graph should form 1 cluster, got %d", res.NumClusters)
		}
		for i, label := range res.Labels {
			if label != 0 {
				t.Errorf("point %d: label = %d; want 0", i, label)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DBSCAN took longer than 5s on n=2000 fully-connected graph; the seed-dedupe regression may be back")
	}
}

// ── Components (single-linkage split helper) ─────────────────────────────────

// linkFromScores builds a link predicate over a symmetric score map:
// two points link when their pair score >= bound.
func linkFromScores(scores map[[2]int]float64, bound float64) func(a, b int) bool {
	return func(a, b int) bool {
		if a > b {
			a, b = b, a
		}
		return scores[[2]int{a, b}] >= bound
	}
}

func TestComponents_ChainSplitsAtStricterBound(t *testing.T) {
	// A-B and B-C are strong (0.70) but A-C is weak (0.30): DBSCAN-style
	// transitive chaining would keep {A,B,C} together at a 0.55 bound.
	// Re-linking at 0.65 must NOT split them (chain still connects via B)
	// but at a bound above both edges everything shatters. The interesting
	// case: drop the B-C edge below the bound and the chain breaks.
	scores := map[[2]int]float64{
		{0, 1}: 0.70, // A-B strong
		{1, 2}: 0.50, // B-C below the stricter bound
		{0, 2}: 0.30, // A-C weak
	}

	got := Components([]int{0, 1, 2}, linkFromScores(scores, 0.65))
	if len(got) != 2 {
		t.Fatalf("expected 2 components ({0,1} and {2}), got %v", got)
	}
	if len(got[0]) != 2 || got[0][0] != 0 || got[0][1] != 1 {
		t.Errorf("first component = %v; want [0 1]", got[0])
	}
	if len(got[1]) != 1 || got[1][0] != 2 {
		t.Errorf("second component = %v; want [2]", got[1])
	}
}

func TestComponents_ChainStaysConnectedThroughMiddle(t *testing.T) {
	// A-B and B-C both clear the bound; A-C does not. Single linkage
	// keeps all three connected through B — this is the definitional
	// difference from requiring a clique.
	scores := map[[2]int]float64{
		{0, 1}: 0.70,
		{1, 2}: 0.70,
		{0, 2}: 0.30,
	}
	got := Components([]int{0, 1, 2}, linkFromScores(scores, 0.65))
	if len(got) != 1 || len(got[0]) != 3 {
		t.Fatalf("expected one 3-member component, got %v", got)
	}
}

func TestComponents_CohesiveClusterPassesThroughUnchanged(t *testing.T) {
	// Every pair clears the bound → one component, members in input order.
	scores := map[[2]int]float64{
		{3, 7}: 0.90, {3, 9}: 0.85, {7, 9}: 0.95,
	}
	got := Components([]int{3, 7, 9}, linkFromScores(scores, 0.65))
	if len(got) != 1 {
		t.Fatalf("expected 1 component, got %v", got)
	}
	want := []int{3, 7, 9}
	for i, v := range want {
		if got[0][i] != v {
			t.Fatalf("component = %v; want %v (input order preserved)", got[0], want)
		}
	}
}

func TestComponents_AllSingletonsWhenNothingLinks(t *testing.T) {
	got := Components([]int{4, 5, 6}, func(a, b int) bool { return false })
	if len(got) != 3 {
		t.Fatalf("expected 3 singleton components, got %v", got)
	}
	for i, want := range []int{4, 5, 6} {
		if len(got[i]) != 1 || got[i][0] != want {
			t.Errorf("component %d = %v; want [%d]", i, got[i], want)
		}
	}
}

func TestComponents_Deterministic(t *testing.T) {
	// Two disjoint families; result ordering must be stable across calls
	// (components by first member's input position, members in input order).
	scores := map[[2]int]float64{
		{0, 2}: 0.9,
		{1, 3}: 0.9,
	}
	first := Components([]int{0, 1, 2, 3}, linkFromScores(scores, 0.5))
	for run := 0; run < 10; run++ {
		got := Components([]int{0, 1, 2, 3}, linkFromScores(scores, 0.5))
		if len(got) != 2 {
			t.Fatalf("run %d: expected 2 components, got %v", run, got)
		}
		for ci := range got {
			if len(got[ci]) != len(first[ci]) {
				t.Fatalf("run %d: component %d = %v; want %v", run, ci, got[ci], first[ci])
			}
			for mi := range got[ci] {
				if got[ci][mi] != first[ci][mi] {
					t.Fatalf("run %d: component %d = %v; want %v", run, ci, got[ci], first[ci])
				}
			}
		}
	}
	// And the expected shape: {0,2} then {1,3}.
	if first[0][0] != 0 || first[0][1] != 2 || first[1][0] != 1 || first[1][1] != 3 {
		t.Errorf("components = %v; want [[0 2] [1 3]]", first)
	}
}

func TestComponents_Empty(t *testing.T) {
	if got := Components(nil, func(a, b int) bool { return true }); len(got) != 0 {
		t.Errorf("expected no components for empty input, got %v", got)
	}
}

func TestGroups_EmptyResult(t *testing.T) {
	res := Result{Labels: []int{-1, -1, -1}, NumClusters: 0}
	g := Groups(res)
	if len(g) != 0 {
		t.Errorf("expected empty groups for all-noise result, got %v", g)
	}
}
