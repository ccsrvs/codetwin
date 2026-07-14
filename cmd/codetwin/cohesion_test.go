package main

// Unit tests for cluster cohesion (min internal pair score) and the
// low-cohesion split: DBSCAN chains pairs transitively, so a cluster
// whose weakest internal pair falls below the report threshold is
// re-linked single-linkage at pair score >= threshold and each
// connected component becomes its own cluster (R4 in
// docs/comparative-algorithms-review.md).

import (
	"math"
	"testing"
)

// symMatrix builds an n×n symmetric similarity matrix from upper-triangle
// entries; the diagonal is 1.0 and unspecified entries are 0.
func symMatrix(n int, entries map[[2]int]float64) [][]float64 {
	m := make([][]float64, n)
	for i := range m {
		m[i] = make([]float64, n)
		m[i][i] = 1.0
	}
	for k, v := range entries {
		m[k[0]][k[1]] = v
		m[k[1]][k[0]] = v
	}
	return m
}

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestClusterStats_AvgAndMin(t *testing.T) {
	m := symMatrix(3, map[[2]int]float64{
		{0, 1}: 0.90,
		{0, 2}: 0.70,
		{1, 2}: 0.50,
	})
	avg, min := clusterStats([]int{0, 1, 2}, m)
	if !almostEqual(avg, 0.70) {
		t.Errorf("avg = %.4f; want 0.70", avg)
	}
	if !almostEqual(min, 0.50) {
		t.Errorf("min = %.4f; want 0.50", min)
	}
}

func TestClusterStats_SingleMemberGuard(t *testing.T) {
	m := symMatrix(2, nil)
	avg, min := clusterStats([]int{0}, m)
	if avg != 0 || min != 0 {
		t.Errorf("single-member stats = (%.2f, %.2f); want (0, 0)", avg, min)
	}
}

func TestBuildReportClusters_CohesiveClusterPassesThrough(t *testing.T) {
	// Every internal pair clears the threshold → no split, one cluster
	// with correct avg and min.
	m := symMatrix(3, map[[2]int]float64{
		{0, 1}: 0.90,
		{0, 2}: 0.80,
		{1, 2}: 0.70,
	})
	groups := map[int][]int{0: {0, 1, 2}}
	names := []string{"a.go", "b.go", "c.go"}

	clusters := buildReportClusters(groups, m, names, nil, 0.50)
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d: %+v", len(clusters), clusters)
	}
	c := clusters[0]
	if len(c.Members) != 3 {
		t.Errorf("members = %v; want all 3", c.Members)
	}
	if !almostEqual(c.Score, 0.80) {
		t.Errorf("Score = %.4f; want 0.80", c.Score)
	}
	if !almostEqual(c.MinScore, 0.70) {
		t.Errorf("MinScore = %.4f; want 0.70", c.MinScore)
	}
}

func TestBuildReportClusters_ChainedClusterSplitsAtThreshold(t *testing.T) {
	// A 4-member DBSCAN chain: {0,1} and {2,3} are strong families,
	// bridged only by the 1-2 edge at 0.66 (enough for eps-linking but
	// below the 0.70 threshold used here). min internal score is 0.10
	// (< threshold), so the cluster re-links at >= 0.70 and splits into
	// two 2-member clusters.
	m := symMatrix(4, map[[2]int]float64{
		{0, 1}: 0.95,
		{2, 3}: 0.90,
		{1, 2}: 0.66, // bridge below threshold
		{0, 2}: 0.10,
		{0, 3}: 0.10,
		{1, 3}: 0.10,
	})
	groups := map[int][]int{0: {0, 1, 2, 3}}
	names := []string{"a.go", "b.go", "c.go", "d.go"}

	clusters := buildReportClusters(groups, m, names, nil, 0.70)
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters after split, got %d: %+v", len(clusters), clusters)
	}
	// Deterministic renumbering by first member name: {a,b} then {c,d}.
	if clusters[0].ID != 0 || clusters[0].Members[0] != "a.go" || clusters[0].Members[1] != "b.go" {
		t.Errorf("cluster 0 = %+v; want ID 0 with members [a.go b.go]", clusters[0])
	}
	if clusters[1].ID != 1 || clusters[1].Members[0] != "c.go" || clusters[1].Members[1] != "d.go" {
		t.Errorf("cluster 1 = %+v; want ID 1 with members [c.go d.go]", clusters[1])
	}
	// Avg and min are recomputed over the split members only.
	if !almostEqual(clusters[0].Score, 0.95) || !almostEqual(clusters[0].MinScore, 0.95) {
		t.Errorf("cluster 0 scores = (%.2f, %.2f); want (0.95, 0.95)", clusters[0].Score, clusters[0].MinScore)
	}
	if !almostEqual(clusters[1].Score, 0.90) || !almostEqual(clusters[1].MinScore, 0.90) {
		t.Errorf("cluster 1 scores = (%.2f, %.2f); want (0.90, 0.90)", clusters[1].Score, clusters[1].MinScore)
	}
}

func TestBuildReportClusters_SingletonComponentsDropAsNoise(t *testing.T) {
	// Chain a-b-c where only a-b survives the threshold: c has no
	// threshold-strength partner, so the split drops it entirely.
	m := symMatrix(3, map[[2]int]float64{
		{0, 1}: 0.90,
		{1, 2}: 0.60, // below threshold
		{0, 2}: 0.10,
	})
	groups := map[int][]int{0: {0, 1, 2}}
	names := []string{"a.go", "b.go", "c.go"}

	clusters := buildReportClusters(groups, m, names, nil, 0.70)
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster (singleton dropped), got %d: %+v", len(clusters), clusters)
	}
	if len(clusters[0].Members) != 2 || clusters[0].Members[0] != "a.go" || clusters[0].Members[1] != "b.go" {
		t.Errorf("members = %v; want [a.go b.go]", clusters[0].Members)
	}
}

func TestBuildReportClusters_WholeClusterCanDissolve(t *testing.T) {
	// A 2-member cluster whose only pair is below threshold splits into
	// two singletons → both drop → no clusters remain.
	m := symMatrix(2, map[[2]int]float64{{0, 1}: 0.60})
	groups := map[int][]int{0: {0, 1}}
	clusters := buildReportClusters(groups, m, []string{"a.go", "b.go"}, nil, 0.70)
	if len(clusters) != 0 {
		t.Fatalf("expected cluster to dissolve entirely, got %+v", clusters)
	}
}

func TestBuildReportClusters_DeterministicIDsAcrossMapOrder(t *testing.T) {
	// Two independent groups arriving via a map: IDs must come out
	// ordered by first member name on every run, regardless of map
	// iteration order.
	m := symMatrix(4, map[[2]int]float64{
		{0, 1}: 0.90,
		{2, 3}: 0.80,
	})
	names := []string{"zz.go", "zx.go", "aa.go", "ab.go"}
	for run := 0; run < 20; run++ {
		groups := map[int][]int{0: {0, 1}, 1: {2, 3}}
		clusters := buildReportClusters(groups, m, names, nil, 0.50)
		if len(clusters) != 2 {
			t.Fatalf("run %d: expected 2 clusters, got %+v", run, clusters)
		}
		// aa.go's family sorts first despite being DBSCAN group 1.
		if clusters[0].ID != 0 || clusters[0].Members[0] != "aa.go" {
			t.Fatalf("run %d: cluster 0 = %+v; want first-member aa.go with ID 0", run, clusters[0])
		}
		if clusters[1].ID != 1 || clusters[1].Members[0] != "zz.go" {
			t.Fatalf("run %d: cluster 1 = %+v; want first-member zz.go with ID 1", run, clusters[1])
		}
	}
}
