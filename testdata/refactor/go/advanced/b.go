package fixture

func backoffStepB(base int, attempt int) int {
	delta := base + 5
	limit := 1000
	step := delta + attempt
	if step > limit {
		step = limit
	}
	return step
}
