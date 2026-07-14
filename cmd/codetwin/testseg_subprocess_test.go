package main

// Subprocess CLI tests for default test-code segregation. The
// testdata/testseg fixture holds two *_test.go files with an identical
// function, producing exactly one test↔test pair (and one test-only
// cluster). The default report must suppress both and print a one-line
// summary; --include-tests must restore them; the JSON schema must gain
// a `suppressed` object by default and stay byte-compatible with the
// pre-segregation contract under --include-tests.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const testsegFixture = "../../testdata/testseg"

func TestTestSeg_DefaultSuppressesTestTestPair(t *testing.T) {
	bin := subprocessBin(t)

	out, err := exec.Command(bin,
		"--plain", "--no-cache", "--no-progress", testsegFixture,
	).Output()
	if err != nil {
		t.Fatalf("run: %v\nstdout:\n%s", err, out)
	}
	s := string(out)

	if !strings.Contains(s, "1 test↔test pair suppressed (--include-tests to show)") {
		t.Errorf("expected the pair suppression summary line:\n%s", s)
	}
	if !strings.Contains(s, "1 test-only cluster suppressed (--include-tests to show)") {
		t.Errorf("expected the cluster suppression summary line:\n%s", s)
	}
	if strings.Contains(s, "util_a_test.go") || strings.Contains(s, "util_b_test.go") {
		t.Errorf("suppressed test↔test endpoints should not render:\n%s", s)
	}
}

func TestTestSeg_IncludeTestsShowsPair(t *testing.T) {
	bin := subprocessBin(t)

	out, err := exec.Command(bin,
		"--plain", "--no-cache", "--no-progress", "--include-tests", testsegFixture,
	).Output()
	if err != nil {
		t.Fatalf("run: %v\nstdout:\n%s", err, out)
	}
	s := string(out)

	if !strings.Contains(s, "util_a_test.go") || !strings.Contains(s, "util_b_test.go") {
		t.Errorf("--include-tests should render the test↔test finding:\n%s", s)
	}
	if strings.Contains(s, "suppressed") {
		t.Errorf("--include-tests should not print a suppression line:\n%s", s)
	}
}

func TestTestSeg_JSONDefaultAddsSuppressedSummary(t *testing.T) {
	bin := subprocessBin(t)

	out, err := exec.Command(bin,
		"--json", "--no-cache", "--no-progress", testsegFixture,
	).Output()
	if err != nil {
		t.Fatalf("run: %v\nstdout:\n%s", err, out)
	}
	var doc struct {
		Pairs      []map[string]any `json:"pairs"`
		Clusters   []map[string]any `json:"clusters"`
		Suppressed *struct {
			TestTestPairs    int `json:"test_test_pairs"`
			TestOnlyClusters int `json:"test_only_clusters"`
		} `json:"suppressed"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("JSON parse: %v\n%s", err, out)
	}
	if len(doc.Pairs) != 0 {
		t.Errorf("default JSON should omit the test↔test pair, got %d pairs", len(doc.Pairs))
	}
	if len(doc.Clusters) != 0 {
		t.Errorf("default JSON should omit the test-only cluster, got %d clusters", len(doc.Clusters))
	}
	if doc.Suppressed == nil {
		t.Fatalf("default JSON should include the suppressed summary object:\n%s", out)
	}
	if doc.Suppressed.TestTestPairs != 1 || doc.Suppressed.TestOnlyClusters != 1 {
		t.Errorf("suppressed counts = %+v, want 1 pair and 1 cluster", *doc.Suppressed)
	}
}

func TestTestSeg_JSONIncludeTestsMatchesLegacyContract(t *testing.T) {
	bin := subprocessBin(t)

	out, err := exec.Command(bin,
		"--json", "--no-cache", "--no-progress", "--include-tests", testsegFixture,
	).Output()
	if err != nil {
		t.Fatalf("run: %v\nstdout:\n%s", err, out)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("JSON parse: %v\n%s", err, out)
	}
	if _, ok := doc["suppressed"]; ok {
		t.Errorf("--include-tests must not emit a suppressed object (CI contract):\n%s", out)
	}
	var pairs []map[string]any
	if err := json.Unmarshal(doc["pairs"], &pairs); err != nil {
		t.Fatalf("pairs parse: %v", err)
	}
	if len(pairs) != 1 {
		t.Errorf("--include-tests should restore the test↔test pair, got %d pairs", len(pairs))
	}
}
