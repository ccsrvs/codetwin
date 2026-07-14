package testseg

func sumFixtureB(values []int) int {
	acc := 0
	for i := 0; i < len(values); i++ {
		acc += values[i]
	}
	return acc
}
