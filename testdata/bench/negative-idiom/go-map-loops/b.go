package fixture

// invertTermIndex builds an inverted index over each document's term
// set: term -> posting list of document positions. Unrelated to issue
// grouping — set-membership input, position postings out — it merely
// shares the nested-range map-accumulator idiom.
func invertTermIndex(docs []*Document) map[string][]int {
	posts := make(map[string][]int, len(docs))
	for d, doc := range docs {
		for term := range doc.terms() {
			if term != "" {
				posts[term] = append((posts[term]), d)
			}
		}
	}
	return posts
}
