package fingerprint

import "testing"

func TestGenerate_BasicTokens(t *testing.T) {
	tokens := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	set := Generate(tokens, DefaultK, DefaultW)
	if len(set) == 0 {
		t.Error("Generate returned empty set for non-trivial input")
	}
}

func TestGenerate_ShortInputReturnsEmpty(t *testing.T) {
	// Tokens shorter than k → no k-grams → empty set
	set := Generate([]string{"a", "b"}, DefaultK, DefaultW)
	if len(set) != 0 {
		t.Errorf("expected empty set for tokens shorter than k, got %d entries", len(set))
	}
}

func TestGenerate_DeterministicForSameInput(t *testing.T) {
	tokens := []string{"a", "b", "c", "d", "e", "f", "g"}
	a := Generate(tokens, DefaultK, DefaultW)
	b := Generate(tokens, DefaultK, DefaultW)
	if Jaccard(a, b) != 1.0 {
		t.Errorf("identical input produced non-identical fingerprints (Jaccard=%v)", Jaccard(a, b))
	}
}

func TestJaccard_IdenticalSets(t *testing.T) {
	a := Set{1: {}, 2: {}, 3: {}}
	b := Set{1: {}, 2: {}, 3: {}}
	if got := Jaccard(a, b); got != 1.0 {
		t.Errorf("Jaccard of identical sets = %v; want 1.0", got)
	}
}

func TestJaccard_DisjointSets(t *testing.T) {
	a := Set{1: {}, 2: {}}
	b := Set{3: {}, 4: {}}
	if got := Jaccard(a, b); got != 0.0 {
		t.Errorf("Jaccard of disjoint sets = %v; want 0.0", got)
	}
}

func TestJaccard_PartialOverlap(t *testing.T) {
	a := Set{1: {}, 2: {}, 3: {}}
	b := Set{2: {}, 3: {}, 4: {}}
	// |intersection| = 2, |union| = 4, ratio = 0.5
	if got := Jaccard(a, b); got != 0.5 {
		t.Errorf("Jaccard partial overlap = %v; want 0.5", got)
	}
}

func TestJaccard_BothEmpty(t *testing.T) {
	if got := Jaccard(Set{}, Set{}); got != 1.0 {
		t.Errorf("Jaccard of two empty sets = %v; want 1.0 (vacuous)", got)
	}
}

func TestJaccard_OneEmpty(t *testing.T) {
	a := Set{1: {}, 2: {}}
	if got := Jaccard(a, Set{}); got != 0.0 {
		t.Errorf("Jaccard with one empty set = %v; want 0.0", got)
	}
}