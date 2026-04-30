package fixture

func priceWithTaxB(amount float64) float64 {
	rounded := round2(amount)
	tax := rounded * 0.085
	total := rounded + tax
	return round2(total)
}
