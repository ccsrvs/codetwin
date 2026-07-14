package refactor

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// ── SliceBlock ────────────────────────────────────────────────────────

func TestSliceBlock_SlicesAbsoluteLineRange(t *testing.T) {
	s := scan.Snippet{
		Name:      "f.go:10-15 host",
		Path:      "/abs/f.go",
		Lang:      tokenizer.Go,
		Code:      "func host() {\n\ta := 1\n\tb := 2\n\tuse(a, b)\n\treturn\n}\n",
		StartLine: 10,
		EndLine:   15,
	}
	got := SliceBlock(s, 11, 13, "f.go:11-13")
	if got.Code != "\ta := 1\n\tb := 2\n\tuse(a, b)\n" {
		t.Errorf("sliced code = %q", got.Code)
	}
	if got.StartLine != 11 || got.EndLine != 13 {
		t.Errorf("slice lines = %d-%d, want 11-13", got.StartLine, got.EndLine)
	}
	if got.Name != "f.go:11-13" || got.Path != "/abs/f.go" || got.Lang != tokenizer.Go {
		t.Errorf("metadata not carried: %+v", got)
	}
}

func TestSliceBlock_ClampsOutOfRange(t *testing.T) {
	s := scan.Snippet{Code: "a\nb\nc\n", StartLine: 5, EndLine: 7, Lang: tokenizer.Go}
	got := SliceBlock(s, 1, 99, "x")
	if got.Code != "a\nb\nc\n" || got.StartLine != 5 || got.EndLine != 7 {
		t.Errorf("clamped slice = %+v", got)
	}
}

// ── Free-identifier heuristic ─────────────────────────────────────────

func TestFreeIdentifiers_Go(t *testing.T) {
	code := "\tif req == nil {\n" +
		"\t\treturn errNilRequest\n" +
		"\t}\n" +
		"\tseen := make(map[string]bool, len(req.Items))\n" +
		"\tfor _, item := range req.Items {\n" +
		"\t\tif item.SKU == \"\" || item.Quantity <= 0 {\n" +
		"\t\t\treturn errBadItem\n" +
		"\t\t}\n" +
		"\t\tseen[item.SKU] = true\n" +
		"\t}\n"
	got := freeIdentifiers(code, tokenizer.Go)
	// req is used but never bound; the two error sentinels are free;
	// seen/item are bound by := and excluded; Items/SKU/Quantity are
	// selectors; make/map/string/bool/len/range/true are builtins.
	want := []string{"req", "errNilRequest", "errBadItem"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("freeIdentifiers = %v, want %v", got, want)
	}
}

func TestFreeIdentifiers_GoSkipsStringsAndComments(t *testing.T) {
	code := "\t// hidden comment mentions ghostVar\n" +
		"\tmsg := \"ghostString stays out\"\n" +
		"\temit(msg, realFree)\n"
	got := freeIdentifiers(code, tokenizer.Go)
	want := []string{"emit", "realFree"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("freeIdentifiers = %v, want %v", got, want)
	}
}

func TestFreeIdentifiers_Python(t *testing.T) {
	code := "    cleaned = []\n" +
		"    for rec in records:\n" +
		"        key = rec.get(\"id\")\n" +
		"        if key is None:\n" +
		"            continue\n" +
		"        label = str(rec.get(\"name\", \"\")).strip()\n" +
		"        cleaned.append((key, label))\n" +
		"    total = wide + narrow\n"
	got := freeIdentifiers(code, tokenizer.Python)
	// records is iterated but never bound; wide/narrow used unbound.
	// cleaned/rec/key/label/total are bound; str is a builtin;
	// get/strip/append are selectors.
	want := []string{"records", "wide", "narrow"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("freeIdentifiers = %v, want %v", got, want)
	}
}

func TestFreeIdentifiers_PythonSubscriptTargetIsUse(t *testing.T) {
	// counts[k] = v binds nothing: counts must already exist.
	code := "    counts[k] = v\n"
	got := freeIdentifiers(code, tokenizer.Python)
	want := []string{"counts", "k", "v"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("freeIdentifiers = %v, want %v", got, want)
	}
}

// ── Dedent / reindent ─────────────────────────────────────────────────

func TestReindentBlock_DedentsCommonPrefixAndReindents(t *testing.T) {
	code := "\t\tif x {\n\t\t\ty()\n\t\t}\n"
	got := reindentBlock(code, "\t")
	want := "\tif x {\n\t\ty()\n\t}\n"
	if got != want {
		t.Errorf("reindentBlock = %q, want %q", got, want)
	}
}

func TestReindentBlock_PythonSpacesAndBlankLines(t *testing.T) {
	code := "        a = 1\n\n        if a:\n            b()\n"
	got := reindentBlock(code, "    ")
	want := "    a = 1\n\n    if a:\n        b()\n"
	if got != want {
		t.Errorf("reindentBlock = %q, want %q", got, want)
	}
}

func TestCommonIndentPrefix_MixedDepthsStopAtShallowest(t *testing.T) {
	lines := []string{"\tif x {", "\t\ty()", "\t}"}
	if got := commonIndentPrefix(lines); got != "\t" {
		t.Errorf("commonIndentPrefix = %q, want tab", got)
	}
}

// ── SynthesizeBlock ───────────────────────────────────────────────────

func blockSnippet(name string, lang tokenizer.Language, code string, startLine int) scan.Snippet {
	return scan.Snippet{
		Name: name, Path: "/tmp/" + name, Lang: lang, Code: code,
		StartLine: startLine,
		EndLine:   startLine + strings.Count(strings.TrimRight(code, "\n"), "\n"),
	}
}

func TestSynthesizeBlock_GoEmitsWrappedHelper(t *testing.T) {
	code := "\tseen := make(map[string]bool, len(req.Items))\n" +
		"\tfor _, item := range req.Items {\n" +
		"\t\tseen[item.SKU] = true\n" +
		"\t}\n"
	a := blockSnippet("a.go:13-16", tokenizer.Go, code, 13)
	b := blockSnippet("b.go:13-16", tokenizer.Go, code, 13)
	al := Align(a, b)
	s := SynthesizeBlock(a, b, "cafe1234", al)
	if s.Note != "" {
		t.Fatalf("unexpected rejection: %q", s.Note)
	}
	if s.HelperName != "extractedBlock_cafe1234" {
		t.Errorf("HelperName = %q", s.HelperName)
	}
	for _, want := range []string{
		"func extractedBlock_cafe1234() {",
		"// TODO(codetwin): parameters not inferred — free identifiers used",
		"// in the block: req",
		"\tseen := make(map[string]bool, len(req.Items))",
		"a.go:13-16 + b.go:13-16 (block cafe1234)",
	} {
		if !strings.Contains(s.HelperSrc, want) {
			t.Errorf("HelperSrc missing %q:\n%s", want, s.HelperSrc)
		}
	}
	if !strings.HasSuffix(s.HelperSrc, "}\n") {
		t.Errorf("Go helper must close its brace:\n%s", s.HelperSrc)
	}
	if s.Confidence != 1.0 {
		t.Errorf("Confidence = %v, want 1.0 for identical blocks", s.Confidence)
	}
}

func TestSynthesizeBlock_PythonEmitsWrappedHelper(t *testing.T) {
	code := "        cleaned = []\n" +
		"        for rec in records:\n" +
		"            cleaned.append(rec)\n"
	a := blockSnippet("a.py:9-11", tokenizer.Python, code, 9)
	b := blockSnippet("b.py:9-11", tokenizer.Python, code, 9)
	al := Align(a, b)
	s := SynthesizeBlock(a, b, "cafe1234", al)
	if s.Note != "" {
		t.Fatalf("unexpected rejection: %q", s.Note)
	}
	if s.HelperName != "extracted_block_cafe1234" {
		t.Errorf("HelperName = %q", s.HelperName)
	}
	for _, want := range []string{
		"def extracted_block_cafe1234():",
		"# TODO(codetwin): parameters not inferred — free identifiers used",
		"# in the block: records",
		"    cleaned = []\n    for rec in records:\n        cleaned.append(rec)\n",
	} {
		if !strings.Contains(s.HelperSrc, want) {
			t.Errorf("HelperSrc missing %q:\n%s", want, s.HelperSrc)
		}
	}
}

func TestSynthesizeBlock_DivergentLineGetsDivergenceComment(t *testing.T) {
	aCode := "\tx := load()\n\tvalidate(x)\n\tstore(x, primaryDB)\n"
	bCode := "\tx := load()\n\tvalidate(x)\n\tstore(x, replicaDB)\n"
	a := blockSnippet("a.go:5-7", tokenizer.Go, aCode, 5)
	b := blockSnippet("b.go:9-11", tokenizer.Go, bCode, 9)
	al := Align(a, b)
	s := SynthesizeBlock(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("unexpected rejection: %q", s.Note)
	}
	if !strings.Contains(s.HelperSrc, "// Divergences (B vs A):") {
		t.Errorf("missing divergence comment:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, "primaryDB") || !strings.Contains(s.HelperSrc, "replicaDB") {
		t.Errorf("divergence comment should show both sides:\n%s", s.HelperSrc)
	}
}

func TestSynthesizeBlock_UnsupportedLanguageRejects(t *testing.T) {
	code := "  const a = 1;\n  use(a);\n"
	a := blockSnippet("a.js:3-4", tokenizer.JavaScript, code, 3)
	b := blockSnippet("b.js:3-4", tokenizer.JavaScript, code, 3)
	s := SynthesizeBlock(a, b, "deadbeef", Align(a, b))
	if s.Note == "" || !strings.Contains(s.Note, "not implemented for javascript") {
		t.Errorf("Note = %q, want a not-implemented rejection", s.Note)
	}
	if s.HelperSrc != "" {
		t.Errorf("HelperSrc must be empty on rejection")
	}
}

func TestSynthesizeBlock_ControlFlowAsymmetryRejects(t *testing.T) {
	aCode := "\tx := load()\n\tif x == nil {\n\t\treturn\n\t}\n\tuse(x)\n"
	bCode := "\tx := load()\n\tif x == nil {\n\t\tlogit(x)\n\t}\n\tuse(x)\n"
	a := blockSnippet("a.go:5-9", tokenizer.Go, aCode, 5)
	b := blockSnippet("b.go:5-9", tokenizer.Go, bCode, 5)
	s := SynthesizeBlock(a, b, "deadbeef", Align(a, b))
	if s.Note == "" || !strings.Contains(s.Note, "control-flow asymmetry") {
		t.Errorf("Note = %q, want control-flow asymmetry rejection", s.Note)
	}
}

// ── Insert-after patch ────────────────────────────────────────────────

func TestBuildInsertAfterPatch_MidFileHunkShape(t *testing.T) {
	file := "l1\nl2\nl3\nl4\nl5\nl6\nl7\n"
	diff := buildInsertAfterPatch("pkg/f.go", file, "helper line\n", 4)
	for _, want := range []string{
		"--- a/pkg/f.go\n",
		"+++ b/pkg/f.go\n",
		"@@ -2,6 +2,9 @@\n", // 3 pre-ctx (l2..l4) + 3 post-ctx (l5..l7); +helper +2 blanks
		" l2\n l3\n l4\n+\n+helper line\n+\n l5\n l6\n l7\n",
	} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff missing %q:\n%s", want, diff)
		}
	}
}

func TestBuildInsertAfterPatch_NoSecondBlankWhenNextLineBlank(t *testing.T) {
	file := "l1\nl2\n\nl4\n"
	diff := buildInsertAfterPatch("f.go", file, "helper\n", 2)
	if !strings.Contains(diff, " l1\n l2\n+\n+helper\n \n l4\n") {
		t.Errorf("expected the existing blank line to serve as trailing separator:\n%s", diff)
	}
}

func TestBuildInsertAfterPatch_AtEOFDegeneratesToAppend(t *testing.T) {
	file := "l1\nl2\n"
	got := buildInsertAfterPatch("f.go", file, "helper\n", 2)
	want := buildAppendPatch("f.go", file, "helper\n")
	if got != want {
		t.Errorf("EOF insert = %q, want append patch %q", got, want)
	}
}

// TestBuildInsertAfterPatch_AppliesClean round-trips a mid-file insert
// through `git apply --check` + `git apply`, mirroring the
// TestBuildPatch_*_AppliesClean convention.
func TestBuildInsertAfterPatch_AppliesClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	src := "package p\n\nfunc a() {\n\tx()\n}\n\nfunc b() {\n\ty()\n}\n"
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "a.go")
	if err := os.WriteFile(dst, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitInit(t, tmp)

	// Insert after line 5 (the closing brace of func a).
	diff := buildInsertAfterPatch("a.go", src, "func extractedBlock_feedf00d() {\n\tx()\n}\n", 5)
	patchFile := filepath.Join(tmp, "p.diff")
	if err := os.WriteFile(patchFile, []byte(diff), 0o644); err != nil {
		t.Fatalf("write patch: %v", err)
	}
	for _, args := range [][]string{{"apply", "--check", patchFile}, {"apply", patchFile}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmp
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\noutput:\n%s\ndiff:\n%s", args, err, out, diff)
		}
	}
	patched, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read patched: %v", err)
	}
	content := string(patched)
	helperIdx := strings.Index(content, "func extractedBlock_feedf00d")
	nextIdx := strings.Index(content, "func b()")
	if helperIdx < 0 || nextIdx < 0 || helperIdx > nextIdx {
		t.Errorf("helper not inserted between func a and func b:\n%s", content)
	}
}
