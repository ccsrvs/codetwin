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

// TestBuildPatch_GoMethodRealworld_AppliesClean confirms the
// realworld-method Go fixture round-trips through `git apply`. The
// receiver-stripping path emits a helper that's a free function (no
// `(r *Repo)` prefix), and we assert the patched file contains that
// free-function helper rather than the original method header.
func TestBuildPatch_GoMethodRealworld_AppliesClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	a, b := loadSnippets(t, "../../testdata/refactor/go/realworld-method")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("synthesis rejected: %q", s.Note)
	}

	tmp := t.TempDir()
	srcA, err := os.ReadFile("../../testdata/refactor/go/realworld-method/a.go")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dstA := filepath.Join(tmp, "a.go")
	if err := os.WriteFile(dstA, srcA, 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	gitInit(t, tmp)

	diff := buildAppendPatch("a.go", string(srcA), s.HelperSrc)
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
	got := string(patched)
	if !strings.Contains(got, "func extracted_FindUserByID_deadbeef(ctx context.Context, id int) (*User, error) {") {
		t.Errorf("patched file missing free-function helper. Content:\n%s", got)
	}
	// The helper's def line must NOT include the receiver.
	if strings.Contains(got, "func (r *Repo) extracted_FindUserByID_") {
		t.Errorf("helper retained the receiver, contradicting v1 contract. Content:\n%s", got)
	}
}

// TestBuildPatch_PythonDecoratedRealworld_AppliesClean is the
// real-world counterpart of TestBuildPatch_PythonSimple_AppliesClean:
// it runs through a fixture that has decorators on the source side,
// confirming the appended helper round-trips through `git apply` even
// when the original chunk includes lines (the decorators) that the
// helper deliberately omits.
func TestBuildPatch_PythonDecoratedRealworld_AppliesClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	a, b := loadSnippets(t, "../../testdata/refactor/python/realworld-decorated")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("synthesis rejected: %q", s.Note)
	}

	tmp := t.TempDir()
	srcA, err := os.ReadFile("../../testdata/refactor/python/realworld-decorated/a.py")
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
	got := string(patched)
	if !strings.Contains(got, "def extracted_load_user_profile_deadbeef") {
		t.Errorf("patched file missing helper. Content:\n%s", got)
	}
	// Confirm no decorator was carried forward onto the helper. If a
	// decorator immediately precedes the helper's def line, our drop
	// contract is broken.
	if strings.Contains(got, "@cached\ndef extracted_") ||
		strings.Contains(got, "@retry(attempts=3)\ndef extracted_") {
		t.Errorf("helper appears to have inherited a decorator. Content:\n%s", got)
	}
}

// TestBuildPatch_JavaSimple_AppliesClean is the Java round-trip for
// the file-scope append CORE (buildAppendPatch — the fallback used
// when no enclosing class is found): synthesize the helper for the
// simple Java fixture, build the diff against a temp git repo, and
// confirm `git apply` accepts it. In-class placement is covered by
// placement_test.go.
func TestBuildPatch_JavaSimple_AppliesClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	a, b := loadSnippets(t, "../../testdata/refactor/java/simple")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("synthesis rejected: %q", s.Note)
	}

	tmp := t.TempDir()
	srcA, err := os.ReadFile("../../testdata/refactor/java/simple/A.java")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dstA := filepath.Join(tmp, "A.java")
	if err := os.WriteFile(dstA, srcA, 0o644); err != nil {
		t.Fatalf("write A.java: %v", err)
	}
	gitInit(t, tmp)

	diff := buildAppendPatch("A.java", string(srcA), s.HelperSrc)
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
	got := string(patched)
	if !strings.Contains(got, "public double extracted_priceWithTaxA_deadbeef(double amount) {") {
		t.Errorf("patched file missing Java helper signature. Content:\n%s", got)
	}
	if !strings.Contains(got, "// Divergences (B vs A):") {
		t.Errorf("patched file missing `//`-style divergence block")
	}
}

// TestBuildPatch_JavaScriptSimple_AppliesClean is the JS round-trip:
// synthesize the helper for the simple JS fixture, build the diff
// against a temp git repo, and confirm `git apply` accepts it.
func TestBuildPatch_JavaScriptSimple_AppliesClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	a, b := loadSnippets(t, "../../testdata/refactor/js/simple")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("synthesis rejected: %q", s.Note)
	}

	tmp := t.TempDir()
	srcA, err := os.ReadFile("../../testdata/refactor/js/simple/a.js")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dstA := filepath.Join(tmp, "a.js")
	if err := os.WriteFile(dstA, srcA, 0o644); err != nil {
		t.Fatalf("write a.js: %v", err)
	}
	gitInit(t, tmp)

	diff := buildAppendPatch("a.js", string(srcA), s.HelperSrc)
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
	got := string(patched)
	if !strings.Contains(got, "function extracted_priceWithTaxA_deadbeef(amount) {") {
		t.Errorf("patched file missing JS helper signature. Content:\n%s", got)
	}
	if !strings.Contains(got, "// Divergences (B vs A):") {
		t.Errorf("patched file missing `//`-style divergence block")
	}
}

// TestBuildPatch_RustSimple_AppliesClean is the Rust round-trip:
// synthesize the helper for the simple Rust fixture, build the diff
// against a temp git repo, and confirm `git apply` accepts it.
func TestBuildPatch_RustSimple_AppliesClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	a, b := loadSnippets(t, "../../testdata/refactor/rust/simple")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("synthesis rejected: %q", s.Note)
	}

	tmp := t.TempDir()
	srcA, err := os.ReadFile("../../testdata/refactor/rust/simple/a.rs")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dstA := filepath.Join(tmp, "a.rs")
	if err := os.WriteFile(dstA, srcA, 0o644); err != nil {
		t.Fatalf("write a.rs: %v", err)
	}
	gitInit(t, tmp)

	diff := buildAppendPatch("a.rs", string(srcA), s.HelperSrc)
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
	got := string(patched)
	if !strings.Contains(got, "fn extracted_price_with_tax_a_deadbeef(amount: f64) -> f64 {") {
		t.Errorf("patched file missing Rust helper signature. Content:\n%s", got)
	}
	if !strings.Contains(got, "// Divergences (B vs A):") {
		t.Errorf("patched file missing `//`-style divergence block")
	}
}

// TestBuildPatch_ElixirSimple_AppliesClean is the Elixir round-trip
// for the file-scope append CORE (buildAppendPatch — the fallback used
// when no enclosing defmodule is found): synthesize the helper for the
// simple Elixir fixture, build the diff against a temp git repo, and
// confirm `git apply` accepts it. In-module placement is covered by
// placement_test.go.
func TestBuildPatch_ElixirSimple_AppliesClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	a, b := loadSnippets(t, "../../testdata/refactor/elixir/simple")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("synthesis rejected: %q", s.Note)
	}

	tmp := t.TempDir()
	srcA, err := os.ReadFile("../../testdata/refactor/elixir/simple/a.ex")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dstA := filepath.Join(tmp, "a.ex")
	if err := os.WriteFile(dstA, srcA, 0o644); err != nil {
		t.Fatalf("write a.ex: %v", err)
	}
	gitInit(t, tmp)

	diff := buildAppendPatch("a.ex", string(srcA), s.HelperSrc)
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
	got := string(patched)
	if !strings.Contains(got, "def extracted_price_with_tax_deadbeef(amount) do") {
		t.Errorf("patched file missing Elixir helper signature. Content:\n%s", got)
	}
	if !strings.Contains(got, "# Divergences (B vs A):") {
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

// TestBuildPatch_UnreadablePath_ReturnsError covers the read-error
// branch: a non-empty Suggestion targeting a path that doesn't exist
// should propagate the os.ReadFile error.
func TestBuildPatch_UnreadablePath_ReturnsError(t *testing.T) {
	_, err := BuildPatch("/nonexistent/codetwin/should/not/exist.go",
		Suggestion{HelperSrc: "func helper() {}\n"})
	if err == nil {
		t.Fatal("expected error reading nonexistent path")
	}
	if !strings.Contains(err.Error(), "read ") {
		t.Errorf("error %q lacks the `read ` prefix from BuildPatch", err)
	}
}

// TestBuildPatch_HappyPath covers the readable-path + non-empty
// suggestion branch (line 30): write a real file, call BuildPatch,
// confirm the diff includes the helper.
func TestBuildPatch_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.go")
	if err := os.WriteFile(path, []byte("package x\n\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := BuildPatch(path, Suggestion{HelperSrc: "func Helper() {}\n"})
	if err != nil {
		t.Fatalf("BuildPatch: %v", err)
	}
	if !strings.Contains(out, "+func Helper() {}") {
		t.Errorf("diff missing helper line:\n%s", out)
	}
}

// TestBuildAppendPatch_ShortFile_ContextClampsToZero covers the
// `ctxStart < 0` branch (lines 59-61): a file with fewer than 3 lines
// has the context window clamp at 0 instead of going negative.
func TestBuildAppendPatch_ShortFile_ContextClampsToZero(t *testing.T) {
	out := buildAppendPatch("x.go", "one line\n", "func Helper() {}\n")
	if !strings.HasPrefix(out, "--- a/x.go\n+++ b/x.go\n@@ -1,") {
		t.Errorf("expected hunk anchored at line 1 for short file, got:\n%s", out)
	}
	if !strings.Contains(out, " one line\n") {
		t.Errorf("short-file context line missing:\n%s", out)
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
		{"init", "--quiet"},
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
