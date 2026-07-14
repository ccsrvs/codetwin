package svca

// ReserveStock is svc-a's unique code: it decrements stock counts for
// each requested item and reports the items that could not be reserved.
// Nothing in svc-b or svc-c resembles it.
func ReserveStock(stock map[string]int, items []string) []string {
	var missing []string
	for _, it := range items {
		n, ok := stock[it]
		if !ok || n == 0 {
			missing = append(missing, it)
			continue
		}
		stock[it] = n - 1
	}
	return missing
}
