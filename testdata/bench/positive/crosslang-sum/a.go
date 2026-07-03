package fixture

// clampedTotal sums the positive values, capping each at limit.
func clampedTotal(values []float64, limit float64) float64 {
	total := 0.0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if value > limit {
			value = limit
		}
		total += value
	}
	return total
}
