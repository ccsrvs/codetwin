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

func TestGroups_EmptyResult(t *testing.T) {
	res := Result{Labels: []int{-1, -1, -1}, NumClusters: 0}
	g := Groups(res)
	if len(g) != 0 {
		t.Errorf("expected empty groups for all-noise result, got %v", g)
	}
}