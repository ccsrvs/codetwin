// Package similarity computes semantic similarity between code snippets
// using TF-IDF weighted token vectors and cosine similarity.
//
// This is the "fuzzy" layer: it catches functionally similar code even when
// structure differs, by comparing the vocabulary distribution of token streams.
package similarity

import "math"

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
