package fixture

import "fmt"

// applyDiscount computes the final invoice total after discounts,
// clamping at the account's minimum charge.
func applyDiscount(invoice *Invoice, rate float64) (float64, error) {
	if rate < 0 || rate > 1 {
		return 0, fmt.Errorf("discount rate %v out of range", rate)
	}
	subtotal := 0.0
	for _, line := range invoice.Lines {
		if line.Quantity <= 0 {
			continue
		}
		subtotal += line.UnitPrice * float64(line.Quantity)
	}
	discounted := subtotal * (1 - rate)
	if discounted < invoice.MinimumCharge {
		return invoice.MinimumCharge, nil
	}
	return discounted, nil
}
