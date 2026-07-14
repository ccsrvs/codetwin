package svca

// ApplyDiscount computes the final price for an order after applying
// tiered discounts and clamping to the floor price. This function was
// copy-pasted into svc-b's billing.go — the shared clone family the
// multirepo fixture exists to surface.
func ApplyDiscount(prices []float64, tier int) float64 {
	total := 0.0
	for _, p := range prices {
		total += p
	}
	discount := 0.0
	switch {
	case tier >= 3:
		discount = 0.20
	case tier == 2:
		discount = 0.10
	case tier == 1:
		discount = 0.05
	}
	final := total * (1.0 - discount)
	if final < 0.99 {
		final = 0.99
	}
	return final
}
