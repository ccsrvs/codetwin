package fixture

import "errors"

// withdraw debits amount from the account balance, enforcing limits.
func withdraw(balance float64, amount float64) (float64, error) {
	if amount <= 0 {
		return balance, errors.New("amount must be positive")
	}
	if amount > balance {
		return balance, errors.New("insufficient funds")
	}
	remaining := balance - amount
	if remaining < 25.0 {
		return balance, errors.New("below minimum balance")
	}
	return remaining, nil
}
