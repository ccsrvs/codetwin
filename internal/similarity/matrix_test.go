package similarity

import (
	"testing"

	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/scan"
)

// makeSnippet constructs a Snippet with the minimum fields BuildMatrix
// reads (Name, Path, NonBlankLn, Tokens, Fps).
func makeSnippet(name, path string, tokens []string) scan.Snippet {
	ps := fingerprint.GeneratePositional(tokens, fingerprint.DefaultK, fingerprint.DefaultW)
	return scan.Snippet{
		Name:       name,
		Path:       path,
		Tokens:     tokens,
		NonBlankLn: 30,
		Fps:        ps,
	}
}

func vectorsFor(snips []scan.Snippet) []NormalizedVector {
	streams := make([][]string, len(snips))
	for i, s := range snips {
		streams[i] = s.Tokens
	}
	corpus := NewCorpus(streams)
	out := make([]NormalizedVector, len(snips))
	for i, s := range snips {
		out[i] = Normalize(corpus.Vectorize(s.Tokens))
	}
	return out
}

func TestBuildMatrix_GivenIdenticalSnippets_When_Build_Then_PairScoreIsHigh(t *testing.T) {
	tokens := []string{"VAR", "=", "VAR", ".", "len", "(", ")", "for", "VAR", "in", "VAR", "VAR", "+=", "VAR"}
	snips := []scan.Snippet{
		makeSnippet("a/sum.go", "/a.go", tokens),
		makeSnippet("b/sum.go", "/b.go", tokens),
	}
	vectors := vectorsFor(snips)

	matrix, pairs := BuildMatrix(snips, vectors, 0, nil)

	if matrix[0][1] < 0.9 {
		t.Errorf("matrix[0][1] = %v, want >= 0.9 for identical snippets", matrix[0][1])
	}
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].Score < 0.9 {
		t.Errorf("pair score = %v, want >= 0.9", pairs[0].Score)
	}
}

func TestBuildMatrix_GivenNestedSnippetsInSameFile_When_Build_Then_PairIsSuppressed(t *testing.T) {
	tokens := []string{"VAR", "=", "VAR", ".", "len", "(", ")", "VAR", "+=", "VAR"}
	a := makeSnippet("foo.py:1-50", "/foo.py", tokens)
	a.StartLine = 1
	a.EndLine = 50
	b := makeSnippet("foo.py:10-30", "/foo.py", tokens)
	b.StartLine = 10
	b.EndLine = 30
	snips := []scan.Snippet{a, b}
	vectors := vectorsFor(snips)

	matrix, pairs := BuildMatrix(snips, vectors, 0, nil)

	if matrix[0][1] != 0 {
		t.Errorf("matrix[0][1] = %v, want 0 for nested same-file snippets", matrix[0][1])
	}
	if len(pairs) != 0 {
		t.Errorf("expected 0 pairs (nested suppressed), got %d", len(pairs))
	}
}

func TestBuildMatrix_GivenLessThanTwoSnippets_When_Build_Then_ReturnsEmptyPairs(t *testing.T) {
	matrix, pairs := BuildMatrix(nil, nil, 0, nil)
	if len(matrix) != 0 {
		t.Errorf("expected empty matrix for empty input, got len %d", len(matrix))
	}
	if pairs != nil {
		t.Errorf("expected nil pairs for empty input, got %v", pairs)
	}

	one := []scan.Snippet{makeSnippet("a", "/a", []string{"VAR"})}
	matrix, pairs = BuildMatrix(one, vectorsFor(one), 0, nil)
	if len(matrix) != 1 || matrix[0][0] != 1.0 {
		t.Errorf("matrix diagonal not initialized for single snippet: %v", matrix)
	}
	if pairs != nil {
		t.Errorf("expected nil pairs for single snippet, got %v", pairs)
	}
}

func TestBuildMatrix_PopulatesLanguageOnPairs(t *testing.T) {
	tokens := []string{"VAR", "=", "VAR", ".", "len", "(", ")", "for", "VAR", "in", "VAR", "VAR", "+=", "VAR"}
	a := makeSnippet("a/sum.go", "/a.go", tokens)
	a.Lang = "Go"
	b := makeSnippet("b/sum.py", "/b.py", tokens)
	b.Lang = "Python"
	snips := []scan.Snippet{a, b}
	vectors := vectorsFor(snips)

	_, pairs := BuildMatrix(snips, vectors, 0, nil)

	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].LangA != "Go" || pairs[0].LangB != "Python" {
		t.Errorf("pair langs = %q/%q, want Go/Python", pairs[0].LangA, pairs[0].LangB)
	}
}

func TestBuildMatrix_PopulatesPairID(t *testing.T) {
	tokens := []string{"VAR", "=", "VAR", ".", "len", "(", ")", "for", "VAR", "in", "VAR", "VAR", "+=", "VAR"}
	snips := []scan.Snippet{
		makeSnippet("a/sum.go", "/a.go", tokens),
		makeSnippet("b/sum.go", "/b.go", tokens),
	}
	vectors := vectorsFor(snips)

	_, pairs := BuildMatrix(snips, vectors, 0, nil)

	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	id := pairs[0].ID
	if len(id) != 8 {
		t.Errorf("pair ID = %q, want 8-char hex", id)
	}
	for _, r := range id {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Errorf("pair ID = %q has non-hex char %q", id, r)
		}
	}
}

func TestBuildMatrix_PairIDIsOrderInvariantAndStable(t *testing.T) {
	tokens := []string{"VAR", "=", "VAR", ".", "len", "(", ")", "for", "VAR", "in", "VAR", "VAR", "+=", "VAR"}
	a := makeSnippet("a/sum.go", "/a.go", tokens)
	b := makeSnippet("b/sum.go", "/b.go", tokens)
	vectors := vectorsFor([]scan.Snippet{a, b})

	_, pairsAB := BuildMatrix([]scan.Snippet{a, b}, vectors, 0, nil)
	_, pairsBA := BuildMatrix([]scan.Snippet{b, a}, vectors, 0, nil)

	if len(pairsAB) != 1 || len(pairsBA) != 1 {
		t.Fatalf("expected 1 pair from each ordering, got %d / %d", len(pairsAB), len(pairsBA))
	}
	if pairsAB[0].ID != pairsBA[0].ID {
		t.Errorf("pair ID not order-invariant: %q vs %q", pairsAB[0].ID, pairsBA[0].ID)
	}
	// Re-run with the original ordering — same input, same output.
	_, pairsAB2 := BuildMatrix([]scan.Snippet{a, b}, vectors, 0, nil)
	if pairsAB[0].ID != pairsAB2[0].ID {
		t.Errorf("pair ID not deterministic across runs: %q vs %q", pairsAB[0].ID, pairsAB2[0].ID)
	}
}

func TestBuildMatrix_GivenOnPairDoneCallback_When_Build_Then_TotalArgIsPairCount(t *testing.T) {
	tokens := []string{"VAR", "=", "VAR", "+", "VAR"}
	snips := []scan.Snippet{
		makeSnippet("a", "/a", tokens),
		makeSnippet("b", "/b", tokens),
		makeSnippet("c", "/c", tokens),
		makeSnippet("d", "/d", tokens),
	}
	vectors := vectorsFor(snips)

	var lastTotal int64
	BuildMatrix(snips, vectors, 0, func(_, total int64) {
		lastTotal = total
	})

	// 4 snippets → C(4,2) = 6 pairs
	if lastTotal != 6 {
		t.Errorf("onPairDone total = %d, want 6", lastTotal)
	}
}

func TestBuildMatrix_IdenticalShortSnippetsGetStructuralCredit(t *testing.T) {
	// Enough tokens for exactly one k-gram — fewer than one full
	// winnowing window. Identical snippets must still share
	// fingerprints and score structural 1.0.
	tokens := make([]string, fingerprint.DefaultK)
	for i := range tokens {
		if i%2 == 0 {
			tokens[i] = "VAR"
		} else {
			tokens[i] = "tok" + string(rune('a'+i))
		}
	}
	snips := []scan.Snippet{
		makeSnippet("a/short.py", "/a.py", tokens),
		makeSnippet("b/short.py", "/b.py", tokens),
	}
	vectors := vectorsFor(snips)

	matrix, pairs := BuildMatrix(snips, vectors, 0, nil)

	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].Structural != 1.0 {
		t.Errorf("structural = %v, want 1.0 for identical short snippets", pairs[0].Structural)
	}
	if matrix[0][1] < 0.9 {
		t.Errorf("matrix[0][1] = %v, want >= 0.9 for identical short snippets", matrix[0][1])
	}
}
