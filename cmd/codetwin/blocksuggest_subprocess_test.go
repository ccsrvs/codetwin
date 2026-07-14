package main

// Subprocess CLI tests for `--suggest <block-id>` / `--suggest-all` on
// partial clones (block-level findings). Same conventions as
// refactor_subprocess_test.go: build once in TestMain, drive the
// binary, assert on the stdout/stderr/exit-code surface.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// discoverBlockID runs `--json` against the fixture and returns the
// first partial clone's ID.
func discoverBlockID(t *testing.T, bin string, args ...string) string {
	t.Helper()
	doc := runBlockJSON(t, bin, args...)
	if len(doc.PartialClones) == 0 || doc.PartialClones[0].ID == "" {
		t.Fatalf("expected a partial clone with an id from %v", args)
	}
	return doc.PartialClones[0].ID
}

// TestSuggestBlock_VerbatimGo_ExitsZeroAndPrintsDiff: contract (a) —
// `--suggest <block-id>` on the verbatim-go fixture exits 0 and prints
// a unified diff containing the wrapped helper with the block's lines.
func TestSuggestBlock_VerbatimGo_ExitsZeroAndPrintsDiff(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/bench/blocks/positive/verbatim-go"
	blockID := discoverBlockID(t, bin, fixtureDir)

	cmd := exec.Command(bin, "--no-cache", "--no-progress",
		"--suggest", blockID, fixtureDir)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("--suggest exited non-zero: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr.String())
	}
	diff := string(stdout)
	if !strings.Contains(diff, "@@") {
		t.Errorf("stdout missing hunk header. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "func extractedBlock_"+blockID+"() {") {
		t.Errorf("stdout missing wrapped block helper. Got:\n%s", diff)
	}
	// The fixture's shared block body must be inside the helper.
	for _, want := range []string{
		"+\tseen := make(map[string]bool, len(req.Items))",
		"+\t\tseen[item.SKU] = true",
		"+// TODO(codetwin): parameters not inferred",
	} {
		if !strings.Contains(diff, want) {
			t.Errorf("stdout missing %q. Got:\n%s", want, diff)
		}
	}
	// The hosts' own headers are boundary noise, never helper body.
	if strings.Contains(diff, "+\tfunc exportOrderRows") {
		t.Errorf("host function header leaked into the helper body:\n%s", diff)
	}
}

// TestSuggestBlock_VerbatimGo_DiffAppliesClean: contract (b) — the
// emitted diff round-trips `git apply --check` + `git apply` in a temp
// git repo (mirroring the TestBuildPatch_*_AppliesClean convention).
// The binary runs with the tempdir as cwd; snippet paths become
// absolute, so the apply uses a computed -p<n> strip depth.
func TestSuggestBlock_VerbatimGo_DiffAppliesClean(t *testing.T) {
	bin := subprocessBin(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval tempdir: %v", err)
	}
	for _, name := range []string{"a.go", "b.go"} {
		src, err := os.ReadFile(filepath.Join("../../testdata/bench/blocks/positive/verbatim-go", name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(tmp, name), src, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	initCmd := exec.Command("git", "init", "-q")
	initCmd.Dir = tmp
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	jsonCmd := exec.Command(bin, "--json", "--no-cache", "--no-progress", ".")
	jsonCmd.Dir = tmp
	jsonOut, err := jsonCmd.Output()
	if err != nil {
		t.Fatalf("--json in tempdir: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc blockJSONDoc
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	if len(doc.PartialClones) == 0 {
		t.Fatalf("expected a partial clone in the tempdir scan:\n%s", jsonOut)
	}
	blockID := doc.PartialClones[0].ID

	sugCmd := exec.Command(bin, "--no-cache", "--no-progress", "--suggest", blockID, ".")
	sugCmd.Dir = tmp
	diff, err := sugCmd.Output()
	if err != nil {
		t.Fatalf("--suggest in tempdir: %v", err)
	}
	patchFile := filepath.Join(tmp, "p.diff")
	if err := os.WriteFile(patchFile, diff, 0o644); err != nil {
		t.Fatalf("write patch: %v", err)
	}

	// Diff paths are `a/<abs-path-minus-leading-slash>`; strip depth =
	// the `a/` prefix + every component of tmp.
	strip := 2 + strings.Count(strings.TrimPrefix(filepath.ToSlash(tmp), "/"), "/")
	pFlag := "-p" + itoa(strip)
	for _, args := range [][]string{
		{"apply", "--check", pFlag, patchFile},
		{"apply", pFlag, patchFile},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmp
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\noutput:\n%s\ndiff:\n%s", args, err, out, diff)
		}
	}
	patched, err := os.ReadFile(filepath.Join(tmp, "a.go"))
	if err != nil {
		t.Fatalf("read patched: %v", err)
	}
	if !strings.Contains(string(patched), "func extractedBlock_"+blockID) {
		t.Errorf("patched a.go missing the helper:\n%s", patched)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestSuggestBlock_UnknownID_ExitsOneWithHint: contract (c) — an ID
// matching neither a pair nor a block errors out with the lookup hint,
// exit 1, nothing on stdout.
func TestSuggestBlock_UnknownID_ExitsOneWithHint(t *testing.T) {
	bin := subprocessBin(t)
	cmd := exec.Command(bin, "--no-cache", "--no-progress",
		"--suggest", "ffffffff", "../../testdata/bench/blocks/positive/verbatim-go")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got err=%v stderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no pair or partial clone matches id") {
		t.Errorf("stderr missing lookup hint. Got:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout, got:\n%s", stdout.String())
	}
}

// TestSuggestBlock_VerbatimPython_ExitsZeroAndPrintsDiff: contract (d)
// — the Python block fixture produces a wrapped `def` helper.
func TestSuggestBlock_VerbatimPython_ExitsZeroAndPrintsDiff(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/bench/blocks/positive/verbatim-python"
	blockID := discoverBlockID(t, bin, fixtureDir)

	cmd := exec.Command(bin, "--no-cache", "--no-progress",
		"--suggest", blockID, fixtureDir)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("--suggest exited non-zero: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr.String())
	}
	diff := string(stdout)
	if !strings.Contains(diff, "def extracted_block_"+blockID+"():") {
		t.Errorf("stdout missing wrapped Python helper. Got:\n%s", diff)
	}
	for _, want := range []string{
		"@@",
		"+    cleaned = []",
		"+# TODO(codetwin): parameters not inferred",
	} {
		if !strings.Contains(diff, want) {
			t.Errorf("stdout missing %q. Got:\n%s", want, diff)
		}
	}
}

// TestSuggestBlock_UnsupportedLanguage_ExitsOneWithNote: contract (e)
// — a JS block finding is real, but block-mode synthesis only ships Go
// and Python; the CLI must print a note and exit 1.
func TestSuggestBlock_UnsupportedLanguage_ExitsOneWithNote(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/blocksuggest/js"
	blockID := discoverBlockID(t, bin, fixtureDir)

	cmd := exec.Command(bin, "--no-cache", "--no-progress",
		"--suggest", blockID, fixtureDir)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got err=%v stderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "note: block extraction not implemented for javascript") {
		t.Errorf("stderr missing the not-implemented note. Got:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout on rejection, got:\n%s", stdout.String())
	}
}

// TestSuggestAll_PopulatesSuggestedPatchOnPartialClones: --suggest-all
// --json embeds suggested_patch on visible partial clones — a real
// diff for supported languages, a note for unsupported ones.
func TestSuggestAll_PopulatesSuggestedPatchOnPartialClones(t *testing.T) {
	bin := subprocessBin(t)

	type suggestAllDoc struct {
		PartialClones []struct {
			ID             string `json:"id"`
			SuggestedPatch *struct {
				UnifiedDiff string  `json:"unified_diff"`
				HelperName  string  `json:"helper_name"`
				Confidence  float64 `json:"confidence"`
				Note        string  `json:"note"`
			} `json:"suggested_patch"`
		} `json:"partial_clones"`
	}
	run := func(dir string) suggestAllDoc {
		out, err := exec.Command(bin, "--json", "--no-cache", "--no-progress",
			"--suggest-all", dir).Output()
		if err != nil {
			t.Fatalf("--suggest-all %s: %v\nstdout:\n%s", dir, err, out)
		}
		var doc suggestAllDoc
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatalf("parse JSON: %v\nstdout:\n%s", err, out)
		}
		if len(doc.PartialClones) == 0 || doc.PartialClones[0].SuggestedPatch == nil {
			t.Fatalf("expected partial clone with suggested_patch in %s:\n%s", dir, out)
		}
		return doc
	}

	goDoc := run("../../testdata/bench/blocks/positive/verbatim-go")
	sp := goDoc.PartialClones[0].SuggestedPatch
	if sp.UnifiedDiff == "" || sp.Note != "" {
		t.Errorf("Go block: want a diff and no note, got note=%q", sp.Note)
	}
	if !strings.HasPrefix(sp.HelperName, "extractedBlock_") {
		t.Errorf("Go block helper_name = %q, want extractedBlock_… prefix", sp.HelperName)
	}
	if sp.Confidence <= 0 {
		t.Errorf("Go block confidence = %v, want > 0", sp.Confidence)
	}

	jsDoc := run("../../testdata/blocksuggest/js")
	jsp := jsDoc.PartialClones[0].SuggestedPatch
	if jsp.UnifiedDiff != "" {
		t.Errorf("JS block must not carry a diff, got:\n%s", jsp.UnifiedDiff)
	}
	if !strings.Contains(jsp.Note, "not implemented for javascript") {
		t.Errorf("JS block note = %q, want not-implemented", jsp.Note)
	}
}
