package similarity

import (
	"math"
	"testing"
)

func TestNewCorpus_RareTermsHaveHigherIDF(t *testing.T) {
	// Terms are token n-grams: "a b c" appears in all three docs,
	// "x y z" in only one → the rare n-gram gets the higher IDF.
	streams := [][]string{
		{"a", "b", "c"},
		{"a", "b", "c", "d"},
		{"a", "b", "c", "x", "y", "z"},
	}
	c := NewCorpus(streams)
	common := "a\x00b\x00c"
	rare := "x\x00y\x00z"
	if c.idf[common] == 0 || c.idf[rare] == 0 {
		t.Fatalf("expected both n-gram terms in corpus: idf[%q]=%v idf[%q]=%v",
			common, c.idf[common], rare, c.idf[rare])
	}
	if c.idf[common] >= c.idf[rare] {
		t.Errorf("rare term should have higher IDF: idf[common]=%v idf[rare]=%v",
			c.idf[common], c.idf[rare])
	}
}

func TestVectorize_NonEmpty(t *testing.T) {
	tokens := []string{"a", "b", "c", "d", "e", "f", "g"}
	c := NewCorpus([][]string{tokens})
	v := c.Vectorize(tokens)
	if len(v) == 0 {
		t.Error("Vectorize returned empty vector for non-empty tokens")
	}
}

func TestVectorize_BelowEvidenceFloorIsEmpty(t *testing.T) {
	// A stream yielding fewer than semanticMinTerms n-gram terms has too
	// little evidence for cosine to mean anything — the vector must be
	// empty so the pair falls back to structural-only scoring.
	c := NewCorpus([][]string{{"a", "b", "c"}})
	if v := c.Vectorize([]string{"a", "b", "c"}); len(v) != 0 {
		t.Errorf("expected empty vector below the evidence floor, got %v", v)
	}
}

func TestVectorize_DropsPunctuationTokens(t *testing.T) {
	// Punctuation tokens must not influence the semantic vector: two
	// streams identical except for punctuation vectorize identically.
	base := []string{"if", "VAR", "return", "VAR", "NUM", "for", "VAR"}
	withPunct := []string{"if", "(", "VAR", ")", "return", "VAR", "+", "NUM", "for", "VAR"}
	c := NewCorpus([][]string{base, withPunct})
	va := Normalize(c.Vectorize(base))
	vb := Normalize(c.Vectorize(withPunct))
	if got := CosineFromNormalized(va, vb); got < 0.999 {
		t.Errorf("punctuation changed the semantic vector: cosine = %v, want 1.0", got)
	}
}

func TestVectorize_CrossLanguageKeywordsCanonicalized(t *testing.T) {
	// `func`/`def` and `range`/`in` collapse to shared canonical tokens,
	// so equivalent Go and Python streams vectorize identically.
	golang := []string{"func", "VAR", "VAR", "VAR", "for", "_", "VAR", "range", "VAR", "VAR", "VAR", "return", "VAR"}
	python := []string{"def", "VAR", "VAR", "VAR", "for", "_", "VAR", "in", "VAR", "VAR", "VAR", "return", "VAR"}
	c := NewCorpus([][]string{golang, python})
	va := Normalize(c.Vectorize(golang))
	vb := Normalize(c.Vectorize(python))
	if got := CosineFromNormalized(va, vb); got < 0.999 {
		t.Errorf("canonicalized cross-language streams should vectorize equal: cosine = %v", got)
	}
}

func TestVectorize_EmptyTokens(t *testing.T) {
	c := NewCorpus([][]string{{"a"}})
	if v := c.Vectorize(nil); len(v) != 0 {
		t.Errorf("Vectorize of empty tokens should return empty vector, got %v", v)
	}
}

func TestCosine_IdenticalVectors(t *testing.T) {
	v := Vector{"a": 1.0, "b": 2.0}
	if got := Cosine(v, v); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("cosine of vector with itself = %v; want 1.0", got)
	}
}

func TestCosine_OrthogonalVectors(t *testing.T) {
	a := Vector{"a": 1.0}
	b := Vector{"b": 1.0}
	if got := Cosine(a, b); got != 0.0 {
		t.Errorf("cosine of orthogonal vectors = %v; want 0.0", got)
	}
}

func TestCosine_EmptyVector(t *testing.T) {
	if got := Cosine(Vector{}, Vector{"a": 1.0}); got != 0.0 {
		t.Errorf("cosine with empty vector = %v; want 0.0", got)
	}
}

func TestCosine_Symmetric(t *testing.T) {
	a := Vector{"a": 1.0, "b": 2.0}
	b := Vector{"a": 0.5, "c": 1.5}
	ab := Cosine(a, b)
	ba := Cosine(b, a)
	if math.Abs(ab-ba) > 1e-9 {
		t.Errorf("Cosine should be symmetric: a→b=%v, b→a=%v", ab, ba)
	}
}

func TestCosineFromNormalized_BitDeterministicAcrossCalls(t *testing.T) {
	// Without sorted-key iteration, Go's randomized map iteration would
	// give slightly different float sums on each call, breaking
	// stable-sort ties for pairs with otherwise-equal scores. The norm
	// must therefore be sum-stable across calls on the same vector.
	v := Vector{
		"alpha": 0.5, "beta": 0.7, "gamma": 0.3, "delta": 0.9,
		"epsilon": 0.1, "zeta": 0.4, "eta": 0.6, "theta": 0.2,
	}
	a := Normalize(v)
	b := Normalize(v)
	if a.Norm != b.Norm {
		t.Errorf("two Normalize calls on the same vector produced different norms: %v vs %v", a.Norm, b.Norm)
	}
	score1 := CosineFromNormalized(a, b)
	score2 := CosineFromNormalized(a, b)
	if score1 != score2 {
		t.Errorf("two CosineFromNormalized calls on identical inputs differ: %v vs %v", score1, score2)
	}
}

func TestCosineFromNormalized_MatchesCosine(t *testing.T) {
	// The precomputed-norm variant must produce identical scores to the
	// straightforward Cosine for the same inputs.
	cases := []struct {
		a, b Vector
	}{
		{Vector{"a": 1.0, "b": 2.0}, Vector{"a": 0.5, "c": 1.5}},
		{Vector{"x": 3.0}, Vector{"x": 3.0}},
		{Vector{"a": 1.0}, Vector{"b": 1.0}}, // orthogonal
		{Vector{}, Vector{"a": 1.0}},         // empty
	}
	for _, c := range cases {
		want := Cosine(c.a, c.b)
		got := CosineFromNormalized(Normalize(c.a), Normalize(c.b))
		if math.Abs(want-got) > 1e-12 {
			t.Errorf("Cosine=%v, CosineFromNormalized=%v for a=%v b=%v", want, got, c.a, c.b)
		}
	}
}

func TestCombined_WeightedAverage(t *testing.T) {
	if got := Combined(0.8, 0.4, 0.5); math.Abs(got-0.6) > 1e-9 {
		t.Errorf("Combined(0.8, 0.4, 0.5) = %v; want 0.6", got)
	}
	if got := Combined(0.8, 0.4, 1.0); got != 0.8 {
		t.Errorf("Combined(0.8, 0.4, 1.0) = %v; want 0.8 (all structural)", got)
	}
	if got := Combined(0.8, 0.4, 0.0); got != 0.4 {
		t.Errorf("Combined(0.8, 0.4, 0.0) = %v; want 0.4 (all semantic)", got)
	}
}
func TestLengthDampen(t *testing.T) {
	cases := []struct {
		name                   string
		score                  float64
		linesA, linesB, thresh int
		want                   float64
	}{
		{"threshold=0 passes score through", 0.95, 5, 5, 0, 0.95},
		{"negative threshold passes score through", 0.95, 5, 5, -1, 0.95},
		{"min(25,30)=25 >= threshold: unchanged", 1.0, 25, 30, 20, 1.0},
		{"min=threshold: unchanged", 0.7, 20, 100, 20, 0.7},
		{"min=5,thresh=20: multiplier 0.625", 1.0, 5, 5, 20, 0.625},
		{"min=10,thresh=20: multiplier 0.75 (uses min)", 1.0, 10, 30, 20, 0.75},
		{"min=2,thresh=10: multiplier 0.6 on score 0.5", 0.5, 2, 100, 10, 0.30},
		{"linesA=0 passes through", 0.9, 0, 5, 20, 0.9},
		{"linesA<0 passes through", 0.9, -3, 5, 20, 0.9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := LengthDampen(tc.score, tc.linesA, tc.linesB, tc.thresh)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("LengthDampen(%v, %d, %d, %d) = %v; want %v",
					tc.score, tc.linesA, tc.linesB, tc.thresh, got, tc.want)
			}
		})
	}
}
