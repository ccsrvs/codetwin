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