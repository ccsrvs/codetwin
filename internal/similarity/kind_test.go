package similarity

// Mixed-kind suppression tests (§5.2 class-level granularity). Class
// chunks are whole-container spans; scoring them against function-level
// chunks re-introduces exactly the dilution the splitter exists to
// avoid (a big class weakly resembling a small function is not a
// finding). BuildMatrix therefore only scores class chunks against
// other class chunks, leaving mixed-kind matrix cells at 0 — the same
// short-circuit shape as chunksNestedSameFile.

import (
	"testing"

	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

func TestBuildMatrix_MixedKindPairAcrossFilesIsSuppressed(t *testing.T) {
	tokens := []string{"VAR", "=", "VAR", ".", "len", "(", ")", "for", "VAR", "in", "VAR", "VAR", "+=", "VAR"}
	cls := makeSnippet("a.py:1-30 Ledger", "/a.py", tokens)
	cls.Kind = splitter.KindClass
	fn := makeSnippet("b.py:1-30 helper", "/b.py", tokens)
	fn.Kind = splitter.KindFunction
	snips := []scan.Snippet{cls, fn}
	vectors := vectorsFor(snips)

	matrix, pairs, blockCands := BuildMatrix(snips, vectors, 0, 0.50, nil)

	if matrix[0][1] != 0 {
		t.Errorf("matrix[0][1] = %v, want 0 for a cross-file class-vs-function pair", matrix[0][1])
	}
	if len(pairs) != 0 {
		t.Errorf("expected 0 materialized pairs for a mixed-kind pair, got %d", len(pairs))
	}
	if len(blockCands) != 0 {
		t.Errorf("mixed-kind pairs must not feed block candidates, got %v", blockCands)
	}
}

func TestBuildMatrix_ClassClassPairAcrossFilesScores(t *testing.T) {
	tokens := []string{"VAR", "=", "VAR", ".", "len", "(", ")", "for", "VAR", "in", "VAR", "VAR", "+=", "VAR"}
	a := makeSnippet("a.py:1-30 Ledger", "/a.py", tokens)
	a.Kind = splitter.KindClass
	b := makeSnippet("b.py:1-30 Register", "/b.py", tokens)
	b.Kind = splitter.KindClass
	snips := []scan.Snippet{a, b}
	vectors := vectorsFor(snips)

	matrix, pairs, _ := BuildMatrix(snips, vectors, 0, 0.50, nil)

	if matrix[0][1] < 0.9 {
		t.Errorf("matrix[0][1] = %v, want >= 0.9 for identical class chunks", matrix[0][1])
	}
	if len(pairs) != 1 {
		t.Fatalf("expected 1 class-class pair, got %d", len(pairs))
	}
}

// TestBuildMatrix_ClassVsOwnMethodSameFileSuppressed pins the §5.2
// noise-control invariant: a class chunk and a method chunk inside it
// come from the same file with fully contained line ranges, so the
// pre-existing chunksNestedSameFile filter suppresses the pair (the
// kind gate independently suppresses it too — belt and braces).
func TestBuildMatrix_ClassVsOwnMethodSameFileSuppressed(t *testing.T) {
	tokens := []string{"VAR", "=", "VAR", ".", "len", "(", ")", "VAR", "+=", "VAR"}
	cls := makeSnippet("a.py:1-30 Ledger", "/a.py", tokens)
	cls.Kind = splitter.KindClass
	cls.StartLine, cls.EndLine = 1, 30
	meth := makeSnippet("a.py:5-12 add_item", "/a.py", tokens)
	meth.Kind = splitter.KindFunction
	meth.StartLine, meth.EndLine = 5, 12
	snips := []scan.Snippet{cls, meth}
	vectors := vectorsFor(snips)

	matrix, pairs, _ := BuildMatrix(snips, vectors, 0, 0.50, nil)

	if matrix[0][1] != 0 {
		t.Errorf("matrix[0][1] = %v, want 0 for class-vs-own-method", matrix[0][1])
	}
	if len(pairs) != 0 {
		t.Errorf("expected 0 pairs for class-vs-own-method, got %d", len(pairs))
	}
}

// TestElixirModuleChunks_KindGateAndNestingSuppression pins the §5.2
// noise-control invariants for Elixir `defmodule` spans specifically,
// on REAL splitter output (not hand-built snippets): a module chunk
// never compares against a loose def across files (kind gate), and a
// module chunk vs one of its own defs is caught by the same-file
// nesting filter (belt) in addition to the kind gate (braces).
func TestElixirModuleChunks_KindGateAndNestingSuppression(t *testing.T) {
	codeA := `defmodule LedgerA do
  def add(n, x) do
    n + x
  end

  def sub(n, x) do
    n - x
  end
end
`
	codeB := `defmodule LedgerB do
  def plus(n, x) do
    n + x
  end

  def minus(n, x) do
    n - x
  end
end
`
	toSnippets := func(path, code string) (mod, def scan.Snippet) {
		var haveMod, haveDef bool
		for _, c := range splitter.Split(path, code, tokenizer.Elixir) {
			s := scan.Snippet{
				Name: c.Name(), Path: c.Path,
				StartLine: c.StartLine, EndLine: c.EndLine, Kind: c.Kind,
			}
			switch c.Kind {
			case splitter.KindClass:
				mod, haveMod = s, true
			default:
				if !haveDef {
					def, haveDef = s, true
				}
			}
		}
		if !haveMod || !haveDef {
			t.Fatalf("%s: expected a module chunk and a def chunk, got mod=%v def=%v", path, haveMod, haveDef)
		}
		return mod, def
	}
	modA, defA := toSnippets("/a.ex", codeA)
	modB, defB := toSnippets("/b.ex", codeB)

	if ComparableKinds(modA, defB) || ComparableKinds(modB, defA) {
		t.Error("a defmodule chunk must never be comparable with a cross-file def chunk (kind gate)")
	}
	if !ComparableKinds(modA, modB) {
		t.Error("cross-file module↔module chunks must stay comparable — that's the §5.2 value-add")
	}
	if !ComparableKinds(defA, defB) {
		t.Error("def↔def chunks must stay comparable")
	}
	if !chunksNestedSameFile(modA, defA) {
		t.Errorf("module span %d-%d must contain its own def span %d-%d (same-file nesting suppression)",
			modA.StartLine, modA.EndLine, defA.StartLine, defA.EndLine)
	}
	if chunksNestedSameFile(modA, defB) {
		t.Error("cross-file spans must not be treated as nested")
	}
}

func TestComparableKinds(t *testing.T) {
	cls := scan.Snippet{Kind: splitter.KindClass}
	fn := scan.Snippet{Kind: splitter.KindFunction}
	legacy := scan.Snippet{} // zero Kind (pre-§5.2 construction) behaves as function
	cases := []struct {
		name string
		a, b scan.Snippet
		want bool
	}{
		{"class vs class", cls, cls, true},
		{"function vs function", fn, fn, true},
		{"class vs function", cls, fn, false},
		{"function vs class", fn, cls, false},
		{"legacy zero kind vs function", legacy, fn, true},
		{"legacy zero kind vs class", legacy, cls, false},
	}
	for _, c := range cases {
		if got := ComparableKinds(c.a, c.b); got != c.want {
			t.Errorf("%s: ComparableKinds = %v, want %v", c.name, got, c.want)
		}
	}
}
