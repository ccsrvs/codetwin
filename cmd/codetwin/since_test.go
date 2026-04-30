package main

import (
	"testing"

	"github.com/ccsrvs/codetwin/internal/git"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
)

func mkSnippetAt(name, path string, start, end int) scan.Snippet {
	return scan.Snippet{Name: name, Path: path, StartLine: start, EndLine: end}
}

func TestFilterPairsBySince_GivenNoOverlap_DropsPair(t *testing.T) {
	root := "/tmp/repo"
	snips := []scan.Snippet{
		mkSnippetAt("a.go SumA", "/tmp/repo/a.go", 10, 20),
		mkSnippetAt("b.go SumB", "/tmp/repo/b.go", 50, 60),
	}
	pairs := []report.Pair{{NameA: "a.go SumA", NameB: "b.go SumB", Score: 0.9}}
	// Diff touches only c.go, which neither snippet lives in.
	diff := git.DiffMap{"c.go": {{Start: 1, End: 100}}}

	out, dropped := filterPairsBySince(pairs, snips, root, diff)

	if len(out) != 0 {
		t.Errorf("expected 0 pairs after --since filter, got %d", len(out))
	}
	if dropped != 1 {
		t.Errorf("dropped count: got %d, want 1", dropped)
	}
}

func TestFilterPairsBySince_GivenOneEndpointTouches_KeepsPair(t *testing.T) {
	root := "/tmp/repo"
	snips := []scan.Snippet{
		mkSnippetAt("a.go SumA", "/tmp/repo/a.go", 10, 20),
		mkSnippetAt("b.go SumB", "/tmp/repo/b.go", 50, 60),
	}
	pairs := []report.Pair{{NameA: "a.go SumA", NameB: "b.go SumB", Score: 0.9}}
	// Diff touches a.go in the snippet's range — keep the pair.
	diff := git.DiffMap{"a.go": {{Start: 15, End: 18}}}

	out, dropped := filterPairsBySince(pairs, snips, root, diff)

	if len(out) != 1 {
		t.Errorf("expected 1 pair kept, got %d", len(out))
	}
	if dropped != 0 {
		t.Errorf("dropped count: got %d, want 0", dropped)
	}
}

func TestFilterPairsBySince_GivenBothEndpointsTouch_KeepsOnce(t *testing.T) {
	root := "/tmp/repo"
	snips := []scan.Snippet{
		mkSnippetAt("a.go SumA", "/tmp/repo/a.go", 10, 20),
		mkSnippetAt("b.go SumB", "/tmp/repo/b.go", 50, 60),
	}
	pairs := []report.Pair{{NameA: "a.go SumA", NameB: "b.go SumB", Score: 0.9}}
	diff := git.DiffMap{
		"a.go": {{Start: 15, End: 18}},
		"b.go": {{Start: 55, End: 58}},
	}

	out, dropped := filterPairsBySince(pairs, snips, root, diff)

	if len(out) != 1 {
		t.Errorf("expected exactly 1 pair, got %d", len(out))
	}
	if dropped != 0 {
		t.Errorf("dropped count: got %d, want 0", dropped)
	}
}

func TestFilterPairsBySince_GivenSnippetOutsideRepo_DropsPair(t *testing.T) {
	root := "/tmp/repo"
	snips := []scan.Snippet{
		// Path is outside the repo root — Touches returns false even
		// if the diff names the same basename, since the relative
		// path resolves to an upward traversal.
		mkSnippetAt("a.go SumA", "/elsewhere/a.go", 10, 20),
		mkSnippetAt("b.go SumB", "/tmp/repo/b.go", 50, 60),
	}
	pairs := []report.Pair{{NameA: "a.go SumA", NameB: "b.go SumB", Score: 0.9}}
	diff := git.DiffMap{"a.go": {{Start: 10, End: 20}}}

	out, _ := filterPairsBySince(pairs, snips, root, diff)
	if len(out) != 0 {
		t.Errorf("expected 0 pairs (out-of-repo snippet shouldn't match), got %d", len(out))
	}
}

func TestFilterClustersBySince_GivenNoMemberTouches_DropsCluster(t *testing.T) {
	root := "/tmp/repo"
	snips := []scan.Snippet{
		mkSnippetAt("a", "/tmp/repo/a.go", 10, 20),
		mkSnippetAt("b", "/tmp/repo/b.go", 50, 60),
	}
	clusters := []report.Cluster{
		{ID: 0, Members: []string{"a", "b"}, Score: 0.8},
	}
	diff := git.DiffMap{"unrelated.go": {{Start: 1, End: 5}}}

	out := filterClustersBySince(clusters, snips, root, diff)
	if len(out) != 0 {
		t.Errorf("expected 0 clusters, got %d", len(out))
	}
}

func TestFilterClustersBySince_GivenAnyMemberTouches_KeepsCluster(t *testing.T) {
	root := "/tmp/repo"
	snips := []scan.Snippet{
		mkSnippetAt("a", "/tmp/repo/a.go", 10, 20),
		mkSnippetAt("b", "/tmp/repo/b.go", 50, 60),
		mkSnippetAt("c", "/tmp/repo/c.go", 100, 120),
	}
	clusters := []report.Cluster{
		{ID: 0, Members: []string{"a", "b", "c"}, Score: 0.8},
	}
	diff := git.DiffMap{"c.go": {{Start: 105, End: 110}}}

	out := filterClustersBySince(clusters, snips, root, diff)
	if len(out) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(out))
	}
	if out[0].ID != 0 {
		t.Errorf("kept wrong cluster: %+v", out[0])
	}
}
