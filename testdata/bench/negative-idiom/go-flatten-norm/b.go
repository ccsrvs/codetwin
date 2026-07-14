package fixture

import "math"

// weightNorm computes the L2 norm of a weight vector, accumulating in
// sorted-key order so the float sum is deterministic. Shares only the
// "loop over a map, build a slice" idiom with flattening code.
func weightNorm(weights map[string]float64) float64 {
	keys := make([]string, 0, len(weights))
	for k := range weights {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var total float64
	for _, k := range keys {
		total += weights[k] * weights[k]
	}
	return math.Sqrt(total)
}
