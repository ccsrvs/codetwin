package report

// Unit tests for default test-code segregation: test↔test pairs and
// test-only clusters are suppressed (and counted) by Prepare unless
// Options.IncludeTests is set; test↔production pairs and mixed clusters
// always render.

import (
	"bytes"
	"strings"
	"testing"
)

func segPairs() []Pair {
	return []Pair{
		{NameA: "a_test.go:1-9 TestA", NameB: "b_test.go:1-9 TestB", Score: 0.98, IsTestA: true, IsTestB: true},
		{NameA: "a_test.go:1-9 TestA", NameB: "prod.go:1-9 Prod", Score: 0.90, IsTestA: true, IsTestB: false},
		{NameA: "prod.go:1-9 Prod", NameB: "prod2.go:1-9 Prod2", Score: 0.80},
		// Below-threshold test↔test pair: already invisible, must NOT be
		// counted as suppressed.
		{NameA: "c_test.go:1-4 TestC", NameB: "d_test.go:1-4 TestD", Score: 0.30, IsTestA: true, IsTestB: true},
	}
}

func TestPrepare_SuppressesTestTestPairsByDefault(t *testing.T) {
	out, _, sup := Prepare(segPairs(), nil, Options{Threshold: 0.5, Sort: SortScore})
	if len(out) != 2 {
		t.Fatalf("expected 2 visible pairs (test↔prod + prod↔prod), got %d", len(out))
	}
	for _, p := range out {
		if p.IsTestA && p.IsTestB {
			t.Errorf("test↔test pair leaked through: %s / %s", p.NameA, p.NameB)
		}
	}
	if sup.TestTestPairs != 1 {
		t.Errorf("suppressed count = %d, want 1 (below-threshold pair must not count)", sup.TestTestPairs)
	}
}

func TestPrepare_IncludeTestsRestoresEverything(t *testing.T) {
	out, _, sup := Prepare(segPairs(), nil, Options{Threshold: 0.5, Sort: SortScore, IncludeTests: true})
	if len(out) != 3 {
		t.Fatalf("expected 3 visible pairs with IncludeTests, got %d", len(out))
	}
	if sup.TestTestPairs != 0 || sup.TestOnlyClusters != 0 {
		t.Errorf("nothing should be suppressed with IncludeTests, got %+v", sup)
	}
}

func TestPrepare_VerboseStillSuppressesTestTestPairs(t *testing.T) {
	// --verbose lifts the threshold, not the test segregation: the two
	// test↔test pairs (including the weak one, which would now render)
	// are suppressed and counted.
	out, _, sup := Prepare(segPairs(), nil, Options{Threshold: 0.5, Verbose: true, Sort: SortScore})
	if len(out) != 2 {
		t.Fatalf("expected 2 visible pairs under --verbose, got %d", len(out))
	}
	if sup.TestTestPairs != 2 {
		t.Errorf("suppressed count = %d, want 2 (verbose would have rendered both)", sup.TestTestPairs)
	}
}

func TestPrepare_SuppressesTestOnlyClusters(t *testing.T) {
	clusters := []Cluster{
		{ID: 0, Members: []string{"a_test.go:1-9 TestA", "b_test.go:1-9 TestB"}, Score: 0.9, TestOnly: true},
		{ID: 1, Members: []string{"a_test.go:1-9 TestA", "prod.go:1-9 Prod"}, Score: 0.8}, // mixed: kept
		{ID: 2, Members: []string{"prod.go:1-9 Prod", "prod2.go:1-9 Prod2"}, Score: 0.7},
	}
	_, out, sup := Prepare(nil, clusters, Options{Sort: SortScore})
	if len(out) != 2 {
		t.Fatalf("expected 2 visible clusters (mixed + prod), got %d", len(out))
	}
	for _, c := range out {
		if c.TestOnly {
			t.Errorf("test-only cluster leaked through: %v", c.Members)
		}
	}
	if sup.TestOnlyClusters != 1 {
		t.Errorf("suppressed cluster count = %d, want 1", sup.TestOnlyClusters)
	}

	_, out, sup = Prepare(nil, clusters, Options{Sort: SortScore, IncludeTests: true})
	if len(out) != 3 || sup.TestOnlyClusters != 0 {
		t.Errorf("IncludeTests should keep all 3 clusters and count 0, got %d clusters, %+v", len(out), sup)
	}
}

func TestPrepare_LimitAppliesAfterTestSuppression(t *testing.T) {
	pairs := []Pair{
		{NameA: "t1_test.go", NameB: "t2_test.go", Score: 0.99, IsTestA: true, IsTestB: true},
		{NameA: "p1.go", NameB: "p2.go", Score: 0.90},
		{NameA: "p3.go", NameB: "p4.go", Score: 0.80},
	}
	out, _, sup := Prepare(pairs, nil, Options{Threshold: 0.5, Sort: SortScore, Limit: 2})
	if len(out) != 2 {
		t.Fatalf("limit should apply to what remains after suppression, got %d pairs", len(out))
	}
	if out[0].NameA != "p1.go" || out[1].NameA != "p3.go" {
		t.Errorf("expected the two production pairs, got %s and %s", out[0].NameA, out[1].NameA)
	}
	if sup.TestTestPairs != 1 {
		t.Errorf("suppressed count = %d, want 1", sup.TestTestPairs)
	}
}

func TestRender_PrintsSuppressionSummaryLine(t *testing.T) {
	var buf bytes.Buffer
	Render(&buf, segPairs(), nil, Options{Plain: true, Threshold: 0.5})
	out := buf.String()
	if !strings.Contains(out, "1 test↔test pair suppressed (--include-tests to show)") {
		t.Errorf("expected suppression summary line, got:\n%s", out)
	}
	if strings.Contains(out, "b_test.go:1-9 TestB") {
		t.Errorf("suppressed test↔test pair should not render:\n%s", out)
	}
	if !strings.Contains(out, "prod.go:1-9 Prod") {
		t.Errorf("test↔prod pair must still render:\n%s", out)
	}
}

func TestRender_IncludeTestsOmitsSuppressionLine(t *testing.T) {
	var buf bytes.Buffer
	Render(&buf, segPairs(), nil, Options{Plain: true, Threshold: 0.5, IncludeTests: true})
	out := buf.String()
	if strings.Contains(out, "suppressed") {
		t.Errorf("no suppression line expected with IncludeTests:\n%s", out)
	}
	if !strings.Contains(out, "b_test.go:1-9 TestB") {
		t.Errorf("test↔test pair should render with IncludeTests:\n%s", out)
	}
}

func TestRender_EmptyReportStillNotesSuppressions(t *testing.T) {
	pairs := []Pair{
		{NameA: "a_test.go", NameB: "b_test.go", Score: 0.99, IsTestA: true, IsTestB: true},
	}
	var buf bytes.Buffer
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.5})
	out := buf.String()
	if !strings.Contains(out, "No similarities found") {
		t.Errorf("expected the empty-report banner:\n%s", out)
	}
	if !strings.Contains(out, "1 test↔test pair suppressed") {
		t.Errorf("empty report should still note the suppression:\n%s", out)
	}
}

func TestGroupDigits(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"}, {7, "7"}, {999, "999"}, {1000, "1,000"},
		{1819, "1,819"}, {1234567, "1,234,567"},
	}
	for _, c := range cases {
		if got := groupDigits(c.n); got != c.want {
			t.Errorf("groupDigits(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
