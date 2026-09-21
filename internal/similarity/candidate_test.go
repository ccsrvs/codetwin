package similarity

import (
	"reflect"
	"testing"

	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/scan"
)

func candidateVector(values Vector) NormalizedVector { return Normalize(values) }

func TestSemanticCandidateIndexReturnsDeterministicSharedTermPairs(t *testing.T) {
	vectors := []NormalizedVector{
		candidateVector(Vector{"a": 1, "b": 1}),
		candidateVector(Vector{"b": 2}),
		candidateVector(Vector{"c": 1}),
		candidateVector(Vector{"a": 1, "c": 1}),
		candidateVector(Vector{}),
	}
	index := NewSemanticCandidateIndex(vectors)
	wants := [][]int{{1, 3}, {}, {3}, {}, {}}
	for i, want := range wants {
		first := index.Candidates(i)
		second := index.Candidates(i)
		if !reflect.DeepEqual(first, want) {
			t.Errorf("Candidates(%d) = %v, want %v", i, first, want)
		}
		if !reflect.DeepEqual(second, first) {
			t.Errorf("Candidates(%d) changed across calls: %v then %v", i, first, second)
		}
	}
}

func TestSemanticCandidateIndexContainsEveryPositiveCosinePair(t *testing.T) {
	vectors := []NormalizedVector{
		candidateVector(Vector{"shared": 1, "a": 2}),
		candidateVector(Vector{"shared": 3, "b": 1}),
		candidateVector(Vector{"only-c": 1}),
		candidateVector(Vector{}),
	}
	index := NewSemanticCandidateIndex(vectors)
	for i := 0; i < len(vectors); i++ {
		candidates := make(map[int]bool)
		for _, j := range index.Candidates(i) {
			candidates[j] = true
		}
		for j := i + 1; j < len(vectors); j++ {
			if cosine := CosineFromNormalized(vectors[i], vectors[j]); cosine > 0 && !candidates[j] {
				t.Errorf("positive-cosine pair (%d,%d)=%v missing from candidates", i, j, cosine)
			}
		}
	}
}

func TestSemanticCandidateIndexUsesHighestWeightTermsWithLexicalTieBreak(t *testing.T) {
	left := Vector{"shared-low": 0.1}
	for i := 0; i < semanticCandidateTerms; i++ {
		left[string(rune('a'+i))] = 2
	}
	vectors := []NormalizedVector{
		candidateVector(left),
		candidateVector(Vector{"shared-low": 1}),
		candidateVector(Vector{"a": 1}),
		candidateVector(Vector{"p": 1}),
	}
	index := NewSemanticCandidateIndex(vectors)
	// The low-weight shared term is outside the bounded retrieval set. With
	// equal weights, lexical order selects a..p deterministically.
	if got, want := index.Candidates(0), []int{2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates(0) = %v, want %v", got, want)
	}
}

func TestBuildGraphReportsStructuralAndSemanticCandidateUnion(t *testing.T) {
	snippets := []scan.Snippet{
		makeSnippet("a", "/a", []string{"a", "a", "a", "a", "a", "a"}),
		makeSnippet("b", "/b", []string{"b", "b", "b", "b", "b", "b"}),
		makeSnippet("c", "/c", []string{"c", "c", "c", "c", "c", "c"}),
	}
	// a↔b is structural-only; b↔c is semantic-only. a↔c has no evidence.
	snippets[0].Fps.Set = fingerprint.Set{7: {}}
	snippets[1].Fps.Set = fingerprint.Set{7: {}}
	snippets[2].Fps.Set = fingerprint.Set{}
	vectors := []NormalizedVector{
		candidateVector(Vector{"only-a": 1}),
		candidateVector(Vector{"shared": 1, "only-b": 1}),
		candidateVector(Vector{"shared": 1, "only-c": 1}),
	}

	var selected, total int64
	BuildGraph(snippets, vectors, 0, 0.50, nil, MatrixOptions{
		OnCandidates: func(gotSelected, gotTotal int64) {
			selected, total = gotSelected, gotTotal
		},
	})
	if selected != 2 || total != 3 {
		t.Errorf("candidate union selected %d/%d pairs, want 2/3", selected, total)
	}

	BuildMatrix(snippets, vectors, 0, 0.50, nil, MatrixOptions{
		OnCandidates: func(gotSelected, gotTotal int64) {
			selected, total = gotSelected, gotTotal
		},
	})
	if selected != 3 || total != 3 {
		t.Errorf("exhaustive oracle selected %d/%d pairs, want 3/3", selected, total)
	}
}

func TestBuildGraphReportsZeroCandidatesForEmptyInput(t *testing.T) {
	called := false
	scoreCalled := false
	state := cache.New()
	BuildGraph(nil, nil, 0, 0.50, nil, MatrixOptions{
		ScoreCache: state,
		OnCandidates: func(selected, total int64) {
			called = true
			if selected != 0 || total != 0 {
				t.Errorf("empty candidate counts = %d/%d, want 0/0", selected, total)
			}
		},
		OnScoreCache: func(hits, misses int64) {
			scoreCalled = true
			if hits != 0 || misses != 0 {
				t.Errorf("empty score-cache counts = %d/%d, want 0/0", hits, misses)
			}
		},
	})
	if !called {
		t.Error("candidate callback was not called")
	}
	if !scoreCalled || state.LoadPairScoreSnapshot().Scores == nil {
		t.Error("score-cache callback/snapshot was not initialized")
	}
}
