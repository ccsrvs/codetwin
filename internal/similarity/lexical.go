package similarity

// MinLexicalTerms is the evidence floor for the lexical sub-score: a
// pair is only *measured* (Pair.LexicalComputed) when both snippets
// carry at least this many lexical terms. Jaccard over a five-element
// set moves in 0.1+ steps — one incidentally shared word flips the
// verdict — so tiny vocabularies can't support a content judgment in
// either direction. Same rationale as semanticMinTerms for the cosine
// layer. Unmeasured pairs fall through to the ordinary score bands
// (including the exact-clone length gate), never to structural twin.
const MinLexicalTerms = 8

// LexicalJaccard computes Jaccard similarity between two sorted,
// deduplicated lexical term sets (scan.Snippet.LexTerms, produced by
// tokenizer.LexicalTerms). A linear merge over the sorted inputs — no
// allocation, O(len(a)+len(b)).
//
// Returns 0 when either set is empty. Callers deciding whether the
// measurement is *meaningful* gate on MinLexicalTerms rather than on
// the returned value, so sparse vocabularies read as "not computed"
// instead of "content-divergent".
func LexicalJaccard(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	i, j, inter := 0, 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			inter++
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}
