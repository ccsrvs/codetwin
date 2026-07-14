package fixture

// flattenSamples collects every reading from a per-sensor map into one
// flat slice, in map iteration order.
func flattenSamples(bySensor map[string][]float64) []float64 {
	flat := make([]float64, 0, len(bySensor))
	for _, readings := range bySensor {
		for _, r := range readings {
			flat = append(flat, r)
		}
	}
	return flat
}
