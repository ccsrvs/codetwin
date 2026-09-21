package bench

import (
	"sort"
	"strings"
	"testing"

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
