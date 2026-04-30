package similarity

import (
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
)

// PairNoiseFloor is the minimum combined score below which a pair is
// dropped from the materialized list. The matrix still records the true
// value so DBSCAN clustering is unaffected; this only bounds the memory
// footprint of the returned slice on big repos. 0.05 is well below any
// user-visible threshold.
const PairNoiseFloor = 0.05

// BuildMatrix computes the all-pairs similarity matrix and the
// materialized pair list above PairNoiseFloor in a single pass. Work is
// sharded across runtime.NumCPU() goroutines using a stripe partition
// (worker w handles rows where i % numWorkers == w), which balances
// small-row and big-row work. Each worker writes to its own pair buffer
// and to disjoint matrix cells, so no synchronization is needed beyond
// the final WaitGroup join.
//
// onPairDone, if non-nil, is invoked after each comparison with the
// running done count. It's called from worker goroutines, so it must
// be cheap and concurrent-safe.
func BuildMatrix(
	snippets []scan.Snippet,
	vectors []NormalizedVector,
	minConfLines int,
	onPairDone func(done, total int64),
) ([][]float64, []report.Pair) {
	n := len(snippets)
	matrix := make([][]float64, n)
	for i := range matrix {
		matrix[i] = make([]float64, n)
		matrix[i][i] = 1.0
	}

	totalPairs := int64(n) * int64(n-1) / 2
	if n < 2 {
		return matrix, nil
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
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			var local []report.Pair
			batchProgress := int64(0)
			for i := workerID; i < n; i += workers {
				// Candidates: any j > i that shares a fingerprint with i.
				// Pairs not in this set get structural=0 without paying for
				// a Jaccard call. We still compute cosine for every pair so
				// cross-language semantic-only matches still surface.
				cands := make(map[int]struct{})
				for h := range snippets[i].Fps.Set {
					for _, k := range hashIndex[h] {
						if k > i {
							cands[k] = struct{}{}
						}
					}
				}

				for j := i + 1; j < n; j++ {
					// Suppress nesting false positives: an outer function
					// that happens to contain a closure / inner def is not
					// a duplicate of that closure. Leaving the matrix at 0
					// also keeps DBSCAN from clustering them.
					if chunksNestedSameFile(snippets[i], snippets[j]) {
						batchProgress++
						continue
					}

					var structural float64
					if _, ok := cands[j]; ok {
						structural = fingerprint.Jaccard(snippets[i].Fps.Set, snippets[j].Fps.Set)
					}
					semantic := CosineFromNormalized(vectors[i], vectors[j])
					combined := Combined(structural, semantic, 0.5)
					// Length-aware confidence: dampen short-snippet matches
					// before they reach the matrix so DBSCAN sees the same
					// view of the world the report does. structural and
					// semantic stay raw — only the combined score is
					// adjusted, since that's what feeds clustering and
					// thresholding.
					combined = LengthDampen(
						combined, snippets[i].NonBlankLn, snippets[j].NonBlankLn, minConfLines)
					matrix[i][j] = combined
					matrix[j][i] = combined

					batchProgress++
					if combined < PairNoiseFloor {
						continue
					}
					local = append(local, report.Pair{
						NameA:      snippets[i].Name,
						NameB:      snippets[j].Name,
						Structural: structural,
						Semantic:   semantic,
						Score:      combined,
						LinesA:     snippets[i].NonBlankLn,
						LinesB:     snippets[j].NonBlankLn,
						LangA:      string(snippets[i].Lang),
						LangB:      string(snippets[j].Lang),
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
		}(w)
	}
	wg.Wait()

	total := 0
	for _, p := range pairsByWorker {
		total += len(p)
	}
	pairs := make([]report.Pair, 0, total)
	for _, p := range pairsByWorker {
		pairs = append(pairs, p...)
	}
	return matrix, pairs
}

// buildHashIndex builds an inverted index from fingerprint hash → snippet
// indices that selected that hash. Lets BuildMatrix skip Jaccard work
// for snippet pairs that share zero fingerprints (those structural
// scores would be 0 anyway, and on a typical big repo most pairs fall
// into this bucket).
func buildHashIndex(snippets []scan.Snippet) map[uint32][]int {
	idx := make(map[uint32][]int)
	for i, s := range snippets {
		for h := range s.Fps.Set {
			idx[h] = append(idx[h], i)
		}
	}
	return idx
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
