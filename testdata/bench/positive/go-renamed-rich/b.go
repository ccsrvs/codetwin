package fixture

import "fmt"

// computeRebate is applyDiscount under a typical rename: the function
// and its locals are renamed, but the shared type's fields, the helper
// calls, and the error string — the vocabulary a real rename keeps —
// are untouched.
func computeRebate(invoice *Invoice, fraction float64) (float64, error) {
	if fraction < 0 || fraction > 1 {
		return 0, fmt.Errorf("discount rate %v out of range", fraction)
	}
	base := 0.0
	for _, item := range invoice.Lines {
		if item.Quantity <= 0 {
			continue
		}
		base += item.UnitPrice * float64(item.Quantity)
	}
	reduced := base * (1 - fraction)
	if reduced < invoice.MinimumCharge {
		return invoice.MinimumCharge, nil
	}
	return reduced, nil
}
