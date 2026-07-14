package svcc

// svc-c contains a same-repo clone pair (the two accumulators below).
// It deliberately holds NO member of the cross-repo ApplyDiscount
// family, so --cross-repo-only must drop every svc-c finding.

// AccumulateLatency folds request latencies into a running histogram
// bucket map keyed by the rounded millisecond value.
func AccumulateLatency(samples []float64, buckets map[int]int) int {
	count := 0
	for _, s := range samples {
		if s < 0 {
			continue
		}
		key := int(s * 1000.0)
		buckets[key] = buckets[key] + 1
		count = count + 1
	}
	return count
}

// AccumulatePayload folds response payload sizes into a running
// histogram bucket map keyed by the rounded kilobyte value.
func AccumulatePayload(sizes []float64, buckets map[int]int) int {
	count := 0
	for _, s := range sizes {
		if s < 0 {
			continue
		}
		key := int(s / 1024.0)
		buckets[key] = buckets[key] + 1
		count = count + 1
	}
	return count
}
