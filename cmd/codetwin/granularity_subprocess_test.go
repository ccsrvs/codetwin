package main

// Subprocess CLI tests for --granularity (review §5.1). These guard the
// flag wiring: the in-process pinning tests in granularity_test.go prove
// the pipeline behaves at both granularities, but only a real binary run
// catches a broken flag definition, a config-default plumbing miss, or a
// wrong exit code on an invalid value.

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const granularityFixture = "../../testdata/granularity"

// granularityJSONPairs runs the binary with the given extra args against
// the granularity fixture and returns the parsed pairs.
func granularityJSONPairs(t *testing.T, extra ...string) []struct {
	FileA string  `json:"file_a"`
	FileB string  `json:"file_b"`
	Score float64 `json:"score"`
} {
	t.Helper()
	bin := subprocessBin(t)
	args := append([]string{"--json", "--no-cache", "--no-progress"}, extra...)
	args = append(args, granularityFixture)
	out, err := exec.Command(bin, args...).Output()
	if err != nil {
		t.Fatalf("run %v: %v\nstdout:\n%s", extra, err, out)
	}
	var doc struct {
		Pairs []struct {
			FileA string  `json:"file_a"`
			FileB string  `json:"file_b"`
			Score float64 `json:"score"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, out)
	}
	return doc.Pairs
}

func TestGranularity_FileModeJSONReportsOneWholeFilePair(t *testing.T) {
	pairs := granularityJSONPairs(t, "--granularity", "file")
	if len(pairs) != 1 {
		t.Fatalf("file mode: expected exactly 1 whole-file pair, got %d: %+v", len(pairs), pairs)
	}
	p := pairs[0]
	// Whole-file snippet names are bare paths — no ":start-end symbol".
	if strings.Contains(p.FileA, ":") || strings.Contains(p.FileB, ":") {
		t.Errorf("file-mode pair endpoints should be bare paths, got %q and %q", p.FileA, p.FileB)
	}
	if !strings.HasSuffix(p.FileA, "module_a.go") || !strings.HasSuffix(p.FileB, "module_b.go") {
		t.Errorf("expected the module_a/module_b file pair, got %q and %q", p.FileA, p.FileB)
	}
	if p.Score < 0.65 {
		t.Errorf("file pair score = %.3f, want >= 0.65", p.Score)
	}
}

func TestGranularity_FileModeTerminalRendersFilePair(t *testing.T) {
	bin := subprocessBin(t)
	out, err := exec.Command(bin,
		"--plain", "--no-cache", "--no-progress",
		"--granularity", "file", granularityFixture,
	).Output()
	if err != nil {
		t.Fatalf("run: %v\nstdout:\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, "module_a.go") || !strings.Contains(s, "module_b.go") {
		t.Errorf("terminal report should show the whole-file pair endpoints:\n%s", s)
	}
	for _, sym := range []string{"parseRecords", "renderSummary", "mergeCounts"} {
		if strings.Contains(s, sym) {
			t.Errorf("file mode must not render function-level symbol %q:\n%s", sym, s)
		}
	}
}

func TestGranularity_DefaultRemainsFunctionLevel(t *testing.T) {
	pairs := granularityJSONPairs(t)
	if len(pairs) != 3 {
		t.Fatalf("default (function) mode: expected the 3 counterpart function pairs, got %d: %+v", len(pairs), pairs)
	}
	for _, p := range pairs {
		if !strings.Contains(p.FileA, ":") || !strings.Contains(p.FileB, ":") {
			t.Errorf("function-mode endpoints should carry line ranges and symbols, got %q and %q", p.FileA, p.FileB)
		}
	}
}

func TestGranularity_InvalidValueExitsOneWithMessage(t *testing.T) {
	bin := subprocessBin(t)
	out, err := exec.Command(bin,
		"--no-cache", "--no-progress",
		"--granularity", "class", granularityFixture,
	).Output()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected the run to fail, got err=%v\nstdout:\n%s", err, out)
	}
	if code := ee.ExitCode(); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	stderr := string(ee.Stderr)
	if !strings.Contains(stderr, `invalid granularity "class"`) ||
		!strings.Contains(stderr, "function, file") {
		t.Errorf("stderr should name the bad value and list valid ones, got:\n%s", stderr)
	}
}

// TestGranularity_ConfigDefaultAppliesAndCLIWins: `granularity` in
// .codetwin.json defaults must switch the mode, and an explicit CLI flag
// must still win over the config value.
func TestGranularity_ConfigDefaultAppliesAndCLIWins(t *testing.T) {
	bin := subprocessBin(t)
	absBin, err := filepath.Abs(bin)
	if err != nil {
		t.Fatalf("abs bin: %v", err)
	}
	absFixture, err := filepath.Abs(granularityFixture)
	if err != nil {
		t.Fatalf("abs fixture: %v", err)
	}
	dir := t.TempDir()
	cfg := `{"defaults": {"granularity": "file"}}`
	if err := os.WriteFile(filepath.Join(dir, ".codetwin.json"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	run := func(extra ...string) []byte {
		t.Helper()
		args := append([]string{"--json", "--no-cache", "--no-progress"}, extra...)
		args = append(args, absFixture)
		cmd := exec.Command(absBin, args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("run %v: %v\nstdout:\n%s", extra, err, out)
		}
		return out
	}
	parsePairs := func(out []byte) []struct {
		FileA string `json:"file_a"`
	} {
		t.Helper()
		var doc struct {
			Pairs []struct {
				FileA string `json:"file_a"`
			} `json:"pairs"`
		}
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatalf("parse JSON: %v\nstdout:\n%s", err, out)
		}
		return doc.Pairs
	}

	// Config default alone → file mode: one bare-path pair.
	pairs := parsePairs(run())
	if len(pairs) != 1 || strings.Contains(pairs[0].FileA, ":") {
		t.Errorf("config default granularity=file should yield the single whole-file pair, got %+v", pairs)
	}

	// Explicit CLI flag wins over the config default.
	pairs = parsePairs(run("--granularity", "function"))
	if len(pairs) != 3 {
		t.Errorf("--granularity function must override the config default, got %d pairs: %+v", len(pairs), pairs)
	}
}

func TestGranularity_ConfigInvalidValueExitsOne(t *testing.T) {
	bin := subprocessBin(t)
	absBin, err := filepath.Abs(bin)
	if err != nil {
		t.Fatalf("abs bin: %v", err)
	}
	absFixture, err := filepath.Abs(granularityFixture)
	if err != nil {
		t.Fatalf("abs fixture: %v", err)
	}
	dir := t.TempDir()
	cfg := `{"defaults": {"granularity": "module"}}`
	if err := os.WriteFile(filepath.Join(dir, ".codetwin.json"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cmd := exec.Command(absBin, "--json", "--no-cache", "--no-progress", absFixture)
	cmd.Dir = dir
	out, err := cmd.Output()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected the run to fail on the bad config value, got err=%v\nstdout:\n%s", err, out)
	}
	if code := ee.ExitCode(); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stderr := string(ee.Stderr); !strings.Contains(stderr, `invalid granularity "module"`) {
		t.Errorf("stderr should name the bad config value, got:\n%s", stderr)
	}
}
