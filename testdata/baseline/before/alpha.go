package baselinefix

// Family A — numeric filtering loops. Two members in the "before"
// snapshot; the "after" tree adds SumEvenC (drift: member-added).

func SumEvenA(values []int) int {
	total := 0
	for _, v := range values {
		if v%2 != 0 {
			continue
		}
		if v > 100 {
			total += 100
			continue
		}
		total += v
		if total < 0 {
			total = 0
		}
	}
	return total
}

func SumEvenB(numbers []int) int {
	acc := 0
	for _, n := range numbers {
		if n%2 != 0 {
			continue
		}
		if n > 100 {
			acc += 100
			continue
		}
		acc += n
		if acc < 0 {
			acc = 0
		}
	}
	return acc
}
