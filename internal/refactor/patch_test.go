package refactor

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildPatch_GoSimple_AppliesClean is the round-trip integration
// test: synthesize a starter helper for the simple Go fixture, build
// a unified diff, copy the fixture into a temp dir, init a git repo,
// and run `git apply --check` against the diff. Exit 0 means the diff
// is well-formed and the patched file would be byte-accurate.
func TestBuildPatch_GoSimple_AppliesClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	a, b := loadSnippets(t, "../../testdata/refactor/go/simple")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("synthesis rejected: %q", s.Note)
	}

	tmp := t.TempDir()
	srcA, err := os.ReadFile("../../testdata/refactor/go/simple/a.go")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dstA := filepath.Join(tmp, "a.go")
	if err := os.WriteFile(dstA, srcA, 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}

	gitInit(t, tmp)

	// Build the patch with the path that matches the file inside the
	// temp git repo (relative path "a.go", not the testdata path).
	diff := buildAppendPatch("a.go", string(srcA), s.HelperSrc)
	patchFile := filepath.Join(tmp, "p.diff")
	if err := os.WriteFile(patchFile, []byte(diff), 0o644); err != nil {
		t.Fatalf("write patch: %v", err)
	}

	cmd := exec.Command("git", "apply", "--check", patchFile)
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git apply --check failed: %v\noutput:\n%s\ndiff:\n%s",
			err, out, diff)
	}

	// Apply for real and verify the helper appears.
	cmd = exec.Command("git", "apply", patchFile)
	cmd.Dir = tmp
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git apply failed: %v\noutput:\n%s", err, out)
	}
	patched, err := os.ReadFile(dstA)
	if err != nil {
		t.Fatalf("read patched: %v", err)
	}
	if !strings.Contains(string(patched), "func extracted_priceWithTaxA_deadbeef") {
		t.Errorf("patched file does not contain the helper. Content:\n%s", patched)
	}
	if !strings.Contains(string(patched), "// Divergences (B vs A):") {
		t.Errorf("patched file missing divergence comment block")
	}
}

// TestBuildPatch_PythonSimple_AppliesClean is the Python round-trip:
// synthesize the helper for the simple Python fixture, build the
// diff against a temp git repo, and confirm `git apply` accepts it
// and produces a file containing the new `def extracted_…` helper.
func TestBuildPatch_PythonSimple_AppliesClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	a, b := loadSnippets(t, "../../testdata/refactor/python/simple")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("synthesis rejected: %q", s.Note)
	}

	tmp := t.TempDir()
	srcA, err := os.ReadFile("../../testdata/refactor/python/simple/a.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dstA := filepath.Join(tmp, "a.py")
	if err := os.WriteFile(dstA, srcA, 0o644); err != nil {
		t.Fatalf("write a.py: %v", err)
	}
	gitInit(t, tmp)

	diff := buildAppendPatch("a.py", string(srcA), s.HelperSrc)
	patchFile := filepath.Join(tmp, "p.diff")
	if err := os.WriteFile(patchFile, []byte(diff), 0o644); err != nil {
		t.Fatalf("write patch: %v", err)
	}

	cmd := exec.Command("git", "apply", "--check", patchFile)
	cmd.Dir = tmp
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git apply --check failed: %v\noutput:\n%s\ndiff:\n%s",
			err, out, diff)
	}
	cmd = exec.Command("git", "apply", patchFile)
	cmd.Dir = tmp
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git apply failed: %v\noutput:\n%s", err, out)
	}
	patched, err := os.ReadFile(dstA)
	if err != nil {
		t.Fatalf("read patched: %v", err)
	}
	if !strings.Contains(string(patched), "def extracted_price_with_tax_a_deadbeef") {
		t.Errorf("patched file missing Python helper. Content:\n%s", patched)
	}
	if !strings.Contains(string(patched), "# Divergences (B vs A):") {
		t.Errorf("patched file missing `#`-style divergence block")
	}
}

func TestBuildPatch_EmptySuggestion_ReturnsEmpty(t *testing.T) {
	out, err := BuildPatch("/nonexistent/path", Suggestion{Note: "rejected: ..."})
	if err != nil {
		t.Fatalf("expected nil error for empty suggestion, got %v", err)
	}
	if out != "" {
		t.Errorf("expected empty diff, got %q", out)
	}
}

func TestBuildAppendPatch_HasContextAndAddedLines(t *testing.T) {
	file := "package x\n\nfunc Foo() {}\n"
	helper := "func Helper() {}\n"
	out := buildAppendPatch("x.go", file, helper)
	if !strings.HasPrefix(out, "--- a/x.go\n+++ b/x.go\n") {
		t.Errorf("missing diff header: %q", out)
	}
	if !strings.Contains(out, "@@ ") {
		t.Errorf("missing hunk header: %q", out)
	}
	if !bytes.Contains([]byte(out), []byte(" func Foo() {}")) {
		t.Errorf("missing context line. Diff:\n%s", out)
	}
	if !bytes.Contains([]byte(out), []byte("+func Helper() {}")) {
		t.Errorf("missing added line. Diff:\n%s", out)
	}
}

// TestBuildAppendPatch_AppliesAgainstFileWithTrailingBlank guards the
// regression we hit during self-host: a file ending in `}\n\n` (Go
// test files do this) needs the trailing blank line preserved in
// the hunk's pre-image so git apply's view stays in sync with ours.
func TestBuildAppendPatch_AppliesAgainstFileWithTrailingBlank(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	file := "package x\n\nfunc Foo() {}\n"
	// Simulate a file with a trailing blank line.
	fileWithBlank := file + "\n"
	helper := "func Helper() {}\n"

	tmp := t.TempDir()
	dst := filepath.Join(tmp, "x.go")
	if err := os.WriteFile(dst, []byte(fileWithBlank), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitInit(t, tmp)

	diff := buildAppendPatch("x.go", fileWithBlank, helper)
	patchFile := filepath.Join(tmp, "p.diff")
	if err := os.WriteFile(patchFile, []byte(diff), 0o644); err != nil {
		t.Fatalf("write patch: %v", err)
	}
	cmd := exec.Command("git", "apply", "--check", patchFile)
	cmd.Dir = tmp
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git apply --check failed on trailing-blank file: %v\n%s\ndiff:\n%s",
			err, out, diff)
	}
}

// gitInit makes the temp dir a minimal git repo with one commit so
// `git apply --check` has something to compare against.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	cfgFlags := []string{
		"-c", "user.email=test@codetwin",
		"-c", "user.name=test",
		"-c", "commit.gpgsign=false",
		"-c", "tag.gpgsign=false",
		"-c", "gpg.format=openpgp",
	}
	for _, args := range [][]string{
		append([]string{"init", "--quiet"}),
		append(append([]string{}, cfgFlags...), "add", "."),
		append(append([]string{}, cfgFlags...), "commit", "--quiet", "-m", "init"),
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\noutput: %s", args, err, out)
		}
	}
}
