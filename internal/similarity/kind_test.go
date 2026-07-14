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
