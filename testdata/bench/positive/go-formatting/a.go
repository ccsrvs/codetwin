package fixture

// mergeCounts folds src into dst, summing values for shared keys.
func mergeCounts(dst, src map[string]int) map[string]int {
	for key, value := range src {
		existing, ok := dst[key]
		if ok {
			dst[key] = existing + value
		} else {
			dst[key] = value
		}
	}
	return dst
}
