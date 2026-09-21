package similarity

import "sort"

// semanticCandidateTerms bounds the number of high-signal postings emitted
// per document. Candidate retrieval may be approximate; final scoring is not.
const semanticCandidateTerms = 16

// SemanticCandidateIndex is an inverted index over each normalized sparse
// TF-IDF vector's highest-weight terms. It is used only for retrieval; every
// selected pair still receives CodeTwin's exact cosine and combined scores.
type SemanticCandidateIndex struct {
	terms    [][]string
	postings map[string][]int
}

// NewSemanticCandidateIndex builds deterministic term postings. Terms rank by
// descending TF-IDF weight with lexical tie-breaking; document indices are
// appended in ascending order.
func NewSemanticCandidateIndex(vectors []NormalizedVector) *SemanticCandidateIndex {
	index := &SemanticCandidateIndex{
		terms:    make([][]string, len(vectors)),
		postings: make(map[string][]int),
	}
	for document, vector := range vectors {
		terms := append([]string(nil), vector.Keys...)
		sort.Slice(terms, func(i, j int) bool {
			left, right := vector.V[terms[i]], vector.V[terms[j]]
			if left != right {
				return left > right
			}
			return terms[i] < terms[j]
		})
		if len(terms) > semanticCandidateTerms {
			terms = terms[:semanticCandidateTerms]
		}
		index.terms[document] = terms
		for _, term := range terms {
			index.postings[term] = append(index.postings[term], document)
		}
	}
	return index
}

func (index *SemanticCandidateIndex) addCandidates(document int, candidates map[int]struct{}) {
	for _, term := range index.terms[document] {
		for _, other := range index.postings[term] {
			if other > document {
				candidates[other] = struct{}{}
			}
		}
	}
}

// Candidates returns ascending, unique indices greater than document that
// share at least one semantic term with it.
func (index *SemanticCandidateIndex) Candidates(document int) []int {
	set := make(map[int]struct{})
	index.addCandidates(document, set)
	candidates := make([]int, 0, len(set))
	for other := range set {
		candidates = append(candidates, other)
	}
	sort.Ints(candidates)
	return candidates
}
