// Package similarity computes semantic similarity between code snippets
// using TF-IDF weighted token vectors and cosine similarity.
//
// This is the "fuzzy" layer: it catches functionally similar code even when
// structure differs, by comparing the vocabulary distribution of token streams.
package similarity

import (
	"math"
	"sort"
)

// Vector is a sparse TF-IDF weighted term vector.
type Vector map[string]float64

// Corpus holds the IDF weights computed from a collection of token streams.
type Corpus struct {
	idf map[string]float64
	n   int
}

// NewCorpus builds IDF weights from a collection of token streams.
// Call this once on all snippets, then use Vectorize per snippet.
func NewCorpus(tokenStreams [][]string) *Corpus {
	n := len(tokenStreams)
	df := make(map[string]int)
	for _, tokens := range tokenStreams {
		seen := make(map[string]bool)
		for _, t := range tokens {
			if !seen[t] {
				df[t]++
				seen[t] = true
			}
		}
	}

	idf := make(map[string]float64, len(df))
	for term, freq := range df {
		// Smoothed IDF to avoid zero-division and dampen very common tokens
		idf[term] = math.Log(float64(n+1)/float64(freq+1)) + 1.0
	}

	return &Corpus{idf: idf, n: n}
}

// Vectorize produces a TF-IDF vector for a single token stream.
func (c *Corpus) Vectorize(tokens []string) Vector {
	if len(tokens) == 0 {
		return Vector{}
	}

	tf := make(map[string]float64)
	for _, t := range tokens {
		tf[t]++
	}

	vec := make(Vector, len(tf))
	for term, count := range tf {
		idfVal := c.idf[term]
		if idfVal == 0 {
			idfVal = 1.0 // unseen term: treat as IDF=1
		}
		vec[term] = (count / float64(len(tokens))) * idfVal
	}
	return vec
}

// NormalizedVector pairs a Vector with its precomputed L2 norm and a sorted
// key list. Cached norms cut O(n²) re-computation; the sorted keys ensure
// the inner-loop dot product iterates terms in a deterministic order so
// floating-point sums (and thus pair scores) are bit-identical across
// runs. Without that, Go's randomized map iteration would produce slightly
// different scores each run, which a stable sort would then order
// differently when many pairs tie at the displayed precision.
type NormalizedVector struct {
	V    Vector
	Keys []string // sorted keys of V, for deterministic iteration
	Norm float64  // sqrt(sum of squares)
}

// Normalize precomputes the L2 norm and sorted key list so subsequent
// CosineFromNormalized calls don't redo either. The norm itself is summed
// in sorted-key order so the same vector always yields the same norm bit-
// for-bit.
func Normalize(v Vector) NormalizedVector {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sumSq float64
	for _, k := range keys {
		val := v[k]
		sumSq += val * val
	}
	return NormalizedVector{V: v, Keys: keys, Norm: math.Sqrt(sumSq)}
}

// CosineFromNormalized computes cosine similarity using precomputed norms,
// iterating the smaller vector's sorted keys so the dot product is
// deterministic.
func CosineFromNormalized(a, b NormalizedVector) float64 {
	if a.Norm == 0 || b.Norm == 0 || len(a.V) == 0 || len(b.V) == 0 {
		return 0
	}
	smallKeys, small, large := a.Keys, a.V, b.V
	if len(b.V) < len(a.V) {
		smallKeys, small, large = b.Keys, b.V, a.V
	}
	var dot float64
	for _, term := range smallKeys {
		if vb, ok := large[term]; ok {
			dot += small[term] * vb
		}
	}
	return dot / (a.Norm * b.Norm)
}

// Cosine returns the cosine similarity between two vectors in [0, 1].
// Returns 0 if either vector is the zero vector.
func Cosine(a, b Vector) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for term, va := range a {
		normA += va * va
		if vb, ok := b[term]; ok {
			dot += va * vb
		}
	}
	for _, vb := range b {
		normB += vb * vb
	}

	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// Combined returns a weighted blend of structural (Jaccard) and
// semantic (cosine) similarity scores.
func Combined(structural, semantic, structuralWeight float64) float64 {
	semanticWeight := 1.0 - structuralWeight
	return structural*structuralWeight + semantic*semanticWeight
}

// LengthDampen scales a similarity score down when the smaller snippet
// has fewer than `threshold` non-blank lines. The multiplier ramps
// linearly from 0.5 at 0 lines to 1.0 at `threshold` lines, then stays
// at 1.0. A `threshold` of 0 (or non-positive line counts) returns the
// score unchanged.
//
// Rationale: short snippets share their entire shape by necessity (API
// surface, language grammar). A 100% match on two 5-line functions
// carries less evidence than the same match on two 25-line functions;
// the dampener encodes that confidence into the score so downstream
// consumers (the report, DBSCAN clustering) see a consistent view.
func LengthDampen(score float64, linesA, linesB, threshold int) float64 {
	if threshold <= 0 {
		return score
	}
	minLn := linesA
	if linesB < minLn {
		minLn = linesB
	}
	if minLn <= 0 {
		return score
	}
	if minLn >= threshold {
		return score
	}
	mult := 0.5 + 0.5*float64(minLn)/float64(threshold)
	return score * mult
}
