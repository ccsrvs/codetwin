package fixture

func runB() {
	go func() {
		x := 1
		y := x + 2
		_ = y
	}()
}
