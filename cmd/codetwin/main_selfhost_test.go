package main

// Self-host integration tests: run the codetwin binary against its own
// `internal/` source tree to confirm the tool doesn't crash, returns
// exit 0, and produces parseable JSON on real-world code. Closes the
// "Self-host" testing layer described in docs/roadmap.md (line 281)
// for the standing 25.2% coverage gap on cmd/codetwin.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestSelfHost_RunsCleanOnInternal: the binary applied to ./internal at
// a sane threshold exits 0 and emits valid JSON with a `pairs` array
// (count is unstable across edits, so we don't assert on it).
func TestSelfHost_RunsCleanOnInternal(t *testing.T) {
	bin := subprocessBin(t)

	cmd := exec.Command(bin,
		"--threshold", "0.85",
		"--no-cache", "--no-progress",
		"--json", "../../internal",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("self-host run failed: %v\nstderr:\n%s", err, stderr.String())
	}
	var doc struct {
		Pairs    []map[string]any `json:"pairs"`
		Clusters []map[string]any `json:"clusters"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("self-host JSON did not parse: %v\nstdout (first 400 bytes):\n%s",
			err, truncate(string(stdout), 400))
	}
}

// TestSelfHost_SuggestAllRunsCleanOnInternal: combining --suggest-all
// with --json on real source must not crash. This guards the
// cross-feature interaction between the suggestion pipeline and the
// detection JSON output, which unit tests can't catch.
func TestSelfHost_SuggestAllRunsCleanOnInternal(t *testing.T) {
	bin := subprocessBin(t)

	cmd := exec.Command(bin,
		"--threshold", "0.85",
		"--no-cache", "--no-progress",
		"--suggest-all", "--json", "../../internal",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("self-host --suggest-all run failed: %v\nstderr:\n%s",
			err, stderr.String())
	}
	if !json.Valid(stdout) {
		t.Fatalf("self-host --suggest-all output is not valid JSON. First 400 bytes:\n%s",
			truncate(string(stdout), 400))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
