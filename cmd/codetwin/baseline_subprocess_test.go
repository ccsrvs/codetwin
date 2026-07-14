package main

// Subprocess CLI tests for --update-baseline / --baseline (roadmap bet
// #5). These run the built binary against testdata/baseline/{before,
// after} and assert on the full contract: stdout still carries the
// normal report, drift events go to stderr one line each, --baseline
// exits 1 on any drift and 0 on none, --update-baseline always exits 0,
// the two flags are mutually exclusive, schema/params mismatches fail
// with clear errors, and --json grows a `drift` array.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runBaselineCmd runs the binary with args, returning stdout, stderr,
// and the exit code (0 for success).
func runBaselineCmd(t *testing.T, bin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %v: %v", args, err)
		}
		return out.String(), errb.String(), ee.ExitCode()
	}
	return out.String(), errb.String(), 0
}

func countOccurrences(s, sub string) int {
	return strings.Count(s, sub)
}

// TestBaselineCLI_UpdateThenCompare_EmitsDriftAndExit1 is the roadmap's
// end-to-end case: snapshot before/, compare after/, expect exactly one
// member-added, one member-removed, one member-changed on stderr and
// exit code 1.
func TestBaselineCLI_UpdateThenCompare_EmitsDriftAndExit1(t *testing.T) {
	bin := subprocessBin(t)
	file := filepath.Join(t.TempDir(), "base.json")

	stdout, stderr, code := runBaselineCmd(t, bin,
		"--update-baseline", file, "--no-cache", "--no-progress", "--plain",
		"../../testdata/baseline/before")
	if code != 0 {
		t.Fatalf("--update-baseline exit = %d, want 0\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "MergeCounts") {
		t.Errorf("--update-baseline should still print the normal report:\n%s", stdout)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("baseline file not written: %v", err)
	}

	stdout, stderr, code = runBaselineCmd(t, bin,
		"--baseline", file, "--no-cache", "--no-progress", "--plain",
		"../../testdata/baseline/after")
	if code != 1 {
		t.Fatalf("--baseline with drift: exit = %d, want 1\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "MergeCounts") {
		t.Errorf("--baseline should still print the normal report on stdout:\n%s", stdout)
	}
	for _, wantLine := range []string{
		"drift: member-added cluster 0: alpha.go SumEvenC",
		"drift: member-removed cluster 1: beta.go MergeCountsC",
		"drift: member-changed cluster 2: gamma.go ParseRecordB",
	} {
		if countOccurrences(stderr, wantLine) != 1 {
			t.Errorf("stderr should contain exactly one %q line:\n%s", wantLine, stderr)
		}
	}
	if got := countOccurrences(stderr, "drift:"); got != 3 {
		t.Errorf("stderr should carry exactly 3 drift lines, got %d:\n%s", got, stderr)
	}
	if strings.Contains(stderr, "cluster-appeared") || strings.Contains(stderr, "cluster-dissolved") {
		t.Errorf("no cluster should appear or dissolve in this fixture:\n%s", stderr)
	}
	if strings.Contains(stdout, "drift:") {
		t.Errorf("drift events belong on stderr, not stdout:\n%s", stdout)
	}
}

// TestBaselineCLI_NoDrift_ExitsZeroSilently: comparing a tree against
// its own snapshot is quiet and green.
func TestBaselineCLI_NoDrift_ExitsZeroSilently(t *testing.T) {
	bin := subprocessBin(t)
	file := filepath.Join(t.TempDir(), "base.json")

	_, stderr, code := runBaselineCmd(t, bin,
		"--update-baseline", file, "--no-cache", "--no-progress", "--plain",
		"../../testdata/baseline/before")
	if code != 0 {
		t.Fatalf("--update-baseline exit = %d, want 0\nstderr:\n%s", code, stderr)
	}
	_, stderr, code = runBaselineCmd(t, bin,
		"--baseline", file, "--no-cache", "--no-progress", "--plain",
		"../../testdata/baseline/before")
	if code != 0 {
		t.Fatalf("--baseline without drift: exit = %d, want 0\nstderr:\n%s", code, stderr)
	}
	if strings.Contains(stderr, "drift:") {
		t.Errorf("no-drift run must not print drift lines:\n%s", stderr)
	}
}

// TestBaselineCLI_UpdateBaseline_IsByteDeterministic: two snapshots of
// the same tree are byte-identical (the schema has no timestamp).
func TestBaselineCLI_UpdateBaseline_IsByteDeterministic(t *testing.T) {
	bin := subprocessBin(t)
	dir := t.TempDir()
	f1 := filepath.Join(dir, "one.json")
	f2 := filepath.Join(dir, "two.json")

	for _, f := range []string{f1, f2} {
		_, stderr, code := runBaselineCmd(t, bin,
			"--update-baseline", f, "--no-cache", "--no-progress", "--plain",
			"../../testdata/baseline/before")
		if code != 0 {
			t.Fatalf("--update-baseline exit = %d\nstderr:\n%s", code, stderr)
		}
	}
	b1, err1 := os.ReadFile(f1)
	b2, err2 := os.ReadFile(f2)
	if err1 != nil || err2 != nil {
		t.Fatalf("read snapshots: %v / %v", err1, err2)
	}
	if string(b1) != string(b2) {
		t.Errorf("two --update-baseline runs over the same tree must be byte-identical:\n%s\n---\n%s", b1, b2)
	}
}

// TestBaselineCLI_MutuallyExclusiveFlags: --baseline plus
// --update-baseline is an immediate usage error.
func TestBaselineCLI_MutuallyExclusiveFlags(t *testing.T) {
	bin := subprocessBin(t)
	file := filepath.Join(t.TempDir(), "base.json")

	_, stderr, code := runBaselineCmd(t, bin,
		"--baseline", file, "--update-baseline", file,
		"--no-cache", "--no-progress",
		"../../testdata/baseline/before")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "mutually exclusive") {
		t.Errorf("stderr should explain the flags are mutually exclusive:\n%s", stderr)
	}
}

// TestBaselineCLI_SchemaMismatch_ClearError: a future-versioned
// baseline file is rejected with a message naming the schema version
// and the fix.
func TestBaselineCLI_SchemaMismatch_ClearError(t *testing.T) {
	bin := subprocessBin(t)
	file := filepath.Join(t.TempDir(), "base.json")
	if err := os.WriteFile(file, []byte(`{"schema_version": 999, "clusters": []}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runBaselineCmd(t, bin,
		"--baseline", file, "--no-cache", "--no-progress",
		"../../testdata/baseline/before")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "schema version") || !strings.Contains(stderr, "--update-baseline") {
		t.Errorf("stderr should name the schema version and the fix:\n%s", stderr)
	}
}

// TestBaselineCLI_ParamsMismatch_ClearError: comparing with different
// scan parameters than the snapshot was taken with is user error, not
// drift.
func TestBaselineCLI_ParamsMismatch_ClearError(t *testing.T) {
	bin := subprocessBin(t)
	file := filepath.Join(t.TempDir(), "base.json")

	_, stderr, code := runBaselineCmd(t, bin,
		"--update-baseline", file, "--no-cache", "--no-progress", "--plain",
		"../../testdata/baseline/before")
	if code != 0 {
		t.Fatalf("--update-baseline exit = %d\nstderr:\n%s", code, stderr)
	}
	_, stderr, code = runBaselineCmd(t, bin,
		"--baseline", file, "--threshold", "0.8",
		"--no-cache", "--no-progress", "--plain",
		"../../testdata/baseline/before")
	if code != 1 {
		t.Fatalf("params mismatch: exit = %d, want 1\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "scan parameters") || !strings.Contains(stderr, "threshold") {
		t.Errorf("stderr should name the mismatched parameter:\n%s", stderr)
	}
	if strings.Contains(stderr, "drift:") {
		t.Errorf("a params mismatch must not be reported as drift:\n%s", stderr)
	}
}

// TestBaselineCLI_JSONAddsDriftArray: with --json the drift events also
// land in a `drift` array in the JSON document (stderr still carries
// the lines; exit code still 1).
func TestBaselineCLI_JSONAddsDriftArray(t *testing.T) {
	bin := subprocessBin(t)
	file := filepath.Join(t.TempDir(), "base.json")

	_, stderr, code := runBaselineCmd(t, bin,
		"--update-baseline", file, "--no-cache", "--no-progress", "--json",
		"../../testdata/baseline/before")
	if code != 0 {
		t.Fatalf("--update-baseline exit = %d\nstderr:\n%s", code, stderr)
	}

	stdout, stderr, code := runBaselineCmd(t, bin,
		"--baseline", file, "--no-cache", "--no-progress", "--json",
		"../../testdata/baseline/after")
	if code != 1 {
		t.Fatalf("--baseline --json with drift: exit = %d, want 1\nstderr:\n%s", code, stderr)
	}
	var doc struct {
		Drift []struct {
			Kind    string `json:"kind"`
			Cluster int    `json:"cluster"`
			Detail  string `json:"detail"`
		} `json:"drift"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("JSON parse: %v\n%s", err, stdout)
	}
	if len(doc.Drift) != 3 {
		t.Fatalf("drift array should carry 3 events, got %d:\n%s", len(doc.Drift), stdout)
	}
	kinds := map[string]int{}
	for _, e := range doc.Drift {
		kinds[e.Kind]++
	}
	for _, k := range []string{"member-added", "member-removed", "member-changed"} {
		if kinds[k] != 1 {
			t.Errorf("drift array should carry exactly one %s, got %d", k, kinds[k])
		}
	}
	if countOccurrences(stderr, "drift:") != 3 {
		t.Errorf("--json should still print drift lines to stderr:\n%s", stderr)
	}
}

// TestBaselineCLI_NoDriftJSON_OmitsDriftArray: the JSON schema is
// unchanged for CI consumers when there is nothing to report.
func TestBaselineCLI_NoDriftJSON_OmitsDriftArray(t *testing.T) {
	bin := subprocessBin(t)
	file := filepath.Join(t.TempDir(), "base.json")

	_, stderr, code := runBaselineCmd(t, bin,
		"--update-baseline", file, "--no-cache", "--no-progress", "--json",
		"../../testdata/baseline/before")
	if code != 0 {
		t.Fatalf("--update-baseline exit = %d\nstderr:\n%s", code, stderr)
	}
	stdout, _, code := runBaselineCmd(t, bin,
		"--baseline", file, "--no-cache", "--no-progress", "--json",
		"../../testdata/baseline/before")
	if code != 0 {
		t.Fatalf("no-drift --json exit = %d, want 0", code)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("JSON parse: %v", err)
	}
	if _, ok := doc["drift"]; ok {
		t.Errorf("no-drift JSON must omit the drift key (schema stability):\n%s", stdout)
	}
}

// TestBaselineCLI_MissingBaselineFile_ClearError: pointing --baseline
// at a nonexistent file is an error, not an empty comparison.
func TestBaselineCLI_MissingBaselineFile_ClearError(t *testing.T) {
	bin := subprocessBin(t)

	_, stderr, code := runBaselineCmd(t, bin,
		"--baseline", filepath.Join(t.TempDir(), "nope.json"),
		"--no-cache", "--no-progress",
		"../../testdata/baseline/before")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "baseline") {
		t.Errorf("stderr should mention the baseline file problem:\n%s", stderr)
	}
}
