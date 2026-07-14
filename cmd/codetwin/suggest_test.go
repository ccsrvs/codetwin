package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// loadFixtureSnippets reads a fixture pair through the same pipeline
// scan.ProcessFile uses and returns one Snippet per file (the function
// under test, picked by symbol prefix `a` / `b`).
func loadFixtureSnippets(t *testing.T, aPath, bPath string) []scan.Snippet {
	t.Helper()
	return []scan.Snippet{
		loadFixtureSnippet(t, aPath, "a"),
		loadFixtureSnippet(t, bPath, "b"),
	}
}

func loadFixtureSnippet(t *testing.T, path, prefer string) scan.Snippet {
	t.Helper()
	code, err := readFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lang := tokenizer.Detect(path, code)
	chunks := splitter.Split(path, code, lang)
	if len(chunks) == 0 {
		t.Fatalf("no chunks from %s", path)
	}
	var ch splitter.Chunk
	picked := false
	for _, c := range chunks {
		if c.Symbol != "" && strings.Contains(strings.ToLower(c.Symbol), prefer) {
			ch = c
			picked = true
			break
		}
	}
	if !picked {
		ch = chunks[0]
		for _, c := range chunks[1:] {
			if len(c.Code) > len(ch.Code) {
				ch = c
			}
		}
	}
	tokens, lines := tokenizer.TokenizeWithLines(ch.Code, lang)
	ps := fingerprint.GeneratePositional(tokens, fingerprint.DefaultK, fingerprint.DefaultW)
	return scan.Snippet{
		Name: ch.Name(), Path: path, Lang: lang, Code: ch.Code,
		StartLine: ch.StartLine, EndLine: ch.EndLine,
		Tokens: tokens, Lines: lines, Fps: ps,
	}
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// captureStdout temporarily redirects os.Stdout into buf, runs fn, and
// restores stdout. Used to assert on emitSuggestion's printed diff
// without spawning a subprocess. Returns true so callers can assert
// the closure ran (defensive: a test author who forgets to call fn
// would otherwise see a silent pass).
func captureStdout(t *testing.T, buf *strings.Builder, fn func()) bool {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()

	fn()

	w.Close()
	os.Stdout = old
	<-done
	return true
}

func TestEmitSuggestion_FixtureGoSimple(t *testing.T) {
	snips := loadFixtureSnippets(t,
		"../../testdata/refactor/go/simple/a.go",
		"../../testdata/refactor/go/simple/b.go",
	)
	pair := report.Pair{
		ID:    report.PairID(snips[0].Name, snips[1].Name),
		NameA: snips[0].Name, NameB: snips[1].Name,
		LangA: "go", LangB: "go",
	}

	var buf strings.Builder
	captured := captureStdout(t, &buf, func() {
		if err := emitSuggestion(pair.ID, []report.Pair{pair}, nil, snips); err != nil {
			t.Fatalf("emitSuggestion error: %v", err)
		}
	})
	if !captured {
		t.Fatal("captureStdout did not run the closure")
	}
	out := buf.String()
	if !strings.Contains(out, "--- a/") {
		t.Errorf("expected diff header in output. Got:\n%s", out)
	}
	if !strings.Contains(out, "func extracted_priceWithTaxA_") {
		t.Errorf("expected helper function in output. Got:\n%s", out)
	}
	if !strings.Contains(out, "0.07") || !strings.Contains(out, "0.085") {
		t.Errorf("expected divergence comment with both literals. Got:\n%s", out)
	}
}

func TestEmitSuggestion_UnknownID_ReturnsHintError(t *testing.T) {
	err := emitSuggestion("notfound", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown id")
	}
	if !strings.Contains(err.Error(), "no pair or partial clone matches id") {
		t.Errorf("error %q lacks the 'no pair or partial clone matches' hint", err)
	}
}

func TestBuildSuggestionMap_GoFixture_PopulatesPatch(t *testing.T) {
	snips := loadFixtureSnippets(t,
		"../../testdata/refactor/go/simple/a.go",
		"../../testdata/refactor/go/simple/b.go",
	)
	pair := report.Pair{
		ID:    report.PairID(snips[0].Name, snips[1].Name),
		NameA: snips[0].Name, NameB: snips[1].Name,
		LangA: "go", LangB: "go",
	}
	out := buildSuggestionMap([]report.Pair{pair}, snips)
	patch, ok := out[pair.ID]
	if !ok {
		t.Fatalf("no patch produced for pair %s", pair.ID)
	}
	if patch.UnifiedDiff == "" {
		t.Errorf("expected UnifiedDiff to be set; Note=%q", patch.Note)
	}
	if patch.HelperName == "" || !strings.HasPrefix(patch.HelperName, "extracted_") {
		t.Errorf("HelperName = %q, want extracted_… prefix", patch.HelperName)
	}
	if patch.Confidence <= 0 {
		t.Errorf("Confidence = %v, want > 0", patch.Confidence)
	}
}

func TestBuildSuggestionMap_PythonFixture_PopulatesPatch(t *testing.T) {
	snips := loadFixtureSnippets(t,
		"../../testdata/refactor/python/simple/a.py",
		"../../testdata/refactor/python/simple/b.py",
	)
	pair := report.Pair{
		ID:    report.PairID(snips[0].Name, snips[1].Name),
		NameA: snips[0].Name, NameB: snips[1].Name,
		LangA: "python", LangB: "python",
	}
	out := buildSuggestionMap([]report.Pair{pair}, snips)
	patch, ok := out[pair.ID]
	if !ok {
		t.Fatalf("no patch produced for pair %s", pair.ID)
	}
	if patch.UnifiedDiff == "" {
		t.Errorf("expected UnifiedDiff to be set; Note=%q", patch.Note)
	}
	if !strings.Contains(patch.UnifiedDiff, "def extracted_price_with_tax_a_") {
		t.Errorf("expected Python helper def in UnifiedDiff. Got:\n%s", patch.UnifiedDiff)
	}
	if !strings.Contains(patch.UnifiedDiff, "# Divergences (B vs A):") {
		t.Errorf("expected Python-style `#` divergence header. Got:\n%s", patch.UnifiedDiff)
	}
	if patch.HelperName == "" || !strings.HasPrefix(patch.HelperName, "extracted_") {
		t.Errorf("HelperName = %q, want extracted_… prefix", patch.HelperName)
	}
	if patch.Confidence <= 0 {
		t.Errorf("Confidence = %v, want > 0", patch.Confidence)
	}
}

func TestBuildSuggestionMap_JSFixture_PopulatesPatch(t *testing.T) {
	snips := loadFixtureSnippets(t,
		"../../testdata/refactor/js/simple/a.js",
		"../../testdata/refactor/js/simple/b.js",
	)
	pair := report.Pair{
		ID:    report.PairID(snips[0].Name, snips[1].Name),
		NameA: snips[0].Name, NameB: snips[1].Name,
		LangA: "javascript", LangB: "javascript",
	}
	out := buildSuggestionMap([]report.Pair{pair}, snips)
	patch, ok := out[pair.ID]
	if !ok {
		t.Fatalf("no patch entry for pair %s", pair.ID)
	}
	if patch.UnifiedDiff == "" {
		t.Errorf("expected UnifiedDiff to be set; Note=%q", patch.Note)
	}
	if !strings.Contains(patch.UnifiedDiff, "function extracted_priceWithTaxA_") {
		t.Errorf("expected JS helper signature in UnifiedDiff. Got:\n%s", patch.UnifiedDiff)
	}
	if !strings.Contains(patch.UnifiedDiff, "// Divergences (B vs A):") {
		t.Errorf("expected `//`-style divergence header. Got:\n%s", patch.UnifiedDiff)
	}
	if !strings.HasPrefix(patch.HelperName, "extracted_priceWithTaxA_") {
		t.Errorf("HelperName = %q, want extracted_priceWithTaxA_… prefix", patch.HelperName)
	}
	if patch.Confidence <= 0 {
		t.Errorf("Confidence = %v, want > 0", patch.Confidence)
	}
}
