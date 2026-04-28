package fingerprint

import (
	"sort"
	"testing"
)

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

func TestGeneratePositional_SetMatchesGenerate(t *testing.T) {
	tokens := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	plain := Generate(tokens, DefaultK, DefaultW)
	pos := GeneratePositional(tokens, DefaultK, DefaultW)
	if len(plain) != len(pos.Set) {
		t.Errorf("Generate set size %d != GeneratePositional set size %d", len(plain), len(pos.Set))
	}
	for h := range plain {
		if _, ok := pos.Set[h]; !ok {
			t.Errorf("hash %d in Generate but missing from GeneratePositional", h)
		}
	}
	if pos.K != DefaultK {
		t.Errorf("expected K=%d on PositionalSet, got %d", DefaultK, pos.K)
	}
}

func TestGeneratePositional_PositionsAreInRange(t *testing.T) {
	tokens := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	pos := GeneratePositional(tokens, DefaultK, DefaultW)
	maxPos := len(tokens) - DefaultK // last valid k-gram start
	for hash, positions := range pos.Positions {
		for _, p := range positions {
			if p < 0 || p > maxPos {
				t.Errorf("hash %d: position %d out of range [0, %d]", hash, p, maxPos)
			}
		}
	}
}

func TestMatchRange_NoOverlap(t *testing.T) {
	a := PositionalSet{
		Set:       Set{1: {}, 2: {}},
		Positions: map[uint32][]int{1: {0}, 2: {3}},
		K:         5,
	}
	b := PositionalSet{
		Set:       Set{99: {}},
		Positions: map[uint32][]int{99: {0}},
		K:         5,
	}
	first, last := MatchRange(a, b)
	if first != -1 || last != -1 {
		t.Errorf("expected (-1, -1) for disjoint sets, got (%d, %d)", first, last)
	}
}

func TestMatchRange_SpansMatchingPositions(t *testing.T) {
	// Hash 7 matches at positions 2 and 9 in a; hash 8 only in a.
	// Range should span 2 to 9 (the matching positions).
	a := PositionalSet{
		Set:       Set{7: {}, 8: {}},
		Positions: map[uint32][]int{7: {2, 9}, 8: {15}},
		K:         5,
	}
	b := PositionalSet{
		Set:       Set{7: {}},
		Positions: map[uint32][]int{7: {0}},
		K:         5,
	}
	first, last := MatchRange(a, b)
	if first != 2 || last != 9 {
		t.Errorf("expected (2, 9), got (%d, %d)", first, last)
	}
}

func TestMatchRange_IdenticalInputsCoverFullStream(t *testing.T) {
	tokens := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	pos := GeneratePositional(tokens, DefaultK, DefaultW)
	first, last := MatchRange(pos, pos)
	if first < 0 || last < first {
		t.Fatalf("expected valid range, got (%d, %d)", first, last)
	}
	// Last possible k-gram start = len(tokens) - K.
	maxStart := len(tokens) - DefaultK
	if last > maxStart {
		t.Errorf("last position %d exceeds max k-gram start %d", last, maxStart)
	}
}

func TestHashes_FlattensAllElements(t *testing.T) {
	s := Set{1: {}, 2: {}, 3: {}}
	got := Hashes(s)
	if len(got) != 3 {
		t.Fatalf("expected 3 hashes, got %d", len(got))
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	want := []uint32{1, 2, 3}
	for i, h := range want {
		if got[i] != h {
			t.Errorf("hashes[%d]: got %d, want %d", i, got[i], h)
		}
	}
}

func TestHashes_EmptySetReturnsEmpty(t *testing.T) {
	got := Hashes(Set{})
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestHashes_NilSetReturnsEmpty(t *testing.T) {
	got := Hashes(nil)
	if len(got) != 0 {
		t.Errorf("expected empty slice for nil set, got %v", got)
	}
}

func TestHashes_RoundTripPreservesMembership(t *testing.T) {
	original := Set{42: {}, 99: {}, 7: {}}
	flat := Hashes(original)
	rebuilt := make(Set, len(flat))
	for _, h := range flat {
		rebuilt[h] = struct{}{}
	}
	if Jaccard(original, rebuilt) != 1.0 {
		t.Errorf("round trip lost membership: original=%v rebuilt=%v", original, rebuilt)
	}
}