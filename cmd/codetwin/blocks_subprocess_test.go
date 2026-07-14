package main

// Subprocess CLI tests for block-level partial-clone detection (review
// §5.3): the PARTIAL CLONES terminal section, the `partial_clones`
// JSON array, the --min-block-lines flag (including 0 = disabled), and
// test↔test block suppression under the default test segregation.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

type blockJSONDoc struct {
	PartialClones []struct {
		ID          string  `json:"id"`
		FileA       string  `json:"file_a"`
		StartLineA  int     `json:"start_line_a"`
		EndLineA    int     `json:"end_line_a"`
		SymbolA     string  `json:"symbol_a"`
		FileB       string  `json:"file_b"`
		StartLineB  int     `json:"start_line_b"`
		EndLineB    int     `json:"end_line_b"`
		SymbolB     string  `json:"symbol_b"`
		Containment float64 `json:"containment"`
		LinesA      int     `json:"lines_a"`
		LinesB      int     `json:"lines_b"`
	} `json:"partial_clones"`
	Suppressed *struct {
		TestTestBlocks int `json:"test_test_blocks"`
	} `json:"suppressed"`
}

func runBlockJSON(t *testing.T, bin string, args ...string) blockJSONDoc {
	t.Helper()
	full := append([]string{"--json", "--no-cache", "--no-progress"}, args...)
	out, err := exec.Command(bin, full...).Output()
	if err != nil {
		t.Fatalf("run %v: %v\nstdout:\n%s", args, err, out)
	}
	var doc blockJSONDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, out)
	}
	return doc
}

// TestBlocks_VerbatimFixtureSurfacesAsPartialClone is the review's
// dilution demo end-to-end: a 16-line verbatim block inside two
// ~45-line distinct hosts scores ~0.42 at function level (invisible at
// the default threshold) yet must surface as a partial clone with the
// full JSON schema.
func TestBlocks_VerbatimFixtureSurfacesAsPartialClone(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/bench/blocks/positive/verbatim-go"

	doc := runBlockJSON(t, bin, fixtureDir)
	if len(doc.PartialClones) != 1 {
		t.Fatalf("expected exactly 1 partial clone, got %d", len(doc.PartialClones))
	}
	pc := doc.PartialClones[0]
	if len(pc.ID) != 8 {
		t.Errorf("partial clone ID = %q, want 8-char hex", pc.ID)
	}
	if !strings.HasSuffix(pc.FileA, "a.go") || !strings.HasSuffix(pc.FileB, "b.go") {
		t.Errorf("files = %q / %q, want the fixture's a.go / b.go", pc.FileA, pc.FileB)
	}
	// Expected shared block: a.go 13-28 / b.go 13-28 (fixture header);
	// boundary tokens may extend the range by a line on either end.
	if pc.StartLineA > 13 || pc.EndLineA < 28 || pc.StartLineB > 13 || pc.EndLineB < 28 {
		t.Errorf("block ranges a:%d-%d b:%d-%d do not cover the expected 13-28 block",
			pc.StartLineA, pc.EndLineA, pc.StartLineB, pc.EndLineB)
	}
	if pc.SymbolA != "exportOrderRows" || pc.SymbolB != "dispatchJobs" {
		t.Errorf("symbols = %q / %q, want exportOrderRows / dispatchJobs", pc.SymbolA, pc.SymbolB)
	}
	if pc.Containment < 0.8 {
		t.Errorf("containment = %v, want >= 0.8", pc.Containment)
	}
	if pc.LinesA < 8 || pc.LinesB < 8 {
		t.Errorf("block lines = %d/%d, want >= 8 on both sides", pc.LinesA, pc.LinesB)
	}
}

// TestBlocks_TerminalSectionRenders: the same fixture through the
// plain terminal renderer must produce the PARTIAL CLONES section with
// the ⊂ container notation and a summary count.
func TestBlocks_TerminalSectionRenders(t *testing.T) {
	bin := subprocessBin(t)
	out, err := exec.Command(bin, "--plain", "--no-cache", "--no-progress",
		"../../testdata/bench/blocks/positive/verbatim-go").Output()
	if err != nil {
		t.Fatalf("terminal run: %v\nstdout:\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{
		"PARTIAL CLONES",
		"[PARTIAL CLONE   ]",
		"contained",
		"⊂ exportOrderRows",
		"⊂ dispatchJobs",
		"Partial clones    1",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("terminal output missing %q:\n%s", want, text)
		}
	}
}

// TestBlocks_MinBlockLinesZeroDisables: --min-block-lines 0 must turn
// the channel off entirely — no partial_clones key in JSON, no section
// in the terminal report.
func TestBlocks_MinBlockLinesZeroDisables(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/bench/blocks/positive/verbatim-go"

	out, err := exec.Command(bin, "--json", "--no-cache", "--no-progress",
		"--min-block-lines", "0", fixtureDir).Output()
	if err != nil {
		t.Fatalf("json run: %v\nstdout:\n%s", err, out)
	}
	if strings.Contains(string(out), "partial_clones") {
		t.Errorf("--min-block-lines 0 must omit partial_clones from JSON:\n%s", out)
	}

	term, err := exec.Command(bin, "--plain", "--no-cache", "--no-progress",
		"--min-block-lines", "0", fixtureDir).Output()
	if err != nil {
		t.Fatalf("terminal run: %v\nstdout:\n%s", err, term)
	}
	if strings.Contains(string(term), "PARTIAL CLONE") {
		t.Errorf("--min-block-lines 0 must not render the section:\n%s", term)
	}
}

// TestBlocks_MinBlockLinesRaisedFiltersSmallBlocks: the verbatim
// fixture's block is 16 lines; a floor above that must reject it.
func TestBlocks_MinBlockLinesRaisedFiltersSmallBlocks(t *testing.T) {
	bin := subprocessBin(t)
	doc := runBlockJSON(t, bin, "--min-block-lines", "25",
		"../../testdata/bench/blocks/positive/verbatim-go")
	if len(doc.PartialClones) != 0 {
		t.Errorf("min-block-lines 25 should reject the 16-line block, got %d findings", len(doc.PartialClones))
	}
}

// TestBlocks_TestTestSuppressedByDefault: the blockseg fixture hosts
// the verbatim block inside two _test.go files. Default runs suppress
// the finding (counted in suppressed.test_test_blocks and the terminal
// summary); --include-tests restores it.
func TestBlocks_TestTestSuppressedByDefault(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/blockseg"

	doc := runBlockJSON(t, bin, fixtureDir)
	if len(doc.PartialClones) != 0 {
		t.Errorf("test↔test partial clone must be suppressed by default, got %d", len(doc.PartialClones))
	}
	if doc.Suppressed == nil || doc.Suppressed.TestTestBlocks != 1 {
		t.Errorf("suppressed.test_test_blocks = %+v, want 1", doc.Suppressed)
	}

	restored := runBlockJSON(t, bin, "--include-tests", fixtureDir)
	if len(restored.PartialClones) != 1 {
		t.Errorf("--include-tests should restore the partial clone, got %d", len(restored.PartialClones))
	}
	if restored.Suppressed != nil && restored.Suppressed.TestTestBlocks != 0 {
		t.Errorf("--include-tests should not report suppressed blocks: %+v", restored.Suppressed)
	}

	term, err := exec.Command(bin, "--plain", "--no-cache", "--no-progress", fixtureDir).Output()
	if err != nil {
		t.Fatalf("terminal run: %v\nstdout:\n%s", err, term)
	}
	if !strings.Contains(string(term), "1 test↔test partial clone suppressed (--include-tests to show)") {
		t.Errorf("terminal summary missing suppressed partial-clone line:\n%s", term)
	}
}

// TestBlocks_LimitApplies: --limit must cap the partial-clones list
// like it caps pairs and clusters.
func TestBlocks_LimitApplies(t *testing.T) {
	bin := subprocessBin(t)
	// The two block fixtures produce one finding each when scanned
	// together (four distinct hosts, two shared blocks).
	all := runBlockJSON(t, bin,
		"../../testdata/bench/blocks/positive/verbatim-go",
		"../../testdata/bench/blocks/positive/renamed-go")
	if len(all.PartialClones) < 2 {
		t.Fatalf("expected >= 2 partial clones across the two fixtures, got %d", len(all.PartialClones))
	}
	limited := runBlockJSON(t, bin, "--limit", "1",
		"../../testdata/bench/blocks/positive/verbatim-go",
		"../../testdata/bench/blocks/positive/renamed-go")
	if len(limited.PartialClones) != 1 {
		t.Errorf("--limit 1: got %d partial clones, want 1", len(limited.PartialClones))
	}
}
