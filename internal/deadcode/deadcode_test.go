package deadcode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/scan"
)

// scanDir runs the real scan pipeline over a temp directory so tests
// exercise the same splitter/tokenizer path production uses.
func scanDir(t *testing.T, files map[string]string) []scan.Snippet {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for name, code := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	c := cache.New()
	snippets, warnings := scan.ProcessFiles(paths, 1, nil, c, "", scan.GranularityFunction, nil)
	if len(warnings) > 0 {
		t.Fatalf("scan warnings: %v", warnings)
	}
	return snippets
}

func findingsBySymbol(fs []Finding) map[string]Finding {
	m := map[string]Finding{}
	for _, f := range fs {
		m[f.Symbol] = f
	}
	return m
}

func TestGoVerdicts(t *testing.T) {
	snippets := scanDir(t, map[string]string{
		"a.go": `package a

func deadPrivate() int {
	return 1
}

func DeadExported() int {
	return 2
}

func liveHelper() int {
	return 3
}

func Caller() int {
	return liveHelper()
}

func testOnlyHelper() int {
	return 4
}
`,
		"a_test.go": `package a

import "testing"

func TestCaller(t *testing.T) {
	if testOnlyHelper() == 0 {
		t.Fail()
	}
	_ = Caller()
}
`,
	})
	findings, warns := Analyze(snippets)
	if len(warns) > 0 {
		t.Fatalf("warnings: %v", warns)
	}
	got := findingsBySymbol(findings)

	if f, ok := got["deadPrivate"]; !ok || f.Verdict != VerdictDead || f.Exported {
		t.Errorf("deadPrivate: want dead/unexported finding, got %+v (present=%v)", f, ok)
	}
	if f, ok := got["DeadExported"]; !ok || f.Verdict != VerdictUnusedInScan || !f.Exported {
		t.Errorf("DeadExported: want unused-in-scan/exported, got %+v (present=%v)", f, ok)
	}
	if f, ok := got["testOnlyHelper"]; !ok || f.Verdict != VerdictTestOnly || f.TestRefs == 0 {
		t.Errorf("testOnlyHelper: want test-only with test refs, got %+v (present=%v)", f, ok)
	}
	// Caller is itself only referenced from the test file, so it reports
	// test-only even though it is exported — test-only outranks the
	// advisory exported tier.
	if f, ok := got["Caller"]; !ok || f.Verdict != VerdictTestOnly {
		t.Errorf("Caller: want test-only, got %+v (present=%v)", f, ok)
	}
	for _, alive := range []string{"liveHelper", "TestCaller"} {
		if _, ok := got[alive]; ok {
			t.Errorf("%s should not be reported (referenced or test entry point)", alive)
		}
	}
}

func TestSelfReferenceDoesNotKeepAlive(t *testing.T) {
	// Recursion inside the definition span must not count as a reference.
	snippets := scanDir(t, map[string]string{
		"r.go": `package r

func lonelyRecursive(n int) int {
	if n == 0 {
		return 0
	}
	return lonelyRecursive(n - 1)
}

func anchor() int {
	return 1
}

func useAnchor() int {
	return anchor()
}
`,
	})
	findings, _ := Analyze(snippets)
	got := findingsBySymbol(findings)
	if f, ok := got["lonelyRecursive"]; !ok || f.Verdict != VerdictDead {
		t.Errorf("recursive-only function should be dead, got %+v (present=%v)", f, ok)
	}
}

func TestStringLiteralReferenceKeepsAlive(t *testing.T) {
	snippets := scanDir(t, map[string]string{
		"s.py": `import importlib


def dynamic_target():
    return 1


def dispatcher():
    return globals()["dynamic_target"]()
`,
	})
	findings, _ := Analyze(snippets)
	if _, ok := findingsBySymbol(findings)["dynamic_target"]; ok {
		t.Errorf("string-literal mention should keep dynamic_target alive")
	}
}

func TestCommentMentionDoesNotKeepAlive(t *testing.T) {
	snippets := scanDir(t, map[string]string{
		"c.go": `package c

// commentGhost is only ever mentioned right here, in prose: commentGhost.
func commentGhost() int {
	return 1
}

func used() int {
	return 2
}

func user() int {
	return used()
}
`,
	})
	findings, _ := Analyze(snippets)
	if f, ok := findingsBySymbol(findings)["commentGhost"]; !ok || f.Verdict != VerdictDead {
		t.Errorf("comment mentions must not keep commentGhost alive, got %+v (present=%v)", f, ok)
	}
}

func TestSuppressedNamesNeverReported(t *testing.T) {
	snippets := scanDir(t, map[string]string{
		"m.go": `package main

func main() {
}

func init() {
}

type T struct{ v int }

func (t T) String() string {
	return "t"
}
`,
		"p.py": `class C:
    def __init__(self):
        self.x = 1

    def __repr__(self):
        return "C"
`,
		"e.ex": `defmodule Server do
  def handle_call(msg, _from, state), do: {:reply, msg, state}
  defp truly_dead(x), do: x
end
`,
	})
	findings, _ := Analyze(snippets)
	got := findingsBySymbol(findings)
	for _, sym := range []string{"main", "init", "String", "__init__", "__repr__", "handle_call"} {
		if _, ok := got[sym]; ok {
			t.Errorf("suppressed name %s must never be a finding", sym)
		}
	}
	if f, ok := got["truly_dead"]; !ok || f.Verdict != VerdictDead || f.Exported {
		t.Errorf("Elixir defp truly_dead: want dead/private, got %+v (present=%v)", f, ok)
	}
}

func TestCrossFileAndCrossLanguageReferences(t *testing.T) {
	// A Go symbol referenced from another Go file is alive; sharing a
	// name with a JS function keeps both alive (conservative same-name
	// aggregation).
	snippets := scanDir(t, map[string]string{
		"lib.go":   "package lib\n\nfunc sharedName() int {\n\treturn 1\n}\n",
		"use.go":   "package lib\n\nfunc use() int {\n\treturn sharedName()\n}\n",
		"other.js": "function sharedName() { return 2 }\n",
	})
	findings, _ := Analyze(snippets)
	got := findingsBySymbol(findings)
	if _, ok := got["sharedName"]; ok {
		t.Errorf("sharedName referenced in use.go must keep every same-named definition alive")
	}
	if f, ok := got["use"]; !ok || f.Verdict != VerdictDead {
		t.Errorf("use() itself is unreferenced and should be dead, got %+v (present=%v)", f, ok)
	}
}

func TestUnreferencedTestHelperIsDead(t *testing.T) {
	snippets := scanDir(t, map[string]string{
		"h_test.go": `package h

import "testing"

func orphanHelper() int {
	return 1
}

func TestSomething(t *testing.T) {
	_ = 1
}
`,
	})
	findings, _ := Analyze(snippets)
	got := findingsBySymbol(findings)
	if _, ok := got["orphanHelper"]; !ok {
		t.Errorf("a test helper no test references should be reported")
	}
	if _, ok := got["TestSomething"]; ok {
		t.Errorf("test entry points must be suppressed")
	}
}

func TestExportDetectionPerLanguage(t *testing.T) {
	snippets := scanDir(t, map[string]string{
		"v.rs": "pub fn pub_dead() {}\nfn priv_dead() {}\nfn caller() { }\npub fn user() { caller(); }\n",
		"w.js": "export function exp_dead() { return 1 }\nfunction priv_dead_js() { return 2 }\n",
	})
	findings, _ := Analyze(snippets)
	got := findingsBySymbol(findings)
	if f, ok := got["pub_dead"]; !ok || f.Verdict != VerdictUnusedInScan {
		t.Errorf("rust pub fn: want unused-in-scan, got %+v (present=%v)", f, ok)
	}
	if f, ok := got["priv_dead"]; !ok || f.Verdict != VerdictDead {
		t.Errorf("rust private fn: want dead, got %+v (present=%v)", f, ok)
	}
	if f, ok := got["exp_dead"]; !ok || f.Verdict != VerdictUnusedInScan {
		t.Errorf("js export function: want unused-in-scan, got %+v (present=%v)", f, ok)
	}
	if f, ok := got["priv_dead_js"]; !ok || f.Verdict != VerdictDead {
		t.Errorf("js module-private fn: want dead, got %+v (present=%v)", f, ok)
	}
}
