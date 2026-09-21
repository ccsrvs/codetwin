package bench

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/similarity"
)

const (
	qualityPrecisionMin       = 0.95
	qualityRecallMin          = 0.95
	qualityF1Min              = 0.95
	crossLanguageRecallMin    = 1.00
	qualityRepresentativeStep = 10
)

type loadedQualityCase struct {
	benchCase
	a, b []scan.Snippet
}

func qualityObservations(t *testing.T) []Observation {
	t.Helper()
	cases := collectCases(t)
	loaded := make([]loadedQualityCase, 0, len(cases))
	var streams [][]string
	for _, c := range cases {
		caseMin := minLines
		if c.shortNegative {
			caseMin = shortNegativeMinLines
		}
		a, b := caseSnippets(t, c.dir, caseMin)
		loaded = append(loaded, loadedQualityCase{benchCase: c, a: a, b: b})
		for _, snippet := range append(append([]scan.Snippet{}, a...), b...) {
			streams = append(streams, snippet.Tokens)
		}
	}
	corpus := similarity.NewCorpus(streams)
	vector := func(snippet scan.Snippet) similarity.NormalizedVector {
		return similarity.Normalize(corpus.Vectorize(snippet.Tokens))
	}

	observations := make([]Observation, 0, len(loaded))
	for _, c := range loaded {
		bestScore := 0.0
		crossLanguage := false
		for _, a := range c.a {
			va := vector(a)
			for _, b := range c.b {
				if !similarity.ComparableKinds(a, b) {
					continue
				}
				score := pairScore(a, b, va, vector(b)).combined
				score = similarity.LengthDampen(
					score, a.NonBlankLn, b.NonBlankLn, similarity.DefaultMinConfidenceLines)
				if score > bestScore {
					bestScore = score
					crossLanguage = a.Lang != b.Lang
				}
			}
		}
		category := strings.SplitN(c.name, "/", 2)[0]
		if c.twin {
			category = "structural-twin"
		}
		observations = append(observations, Observation{
			Name:          c.name,
			Category:      category,
			Expected:      c.positive || c.twin,
			Detected:      bestScore >= defaultThreshold,
			CrossLanguage: crossLanguage,
		})
	}
	sort.Slice(observations, func(i, j int) bool { return observations[i].Name < observations[j].Name })
	return observations
}

func assertQualityGate(t *testing.T, name string, report QualityReport) {
	t.Helper()
	t.Logf("%s: TP=%d FP=%d TN=%d FN=%d precision=%.3f recall=%.3f F1=%.3f",
		name,
		report.Overall.TruePositives, report.Overall.FalsePositives,
		report.Overall.TrueNegatives, report.Overall.FalseNegatives,
		report.Overall.Precision(), report.Overall.Recall(), report.Overall.F1())
	if report.Overall.Precision() < qualityPrecisionMin {
		t.Errorf("%s precision %.3f < %.3f", name, report.Overall.Precision(), qualityPrecisionMin)
	}
	if report.Overall.Recall() < qualityRecallMin {
		t.Errorf("%s recall %.3f < %.3f", name, report.Overall.Recall(), qualityRecallMin)
	}
	if report.Overall.F1() < qualityF1Min {
		t.Errorf("%s F1 %.3f < %.3f", name, report.Overall.F1(), qualityF1Min)
	}
}

func TestBench_QualityMetricsGate(t *testing.T) {
	observations := qualityObservations(t)
	if len(observations) < qualityRepresentativeStep {
		t.Fatalf("ground-truth corpus has only %d cases; want at least %d", len(observations), qualityRepresentativeStep)
	}
	report := Evaluate(observations)
	assertQualityGate(t, "full corpus", report)
	for _, observation := range observations {
		if observation.Expected != observation.Detected {
			t.Logf("misclassified %s: expected=%v detected=%v category=%s",
				observation.Name, observation.Expected, observation.Detected, observation.Category)
		}
	}
	if recall := report.CrossLanguage.Recall(); recall < crossLanguageRecallMin {
		t.Errorf("cross-language recall %.3f < %.3f", recall, crossLanguageRecallMin)
	}
	for category, metrics := range report.ByCategory {
		t.Logf("category %-18s TP=%d FP=%d TN=%d FN=%d",
			category, metrics.TruePositives, metrics.FalsePositives,
			metrics.TrueNegatives, metrics.FalseNegatives)
	}

	// Report quality at deterministic corpus-size checkpoints. Each prefix is
	// a reproducible labeled slice; the full-corpus gate above remains the
	// release criterion.
	for size := qualityRepresentativeStep; size < len(observations); size += qualityRepresentativeStep {
		checkpoint := Evaluate(observations[:size])
		t.Logf("%d-case checkpoint: precision=%.3f recall=%.3f F1=%.3f",
			size, checkpoint.Overall.Precision(), checkpoint.Overall.Recall(), checkpoint.Overall.F1())
	}
}

func TestBench_SemanticCandidatesMatchExhaustive(t *testing.T) {
	var snippets []scan.Snippet
	for _, c := range collectCases(t) {
		caseMin := minLines
		if c.shortNegative {
			caseMin = shortNegativeMinLines
		}
		a, b := caseSnippets(t, c.dir, caseMin)
		snippets = append(snippets, a...)
		snippets = append(snippets, b...)
	}

	streams := make([][]string, len(snippets))
	for i := range snippets {
		streams[i] = snippets[i].Tokens
	}
	corpus := similarity.NewCorpus(streams)
	vectors := make([]similarity.NormalizedVector, len(snippets))
	for i := range snippets {
		vectors[i] = similarity.Normalize(corpus.Vectorize(snippets[i].Tokens))
	}

	matrix, wantPairs, wantBlocks := similarity.BuildMatrix(
		snippets, vectors, similarity.DefaultMinConfidenceLines, defaultThreshold, nil,
	)
	var selected, total int64
	scoreState := cache.New()
	var scoreHits, scoreMisses int64
	graph, gotPairs, gotBlocks := similarity.BuildGraph(
		snippets, vectors, similarity.DefaultMinConfidenceLines, defaultThreshold, nil,
		similarity.MatrixOptions{
			ScoreCache: scoreState,
			OnCandidates: func(gotSelected, gotTotal int64) {
				selected, total = gotSelected, gotTotal
			},
			OnScoreCache: func(hits, misses int64) {
				scoreHits, scoreMisses = hits, misses
			},
		},
	)

	if !reflect.DeepEqual(gotPairs, wantPairs) {
		t.Errorf("candidate pair findings differ from exhaustive findings")
	}
	if !reflect.DeepEqual(gotBlocks, wantBlocks) {
		t.Errorf("candidate block findings differ from exhaustive findings")
	}
	for i := range matrix {
		for j := i + 1; j < len(matrix[i]); j++ {
			got, want := graph.Score(i, j), matrix[i][j]
			if (got >= defaultThreshold || want >= defaultThreshold) && got != want {
				t.Fatalf("candidate threshold score (%d,%d) = %v, exhaustive = %v", i, j, got, want)
			}
		}
	}
	if total > 0 && selected >= total {
		t.Errorf("candidate retrieval selected %d/%d pairs; want measurable pruning", selected, total)
	}
	if scoreHits != 0 || scoreMisses == 0 {
		t.Errorf("cold score cache = %d hits/%d misses, want 0/>0", scoreHits, scoreMisses)
	}
	var warmHits, warmMisses int64
	warmGraph, warmPairs, warmBlocks := similarity.BuildGraph(
		snippets, vectors, similarity.DefaultMinConfidenceLines, defaultThreshold, nil,
		similarity.MatrixOptions{
			ScoreCache: scoreState,
			OnScoreCache: func(hits, misses int64) {
				warmHits, warmMisses = hits, misses
			},
		},
	)
	if warmHits != scoreMisses || warmMisses != 0 {
		t.Errorf("warm score cache = %d hits/%d misses, want %d/0", warmHits, warmMisses, scoreMisses)
	}
	if !reflect.DeepEqual(warmPairs, wantPairs) || !reflect.DeepEqual(warmBlocks, wantBlocks) {
		t.Error("warm incremental findings differ from exhaustive findings")
	}
	for i := range matrix {
		for j := i + 1; j < len(matrix[i]); j++ {
			if got, want := warmGraph.Score(i, j), graph.Score(i, j); got != want {
				t.Fatalf("warm incremental score (%d,%d) = %v, cold candidate = %v", i, j, got, want)
			}
		}
	}
	pruned := 0.0
	if total > 0 {
		pruned = 100 * (1 - float64(selected)/float64(total))
	}
	t.Logf("semantic candidates selected %d/%d pairs (%.1f%% pruned) with exhaustive finding equivalence",
		selected, total, pruned)
}
