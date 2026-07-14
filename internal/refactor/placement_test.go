package refactor

// Placement tests for the auto-insertion feature: Java and Elixir
// helpers must land INSIDE the innermost enclosing container (class /
// defmodule) of snippet A's chunk — indented like a sibling member,
// inserted immediately before the container's closing `}`/`end` — so
// the patched file compiles as emitted. The file-scope placement NOTE
// (`NOTE: appended at file scope…`) becomes obsolete for that path and
// must not appear.
//
// Every test round-trips through real `git apply` (not just --check)
// in a tempdir copy of the fixture, then asserts on the patched file's
// content and syntactic plausibility (brace balance for Java, do/end
// balance for Elixir). Compile checks run when the toolchain (javac /
// elixirc) is on PATH and skip gracefully otherwise.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// copyFixture copies every file of a fixture dir into a fresh temp dir
// so patches can be applied without touching testdata.
func copyFixture(t *testing.T, srcDir string) string {
	t.Helper()
	dst := t.TempDir()
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read fixture dir %s: %v", srcDir, err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			t.Fatalf("read fixture file %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", e.Name(), err)
		}
	}
	return dst
}

// applyAbsPatch runs `git apply --check` and then `git apply` inside
// dir. Diffs from BuildPatch carry dir's absolute path (rendered as
// `a/tmp/…/A.java`), so -p strips every leading component down to the
// bare filename, which resolves relative to dir.
func applyAbsPatch(t *testing.T, dir, diff string) {
	t.Helper()
	strip := len(strings.Split(strings.Trim(filepath.ToSlash(dir), "/"), "/")) + 1
	patchFile := filepath.Join(dir, "p.diff")
	if err := os.WriteFile(patchFile, []byte(diff), 0o644); err != nil {
		t.Fatalf("write patch: %v", err)
	}
	for _, args := range [][]string{
		{"apply", fmt.Sprintf("-p%d", strip), "--check", patchFile},
		{"apply", fmt.Sprintf("-p%d", strip), patchFile},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\noutput:\n%s\ndiff:\n%s", args, err, out, diff)
		}
	}
}

// placeHelper runs the full pipeline (load → align → synthesize →
// BuildPatch → git apply) on a tempdir copy of fixtureDir and returns
// the tempdir plus the patched content of fileA.
func placeHelper(t *testing.T, fixtureDir, fileA string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	dir := copyFixture(t, fixtureDir)
	a, b := loadSnippets(t, dir)
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("synthesis rejected: %q", s.Note)
	}
	gitInit(t, dir)
	diff, err := BuildPatch(a.Path, s)
	if err != nil {
		t.Fatalf("BuildPatch: %v", err)
	}
	applyAbsPatch(t, dir, diff)
	patched, err := os.ReadFile(filepath.Join(dir, fileA))
	if err != nil {
		t.Fatalf("read patched %s: %v", fileA, err)
	}
	return dir, string(patched)
}

// elixirDoEndCounts returns the number of do-block openers (lines
// ending in the bare `do` keyword) and closers (lines that are exactly
// `end`). Simple line counting — enough to catch a helper landing
// outside its module.
func elixirDoEndCounts(content string) (opens, ends int) {
	for _, l := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == "end" {
			ends++
		}
		if trimmed == "do" || strings.HasSuffix(strings.TrimRight(l, " \t"), " do") {
			opens++
		}
	}
	return opens, ends
}

// Given the simple Java fixture, when BuildPatch emits the suggestion
// diff and it is applied, then the helper sits INSIDE class A —
// indented one level, after the last existing member, before the
// class's closing `}` — with no file-scope placement NOTE and balanced
// braces.
func TestBuildPatch_JavaSimple_InsertsHelperInsideClass(t *testing.T) {
	_, patched := placeHelper(t, "../../testdata/refactor/java/simple", "A.java")

	if strings.Contains(patched, "NOTE: appended at file scope") {
		t.Errorf("patched file still carries the file-scope placement NOTE:\n%s", patched)
	}
	helperIdx := strings.Index(patched,
		"\n    public double extracted_priceWithTaxA_deadbeef(double amount) {")
	if helperIdx < 0 {
		t.Fatalf("helper not found indented inside the class. Content:\n%s", patched)
	}
	memberIdx := strings.Index(patched, "priceWithTaxA(double amount) {")
	if memberIdx < 0 || helperIdx < memberIdx {
		t.Errorf("helper must land after the last existing member (helper@%d, member@%d)",
			helperIdx, memberIdx)
	}
	if !strings.HasSuffix(patched, "    }\n}\n") {
		t.Errorf("helper must sit immediately before the class's closing brace; tail:\n%q",
			patched[max(0, len(patched)-40):])
	}
	if o, c := strings.Count(patched, "{"), strings.Count(patched, "}"); o != c {
		t.Errorf("brace imbalance after patch: %d '{' vs %d '}'\n%s", o, c, patched)
	}
}

// Given the realworld-nested Java fixture (method inside a static
// nested class), when the suggestion diff is applied, then the helper
// lands inside the INNERMOST enclosing type (Inner, not A) at the
// nested member indent.
func TestBuildPatch_JavaNested_InsertsHelperInsideInnerClass(t *testing.T) {
	_, patched := placeHelper(t, "../../testdata/refactor/java/realworld-nested", "A.java")

	if strings.Contains(patched, "NOTE: appended at file scope") {
		t.Errorf("patched file still carries the file-scope placement NOTE:\n%s", patched)
	}
	if !strings.Contains(patched,
		"\n        public double extracted_priceWithTaxA_deadbeef(double amount) {") {
		t.Fatalf("helper not found at nested-class member indent. Content:\n%s", patched)
	}
	// Helper body closes at the nested indent, then Inner closes, then A.
	if !strings.HasSuffix(patched, "        }\n    }\n}\n") {
		t.Errorf("helper must sit immediately before Inner's closing brace; tail:\n%q",
			patched[max(0, len(patched)-60):])
	}
	if o, c := strings.Count(patched, "{"), strings.Count(patched, "}"); o != c {
		t.Errorf("brace imbalance after patch: %d '{' vs %d '}'\n%s", o, c, patched)
	}
}

// Given the patched Java fixtures, when javac is available, then both
// compile as emitted — the actual point of auto-insertion. Skips when
// javac is not on PATH so toolchain-free CI stays green.
func TestBuildPatch_JavaPatchedFixtures_Compile(t *testing.T) {
	if _, err := exec.LookPath("javac"); err != nil {
		t.Skip("javac not on PATH, skipping compile check")
	}
	for _, fixture := range []string{
		"../../testdata/refactor/java/simple",
		"../../testdata/refactor/java/realworld-nested",
	} {
		fixture := fixture
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			dir, _ := placeHelper(t, fixture, "A.java")
			cmd := exec.Command("javac", "A.java")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				patched, _ := os.ReadFile(filepath.Join(dir, "A.java"))
				t.Errorf("javac failed on the patched file: %v\noutput:\n%s\nfile:\n%s",
					err, out, patched)
			}
		})
	}
}

// Given the simple Elixir fixture, when the suggestion diff is
// applied, then the helper def sits INSIDE the defmodule — indented
// like a sibling def, before the module's closing `end` — with no
// file-scope placement NOTE and balanced do/end blocks.
func TestBuildPatch_ElixirSimple_InsertsHelperInsideModule(t *testing.T) {
	_, patched := placeHelper(t, "../../testdata/refactor/elixir/simple", "a.ex")

	if strings.Contains(patched, "NOTE: appended at file scope") {
		t.Errorf("patched file still carries the file-scope placement NOTE:\n%s", patched)
	}
	helperIdx := strings.Index(patched,
		"\n  def extracted_price_with_tax_deadbeef(amount) do")
	if helperIdx < 0 {
		t.Fatalf("helper not found indented inside the defmodule. Content:\n%s", patched)
	}
	memberIdx := strings.Index(patched, "def price_with_tax(amount) do")
	if memberIdx < 0 || helperIdx < memberIdx {
		t.Errorf("helper must land after the last existing def (helper@%d, member@%d)",
			helperIdx, memberIdx)
	}
	if !strings.HasSuffix(patched, "  end\nend\n") {
		t.Errorf("helper must sit immediately before the module's closing end; tail:\n%q",
			patched[max(0, len(patched)-40):])
	}
	if o, c := elixirDoEndCounts(patched); o != c {
		t.Errorf("do/end imbalance after patch: %d openers vs %d ends\n%s", o, c, patched)
	}
}

// Given the realworld-nested Elixir fixture (def inside a nested
// defmodule), when the suggestion diff is applied, then the helper
// lands inside the INNERMOST defmodule (TaxA, not Billing).
func TestBuildPatch_ElixirNested_InsertsHelperInsideInnerModule(t *testing.T) {
	_, patched := placeHelper(t, "../../testdata/refactor/elixir/realworld-nested", "a.ex")

	if strings.Contains(patched, "NOTE: appended at file scope") {
		t.Errorf("patched file still carries the file-scope placement NOTE:\n%s", patched)
	}
	if !strings.Contains(patched,
		"\n    def extracted_price_with_tax_deadbeef(amount) do") {
		t.Fatalf("helper not found at nested-module def indent. Content:\n%s", patched)
	}
	// Helper closes at the nested indent, then TaxA closes, then Billing.
	if !strings.HasSuffix(patched, "    end\n  end\nend\n") {
		t.Errorf("helper must sit immediately before TaxA's closing end; tail:\n%q",
			patched[max(0, len(patched)-60):])
	}
	if o, c := elixirDoEndCounts(patched); o != c {
		t.Errorf("do/end imbalance after patch: %d openers vs %d ends\n%s", o, c, patched)
	}
}

// Given the patched Elixir fixtures, when elixirc is available, then
// both compile as emitted. Skips when elixirc is not on PATH (the
// common CI case) so toolchain-free environments stay green.
func TestBuildPatch_ElixirPatchedFixtures_Compile(t *testing.T) {
	if _, err := exec.LookPath("elixirc"); err != nil {
		t.Skip("elixirc not on PATH, skipping compile check")
	}
	for _, fixture := range []string{
		"../../testdata/refactor/elixir/simple",
		"../../testdata/refactor/elixir/realworld-nested",
	} {
		fixture := fixture
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			dir, _ := placeHelper(t, fixture, "a.ex")
			cmd := exec.Command("elixirc", "a.ex")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				patched, _ := os.ReadFile(filepath.Join(dir, "a.ex"))
				t.Errorf("elixirc failed on the patched file: %v\noutput:\n%s\nfile:\n%s",
					err, out, patched)
			}
		})
	}
}
