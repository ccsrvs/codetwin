package bench

import (
	"math"
	"testing"
)

func TestEvaluateQualityMetrics(t *testing.T) {
	observations := []Observation{
		{Name: "same-language-hit", Category: "positive", Expected: true, Detected: true},
		{Name: "cross-language-miss", Category: "positive", Expected: true, CrossLanguage: true},
		{Name: "idiom-false-positive", Category: "negative-idiom", Detected: true},
		{Name: "clean-negative", Category: "negative"},
	}

	report := Evaluate(observations)
	if report.Overall != (Metrics{TruePositives: 1, FalsePositives: 1, TrueNegatives: 1, FalseNegatives: 1}) {
		t.Errorf("overall = %+v", report.Overall)
	}
	if got := report.Overall.Precision(); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("precision = %v, want 0.5", got)
	}
	if got := report.Overall.Recall(); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("recall = %v, want 0.5", got)
	}
	if got := report.Overall.F1(); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("F1 = %v, want 0.5", got)
	}
	if got := report.CrossLanguage.Recall(); got != 0 {
		t.Errorf("cross-language recall = %v, want 0", got)
	}
	if got := report.ByCategory["negative-idiom"].FalsePositives; got != 1 {
		t.Errorf("negative-idiom false positives = %d, want 1", got)
	}
}

func TestMetricsZeroDenominators(t *testing.T) {
	var metrics Metrics
	if metrics.Precision() != 1 || metrics.Recall() != 1 || metrics.F1() != 1 {
		t.Errorf("empty metrics should be perfect, got precision=%v recall=%v F1=%v",
			metrics.Precision(), metrics.Recall(), metrics.F1())
	}
	metrics.FalsePositives = 1
	if metrics.Precision() != 0 || metrics.F1() != 0 {
		t.Errorf("false-positive-only metrics should score zero, got precision=%v F1=%v",
			metrics.Precision(), metrics.F1())
	}
	metrics.FalseNegatives = 1
	if metrics.F1() != 0 {
		t.Errorf("all-wrong metrics should have F1 zero, got %v", metrics.F1())
	}
}
