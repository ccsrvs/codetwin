package similarity

import (
	"math"
	"testing"
)

func TestNewCorpus_RareTermsHaveHigherIDF(t *testing.T) {
	streams := [][]string{
		{"a", "b", "c"},
		{"a", "d"},
		{"a", "b"},
	}
	c := NewCorpus(streams)
	// 'a' appears in all 3 docs, 'c' in only 1 → c should have higher IDF
	if c.idf["a"] >= c.idf["c"] {
		t.Errorf("rare term should have higher IDF: idf[a]=%v idf[c]=%v", c.idf["a"], c.idf["c"])
	}
}

func TestVectorize_NonEmpty(t *testing.T) {
	c := NewCorpus([][]string{{"a", "b"}, {"b", "c"}})
	v := c.Vectorize([]string{"a", "b", "b"})
	if len(v) == 0 {
		t.Error("Vectorize returned empty vector for non-empty tokens")
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
		{Vector{}, Vector{"a": 1.0}},          // empty
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
func TestLengthDampen_OffWhenThresholdZero(t *testing.T) {
	if got := LengthDampen(0.95, 5, 5, 0); got != 0.95 {
		t.Errorf("threshold=0 should pass score through; got %v", got)
	}
	if got := LengthDampen(0.95, 5, 5, -1); got != 0.95 {
		t.Errorf("negative threshold should pass score through; got %v", got)
	}
}

func TestLengthDampen_LongSnippetsUnchanged(t *testing.T) {
	if got := LengthDampen(1.0, 25, 30, 20); got != 1.0 {
		t.Errorf("min(25,30)=25 >= 20: should be unchanged; got %v", got)
	}
	if got := LengthDampen(0.7, 20, 100, 20); got != 0.7 {
		t.Errorf("min=threshold should be unchanged; got %v", got)
	}
}

func TestLengthDampen_ShortSnippetsScaled(t *testing.T) {
	// min=5, threshold=20: multiplier = 0.5 + 0.5*(5/20) = 0.625
	if got := LengthDampen(1.0, 5, 5, 20); math.Abs(got-0.625) > 1e-9 {
		t.Errorf("got %v; want 0.625", got)
	}
	// min=10, threshold=20: multiplier = 0.5 + 0.5*(10/20) = 0.75
	if got := LengthDampen(1.0, 10, 30, 20); math.Abs(got-0.75) > 1e-9 {
		t.Errorf("got %v; want 0.75 (uses min line count)", got)
	}
	// min=2, threshold=10: multiplier = 0.5 + 0.5*(2/10) = 0.6
	if got := LengthDampen(0.5, 2, 100, 10); math.Abs(got-0.30) > 1e-9 {
		t.Errorf("got %v; want 0.30 (multiplier 0.6 on score 0.5)", got)
	}
}

func TestLengthDampen_GuardsAgainstBadInput(t *testing.T) {
	// Missing line counts (0 or negative) should leave the score alone.
	if got := LengthDampen(0.9, 0, 5, 20); got != 0.9 {
		t.Errorf("linesA=0: should pass through; got %v", got)
	}
	if got := LengthDampen(0.9, -3, 5, 20); got != 0.9 {
		t.Errorf("linesA<0: should pass through; got %v", got)
	}
}
