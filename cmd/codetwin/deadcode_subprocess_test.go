package main

// Subprocess CLI tests for --dead-code. The testdata/deadcode fixture
// holds one Go file with a dead private function, a dead exported
// function, a test-only function (called from app_test.go), and a live
// helper — plus a Python file with one dead underscore-private helper
// and one live pair. The section and JSON array must appear only when
// the flag is on, and --granularity file must be rejected.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const deadcodeFixture = "../../testdata/deadcode"

func runDeadcode(t *testing.T, args ...string) string {
	t.Helper()
	bin := subprocessBin(t)
	full := append([]string{"--no-cache", "--no-progress"}, args...)
	full = append(full, deadcodeFixture)
	out, err := exec.Command(bin, full...).Output()
	if err != nil {
		t.Fatalf("run %v: %v\nstdout:\n%s", args, err, out)
	}
	return string(out)
}

func TestDeadCode_OffByDefault(t *testing.T) {
	s := runDeadcode(t, "--plain")
	if strings.Contains(s, "DEAD CODE") {
		t.Errorf("DEAD CODE section must not render without --dead-code:\n%s", s)
	}
}

func TestDeadCode_SectionAndVerdicts(t *testing.T) {
	s := runDeadcode(t, "--plain", "--dead-code")
	if !strings.Contains(s, "DEAD CODE") {
		t.Fatalf("expected DEAD CODE section:\n%s", s)
	}
	for _, want := range []string{
		"deadPrivateFn",
		"DeadExportedFn",
		"testOnlyFn",
		"_dead_python_helper",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %s in the DEAD CODE section:\n%s", want, s)
		}
	}
	for _, alive := range []string{"liveHelper", "Runner", "used_python_helper", "python_caller", "TestRunnerAndDiff"} {
		if idx := strings.Index(s, "DEAD CODE"); idx >= 0 && strings.Contains(s[idx:], alive+" ") {
			t.Errorf("%s should not be reported dead:\n%s", alive, s)
		}
	}
	// Verdict labels: private+unreferenced → DEAD, exported → UNUSED IN
	// SCAN, test-referenced production code → TEST-ONLY.
	if !strings.Contains(s, "[DEAD            ]") {
		t.Errorf("expected a DEAD verdict label:\n%s", s)
	}
	if !strings.Contains(s, "[UNUSED IN SCAN  ]") {
		t.Errorf("expected an UNUSED IN SCAN verdict label:\n%s", s)
	}
	if !strings.Contains(s, "[TEST-ONLY       ]") {
		t.Errorf("expected a TEST-ONLY verdict label:\n%s", s)
	}
	if !strings.Contains(s, "Dead code") {
		t.Errorf("expected the summary Dead code line:\n%s", s)
	}
}

func TestDeadCode_JSON(t *testing.T) {
	out := runDeadcode(t, "--json", "--dead-code")
	var doc struct {
		DeadSymbols []struct {
			Name     string `json:"name"`
			Symbol   string `json:"symbol"`
			Kind     string `json:"kind"`
			Lang     string `json:"lang"`
			Exported bool   `json:"exported"`
			Verdict  string `json:"verdict"`
			TestRefs int    `json:"test_refs"`
		} `json:"dead_symbols"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("JSON parse: %v\n%s", err, out)
	}
	verdicts := map[string]string{}
	exported := map[string]bool{}
	for _, d := range doc.DeadSymbols {
		verdicts[d.Symbol] = d.Verdict
		exported[d.Symbol] = d.Exported
	}
	if verdicts["deadPrivateFn"] != "dead" || exported["deadPrivateFn"] {
		t.Errorf("deadPrivateFn: want dead/unexported, got %q exported=%v", verdicts["deadPrivateFn"], exported["deadPrivateFn"])
	}
	if verdicts["DeadExportedFn"] != "unused-in-scan" || !exported["DeadExportedFn"] {
		t.Errorf("DeadExportedFn: want unused-in-scan/exported, got %q exported=%v", verdicts["DeadExportedFn"], exported["DeadExportedFn"])
	}
	if verdicts["testOnlyFn"] != "test-only" {
		t.Errorf("testOnlyFn: want test-only, got %q", verdicts["testOnlyFn"])
	}
	if verdicts["_dead_python_helper"] != "dead" {
		t.Errorf("_dead_python_helper: want dead, got %q", verdicts["_dead_python_helper"])
	}
}

func TestDeadCode_JSONOmittedWithoutFlag(t *testing.T) {
	out := runDeadcode(t, "--json")
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("JSON parse: %v\n%s", err, out)
	}
	if _, ok := doc["dead_symbols"]; ok {
		t.Errorf("dead_symbols must be omitted without --dead-code:\n%s", out)
	}
}

func TestDeadCode_RejectsFileGranularity(t *testing.T) {
	bin := subprocessBin(t)
	out, err := exec.Command(bin,
		"--no-cache", "--no-progress", "--dead-code", "--granularity", "file", deadcodeFixture,
	).CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit for --dead-code --granularity file:\n%s", out)
	}
	if !strings.Contains(string(out), "--dead-code requires --granularity function") {
		t.Errorf("expected the granularity error message, got:\n%s", out)
	}
}
