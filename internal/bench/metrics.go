// Package bench provides reusable detection-quality metrics for CodeTwin's
// labeled ground-truth corpus.
package bench

// Observation is one labeled detection decision.
type Observation struct {
	Name          string
	Category      string
	Expected      bool
	Detected      bool
	CrossLanguage bool
}

// Metrics is a binary-classification confusion matrix.
type Metrics struct {
	TruePositives  int
	FalsePositives int
	TrueNegatives  int
	FalseNegatives int
}

// Precision returns TP/(TP+FP). A corpus with no predicted positives has
// perfect precision because it produced no false-positive findings.
func (m Metrics) Precision() float64 {
	denominator := m.TruePositives + m.FalsePositives
	if denominator == 0 {
		return 1
	}
	return float64(m.TruePositives) / float64(denominator)
}

// Recall returns TP/(TP+FN). A corpus with no expected positives has perfect
// recall because there was nothing to miss.
func (m Metrics) Recall() float64 {
	denominator := m.TruePositives + m.FalseNegatives
	if denominator == 0 {
		return 1
	}
	return float64(m.TruePositives) / float64(denominator)
}

// F1 returns the harmonic mean of precision and recall.
func (m Metrics) F1() float64 {
	precision, recall := m.Precision(), m.Recall()
	if precision+recall == 0 {
		return 0
	}
	return 2 * precision * recall / (precision + recall)
}

func (m *Metrics) add(observation Observation) {
	switch {
	case observation.Expected && observation.Detected:
		m.TruePositives++
	case observation.Expected:
		m.FalseNegatives++
	case observation.Detected:
		m.FalsePositives++
	default:
		m.TrueNegatives++
	}
}

// QualityReport contains aggregate, category, and cross-language metrics.
type QualityReport struct {
	Overall       Metrics
	CrossLanguage Metrics
	ByCategory    map[string]Metrics
}

// Evaluate calculates a deterministic report over labeled observations.
func Evaluate(observations []Observation) QualityReport {
	report := QualityReport{ByCategory: make(map[string]Metrics)}
	for _, observation := range observations {
		report.Overall.add(observation)
		category := report.ByCategory[observation.Category]
		category.add(observation)
		report.ByCategory[observation.Category] = category
		if observation.CrossLanguage {
			report.CrossLanguage.add(observation)
		}
	}
	return report
}
