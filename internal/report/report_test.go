package report

import (
	"strings"
	"testing"
	"time"
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
		{NameA: "high_a", NameB: "high_b", Score: 0.97, Structural: 0.97, Semantic: 0.97},
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

func TestRender_PrintsProvenanceWhenSet(t *testing.T) {
	intro := time.Date(2024, 7, 15, 0, 0, 0, 0, time.UTC)
	pairs := []Pair{{
		NameA: "a.go SumA", NameB: "b.go SumB", Score: 0.9,
		ProvenanceA: &Provenance{
			FirstCommit: "deadbeefcafebabe1234",
			FirstAuthor: "Alice Author",
			FirstTime:   intro,
		},
		ProvenanceB: &Provenance{
			FirstCommit: "feedfacefeedfacefeed",
			FirstAuthor: "Bob Builder",
			FirstTime:   intro,
		},
	}}
	var buf strings.Builder
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0})

	out := buf.String()
	wants := []string{
		"introduced 2024-07-15 by Alice Author (deadbee)", // 7-char short SHA
		"introduced 2024-07-15 by Bob Builder (feedfac)",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("rendered output missing %q\n--- got ---\n%s", w, out)
		}
	}
}

func TestRender_PrintsLastTouchedWhenDistinct(t *testing.T) {
	first := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)
	pairs := []Pair{{
		NameA: "a", NameB: "b", Score: 0.9,
		ProvenanceA: &Provenance{
			FirstCommit: "1111111aaaaaaaa",
			FirstAuthor: "A",
			FirstTime:   first,
			LastCommit:  "2222222bbbbbbbb",
			LastAuthor:  "B",
			LastTime:    last,
		},
	}}
	var buf strings.Builder
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0})

	out := buf.String()
	want := "introduced 2023-01-01 by A (1111111); last touched 2025-06-30 by B (2222222)"
	if !strings.Contains(out, want) {
		t.Errorf("rendered output missing %q\n--- got ---\n%s", want, out)
	}
}

func TestRender_OmitsProvenanceWhenNil(t *testing.T) {
	pairs := []Pair{{NameA: "a", NameB: "b", Score: 0.9}}
	var buf strings.Builder
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0})
	if strings.Contains(buf.String(), "introduced") {
		t.Errorf("output should not mention provenance when none set; got:\n%s", buf.String())
	}
}

func TestShortSHA_TruncatesAndPasses(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"abc", "abc"},
		{"1234567", "1234567"},
		{"abcdef0123456789", "abcdef0"},
	}
	for _, c := range cases {
		if got := shortSHA(c.in); got != c.want {
			t.Errorf("shortSHA(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPrepare_SortByAgeNewestPairsFirst(t *testing.T) {
	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pairs := []Pair{
		{NameA: "old", NameB: "x", Score: 0.5,
			ProvenanceA: &Provenance{FirstTime: t1}, ProvenanceB: &Provenance{FirstTime: t1}},
		{NameA: "newest", NameB: "y", Score: 0.5,
			ProvenanceA: &Provenance{FirstTime: t1}, ProvenanceB: &Provenance{FirstTime: t3}},
		{NameA: "mid", NameB: "z", Score: 0.5,
			ProvenanceA: &Provenance{FirstTime: t2}, ProvenanceB: &Provenance{FirstTime: t1}},
	}
	out, _ := Prepare(pairs, nil, Options{Sort: SortAge, Threshold: 0})
	wantOrder := []string{"newest", "mid", "old"}
	for i, w := range wantOrder {
		if out[i].NameA != w {
			t.Errorf("position %d: got NameA=%q, want %q", i, out[i].NameA, w)
		}
	}
}

func TestPrepare_SortByAgeAscOldestFirst(t *testing.T) {
	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pairs := []Pair{
		{NameA: "newer", NameB: "x", Score: 0.5,
			ProvenanceA: &Provenance{FirstTime: t2}, ProvenanceB: &Provenance{FirstTime: t1}},
		{NameA: "older", NameB: "y", Score: 0.5,
			ProvenanceA: &Provenance{FirstTime: t1}, ProvenanceB: &Provenance{FirstTime: t1}},
	}
	out, _ := Prepare(pairs, nil, Options{Sort: SortAgeAsc, Threshold: 0})
	if out[0].NameA != "older" {
		t.Errorf("expected older first, got %q", out[0].NameA)
	}
}

func TestPrepare_SortByAgePairsWithoutProvenanceSortLast(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pairs := []Pair{
		{NameA: "no-provenance", NameB: "x", Score: 0.5},
		{NameA: "has-provenance", NameB: "y", Score: 0.5,
			ProvenanceA: &Provenance{FirstTime: t1}, ProvenanceB: &Provenance{FirstTime: t1}},
	}
	out, _ := Prepare(pairs, nil, Options{Sort: SortAge, Threshold: 0})
	if out[0].NameA != "has-provenance" {
		t.Errorf("expected provenance-bearing pair first, got %q", out[0].NameA)
	}
	if out[1].NameA != "no-provenance" {
		t.Errorf("expected no-provenance pair last, got %q", out[1].NameA)
	}
}

func TestPrepare_CrossLangOnlyKeepsOnlyDifferentLangs(t *testing.T) {
	pairs := []Pair{
		{NameA: "same1", NameB: "same2", Score: 0.9, LangA: "Go", LangB: "Go"},
		{NameA: "cross1", NameB: "cross2", Score: 0.7, LangA: "Go", LangB: "Python"},
		{NameA: "cross3", NameB: "cross4", Score: 0.6, LangA: "TypeScript", LangB: "Python"},
		{NameA: "same3", NameB: "same4", Score: 0.8, LangA: "Python", LangB: "Python"},
	}
	out, _ := Prepare(pairs, nil, Options{CrossLangOnly: true, Threshold: 0, Sort: SortScore})
	if len(out) != 2 {
		t.Fatalf("expected 2 cross-language pairs, got %d", len(out))
	}
	for _, p := range out {
		if p.LangA == p.LangB {
			t.Errorf("cross-lang-only kept same-lang pair: %s/%s (lang=%s)", p.NameA, p.NameB, p.LangA)
		}
	}
}

func TestPrepare_CrossLangOnlyDropsPairsWithUnknownLang(t *testing.T) {
	// Empty Lang means we don't know the language — we can't confirm it's a
	// cross-lang match, so drop it under --cross-lang-only rather than
	// surfacing a noisy "" / "Go" pair as if it were cross-language.
	pairs := []Pair{
		{NameA: "u1", NameB: "g1", Score: 0.9, LangA: "", LangB: "Go"},
		{NameA: "u2", NameB: "u3", Score: 0.8, LangA: "", LangB: ""},
		{NameA: "g2", NameB: "p1", Score: 0.7, LangA: "Go", LangB: "Python"},
	}
	out, _ := Prepare(pairs, nil, Options{CrossLangOnly: true, Threshold: 0, Sort: SortScore})
	if len(out) != 1 {
		t.Fatalf("expected 1 pair (only the fully-typed cross-lang one), got %d", len(out))
	}
	if out[0].NameA != "g2" {
		t.Errorf("expected the Go/Python pair, got %s/%s", out[0].NameA, out[0].NameB)
	}
}

func TestPrepare_CrossLangOnlyOffKeepsAll(t *testing.T) {
	pairs := []Pair{
		{NameA: "same1", NameB: "same2", Score: 0.9, LangA: "Go", LangB: "Go"},
		{NameA: "cross1", NameB: "cross2", Score: 0.7, LangA: "Go", LangB: "Python"},
	}
	out, _ := Prepare(pairs, nil, Options{Threshold: 0, Sort: SortScore})
	if len(out) != 2 {
		t.Fatalf("expected all 2 pairs when CrossLangOnly is off, got %d", len(out))
	}
}

func TestJSONLabel_BoundariesAreStrict(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{0.97, "exact_clone"},
		{0.95, "near_clone"},
		{0.90, "near_clone"},
		{0.85, "strong_clone"},
		{0.80, "strong_clone"},
		{0.65, "refactor_candidate"},
		{0.50, "refactor_candidate"},
		{0.45, "weak_similarity"},
		{0.30, "weak_similarity"},
	}
	for _, c := range cases {
		if got := JSONLabel(c.score); got != c.want {
			t.Errorf("JSONLabel(%.2f) = %q; want %q", c.score, got, c.want)
		}
	}
}

func TestRender_LabelsByScore(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{0.97, "EXACT CLONE"},
		{0.90, "NEAR CLONE"},
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

func TestExtractPreview_UnlimitedReturnsWhole(t *testing.T) {
	code := "a\nb\nc"
	if got := ExtractPreview(code, 0); got != code {
		t.Errorf("n=0: got %q, want %q", got, code)
	}
	if got := ExtractPreview(code, -1); got != code {
		t.Errorf("n<0: got %q, want %q", got, code)
	}
}

func TestExtractPreview_NLargerThanInput(t *testing.T) {
	code := "a\nb"
	if got := ExtractPreview(code, 10); got != code {
		t.Errorf("got %q, want %q", got, code)
	}
}

func TestExtractPreview_TakesFirstN(t *testing.T) {
	code := "line1\nline2\nline3\nline4"
	got := ExtractPreview(code, 2)
	want := "line1\nline2"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildMatchPreview_FitsReturnsWhole(t *testing.T) {
	code := "a\nb\nc"
	p := BuildMatchPreview(code, []int{1, 2, 3}, 10, 0, 2, 1, 5)
	if p.StartLine != 10 || p.Text != code {
		t.Errorf("expected whole chunk preserved, got %+v", p)
	}
}

func TestBuildMatchPreview_UnlimitedMaxLinesShowsWhole(t *testing.T) {
	code := "a\nb\nc\nd\ne"
	p := BuildMatchPreview(code, []int{1, 2, 3, 4, 5}, 1, 0, 0, 1, 0)
	if p.Text != code {
		t.Errorf("maxLines=0 should show whole chunk, got %q", p.Text)
	}
}

func TestBuildMatchPreview_FocusesOnMatchRange(t *testing.T) {
	// 6-line chunk, maxLines=2, match starts at line 4 (token index 3).
	code := "l1\nl2\nl3\nl4\nl5\nl6"
	tokenLines := []int{1, 2, 3, 4, 5, 6}
	p := BuildMatchPreview(code, tokenLines, 100, 3, 4, 1, 2)
	// chunkStartLine=100, match begins at chunkLine 4 → absolute line 103.
	if p.StartLine != 103 {
		t.Errorf("StartLine: got %d, want 103", p.StartLine)
	}
	if p.Text != "l4\nl5" {
		t.Errorf("Text: got %q, want %q", p.Text, "l4\nl5")
	}
}

func TestBuildMatchPreview_KExtendsLastToken(t *testing.T) {
	// 8-line chunk, maxLines=10 (>8) so the whole chunk is returned —
	// exercises the "fits" path even when k extension would otherwise push
	// endTok out of range.
	code := "1\n2\n3\n4\n5\n6\n7\n8"
	tokenLines := []int{1, 2, 3, 4, 5, 6, 7, 8}
	p := BuildMatchPreview(code, tokenLines, 1, 0, 6, 5, 10)
	if p.Text != code {
		t.Errorf("expected whole chunk, got %q", p.Text)
	}
}

func TestBuildMatchPreview_TruncatesToMaxLines(t *testing.T) {
	// 10-line chunk; match spans 7 lines but maxLines=3, so the selection is
	// clamped. Exercises the `len(selected) > maxLines` branch.
	code := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10"
	tokenLines := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	p := BuildMatchPreview(code, tokenLines, 1, 0, 4, 3, 3)
	if p.StartLine != 1 {
		t.Errorf("StartLine: got %d, want 1", p.StartLine)
	}
	got := strings.Split(p.Text, "\n")
	if len(got) != 3 {
		t.Errorf("expected 3 lines after truncation, got %d: %v", len(got), got)
	}
}

func TestBuildMatchPreview_OutOfRangeFirstTokFallsBack(t *testing.T) {
	// firstTok beyond tokenLines bounds → fall back to first maxLines lines.
	code := "a\nb\nc\nd"
	p := BuildMatchPreview(code, []int{1, 2}, 5, 99, 99, 1, 2)
	if p.StartLine != 5 {
		t.Errorf("StartLine: got %d, want 5", p.StartLine)
	}
	if p.Text != "a\nb" {
		t.Errorf("Text: got %q, want %q", p.Text, "a\nb")
	}
}

