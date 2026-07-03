// Package similarity computes semantic similarity between code snippets
// using TF-IDF weighted token vectors and cosine similarity.
//
// This is the "fuzzy" layer: it catches functionally similar code even when
// structure differs, by comparing the vocabulary distribution of token streams.
package similarity

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Vector is a sparse TF-IDF weighted term vector.
type Vector map[string]float64

// Corpus holds the IDF weights computed from a collection of token streams.
type Corpus struct {
	idf map[string]float64
	n   int
}

// semanticNgram is the term width for the TF-IDF layer. Normalized
// token streams have almost no vocabulary — identifiers are all VAR,
// literals are STR/NUM — so unigram histograms of any two functions
// look alike (unrelated code routinely scores cosine 0.7–0.98, which
// is where report noise comes from). Short n-grams restore sequence
// information while staying far fuzzier than the k=14 winnowing
// fingerprints, so the semantic layer still catches reordered or
// partially rewritten logic.
const semanticNgram = 3

// crossLangCanon maps language-specific keywords onto shared canonical
// tokens so the semantic n-grams of equivalent logic line up across
// languages (`func`/`def`/`fn` all open a function; `nil`/`None`/`null`
// are the same concept). Applied only to the semantic stream — the
// structural fingerprints keep the raw tokens.
var crossLangCanon = map[string]string{
	"func": "FN", "def": "FN", "fn": "FN", "function": "FN", "defp": "FN",
	"elif": "ELIF", "elsif": "ELIF",
	"nil": "NIL", "None": "NIL", "null": "NIL", "undefined": "NIL",
	"True": "true", "False": "false",
	"raise": "THROW", "throw": "THROW", "panic": "THROW",
	"except": "CATCH", "catch": "CATCH", "rescue": "CATCH",
	"range": "in", // Go's `for … := range xs` ≈ Python's `for … in xs`
	"let":   "VARDECL", "var": "VARDECL", "const": "VARDECL",
}

// semanticStream filters and canonicalizes a token stream for the
// TF-IDF layer: punctuation tokens are dropped (pure syntax, different
// in every language) and keywords collapse to cross-language canonical
// forms. The structural layer sees none of this.
func semanticStream(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if !containsWordRune(t) {
			continue // punctuation-only token
		}
		if c, ok := crossLangCanon[t]; ok {
			t = c
		}
		out = append(out, t)
	}
	return out
}

func containsWordRune(s string) bool {
	for _, r := range s {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// semanticTerms converts a token stream into the terms the TF-IDF
// layer operates on: overlapping n-grams (joined with '\x00') over the
// canonicalized, punctuation-free semantic stream. Streams shorter
// than the n-gram width collapse to a single whole-stream term.
func semanticTerms(tokens []string) []string {
	tokens = semanticStream(tokens)
	if len(tokens) == 0 {
		return nil
	}
	if len(tokens) < semanticNgram {
		return []string{strings.Join(tokens, "\x00")}
	}
	terms := make([]string, 0, len(tokens)-semanticNgram+1)
	for i := 0; i+semanticNgram <= len(tokens); i++ {
		terms = append(terms, strings.Join(tokens[i:i+semanticNgram], "\x00"))
	}
	return terms
}

// NewCorpus builds IDF weights from a collection of token streams.
// Call this once on all snippets, then use Vectorize per snippet.
func NewCorpus(tokenStreams [][]string) *Corpus {
	n := len(tokenStreams)
	df := make(map[string]int)
	for _, tokens := range tokenStreams {
		seen := make(map[string]bool)
		for _, t := range semanticTerms(tokens) {
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

// semanticMinTerms is the minimum n-gram term count for a snippet to
// produce a semantic vector at all. A vector with one or two terms
// makes cosine a coin flip — two trivial snippets that happen to share
// their only term score a perfect 1.0. Below this floor the semantic
// layer reports "no evidence" (empty vector → cosine 0) and the
// structural layer alone decides.
const semanticMinTerms = 4

// Vectorize produces a TF-IDF vector for a single token stream.
func (c *Corpus) Vectorize(tokens []string) Vector {
	terms := semanticTerms(tokens)
	if len(terms) < semanticMinTerms {
		return Vector{}
	}

	tf := make(map[string]float64)
	for _, t := range terms {
		tf[t]++
	}

	vec := make(Vector, len(tf))
	for term, count := range tf {
		idfVal := c.idf[term]
		if idfVal == 0 {
			idfVal = 1.0 // unseen term: treat as IDF=1
		}
		// Sublinear TF: a boilerplate n-gram repeated five times is
		// evidence of one shared idiom, not five shared behaviours.
		// Raw counts let repeated idioms dominate the vector mass and
		// pull unrelated functions together.
		vec[term] = (1 + math.Log(count)) * idfVal
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

// CrossLangStructuralWeight is the structural weight for pairs whose
// snippets are in different languages. Winnowing fingerprints hash raw
// keyword sequences, so structurally identical logic in two languages
// shares almost no fingerprints — an even blend would cap every
// cross-language pair near 0.5 no matter how similar the logic. The
// canonicalized semantic layer is the reliable cross-language signal,
// so it carries most of the weight.
const CrossLangStructuralWeight = 0.2

// CombinedForLangs blends structural and semantic scores with
// language-aware weights: an even split when both snippets are the
// same language, semantic-dominant when they differ.
func CombinedForLangs(structural, semantic float64, sameLang bool) float64 {
	if sameLang {
		return Combined(structural, semantic, 0.5)
	}
	return Combined(structural, semantic, CrossLangStructuralWeight)
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
