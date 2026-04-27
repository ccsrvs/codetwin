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
	DefaultK = 5 // k-gram size
	DefaultW = 4 // window size
)

// Set is the fingerprint of a document — a set of selected hashes.
type Set map[uint32]struct{}

// Generate builds a fingerprint Set from a token slice using Winnowing.
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
	for i := 0; i <= len(hashes)-w; i++ {
		window := hashes[i : i+w]
		fps[minHash(window)] = struct{}{}
	}
	return fps
}

// Jaccard returns the Jaccard similarity coefficient between two fingerprint sets.
// Returns 1.0 when both sets are empty (vacuously equal).
func Jaccard(a, b Set) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
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
	m := window[0]
	for _, v := range window[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
