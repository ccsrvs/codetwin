package report

import (
	"strings"
	"testing"
)

func TestRender_NoPairs(t *testing.T) {
	var buf strings.Builder
	Render(&buf, nil, nil, Options{Plain: true, Threshold: 0.30})
	if !strings.Contains(buf.String(), "No similarities found") {
		t.Errorf("expected 'No similarities found' message, got: %s", buf.String())
	}
}

func TestRender_PlainStripsANSI(t *testing.T) {
	var buf strings.Builder
	pairs := []Pair{{NameA: "a.go", NameB: "b.go", Score: 0.9, Structural: 0.8, Semantic: 1.0}}
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.30})
	if strings.Contains(buf.String(), "\033[") {
		t.Errorf("plain mode output contained ANSI escape: %q", buf.String())
	}
}

func TestRender_ColorIncludesANSI(t *testing.T) {
	var buf strings.Builder
	pairs := []Pair{{NameA: "a.go", NameB: "b.go", Score: 0.9, Structural: 0.8, Semantic: 1.0}}
	Render(&buf, pairs, nil, Options{Plain: false, Threshold: 0.30})
	if !strings.Contains(buf.String(), "\033[") {
		t.Error("non-plain mode should include ANSI escape codes")
	}
}

func TestRender_FiltersBelowThreshold(t *testing.T) {
	var buf strings.Builder
	pairs := []Pair{
		{NameA: "high_a.go", NameB: "high_b.go", Score: 0.9, Structural: 0.9, Semantic: 0.9},
		{NameA: "low_a.go", NameB: "low_b.go", Score: 0.2, Structural: 0.2, Semantic: 0.2},
	}
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.50})
	out := buf.String()
	if !strings.Contains(out, "high_a.go") {
		t.Error("high-score pair should appear in output")
	}
	if strings.Contains(out, "low_a.go") {
		t.Error("low-score pair should be filtered out below threshold")
	}
}

func TestRender_VerboseShowsBelowThreshold(t *testing.T) {
	var buf strings.Builder
	pairs := []Pair{
		{NameA: "low_a.go", NameB: "low_b.go", Score: 0.2, Structural: 0.2, Semantic: 0.2},
	}
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.50, Verbose: true})
	if !strings.Contains(buf.String(), "low_a.go") {
		t.Error("verbose mode should show pairs below threshold")
	}
}

func TestRender_IncludesClusters(t *testing.T) {
	var buf strings.Builder
	pairs := []Pair{{NameA: "a.go", NameB: "b.go", Score: 0.9, Structural: 0.9, Semantic: 0.9}}
	clusters := []Cluster{{ID: 0, Members: []string{"a.go", "b.go"}}}
	Render(&buf, pairs, clusters, Options{Plain: true, Threshold: 0.30})
	out := buf.String()
	if !strings.Contains(out, "REFACTORING CLUSTERS") {
		t.Error("output should include the cluster section header")
	}
	if !strings.Contains(out, "Cluster 1") {
		t.Error("output should display cluster as 1-indexed (Cluster 1)")
	}
}

func TestRender_PreviewMode(t *testing.T) {
	var buf strings.Builder
	pairs := []Pair{{NameA: "a.go", NameB: "b.go", Score: 0.9, Structural: 0.9, Semantic: 0.9}}
	previews := map[string]Preview{
		"a.go": {StartLine: 1, Text: "func A() {\n\treturn 1\n}"},
		"b.go": {StartLine: 1, Text: "func B() {\n\treturn 2\n}"},
	}
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.30, Previews: previews})
	out := buf.String()
	if !strings.Contains(out, "func A()") || !strings.Contains(out, "func B()") {
		t.Errorf("preview lines missing from output:\n%s", out)
	}
	if !strings.Contains(out, "1 │") || !strings.Contains(out, "2 │") {
		t.Errorf("expected line-numbered preview prefix in output:\n%s", out)
	}
}

func TestRender_PreviewUsesStartLine(t *testing.T) {
	// A preview that starts at line 42 should render line numbers 42, 43, 44…
	var buf strings.Builder
	pairs := []Pair{{NameA: "a.go", NameB: "b.go", Score: 0.9, Structural: 0.9, Semantic: 0.9}}
	previews := map[string]Preview{
		"a.go": {StartLine: 42, Text: "line one\nline two"},
		"b.go": {StartLine: 1, Text: "x"},
	}
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.30, Previews: previews})
	out := buf.String()
	if !strings.Contains(out, "42 │") || !strings.Contains(out, "43 │") {
		t.Errorf("expected line numbers 42 and 43 in output, got:\n%s", out)
	}
}

func TestRender_PreviewMissingNameIsSilent(t *testing.T) {
	var buf strings.Builder
	pairs := []Pair{{NameA: "a.go", NameB: "b.go", Score: 0.9, Structural: 0.9, Semantic: 0.9}}
	previews := map[string]Preview{"a.go": {StartLine: 1, Text: "func A() {}"}}
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.30, Previews: previews})
	out := buf.String()
	if !strings.Contains(out, "func A()") {
		t.Error("expected preview for a.go in output")
	}
}

func TestRender_PreviewInClusters(t *testing.T) {
	var buf strings.Builder
	pairs := []Pair{{NameA: "a.go", NameB: "b.go", Score: 0.9, Structural: 0.9, Semantic: 0.9}}
	clusters := []Cluster{{ID: 0, Members: []string{"a.go", "b.go"}}}
	previews := map[string]Preview{
		"a.go": {StartLine: 1, Text: "func A() {}"},
		"b.go": {StartLine: 1, Text: "func B() {}"},
	}
	Render(&buf, pairs, clusters, Options{Plain: true, Threshold: 0.30, Previews: previews})
	out := buf.String()
	clusterIdx := strings.Index(out, "REFACTORING CLUSTERS")
	if clusterIdx < 0 {
		t.Fatal("cluster section missing")
	}
	clusterPart := out[clusterIdx:]
	if !strings.Contains(clusterPart, "func A()") || !strings.Contains(clusterPart, "func B()") {
		t.Errorf("expected previews in cluster section, got:\n%s", clusterPart)
	}
}

func TestRender_ClustersSortedByID(t *testing.T) {
	// Clusters arrive in random order from upstream map iteration; Render
	// must stabilize them so output is deterministic and "Cluster 1, 2, 3"
	// always appears in numerical order.
	var buf strings.Builder
	pairs := []Pair{{NameA: "a", NameB: "b", Score: 0.9, Structural: 0.9, Semantic: 0.9}}
	clusters := []Cluster{
		{ID: 2, Members: []string{"x", "y"}},
		{ID: 0, Members: []string{"a", "b"}},
		{ID: 1, Members: []string{"m", "n"}},
	}
	Render(&buf, pairs, clusters, Options{Plain: true, Threshold: 0.30})
	out := buf.String()
	idx1 := strings.Index(out, "Cluster 1")
	idx2 := strings.Index(out, "Cluster 2")
	idx3 := strings.Index(out, "Cluster 3")
	if idx1 < 0 || idx2 < 0 || idx3 < 0 {
		t.Fatalf("expected Cluster 1, 2, 3 in output:\n%s", out)
	}
	if !(idx1 < idx2 && idx2 < idx3) {
		t.Errorf("clusters not in numerical order: positions 1=%d 2=%d 3=%d", idx1, idx2, idx3)
	}
}

func TestRender_PairOrderStableForTiedScores(t *testing.T) {
	// Equal scores must not reorder between renders. Tested by rendering
	// the same input twice and comparing the output — sort.SliceStable
	// guarantees this; sort.Slice does not.
	pairs := []Pair{
		{NameA: "a", NameB: "b", Score: 0.5, Structural: 0.5, Semantic: 0.5},
		{NameA: "c", NameB: "d", Score: 0.5, Structural: 0.5, Semantic: 0.5},
		{NameA: "e", NameB: "f", Score: 0.5, Structural: 0.5, Semantic: 0.5},
	}
	var a, b strings.Builder
	Render(&a, append([]Pair(nil), pairs...), nil, Options{Plain: true, Threshold: 0.30})
	Render(&b, append([]Pair(nil), pairs...), nil, Options{Plain: true, Threshold: 0.30})
	if a.String() != b.String() {
		t.Errorf("Render output not deterministic for tied scores")
	}
}

func TestRender_SummaryReflectsThreshold(t *testing.T) {
	// A pair scoring 0.50 must NOT appear in any summary bucket when the
	// threshold filters it out — otherwise "Refactor targets" claims
	// findings the user can't see.
	pairs := []Pair{
		{NameA: "high_a", NameB: "high_b", Score: 0.90, Structural: 0.9, Semantic: 0.9},
		{NameA: "mid_a", NameB: "mid_b", Score: 0.50, Structural: 0.5, Semantic: 0.5},
	}
	var buf strings.Builder
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.60})
	out := buf.String()

	if !strings.Contains(out, "Pairs shown       1") {
		t.Errorf("summary should report 1 pair shown, got:\n%s", out)
	}
	if !strings.Contains(out, "Exact clones      1") {
		t.Errorf("summary should count the high-score pair, got:\n%s", out)
	}
	if !strings.Contains(out, "Refactor targets  0") {
		t.Errorf("summary must NOT count pairs filtered by threshold; got:\n%s", out)
	}
}

func TestRender_SummaryShowsWeakBucketWhenVerbose(t *testing.T) {
	// In verbose mode all pairs render, including weak similarities (≤ 0.45).
	// The summary should expose a "Weak similarities" line so totals add up.
	pairs := []Pair{
		{NameA: "weak_a", NameB: "weak_b", Score: 0.30, Structural: 0.3, Semantic: 0.3},
	}
	var buf strings.Builder
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.30, Verbose: true})
	out := buf.String()
	if !strings.Contains(out, "Weak similarities 1") {
		t.Errorf("summary should include 'Weak similarities 1' in verbose mode, got:\n%s", out)
	}
}

func TestPrepare_SortBySize(t *testing.T) {
	pairs := []Pair{
		{NameA: "a", NameB: "b", Score: 0.9, LinesA: 5, LinesB: 5},
		{NameA: "c", NameB: "d", Score: 0.5, LinesA: 50, LinesB: 30},
		{NameA: "e", NameB: "f", Score: 0.7, LinesA: 20, LinesB: 20},
	}
	out, _ := Prepare(pairs, nil, Options{Sort: SortSize, Threshold: 0})
	// Expect order by max(LinesA, LinesB) desc: 50, 20, 5
	wantOrder := []string{"c", "e", "a"}
	for i, w := range wantOrder {
		if out[i].NameA != w {
			t.Errorf("position %d: got NameA=%q, want %q", i, out[i].NameA, w)
		}
	}
}

func TestPrepare_SortBySizeAsc(t *testing.T) {
	pairs := []Pair{
		{NameA: "big", NameB: "x", Score: 0.5, LinesA: 50, LinesB: 50},
		{NameA: "tiny", NameB: "y", Score: 0.5, LinesA: 3, LinesB: 3},
		{NameA: "mid", NameB: "z", Score: 0.5, LinesA: 20, LinesB: 20},
	}
	out, _ := Prepare(pairs, nil, Options{Sort: SortSizeAsc, Threshold: 0})
	wantOrder := []string{"tiny", "mid", "big"}
	for i, w := range wantOrder {
		if out[i].NameA != w {
			t.Errorf("position %d: got NameA=%q, want %q", i, out[i].NameA, w)
		}
	}
}

func TestPrepare_SortByName(t *testing.T) {
	pairs := []Pair{
		{NameA: "zeta", NameB: "x", Score: 0.5},
		{NameA: "alpha", NameB: "y", Score: 0.5},
		{NameA: "mu", NameB: "z", Score: 0.5},
	}
	out, _ := Prepare(pairs, nil, Options{Sort: SortName, Threshold: 0})
	wantOrder := []string{"alpha", "mu", "zeta"}
	for i, w := range wantOrder {
		if out[i].NameA != w {
			t.Errorf("position %d: got NameA=%q, want %q", i, out[i].NameA, w)
		}
	}
}

func TestPrepare_SortByScoreAsc(t *testing.T) {
	pairs := []Pair{
		{NameA: "high", NameB: "x", Score: 0.9},
		{NameA: "low", NameB: "y", Score: 0.3},
		{NameA: "mid", NameB: "z", Score: 0.6},
	}
	out, _ := Prepare(pairs, nil, Options{Sort: SortScoreAsc, Threshold: 0})
	wantOrder := []string{"low", "mid", "high"}
	for i, w := range wantOrder {
		if out[i].NameA != w {
			t.Errorf("position %d: got NameA=%q, want %q", i, out[i].NameA, w)
		}
	}
}

func TestPrepare_ClustersSortBySize(t *testing.T) {
	clusters := []Cluster{
		{ID: 0, Members: []string{"a", "b"}, Score: 0.9},
		{ID: 1, Members: []string{"c", "d", "e", "f"}, Score: 0.6},
		{ID: 2, Members: []string{"g", "h", "i"}, Score: 0.7},
	}
	_, out := Prepare(nil, clusters, Options{Sort: SortSize})
	if len(out) != 3 {
		t.Fatalf("expected 3 clusters, got %d", len(out))
	}
	// Expect by member count desc: 4, 3, 2 → IDs 1, 2, 0
	wantIDs := []int{1, 2, 0}
	for i, id := range wantIDs {
		if out[i].ID != id {
			t.Errorf("position %d: got ID=%d, want %d", i, out[i].ID, id)
		}
	}
}

func TestPrepare_ClustersSortByScore(t *testing.T) {
	clusters := []Cluster{
		{ID: 0, Members: []string{"a", "b"}, Score: 0.5},
		{ID: 1, Members: []string{"c", "d"}, Score: 0.9},
		{ID: 2, Members: []string{"e", "f"}, Score: 0.7},
	}
	_, out := Prepare(nil, clusters, Options{Sort: SortScore})
	wantIDs := []int{1, 2, 0}
	for i, id := range wantIDs {
		if out[i].ID != id {
			t.Errorf("position %d: got ID=%d, want %d", i, out[i].ID, id)
		}
	}
}

func TestPrepare_LimitClampsBothSections(t *testing.T) {
	pairs := []Pair{
		{NameA: "p1", Score: 0.9},
		{NameA: "p2", Score: 0.8},
		{NameA: "p3", Score: 0.7},
		{NameA: "p4", Score: 0.6},
	}
	clusters := []Cluster{
		{ID: 0, Members: []string{"a"}, Score: 0.9},
		{ID: 1, Members: []string{"b"}, Score: 0.8},
		{ID: 2, Members: []string{"c"}, Score: 0.7},
	}
	visP, visC := Prepare(pairs, clusters, Options{Sort: SortScore, Limit: 2, Threshold: 0})
	if len(visP) != 2 {
		t.Errorf("pairs: expected 2 after limit, got %d", len(visP))
	}
	if len(visC) != 2 {
		t.Errorf("clusters: expected 2 after limit, got %d", len(visC))
	}
}

func TestPrepare_LimitDoesNotPadShortSection(t *testing.T) {
	clusters := []Cluster{{ID: 0, Members: []string{"a"}, Score: 0.9}}
	_, visC := Prepare(nil, clusters, Options{Limit: 5})
	if len(visC) != 1 {
		t.Errorf("expected 1 cluster (no padding), got %d", len(visC))
	}
}

func TestPrepare_LimitAppliesAfterThresholdFilter(t *testing.T) {
	// 4 pairs, threshold=0.50 keeps 2, limit=3 should still yield 2 (not 3).
	pairs := []Pair{
		{NameA: "high1", Score: 0.9},
		{NameA: "high2", Score: 0.8},
		{NameA: "low1", Score: 0.3},
		{NameA: "low2", Score: 0.2},
	}
	visP, _ := Prepare(pairs, nil, Options{Sort: SortScore, Threshold: 0.5, Limit: 3})
	if len(visP) != 2 {
		t.Errorf("expected 2 pairs after threshold+limit, got %d", len(visP))
	}
}

func TestPrepare_MinConfidenceLinesDampensShortMatches(t *testing.T) {
	// A 5-line match with threshold=20 gets multiplier 0.5 + 0.5*(5/20) = 0.625;
	// a 25-line match (>= threshold) is unchanged.
	pairs := []Pair{
		{NameA: "short", Score: 1.00, Structural: 1, Semantic: 1, LinesA: 5, LinesB: 5},
		{NameA: "long", Score: 1.00, Structural: 1, Semantic: 1, LinesA: 25, LinesB: 30},
	}
	visP, _ := Prepare(pairs, nil, Options{
		Sort:               SortName,
		MinConfidenceLines: 20,
	})
	if len(visP) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(visP))
	}
	var short, long Pair
	for _, p := range visP {
		switch p.NameA {
		case "short":
			short = p
		case "long":
			long = p
		}
	}
	if got, want := short.Score, 0.625; got != want {
		t.Errorf("short.Score = %.4f; want %.4f", got, want)
	}
	if got, want := long.Score, 1.0; got != want {
		t.Errorf("long.Score = %.4f; want %.4f (unchanged)", got, want)
	}
}

func TestPrepare_MinConfidenceLinesOffByDefault(t *testing.T) {
	pairs := []Pair{
		{NameA: "x", Score: 1.0, LinesA: 3, LinesB: 3},
	}
	visP, _ := Prepare(pairs, nil, Options{Sort: SortName})
	if got, want := visP[0].Score, 1.0; got != want {
		t.Errorf("default damping should be off; Score = %.4f, want %.4f", got, want)
	}
}

func TestPrepare_MinConfidenceLinesDoesNotMutateInput(t *testing.T) {
	pairs := []Pair{
		{NameA: "x", Score: 1.0, LinesA: 4, LinesB: 4},
	}
	_, _ = Prepare(pairs, nil, Options{Sort: SortName, MinConfidenceLines: 20})
	if got := pairs[0].Score; got != 1.0 {
		t.Errorf("Prepare mutated caller's slice: Score = %.4f, want 1.0", got)
	}
}

func TestRender_LabelsByScore(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{0.95, "EXACT CLONE"},
		{0.75, "STRONG CLONE"},
		{0.55, "REFACTOR TARGET"},
	}
	for _, c := range cases {
		var buf strings.Builder
		pairs := []Pair{{NameA: "a", NameB: "b", Score: c.score, Structural: c.score, Semantic: c.score}}
		Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.30})
		if !strings.Contains(buf.String(), c.want) {
			t.Errorf("score %.2f: expected label containing %q, got: %s", c.score, c.want, buf.String())
		}
	}
}
