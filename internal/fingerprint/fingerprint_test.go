package fingerprint

import (
	"math/bits"
	"sort"
	"strings"
	"testing"
)

// refGramHashes is the pre-optimization reference pipeline: materialize
// each k-gram as a space-joined string, then hash it byte-by-byte.
// gramHashes must produce bit-identical values — cached fingerprints and
// bench ground-truth scores depend on the hash values, so any deliberate
// change to the byte stream or mixing must bump SchemaVersion instead of
// silently diverging from this reference.
func refGramHashes(tokens []string, k int) []uint32 {
	if len(tokens) < k {
		return nil
	}
	hashes := make([]uint32, 0, len(tokens)-k+1)
	for i := 0; i <= len(tokens)-k; i++ {
		g := strings.Join(tokens[i:i+k], " ")
		h := uint32(2166136261)
		for j := 0; j < len(g); j++ {
			h ^= uint32(g[j])
			h = bits.RotateLeft32(h*16777619, 5)
		}
		hashes = append(hashes, h)
	}
	return hashes
}

func TestGramHashes_MatchesJoinedStringHash(t *testing.T) {
	cases := []struct {
		name   string
		tokens []string
		k      int
	}{
		{"synthetic stream", seqTokens(37), 10},
		{"single gram", seqTokens(5), 5},
		{"too short", seqTokens(4), 5},
		{"k=1", seqTokens(9), 1},
		{"empty tokens present", []string{"a", "", "b", "", "c", "d"}, 3},
		{"realistic punctuation", []string{
			"func", "VAR", "(", "VAR", "[", "]", "STR", ")", "{", "for",
			"VAR", ":=", "range", "VAR", "{", "VAR", "+=", "VAR", "}", "}",
		}, DefaultK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gramHashes(tc.tokens, tc.k)
			want := refGramHashes(tc.tokens, tc.k)
			if len(got) != len(want) {
				t.Fatalf("length mismatch: got %d hashes, want %d", len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("gram %d: gramHashes = %#x, reference = %#x", i, got[i], want[i])
				}
			}
		})
	}
}

// seqTokens builds n distinct tokens ("t0", "t1", …) so tests scale with
// DefaultK instead of hardcoding stream lengths.
func seqTokens(n int) []string {
	tokens := make([]string, n)
	for i := range tokens {
		tokens[i] = "t" + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	return tokens
}

func TestGenerate_BasicTokens(t *testing.T) {
	set := Generate(seqTokens(2*DefaultK), DefaultK, DefaultW)
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
	tokens := seqTokens(DefaultK + 2)
	a := Generate(tokens, DefaultK, DefaultW)
	b := Generate(tokens, DefaultK, DefaultW)
	// Guard against the vacuous pass: Jaccard(empty, empty) == 1.0, so the
	// determinism assertion below only means something if the sets are
	// non-empty.
	if len(a) == 0 {
		t.Fatal("Generate returned empty set for input with k-grams")
	}
	if Jaccard(a, b) != 1.0 {
		t.Errorf("identical input produced non-identical fingerprints (Jaccard=%v)", Jaccard(a, b))
	}
}

// A document with at least one k-gram must produce at least one fingerprint
// (Schleimer et al. §2: winnowing selects at least one hash per window, and
// a document shorter than one full window is itself a window). Without this
// guarantee, snippets with k ≤ len(tokens) < k+w-1 tokens get an empty set
// and can never match anything structurally — not even an identical copy.
func TestGenerate_ShortStreamStillFingerprints(t *testing.T) {
	// Token counts k .. k+w-2 produce 1 .. w-1 k-grams — fewer than one
	// full window.
	for n := DefaultK; n < DefaultK+DefaultW-1; n++ {
		set := Generate(seqTokens(n), DefaultK, DefaultW)
		if len(set) == 0 {
			t.Errorf("n=%d tokens: expected at least one fingerprint, got none", n)
		}
	}
}

func TestGenerate_IdenticalShortStreamsMatch(t *testing.T) {
	tokens := seqTokens(DefaultK) // exactly one k-gram
	a := Generate(tokens, DefaultK, DefaultW)
	b := Generate(append([]string{}, tokens...), DefaultK, DefaultW)
	if len(a) == 0 {
		t.Fatal("expected non-empty fingerprint set for single-k-gram stream")
	}
	if got := Jaccard(a, b); got != 1.0 {
		t.Errorf("identical short streams: Jaccard = %v, want 1.0", got)
	}
	reversed := make([]string, len(tokens))
	for i, tok := range tokens {
		reversed[len(tokens)-1-i] = tok
	}
	other := Generate(reversed, DefaultK, DefaultW)
	if got := Jaccard(a, other); got != 0.0 {
		t.Errorf("different short streams: Jaccard = %v, want 0.0", got)
	}
}

func TestGeneratePositional_ShortStreamMatchesGenerate(t *testing.T) {
	tokens := seqTokens(DefaultK + 1) // 2 k-grams < w
	plain := Generate(tokens, DefaultK, DefaultW)
	pos := GeneratePositional(tokens, DefaultK, DefaultW)
	if len(plain) != 1 || len(pos.Set) != 1 {
		t.Fatalf("expected exactly one fingerprint from short stream, got Generate=%d Positional=%d",
			len(plain), len(pos.Set))
	}
	for h := range plain {
		if _, ok := pos.Set[h]; !ok {
			t.Errorf("hash %d in Generate but missing from GeneratePositional", h)
		}
	}
	maxPos := len(tokens) - DefaultK
	for hash, positions := range pos.Positions {
		for _, p := range positions {
			if p < 0 || p > maxPos {
				t.Errorf("hash %d: position %d out of range [0, %d]", hash, p, maxPos)
			}
		}
	}
}

func TestJaccard(t *testing.T) {
	cases := []struct {
		name string
		a, b Set
		want float64
	}{
		{"identical sets", Set{1: {}, 2: {}, 3: {}}, Set{1: {}, 2: {}, 3: {}}, 1.0},
		{"disjoint sets", Set{1: {}, 2: {}}, Set{3: {}, 4: {}}, 0.0},
		{"partial overlap (2 of 4)", Set{1: {}, 2: {}, 3: {}}, Set{2: {}, 3: {}, 4: {}}, 0.5},
		{"both empty carries no evidence", Set{}, Set{}, 0.0},
		{"one empty", Set{1: {}, 2: {}}, Set{}, 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Jaccard(tc.a, tc.b); got != tc.want {
				t.Errorf("Jaccard(%v, %v) = %v; want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestGeneratePositional_SetMatchesGenerate(t *testing.T) {
	tokens := seqTokens(2 * DefaultK)
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
	tokens := seqTokens(2 * DefaultK)
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
	tokens := seqTokens(2 * DefaultK)
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
