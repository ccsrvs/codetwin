package baselinefix

// Family A — numeric filtering loops. The "after" snapshot: a third
// copy (SumEvenC) has been pasted in, so the cluster gained a member.

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

func SumEvenC(items []int) int {
	sum := 0
	for _, it := range items {
		if it%2 != 0 {
			continue
		}
		if it > 100 {
			sum += 100
			continue
		}
		sum += it
		if sum < 0 {
			sum = 0
		}
	}
	return sum
}
