package fixture

func priceWithTaxA(amount float64) float64 {
	rounded := round2(amount)
	tax := rounded * 0.07
	total := rounded + tax
	return round2(total)
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
