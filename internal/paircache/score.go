// Package paircache defines the persisted exact-score contract shared by the
// similarity engine and cache backend.
package paircache

// Score contains every expensive, input-derived pair sub-score. Presentation
// metadata is rebuilt from current snippets on every run.
type Score struct {
	Structural      float64
	Semantic        float64
	Combined        float64
	Lexical         float64
	LexicalComputed bool
}

// DocumentKey identifies every input that can affect one snippet's pair
// score. It remains stable across snippet reordering.
type DocumentKey [32]byte

// Pair identifies an ordered pair of indices in Snapshot.Documents.
type Pair struct {
	A int
	B int
}

// Snapshot is the exact score state from one completed candidate run.
type Snapshot struct {
	Context   string
	Documents []DocumentKey
	Scores    map[Pair]Score
}

// Store is the minimal concurrent-safe persistence contract used by
// incremental similarity analysis.
type Store interface {
	LoadPairScoreSnapshot() Snapshot
	SavePairScoreSnapshot(snapshot Snapshot)
}
