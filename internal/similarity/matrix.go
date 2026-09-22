package similarity

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/paircache"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/splitter"
)

// materializationFloorMin is the absolute minimum materialization
// default floor, and materializationBand is how far below the user's
// --threshold the floor may reach. See MaterializationFloor.
const (
	materializationFloorMin = 0.30
	materializationBand     = 0.20
)

// BlockCandidateFloor is the combined-score floor of the "gray band"
// from which block-level partial-clone candidates are drawn (review
// §5.3): same-language pairs with nonzero structural evidence whose
// combined score lands in [BlockCandidateFloor, threshold). Pairs at
// or above the threshold already render as function-level findings;
// pairs below 0.20 share too little for a >= 8-line block to hide in
// (a shared block that big lifts even heavily diluted hosts above
// 0.20). The band sits below the materialization floor, so candidates
// are collected as index pairs inside BuildMatrix rather than read
// back from the returned pair slice.
const BlockCandidateFloor = 0.20

// MaterializationFloor returns the minimum combined score below which a
// pair is dropped from the materialized list: min(threshold, max(0.30, threshold−0.20)).
// The matrix still records the true value for every pair, so DBSCAN
// clustering is unaffected; the floor only bounds the memory footprint
// of the returned slice — on an O(n²) scan of a big repo, materializing
// every pair above a tiny constant floor is pure heap waste, since
// nothing below --threshold ever renders.
//
// The floor is threshold-aware rather than constant because --suggest
// deliberately looks up pairs across ALL materialized pairs (not just
// visible ones) so users can target a sub-threshold pair without
// re-tuning --threshold. Keeping a 0.20 band below threshold preserves
// that workflow for the near-misses it exists for, while dropping the
// long tail of unrelated pairs that nothing reads.
func MaterializationFloor(threshold float64) float64 {
	floor := threshold - materializationBand
	if floor < materializationFloorMin {
		floor = materializationFloorMin
	}
	if floor > threshold {
		floor = threshold
	}
	return floor
}

// MatrixOptions controls which scored pairs are retained for reporting.
type MatrixOptions struct {
	// IncludeWeakPairs lowers the materialization floor from
	// MaterializationFloor(threshold) to the 0.30 minimum (or to the
	// threshold itself when that is lower) for verbose reports. It does
	// not change matrix scores or block-candidate selection, and pairs
	// scoring exactly 0 are never materialized.
	IncludeWeakPairs bool

	// OnCandidates receives the number of pairs selected by candidate
	// retrieval and the total possible pair count after scoring completes.
	// BuildGraph reports the union of structural and semantic candidates;
	// BuildMatrix is exhaustive and therefore reports total, total.
	OnCandidates func(selected, total int64)

	// ScoreCache reuses exact scores whose complete scoring inputs are
	// unchanged. It affects performance only; cache misses run the normal
	// exact scorer. OnScoreCache reports cache hits and misses after the run.
	ScoreCache   paircache.Store
	OnScoreCache func(hits, misses int64)

	// ApproximateCandidates skips semantic scoring for pairs that share
	// neither a fingerprint nor one of each side's 16 heaviest TF-IDF
	// terms. It is lossy: cross-language pairs whose shared vocabulary is
	// spread over mid-weight terms are dropped even above threshold. On
	// real repositories it pruned under 10% of pairs and saved no time,
	// so it is off by default and meant only for corpora too large to
	// score exhaustively.
	ApproximateCandidates bool
}

// BuildGraph computes the similarity graph (exhaustive unless
// MatrixOptions.ApproximateCandidates is set), the
// materialized pair list above MaterializationFloor(threshold), and
// the block-candidate index pairs (same-language pairs in the gray
// band [BlockCandidateFloor, threshold) with nonzero structural
// evidence — see BlockCandidateFloor) in a single pass. threshold is
// the user's --threshold value. Work is sharded across
// runtime.NumCPU() goroutines using a stripe partition (worker w
// handles rows where i % numWorkers == w), which balances small-row
// and big-row work. The sparse graph synchronizes shared endpoint rows;
// each worker owns its pair and block-candidate buffers.
//
// Block candidates are indices into snippets ({i, j} with i < j),
// sorted, so downstream block detection is deterministic and the
// memory cost stays two ints per gray-band pair.
//
// onPairDone, if non-nil, is invoked as each possible pair is either scored
// or pruned, with the running visited count. It's called from worker
// goroutines, so it must be cheap and concurrent-safe.
func BuildGraph(
	snippets []scan.Snippet,
	vectors []NormalizedVector,
	minConfLines int,
	threshold float64,
	onPairDone func(done, total int64),
	options ...MatrixOptions,
) (MutableGraph, []report.Pair, [][2]int) {
	graph, pairs, blockCands, _ := BuildGraphContext(
		context.Background(), snippets, vectors, minConfLines, threshold, onPairDone, options...,
	)
	return graph, pairs, blockCands
}

// BuildGraphContext is BuildGraph with cooperative cancellation. A canceled
// run never publishes a partial pair-score snapshot to the incremental cache.
func BuildGraphContext(
	ctx context.Context,
	snippets []scan.Snippet,
	vectors []NormalizedVector,
	minConfLines int,
	threshold float64,
	onPairDone func(done, total int64),
	options ...MatrixOptions,
) (MutableGraph, []report.Pair, [][2]int, error) {
	builder := newCompactGraphBuilder(len(snippets))
	var semanticIndex *SemanticCandidateIndex
	for _, opt := range options {
		if opt.ApproximateCandidates {
			semanticIndex = NewSemanticCandidateIndex(vectors)
		}
	}
	pairs, blockCands, err := buildGraph(
		ctx, builder, snippets, vectors, semanticIndex, minConfLines, threshold, onPairDone, options...,
	)
	return builder.freeze(layoutAuto), pairs, blockCands, err
}

// scoreSink receives each scored pair. buildGraph calls SetScore(i, j)
// with i < j only from the worker that owns row i, in ascending j.
type scoreSink interface {
	SetScore(a, b int, score float64)
}

func buildDenseGraph(
	snippets []scan.Snippet,
	vectors []NormalizedVector,
	minConfLines int,
	threshold float64,
	onPairDone func(done, total int64),
	options ...MatrixOptions,
) (*DenseGraph, []report.Pair, [][2]int) {
	graph := NewDenseGraph(len(snippets))
	pairs, blockCands, _ := buildGraph(
		context.Background(), graph, snippets, vectors, nil, minConfLines, threshold, onPairDone, options...,
	)
	return graph, pairs, blockCands
}

func buildGraph(
	ctx context.Context,
	graph scoreSink,
	snippets []scan.Snippet,
	vectors []NormalizedVector,
	semanticIndex *SemanticCandidateIndex,
	minConfLines int,
	threshold float64,
	onPairDone func(done, total int64),
	options ...MatrixOptions,
) ([]report.Pair, [][2]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	floor := MaterializationFloor(threshold)
	var onCandidates func(selected, total int64)
	var scoreCache paircache.Store
	var onScoreCache func(hits, misses int64)
	for _, opt := range options {
		// Verbose bypasses the threshold-relative band but not the
		// absolute minimum: a verbose report of a large repo must not
		// materialize (and print) every one of its n² pairs.
		if opt.IncludeWeakPairs && floor > materializationFloorMin {
			floor = materializationFloorMin
		}
		if opt.OnCandidates != nil {
			onCandidates = opt.OnCandidates
		}
		if opt.ScoreCache != nil {
			scoreCache = opt.ScoreCache
		}
		if opt.OnScoreCache != nil {
			onScoreCache = opt.OnScoreCache
		}
	}
	n := len(snippets)

	totalPairs := int64(n) * int64(n-1) / 2
	if n < 2 {
		if onCandidates != nil {
			onCandidates(0, totalPairs)
		}
		if scoreCache != nil {
			documents := make([]paircache.DocumentKey, n)
			for i := range snippets {
				documents[i] = snippetScoreDigest(snippets[i], vectors[i])
			}
			scoreCache.SavePairScoreSnapshot(paircache.Snapshot{
				Context:   pairScoreCacheContext(minConfLines),
				Documents: documents,
				Scores:    map[paircache.Pair]paircache.Score{},
			})
		}
		if onScoreCache != nil {
			onScoreCache(0, 0)
		}
		return nil, nil, nil
	}
	var scoreDigests []scoreDigest
	var previousSnapshot paircache.Snapshot
	var previousIndices []int
	sameDocuments := false
	if scoreCache != nil {
		scoreDigests = make([]scoreDigest, n)
		for i := range snippets {
			scoreDigests[i] = snippetScoreDigest(snippets[i], vectors[i])
		}
		previousSnapshot = scoreCache.LoadPairScoreSnapshot()
		previousIndices = make([]int, n)
		for i := range previousIndices {
			previousIndices[i] = -1
		}
		if previousSnapshot.Context == pairScoreCacheContext(minConfLines) {
			oldByDocument := make(map[paircache.DocumentKey]int, len(previousSnapshot.Documents))
			for i, key := range previousSnapshot.Documents {
				if _, duplicate := oldByDocument[key]; duplicate {
					oldByDocument[key] = -1
				} else {
					oldByDocument[key] = i
				}
			}
			for i := range previousIndices {
				if old, ok := oldByDocument[scoreDigests[i]]; ok {
					previousIndices[i] = old
				}
			}
			sameDocuments = len(scoreDigests) == len(previousSnapshot.Documents)
			if sameDocuments {
				for i := range scoreDigests {
					if scoreDigests[i] != previousSnapshot.Documents[i] {
						sameDocuments = false
						break
					}
				}
			}
		}
	}

	hashIndex := buildHashIndex(snippets)

	workers := runtime.NumCPU()
	if workers > n {
		workers = n
	}
	if workers < 1 {
		workers = 1
	}

	var done atomic.Int64
	pairsByWorker := make([][]report.Pair, workers)
	blockCandsByWorker := make([][][2]int, workers)
	candidatesByWorker := make([]int64, workers)
	cacheHitsByWorker := make([]int64, workers)
	cacheMissesByWorker := make([]int64, workers)
	cacheScoresByWorker := make([]map[paircache.Pair]paircache.Score, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			var local []report.Pair
			var localBlockCands [][2]int
			var localCandidates int64
			var localCacheHits, localCacheMisses int64
			var localCacheScores map[paircache.Pair]paircache.Score
			if scoreCache != nil && !sameDocuments {
				localCacheScores = make(map[paircache.Pair]paircache.Score)
			}
			batchProgress := int64(0)
			for i := workerID; i < n; i += workers {
				if ctx.Err() != nil {
					break
				}
				// Structural candidates share a fingerprint. Semantic candidates
				// share at least one indexed high-weight TF-IDF term. Their union
				// bounds the work in candidate mode; exhaustive mode (used by
				// BuildMatrix as a compatibility/quality oracle) leaves
				// semanticIndex nil and scores every comparable pair.
				structuralCands := make(map[int]struct{})
				var candidates map[int]struct{}
				if semanticIndex != nil {
					candidates = make(map[int]struct{})
					semanticIndex.addCandidates(i, candidates)
				}
				for h := range snippets[i].Fps.Set {
					for _, k := range hashIndex[h] {
						if k > i {
							structuralCands[k] = struct{}{}
							if candidates != nil {
								candidates[k] = struct{}{}
							}
						}
					}
				}

				for j := i + 1; j < n; j++ {
					if ctx.Err() != nil {
						break
					}
					if candidates != nil {
						if _, ok := candidates[j]; !ok {
							batchProgress++
							continue
						}
					}
					localCandidates++
					// Suppress nesting false positives: an outer function
					// that happens to contain a closure / inner def is not
					// a duplicate of that closure. Leaving the matrix at 0
					// also keeps DBSCAN from clustering them.
					if chunksNestedSameFile(snippets[i], snippets[j]) {
						batchProgress++
						continue
					}
					// Suppress mixed-kind comparisons: class-span chunks
					// only score against other class chunks (see
					// ComparableKinds). Leaving the matrix at 0 keeps
					// DBSCAN and the block-candidate channel class-pure.
					if !ComparableKinds(snippets[i], snippets[j]) {
						batchProgress++
						continue
					}

					_, structuralCandidate := structuralCands[j]
					var scores paircache.Score
					if scoreCache != nil {
						oldA, oldB := previousIndices[i], previousIndices[j]
						var hit bool
						if oldA >= 0 && oldB >= 0 {
							if oldA > oldB {
								oldA, oldB = oldB, oldA
							}
							scores, hit = previousSnapshot.Scores[paircache.Pair{A: oldA, B: oldB}]
						}
						if hit {
							localCacheHits++
						} else {
							localCacheMisses++
							scores = exactPairScore(
								snippets[i], snippets[j], vectors[i], vectors[j], minConfLines, structuralCandidate)
							if localCacheScores == nil {
								localCacheScores = make(map[paircache.Pair]paircache.Score)
							}
						}
						if !sameDocuments || !hit {
							localCacheScores[paircache.Pair{A: i, B: j}] = scores
						}
					} else {
						scores = exactPairScore(
							snippets[i], snippets[j], vectors[i], vectors[j], minConfLines, structuralCandidate)
					}
					if scores.Combined > 0 {
						graph.SetScore(i, j, scores.Combined)
					}
					sameLang := snippets[i].Lang == snippets[j].Lang

					// Block-candidate gray band (review §5.3): the pair
					// itself won't render (below threshold), but a shared
					// sub-function block might hide inside it. Collected
					// here because the band reaches below the
					// materialization floor. Class-kind pairs are excluded
					// (checking one side suffices — the kind gate above
					// guarantees both sides match): every method inside a
					// container is emitted as its own function chunk and
					// participates in the block channel independently, so
					// container-level block detection only re-finds the
					// same text — and for Go struct+methodset groups the
					// joined non-contiguous Code would make the block
					// detector's chunk-relative line arithmetic report
					// ranges that don't exist in the source.
					if sameLang && scores.Structural > 0 &&
						snippets[i].Kind != splitter.KindClass &&
						scores.Combined >= BlockCandidateFloor && scores.Combined < threshold {
						localBlockCands = append(localBlockCands, [2]int{i, j})
					}

					batchProgress++
					// A pair scoring exactly 0 shares no fingerprint and
					// no vocabulary: it carries no evidence and is never a
					// "weak similarity", whatever the floor.
					if scores.Combined < floor || scores.Combined <= 0 {
						continue
					}
					local = append(local, report.Pair{
						ID:              report.PairID(snippets[i].Name, snippets[j].Name),
						NameA:           snippets[i].Name,
						NameB:           snippets[j].Name,
						Structural:      scores.Structural,
						Semantic:        scores.Semantic,
						Score:           scores.Combined,
						LinesA:          snippets[i].NonBlankLn,
						LinesB:          snippets[j].NonBlankLn,
						LangA:           string(snippets[i].Lang),
						LangB:           string(snippets[j].Lang),
						RepoA:           snippets[i].Repo,
						RepoB:           snippets[j].Repo,
						Lexical:         scores.Lexical,
						LexicalComputed: scores.LexicalComputed,
					})
				}
				// Flush progress in batches per row to avoid hammering the
				// atomic counter per inner-loop iteration.
				if batchProgress > 0 {
					d := done.Add(batchProgress)
					batchProgress = 0
					if onPairDone != nil {
						onPairDone(d, totalPairs)
					}
				}
			}
			pairsByWorker[workerID] = local
			blockCandsByWorker[workerID] = localBlockCands
			candidatesByWorker[workerID] = localCandidates
			cacheHitsByWorker[workerID] = localCacheHits
			cacheMissesByWorker[workerID] = localCacheMisses
			cacheScoresByWorker[workerID] = localCacheScores
		}(w)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if onCandidates != nil {
		var selected int64
		for _, count := range candidatesByWorker {
			selected += count
		}
		onCandidates(selected, totalPairs)
	}
	if scoreCache != nil {
		var misses int64
		for _, count := range cacheMissesByWorker {
			misses += count
		}
		if misses > 0 || !sameDocuments {
			var currentScores map[paircache.Pair]paircache.Score
			if sameDocuments {
				currentScores = make(map[paircache.Pair]paircache.Score, len(previousSnapshot.Scores))
				for key, score := range previousSnapshot.Scores {
					currentScores[key] = score
				}
			} else {
				currentScores = make(map[paircache.Pair]paircache.Score)
			}
			for _, workerScores := range cacheScoresByWorker {
				for key, score := range workerScores {
					currentScores[key] = score
				}
			}
			documents := append([]paircache.DocumentKey(nil), scoreDigests...)
			scoreCache.SavePairScoreSnapshot(paircache.Snapshot{
				Context:   pairScoreCacheContext(minConfLines),
				Documents: documents,
				Scores:    currentScores,
			})
		}
	}
	if onScoreCache != nil {
		var hits, misses int64
		for i := range cacheHitsByWorker {
			hits += cacheHitsByWorker[i]
			misses += cacheMissesByWorker[i]
		}
		onScoreCache(hits, misses)
	}

	total := 0
	for _, p := range pairsByWorker {
		total += len(p)
	}
	pairs := make([]report.Pair, 0, total)
	for _, p := range pairsByWorker {
		pairs = append(pairs, p...)
	}
	totalCands := 0
	for _, c := range blockCandsByWorker {
		totalCands += len(c)
	}
	blockCands := make([][2]int, 0, totalCands)
	for _, c := range blockCandsByWorker {
		blockCands = append(blockCands, c...)
	}
	// Workers finish in arbitrary stripe order; sort so block detection
	// (and therefore its report section) is deterministic across runs.
	sort.Slice(blockCands, func(x, y int) bool {
		if blockCands[x][0] != blockCands[y][0] {
			return blockCands[x][0] < blockCands[y][0]
		}
		return blockCands[x][1] < blockCands[y][1]
	})
	return pairs, blockCands, nil
}

func exactPairScore(
	a, b scan.Snippet,
	va, vb NormalizedVector,
	minConfLines int,
	structuralCandidate bool,
) paircache.Score {
	var structural float64
	if structuralCandidate {
		structural = fingerprint.Jaccard(a.Fps.Set, b.Fps.Set)
	}
	semantic := CosineFromNormalized(va, vb)
	// Unknown↔Unknown counts as the same language. This preserves the
	// corroboration cap and avoids giving two unclassified files the more
	// permissive cross-language blend.
	combined := CombinedForLangs(structural, semantic, a.Lang == b.Lang)
	combined = LengthDampen(combined, a.NonBlankLn, b.NonBlankLn, minConfLines)

	// The lexical sub-score is needed only by the structural-twin label gate.
	var lexical float64
	lexicalComputed := false
	if combined > report.StructuralTwinMinScore &&
		len(a.LexTerms) >= MinLexicalTerms &&
		len(b.LexTerms) >= MinLexicalTerms {
		lexical = LexicalJaccard(a.LexTerms, b.LexTerms)
		lexicalComputed = true
	}
	return paircache.Score{
		Structural:      structural,
		Semantic:        semantic,
		Combined:        combined,
		Lexical:         lexical,
		LexicalComputed: lexicalComputed,
	}
}

// BuildMatrix is the compatibility entry point for callers that still need
// the dense representation. New consumers should call BuildGraph and depend
// on the Graph interface instead.
func BuildMatrix(
	snippets []scan.Snippet,
	vectors []NormalizedVector,
	minConfLines int,
	threshold float64,
	onPairDone func(done, total int64),
	options ...MatrixOptions,
) ([][]float64, []report.Pair, [][2]int) {
	graph, pairs, blockCands := buildDenseGraph(
		snippets, vectors, minConfLines, threshold, onPairDone, options...,
	)
	return graph.Matrix(), pairs, blockCands
}

// buildHashIndex builds an inverted index from fingerprint hash → snippet
// indices that selected that hash. Lets BuildGraph and BuildMatrix skip
// Jaccard work for snippet pairs that share zero fingerprints (those
// structural scores would be 0 anyway, and on a typical big repo most
// pairs fall into this bucket).
func buildHashIndex(snippets []scan.Snippet) map[uint32][]int {
	idx := make(map[uint32][]int)
	for i, s := range snippets {
		for h := range s.Fps.Set {
			idx[h] = append(idx[h], i)
		}
	}
	return idx
}

// ComparableKinds reports whether two snippets sit at the same
// granularity and may therefore be scored against each other. Class
// chunks (splitter.KindClass) only compare against other class chunks:
// a class span weakly resembling a small function or method across
// files is container-vs-part dilution — the exact "washed out by
// unrelated code" noise the splitter exists to avoid — not a clone.
// Cross-file class↔class pairs are the §5.2 value-add and stay
// comparable. The zero Kind (snippets built before the field existed,
// or by tests) behaves as function-kind.
func ComparableKinds(a, b scan.Snippet) bool {
	return (a.Kind == splitter.KindClass) == (b.Kind == splitter.KindClass)
}

// chunksNestedSameFile reports whether two snippets come from the same
// file and one's [StartLine, EndLine] range fully contains the other's.
// Function-level chunks of an outer function and a closure defined inside
// it are necessarily token-overlapping; reporting them as a "100% match"
// is noise — they're not duplicates, the outer just contains the inner.
func chunksNestedSameFile(a, b scan.Snippet) bool {
	if a.Path == "" || b.Path == "" || a.Path != b.Path {
		return false
	}
	aContainsB := a.StartLine <= b.StartLine && a.EndLine >= b.EndLine
	bContainsA := b.StartLine <= a.StartLine && b.EndLine >= a.EndLine
	return aContainsB || bContainsA
}
