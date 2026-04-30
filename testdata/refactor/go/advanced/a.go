package fixture

func backoffStepA(base int, attempt int) int {
	delta := base * 2
	limit := 1000
	step := delta + attempt
	if step > limit {
		step = limit
	}
	return step
}
