package main

// Fixture-driven tests for the clone watchlist (roadmap bet #5).
// testdata/baseline/{before,after} are two snapshots of the same code
// engineered so that, between them:
//
//   - the SumEven* cluster gains a member (SumEvenC pasted in),
//   - the MergeCounts* cluster loses a member (MergeCountsC deleted),
//   - ParseRecordB's body changes while it still clusters with
//     ParseRecordA (an empty-label guard added — the "bug fixed in one
//     copy" case), and
//   - after/gamma.go's header comment grows, shifting every line
//     number below it, so line-range-stripped member keys are load-
//     bearing: ParseRecordA must NOT read as drift.
//
// Each drift event must fire exactly once through the real pipeline
// (scan → similarity → DBSCAN → report clusters → baseline diff).

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/ccsrvs/codetwin/internal/baseline"
	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/cluster"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/similarity"
)

const (
	baselineBeforeDir = "../../testdata/baseline/before"
	baselineAfterDir  = "../../testdata/baseline/after"
)

// baselineFixtureSnapshot runs the real pipeline on a fixture tree with
// the default knobs (threshold 0.50, eps 0.35, min-pts 2, function
// granularity) and packages the resulting clusters as a baseline
// snapshot, exactly as --update-baseline would.
func baselineFixtureSnapshot(t *testing.T, dir string) baseline.Snapshot {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("fixture glob %s: %v (%d files)", dir, err, len(files))
	}
	sort.Strings(files)

	snips, warns := scan.ProcessFiles(files, 3, nil, cache.New(), "", scan.GranularityFunction, nil)
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	sort.Slice(snips, func(i, j int) bool { return snips[i].Name < snips[j].Name })

	streams := make([][]string, len(snips))
	for i, s := range snips {
		streams[i] = s.Tokens
	}
	corpus := similarity.NewCorpus(streams)
	vecs := make([]similarity.NormalizedVector, len(snips))
	for i, s := range snips {
		vecs[i] = similarity.Normalize(corpus.Vectorize(s.Tokens))
	}
	matrix, _, _ := similarity.BuildMatrix(snips, vecs, similarity.DefaultMinConfidenceLines, 0.50, nil)

	distFn := func(i, j int) float64 { return 1.0 - matrix[i][j] }
	groups := cluster.Groups(cluster.DBSCAN(len(snips), 0.35, 2, distFn))
	names := make([]string, len(snips))
	for i, s := range snips {
		names[i] = s.Name
	}
	clusters := buildReportClusters(groups, matrix, names, make([]string, len(snips)), 0.50)

	memberLists := make([][]string, len(clusters))
	for i, c := range clusters {
		memberLists[i] = c.Members
	}
	tokens := make(map[string][]string, len(snips))
	for _, s := range snips {
		tokens[s.Name] = s.Tokens
	}
	return baseline.Snapshot{
		SchemaVersion: baseline.SchemaVersion,
		ToolVersion:   "test",
		Params: baseline.Params{
			Threshold: 0.50, Eps: 0.35, MinPts: 2,
			Granularity: string(scan.GranularityFunction),
		},
		Clusters: baseline.BuildClusters(memberLists, tokens, baseline.NewKeyer([]string{dir})),
	}
}

// TestBaselineFixture_BeforeTree_HasThreeCleanClusters pins the fixture
// itself: three clone families, no cross-family merging, so the drift
// assertions below test the diff — not accidents of clustering.
func TestBaselineFixture_BeforeTree_HasThreeCleanClusters(t *testing.T) {
	snap := baselineFixtureSnapshot(t, baselineBeforeDir)
	if len(snap.Clusters) != 3 {
		t.Fatalf("before tree: want 3 clusters, got %d: %+v", len(snap.Clusters), snap.Clusters)
	}
	sizes := []int{len(snap.Clusters[0].Members), len(snap.Clusters[1].Members), len(snap.Clusters[2].Members)}
	if sizes[0] != 2 || sizes[1] != 3 || sizes[2] != 2 {
		t.Errorf("before cluster sizes = %v, want [2 3 2] (alpha, beta, gamma)", sizes)
	}
	if got := snap.Clusters[0].Members[0].Key; got != "alpha.go SumEvenA" {
		t.Errorf("member keys should be root-relative and line-range-stripped, got %q", got)
	}
}

// TestBaselineFixture_DriftEvents_FireExactlyOnce is the bet #5
// fixture contract: one member-added, one member-removed, one
// member-changed — each exactly once, and nothing else.
func TestBaselineFixture_DriftEvents_FireExactlyOnce(t *testing.T) {
	before := baselineFixtureSnapshot(t, baselineBeforeDir)
	after := baselineFixtureSnapshot(t, baselineAfterDir)

	events := baseline.Diff(before, after)
	if len(events) != 3 {
		t.Fatalf("want exactly 3 drift events, got %d: %v", len(events), events)
	}
	want := map[baseline.EventKind]string{
		baseline.MemberAdded:   "alpha.go SumEvenC",
		baseline.MemberRemoved: "beta.go MergeCountsC",
		baseline.MemberChanged: "gamma.go ParseRecordB",
	}
	seen := map[baseline.EventKind]int{}
	for _, e := range events {
		seen[e.Kind]++
		wantDetail, ok := want[e.Kind]
		if !ok {
			t.Errorf("unexpected event kind %s: %v", e.Kind, e)
			continue
		}
		if e.Detail != wantDetail {
			t.Errorf("%s detail = %q, want %q", e.Kind, e.Detail, wantDetail)
		}
	}
	for kind := range want {
		if seen[kind] != 1 {
			t.Errorf("%s fired %d times, want exactly once", kind, seen[kind])
		}
	}
}

// TestBaselineFixture_SameTree_NoDrift: before vs before is silent —
// and in particular ParseRecordA (identical in both trees, but shifted
// by after/gamma.go's longer header) never appears in any event.
func TestBaselineFixture_SameTree_NoDrift(t *testing.T) {
	a := baselineFixtureSnapshot(t, baselineBeforeDir)
	b := baselineFixtureSnapshot(t, baselineBeforeDir)
	if events := baseline.Diff(a, b); len(events) != 0 {
		t.Errorf("same tree should produce zero drift events, got %v", events)
	}
}
