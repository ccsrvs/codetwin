package main

// Subprocess CLI tests for the structural-twin label band (R6). These
// guard the end-to-end wiring: lexical terms computed at scan time,
// the pair-level lexical Jaccard in BuildMatrix, and the label logic —
// none of which a unit test on report.JSONLabel can catch if the
// plumb-through breaks.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

type twinJSONDoc struct {
	Pairs []struct {
		Score   float64  `json:"score"`
		Label   string   `json:"label"`
		Lexical *float64 `json:"lexical"`
	} `json:"pairs"`
}

func runTwinJSON(t *testing.T, bin string, args ...string) twinJSONDoc {
	t.Helper()
	full := append([]string{"--json", "--no-cache", "--no-progress"}, args...)
	out, err := exec.Command(bin, full...).Output()
	if err != nil {
		t.Fatalf("run %v: %v\nstdout:\n%s", args, err, out)
	}
	var doc twinJSONDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, out)
	}
	return doc
}

// TestTwins_FixtureRendersStructuralTwin: the go-tabletests twins
// fixture is a token-identical pair (score 1.0, 20+ non-blank lines —
// it clears the exact-clone evidence gate) with disjoint vocabulary.
// It must carry the structural_twin label in JSON, expose the lexical
// sub-score, and render STRUCTURAL TWIN in the terminal report.
func TestTwins_FixtureRendersStructuralTwin(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/bench/twins/go-tabletests"

	doc := runTwinJSON(t, bin, fixtureDir)
	if len(doc.Pairs) == 0 {
		t.Fatal("expected the twins pair to surface")
	}
	p := doc.Pairs[0]
	if p.Score <= 0.85 {
		t.Errorf("twins pair score = %v, want > 0.85 (fixture no longer exercises the top bands)", p.Score)
	}
	if p.Label != "structural_twin" {
		t.Errorf("twins pair label = %q, want structural_twin", p.Label)
	}
	if p.Lexical == nil {
		t.Error("twins pair should expose the lexical sub-score in JSON")
	} else if *p.Lexical >= 0.20 {
		t.Errorf("twins pair lexical = %v, want < 0.20", *p.Lexical)
	}

	// Terminal render: same pair, same verdict, plus the summary
	// bucket. --flat so the two-member family renders as a pair (the
	// default cluster-first layout collapses it into its cluster).
	out, err := exec.Command(bin, "--plain", "--flat", "--no-cache", "--no-progress", fixtureDir).Output()
	if err != nil {
		t.Fatalf("terminal run: %v\nstdout:\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "STRUCTURAL TWIN") {
		t.Errorf("expected STRUCTURAL TWIN in terminal output:\n%s", text)
	}
	if strings.Contains(text, "EXACT CLONE") {
		t.Errorf("twins pair must not render as EXACT CLONE:\n%s", text)
	}
	if !strings.Contains(text, "Structural twins") {
		t.Errorf("expected a 'Structural twins' summary line:\n%s", text)
	}
}

// TestTwins_RenamedCloneKeepsExactClone: rename-invariance guard at
// the CLI level. go-renamed is a systematic rename scoring 1.0; its
// shared vocabulary (helper calls, string literals) must keep it above
// the lexical floor, so it still reports exact_clone.
func TestTwins_RenamedCloneKeepsExactClone(t *testing.T) {
	bin := subprocessBin(t)
	doc := runTwinJSON(t, bin, "../../testdata/bench/positive/go-renamed")
	if len(doc.Pairs) == 0 {
		t.Fatal("expected the renamed pair to surface")
	}
	p := doc.Pairs[0]
	if p.Label != "exact_clone" {
		t.Errorf("go-renamed label = %q, want exact_clone (renames must never demote to structural_twin)", p.Label)
	}
	if p.Lexical == nil {
		t.Error("top-band pair should expose the lexical sub-score in JSON")
	} else if *p.Lexical < 0.20 {
		t.Errorf("go-renamed lexical = %v, want >= 0.20 (fixture drifted toward the floor)", *p.Lexical)
	}
}

// TestTwins_BelowBandPairsHaveNoLexicalField: the lexical sub-score is
// computed lazily, only for pairs above the near-clone band; JSON for
// lower-scoring pairs must omit the field entirely.
func TestTwins_BelowBandPairsHaveNoLexicalField(t *testing.T) {
	bin := subprocessBin(t)
	// crosslang-sum scores ~0.64 — well below the 0.85 band.
	doc := runTwinJSON(t, bin, "--threshold", "0.5", "../../testdata/bench/positive/crosslang-sum")
	if len(doc.Pairs) == 0 {
		t.Fatal("expected the crosslang pair to surface")
	}
	for _, p := range doc.Pairs {
		if p.Score <= 0.85 && p.Lexical != nil {
			t.Errorf("pair at score %v should not carry a lexical field (lazy computation)", p.Score)
		}
	}
}
