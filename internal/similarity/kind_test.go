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

// TestGoMethodsetChunks_KindGateAndNestingSuppression pins the §5.2
// invariants for Go struct+methodset groups on REAL splitter output.
// The group chunk is SYNTHETIC (non-contiguous: joined type decl +
// methods under a covering range), which makes the containment claims
// worth pinning explicitly: the covering range contains the group's
// own methods AND the unrelated function interleaved between them, so
// the nesting filter suppresses both same-file shapes; the kind gate
// independently kills every group-vs-function pair, same-file or not.
func TestGoMethodsetChunks_KindGateAndNestingSuppression(t *testing.T) {
	codeA := `package p

type Alpha struct {
	n int
}

func (a *Alpha) Add(x int) int {
	a.n += x
	return a.n
}

func interleaved(s string) string {
	return s + s
}

func (a *Alpha) Sub(x int) int {
	a.n -= x
	return a.n
}
`
	codeB := `package q

type Beta struct {
	n int
}

func (b *Beta) Plus(x int) int {
	b.n += x
	return b.n
}

func (b *Beta) Minus(x int) int {
	b.n -= x
	return b.n
}
`
	toSnippets := func(path, code string) (group scan.Snippet, funcs []scan.Snippet) {
		var haveGroup bool
		for _, c := range splitter.Split(path, code, tokenizer.Go) {
			s := scan.Snippet{
				Name: c.Name(), Path: c.Path,
				StartLine: c.StartLine, EndLine: c.EndLine, Kind: c.Kind,
			}
			if c.Kind == splitter.KindClass {
				group, haveGroup = s, true
			} else {
				funcs = append(funcs, s)
			}
		}
		if !haveGroup || len(funcs) == 0 {
			t.Fatalf("%s: expected a group chunk and function chunks, got group=%v funcs=%d", path, haveGroup, len(funcs))
		}
		return group, funcs
	}
	groupA, funcsA := toSnippets("/a.go", codeA)
	groupB, funcsB := toSnippets("/b.go", codeB)

	if ComparableKinds(groupA, funcsB[0]) || ComparableKinds(groupB, funcsA[0]) {
		t.Error("a methodset group must never be comparable with a cross-file function chunk (kind gate)")
	}
	if !ComparableKinds(groupA, groupB) {
		t.Error("cross-file group↔group chunks must stay comparable — that's the §5.2 value-add")
	}
	// The covering range contains every same-file function chunk here —
	// the group's own methods and the interleaved unrelated function
	// alike (Option-A consequence, documented as acceptable: same-file
	// group findings are rarely the value; cross-file group↔group is).
	for _, f := range funcsA {
		if !chunksNestedSameFile(groupA, f) {
			t.Errorf("group range %d-%d must contain same-file chunk %s (%d-%d)",
				groupA.StartLine, groupA.EndLine, f.Name, f.StartLine, f.EndLine)
		}
	}
	if chunksNestedSameFile(groupA, funcsB[0]) {
		t.Error("cross-file spans must not be treated as nested")
	}
}

// TestRustImplChunks_KindGateAndNestingSuppression is the Rust
// counterpart: impl spans are contiguous containers like Java/JS
// classes, so the module-style invariants carry over directly.
func TestRustImplChunks_KindGateAndNestingSuppression(t *testing.T) {
	codeA := `impl Alpha {
    fn add(&mut self, x: i64) -> i64 {
        self.n += x;
        self.n
    }

    fn sub(&mut self, x: i64) -> i64 {
        self.n -= x;
        self.n
    }
}
`
	codeB := `impl Beta {
    fn plus(&mut self, x: i64) -> i64 {
        self.n += x;
        self.n
    }

    fn minus(&mut self, x: i64) -> i64 {
        self.n -= x;
        self.n
    }
}
`
	toSnippets := func(path, code string) (impl, fn scan.Snippet) {
		var haveImpl, haveFn bool
		for _, c := range splitter.Split(path, code, tokenizer.Rust) {
			s := scan.Snippet{
				Name: c.Name(), Path: c.Path,
				StartLine: c.StartLine, EndLine: c.EndLine, Kind: c.Kind,
			}
			if c.Kind == splitter.KindClass {
				impl, haveImpl = s, true
			} else if !haveFn {
				fn, haveFn = s, true
			}
		}
		if !haveImpl || !haveFn {
			t.Fatalf("%s: expected an impl chunk and a fn chunk, got impl=%v fn=%v", path, haveImpl, haveFn)
		}
		return impl, fn
	}
	implA, fnA := toSnippets("/a.rs", codeA)
	implB, fnB := toSnippets("/b.rs", codeB)

	if ComparableKinds(implA, fnB) || ComparableKinds(implB, fnA) {
		t.Error("an impl chunk must never be comparable with a cross-file fn chunk (kind gate)")
	}
	if !ComparableKinds(implA, implB) {
		t.Error("cross-file impl↔impl chunks must stay comparable — that's the §5.2 value-add")
	}
	if !chunksNestedSameFile(implA, fnA) {
		t.Errorf("impl span %d-%d must contain its own fn span %d-%d (same-file nesting suppression)",
			implA.StartLine, implA.EndLine, fnA.StartLine, fnA.EndLine)
	}
}

// TestBuildMatrix_ClassPairsExcludedFromBlockCandidates: class-kind
// pairs never enter the §5.3 block-candidate channel, even when their
// combined score lands squarely in the gray band. Two reasons, both
// §5.2: (1) redundancy — every method inside a container is emitted as
// its own function chunk, which participates in the block channel
// independently, so container-level block detection re-finds the same
// text; (2) correctness — Go methodset groups have NON-CONTIGUOUS
// joined Code, and the block detector's chunk-relative→absolute line
// arithmetic (snip.StartLine + rel - 1) would report ranges that don't
// correspond to the matched source. The control half of this test
// mirrors the function-kind case that DOES yield a candidate.
func TestBuildMatrix_ClassPairsExcludedFromBlockCandidates(t *testing.T) {
	tokens := []string{"VAR", "=", "VAR", ".", "len", "(", ")", "for", "VAR", "in", "VAR", "VAR", "+=", "VAR"}
	mk := func(name, path string, kind splitter.ChunkKind) scan.Snippet {
		s := makeSnippet(name, path, tokens)
		s.Lang, s.NonBlankLn, s.Kind = "Go", 4, kind
		return s
	}

	// Control: identical short function-kind snippets under a high
	// threshold land in the gray band and become a block candidate.
	fns := []scan.Snippet{mk("a.go:1-4 f", "/a.go", splitter.KindFunction), mk("b.go:1-4 g", "/b.go", splitter.KindFunction)}
	_, _, fnCands := BuildMatrix(fns, vectorsFor(fns), 20, 0.85, nil)
	if len(fnCands) != 1 {
		t.Fatalf("control: function-kind gray-band pair should yield 1 block candidate, got %v", fnCands)
	}

	// Same shape with class kind: no block candidate.
	cls := []scan.Snippet{mk("a.go:1-4 A", "/a.go", splitter.KindClass), mk("b.go:1-4 B", "/b.go", splitter.KindClass)}
	matrix, _, clsCands := BuildMatrix(cls, vectorsFor(cls), 20, 0.85, nil)
	if len(clsCands) != 0 {
		t.Errorf("class-kind pairs must not feed block candidates, got %v", clsCands)
	}
	if matrix[0][1] == 0 {
		t.Error("class↔class pair must still be scored in the matrix (only the block channel is closed)")
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
