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