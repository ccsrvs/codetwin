package main

// Subprocess CLI tests for the default-on length-aware confidence
// dampener (--min-confidence-lines, default 10) and the exact-clone
// evidence gate. These guard the flag wiring: unit tests on
// similarity.LengthDampen can't catch a wrong flag default or a broken
// plumb-through to BuildMatrix.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestDampen_ShortNoisePairHiddenByDefault: the negative-short bench
// fixture is a pair of ~4-line Elixir clauses that share their token
// shape by API force, scoring above the default 0.50 threshold raw.
// With the dampener on by default (min-confidence-lines 10) the pair
// must NOT appear; explicitly disabling the dampener with
// --min-confidence-lines 0 must restore it.
func TestDampen_ShortNoisePairHiddenByDefault(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/bench/negative-short/elixir-short-clause"

	parse := func(args ...string) []struct {
		Score float64 `json:"score"`
		Label string  `json:"label"`
	} {
		t.Helper()
		full := append([]string{"--json", "--no-cache", "--no-progress"}, args...)
		full = append(full, fixtureDir)
		out, err := exec.Command(bin, full...).Output()
		if err != nil {
			t.Fatalf("run %v: %v\nstdout:\n%s", args, err, out)
		}
		var doc struct {
			Pairs []struct {
				Score float64 `json:"score"`
				Label string  `json:"label"`
			} `json:"pairs"`
		}
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatalf("parse JSON: %v\nstdout:\n%s", err, out)
		}
		return doc.Pairs
	}

	if pairs := parse(); len(pairs) != 0 {
		t.Errorf("default run should dampen the short noise pair below threshold, got %d pairs: %+v",
			len(pairs), pairs)
	}
	pairs := parse("--min-confidence-lines", "0")
	if len(pairs) == 0 {
		t.Fatal("--min-confidence-lines 0 should disable the dampener and surface the raw-score pair")
	}
	if pairs[0].Score < 0.50 {
		t.Errorf("undampened pair score = %v, want >= 0.50 (otherwise the fixture no longer exercises the dampener)",
			pairs[0].Score)
	}
}

// TestDampen_ShortIdenticalPairNeverLabeledExactClone: two token-
// identical short functions keep a high score even dampened (the
// multiplier floor is 0.5×), but the report must not call them an
// exact clone — the evidence gate demotes sub-10-line pairs one band.
func TestDampen_ShortIdenticalPairNeverLabeledExactClone(t *testing.T) {
	bin := subprocessBin(t)
	// The JS sum fixtures are two ~7-line functions with identical
	// token shape (rename-invariant clone).
	out, err := exec.Command(bin,
		"--json", "--no-cache", "--no-progress",
		"--min-confidence-lines", "0", // raw 1.0 score, so only the label gate acts
		"../../testdata/sum_a.js", "../../testdata/sum_b.js",
	).Output()
	if err != nil {
		t.Fatalf("run: %v\nstdout:\n%s", err, out)
	}
	var doc struct {
		Pairs []struct {
			Score float64 `json:"score"`
			Label string  `json:"label"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, out)
	}
	if len(doc.Pairs) == 0 {
		t.Fatal("expected the identical sum pair to surface")
	}
	for _, p := range doc.Pairs {
		if p.Score > 0.95 && p.Label == "exact_clone" {
			t.Errorf("short pair (score %v) must not be labeled exact_clone", p.Score)
		}
	}
	if !strings.Contains(string(out), `"near_clone"`) {
		t.Errorf("expected the demoted pair to carry the near_clone label:\n%s", out)
	}
}
