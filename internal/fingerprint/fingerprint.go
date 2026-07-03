// Package fingerprint implements the Winnowing algorithm for generating
// a compact set of hash fingerprints from a token stream.
//
// Winnowing selects the minimum hash within each sliding window, producing
// a fingerprint set that supports fast Jaccard-based similarity queries.
// Two files sharing many minimum-window hashes are structurally similar.
//
// Reference: Schleimer, Wilkerson, Aiken — "Winnowing: Local Algorithms for
// Document Fingerprinting", SIGMOD 2003.
package fingerprint

import "math/bits"

const (
	// DefaultK is the k-gram size. Tuned against the tokenizer's output
	// density: the tokenizer emits punctuation runes as individual tokens
	// (roughly doubling tokens per source line versus word-only streams),
	// so k must be large enough that a k-gram spans several words of
	// source. Too small and ubiquitous punctuation shapes like
	// `( VAR , VAR )` dominate the fingerprint sets, inflating Jaccard
	// similarity between unrelated functions.
	DefaultK = 10
	DefaultW = 4 // winnowing window size
)

// Set is the fingerprint of a document — a set of selected hashes.
type Set map[uint32]struct{}

// PositionalSet is a fingerprint Set augmented with the originating k-gram
// start positions of each selected hash. Multiple windows can select the same
// hash, so each entry maps a hash to the slice of token-positions where it
// was chosen. Use Set for plain Jaccard similarity; use Positions to find
// where a match occurs.
type PositionalSet struct {
	Set       Set
	Positions map[uint32][]int
	K         int // k-gram size used to build this set (each fingerprint covers K tokens)
}

// Generate builds a fingerprint Set from a token slice using Winnowing.
//
// Winnowing guarantees at least one fingerprint for any document that has at
// least one k-gram: when the hash sequence is shorter than the window size w,
// the whole sequence is treated as a single window.
func Generate(tokens []string, k, w int) Set {
	grams := kgrams(tokens, k)
	if len(grams) == 0 {
		return Set{}
	}

	hashes := make([]uint32, len(grams))
	for i, g := range grams {
		hashes[i] = hashString(g)
	}

	fps := Set{}
	if len(hashes) < w {
		fps[minHash(hashes)] = struct{}{}
		return fps
	}
	for i := 0; i <= len(hashes)-w; i++ {
		window := hashes[i : i+w]
		fps[minHash(window)] = struct{}{}
	}
	return fps
}

// GeneratePositional builds a PositionalSet using the same Winnowing
// algorithm as Generate. The token-position recorded for each fingerprint is
// the k-gram start index that produced the hash within the chosen window.
func GeneratePositional(tokens []string, k, w int) PositionalSet {
	grams := kgrams(tokens, k)
	if len(grams) == 0 {
		return PositionalSet{Set: Set{}, Positions: map[uint32][]int{}, K: k}
	}

	hashes := make([]uint32, len(grams))
	for i, g := range grams {
		hashes[i] = hashString(g)
	}

	fps := Set{}
	positions := map[uint32][]int{}
	if len(hashes) < w {
		h, off := minHashAt(hashes)
		fps[h] = struct{}{}
		positions[h] = append(positions[h], off)
		return PositionalSet{Set: fps, Positions: positions, K: k}
	}
	for i := 0; i <= len(hashes)-w; i++ {
		h, off := minHashAt(hashes[i : i+w])
		fps[h] = struct{}{}
		positions[h] = append(positions[h], i+off)
	}
	return PositionalSet{Set: fps, Positions: positions, K: k}
}

// MatchRange returns the inclusive token-position range [first, last] in a's
// coordinate system spanned by fingerprints whose hash also appears in b.
// Returns (-1, -1) when there are no shared hashes. The returned positions
// are k-gram starts; each match covers K tokens, so callers that need the
// full token-coverage of a fingerprint should extend last by (a.K - 1).
func MatchRange(a, b PositionalSet) (first, last int) {
	first, last = -1, -1
	for hash, positions := range a.Positions {
		if _, ok := b.Set[hash]; !ok {
			continue
		}
		for _, p := range positions {
			if first == -1 || p < first {
				first = p
			}
			if p > last {
				last = p
			}
		}
	}
	return
}

func minHashAt(window []uint32) (uint32, int) {
	m := window[0]
	idx := 0
	for i, v := range window[1:] {
		if v < m {
			m = v
			idx = i + 1
		}
	}
	return m, idx
}

// Jaccard returns the Jaccard similarity coefficient between two fingerprint sets.
// Returns 0 when either set is empty: an empty fingerprint set carries no
// evidence of similarity, and "vacuously identical" would report two
// unrelated tiny snippets as a perfect structural match.
func Jaccard(a, b Set) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}

	intersection := 0
	for h := range a {
		if _, ok := b[h]; ok {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// Hashes flattens a Set into a slice of its hash values. Iteration order is
// unspecified — callers that round-trip through cache storage rebuild the
// Set from this slice, so order doesn't matter.
func Hashes(s Set) []uint32 {
	out := make([]uint32, 0, len(s))
	for h := range s {
		out = append(out, h)
	}
	return out
}

// kgrams produces all consecutive k-grams from the token slice as joined strings.
func kgrams(tokens []string, k int) []string {
	if len(tokens) < k {
		return nil
	}
	grams := make([]string, 0, len(tokens)-k+1)
	for i := 0; i <= len(tokens)-k; i++ {
		g := ""
		for j := 0; j < k; j++ {
			if j > 0 {
				g += " "
			}
			g += tokens[i+j]
		}
		grams = append(grams, g)
	}
	return grams
}

// hashString is a fast, deterministic non-cryptographic hash (FNV-1a variant).
func hashString(s string) uint32 {
	const prime = 16777619
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h = bits.RotateLeft32(h*prime, 5)
	}
	return h
}

func minHash(window []uint32) uint32 {
	m, _ := minHashAt(window)
	return m
}
