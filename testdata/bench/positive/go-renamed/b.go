package fixture

import "errors"

// deductStock removes qty units from inventory, enforcing floors.
func deductStock(onHand float64, qty float64) (float64, error) {
	if qty <= 0 {
		return onHand, errors.New("quantity must be positive")
	}
	if qty > onHand {
		return onHand, errors.New("not enough stock")
	}
	left := onHand - qty
	if left < 10.0 {
		return onHand, errors.New("below safety stock")
	}
	return left, nil
}
