package main

// Subprocess CLI tests for class-level granularity (§5.2). These guard
// the end-to-end wiring: class-span chunks emitted by the splitter,
// carried through scan/cache, gated to class↔class comparisons in
// BuildMatrix, and rendered in the report with the class symbol.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestClasses_ClassPairRendersWithClassSymbols: the python-class-clone
// fixture is a renamed class with slightly reordered methods. The
// terminal report must show a class↔class finding carrying both class
// symbols, and the JSON output must contain a pair whose two endpoints
// are the class chunks.
func TestClasses_ClassPairRendersWithClassSymbols(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/bench/classes/python-class-clone"

	// JSON: a pair whose endpoints are the two class chunks.
	jsonOut, err := exec.Command(bin,
		"--json", "--no-cache", "--no-progress", fixtureDir).Output()
	if err != nil {
		t.Fatalf("json run: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			FileA string  `json:"file_a"`
			FileB string  `json:"file_b"`
			Score float64 `json:"score"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	classPair := false
	for _, p := range doc.Pairs {
		names := p.FileA + " | " + p.FileB
		if strings.Contains(names, "InventoryLedger") && strings.Contains(names, "StockRegister") {
			classPair = true
			if p.Score < 0.65 {
				t.Errorf("class pair score = %v, want >= 0.65", p.Score)
			}
		}
	}
	if !classPair {
		t.Errorf("expected an InventoryLedger <-> StockRegister class pair in JSON output:\n%s", jsonOut)
	}

	// Terminal report: the class symbols must appear in the findings.
	out, err := exec.Command(bin,
		"--plain", "--flat", "--no-cache", "--no-progress", fixtureDir).Output()
	if err != nil {
		t.Fatalf("terminal run: %v\nstdout:\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "InventoryLedger") || !strings.Contains(text, "StockRegister") {
		t.Errorf("expected the class symbols InventoryLedger and StockRegister in the terminal report:\n%s", text)
	}
}

// TestClasses_MixedKindProducesNoClassFinding: a class in a.js vs the
// same methods as loose functions in b.js. The class chunk must appear
// in NO pair (mixed-kind suppression), while the methods still match
// the loose functions individually.
func TestClasses_MixedKindProducesNoClassFinding(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/bench/classes/js-class-vs-loose-funcs"

	jsonOut, err := exec.Command(bin,
		"--json", "--no-cache", "--no-progress", fixtureDir).Output()
	if err != nil {
		t.Fatalf("json run: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			FileA string `json:"file_a"`
			FileB string `json:"file_b"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	methodPair := false
	for _, p := range doc.Pairs {
		names := p.FileA + " | " + p.FileB
		if strings.Contains(names, "CartMath") {
			t.Errorf("class chunk CartMath must not appear in any pair (mixed-kind), got %s <-> %s", p.FileA, p.FileB)
		}
		if strings.Contains(names, "subtotal") || strings.Contains(names, "taxFor") {
			methodPair = true
		}
	}
	if !methodPair {
		t.Errorf("expected the class methods to still match the loose functions:\n%s", jsonOut)
	}
}
