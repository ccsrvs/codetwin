// Fixture-driven pinning tests for file-level granularity (review §5.1).
//
// testdata/granularity holds two files that are near-duplicates as modules
// but whose individual functions are pairwise below the strong-clone band:
// module_a.go defines parseRecords + mergeCounts + formatSummary and
// module_b.go carries the same three functions reordered with light edits,
// plus an identical shared declaration block. The contract file mode adds:
// the module-level duplication scores >= 0.65 as one whole-file pair, while
// function mode sees three below-0.65 function pairs and no file pair.
package main_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/similarity"
)

var granularityFixtureFiles = []string{
	"../../testdata/granularity/module_a.go",
	"../../testdata/granularity/module_b.go",
}

// granularityPipeline runs the real scan → corpus → matrix pipeline on the
// fixture at the given granularity with default knobs (min-lines 3,
// min-confidence-lines 10, threshold 0.50) and returns the snippets plus
// every cross-snippet score.
func granularityPipeline(t *testing.T, gran scan.Granularity) ([]scan.Snippet, [][]float64) {
	t.Helper()
	snips, warns := scan.ProcessFiles(granularityFixtureFiles, 3, nil, cache.New(), "", gran, nil)
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	sort.Slice(snips, func(i, j int) bool { return snips[i].Name < snips[j].Name })

	streams := make([][]string, len(snips))
	for i, s := range snips {
		streams[i] = s.Tokens
	}
	corpus := similarity.NewCorpus(streams)
	vecs := make([]similarity.NormalizedVector, len(snips))
	for i, s := range snips {
		vecs[i] = similarity.Normalize(corpus.Vectorize(s.Tokens))
	}
	matrix, _, _ := similarity.BuildMatrix(snips, vecs, similarity.DefaultMinConfidenceLines, 0.50, nil)
	return snips, matrix
}

// isWholeFileName reports whether a snippet name is a bare path (the
// whole-file shape), as opposed to "path:start-end symbol".
func isWholeFileName(name string) bool {
	return strings.HasSuffix(name, ".go")
}

func TestGranularity_FileMode_ReportsModulePairAboveStrongBand(t *testing.T) {
	snips, matrix := granularityPipeline(t, scan.GranularityFile)

	if len(snips) != 2 {
		t.Fatalf("file mode: expected 2 whole-file snippets, got %d", len(snips))
	}
	for _, s := range snips {
		if !isWholeFileName(s.Name) {
			t.Errorf("file-mode snippet name should be the bare path, got %q", s.Name)
		}
	}
	score := matrix[0][1]
	if score < 0.65 {
		t.Errorf("file pair score = %.3f, want >= 0.65 (module-level duplication must land in the strong-clone band)", score)
	}
}

func TestGranularity_FunctionMode_ReportsFunctionPairsButNoFilePair(t *testing.T) {
	snips, matrix := granularityPipeline(t, scan.GranularityFunction)

	if len(snips) != 6 {
		t.Fatalf("function mode: expected 6 function snippets, got %d", len(snips))
	}
	for _, s := range snips {
		if isWholeFileName(s.Name) {
			t.Errorf("function mode must not produce a whole-file snippet, got %q", s.Name)
		}
	}

	var above []string
	for i := 0; i < len(snips); i++ {
		for j := i + 1; j < len(snips); j++ {
			score := matrix[i][j]
			if score < 0.50 {
				continue
			}
			above = append(above, snips[i].Name+" <-> "+snips[j].Name)
			if score >= 0.65 {
				t.Errorf("function pair %s <-> %s = %.3f, want < 0.65: no individual function pair may reach the band the file pair pins",
					snips[i].Name, snips[j].Name, score)
			}
		}
	}
	if len(above) != 3 {
		t.Errorf("function mode should report exactly the 3 counterpart pairs above 0.50, got %d:\n%s",
			len(above), strings.Join(above, "\n"))
	}
}
