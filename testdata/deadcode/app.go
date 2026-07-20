package deadcodefix

// liveHelper is referenced by Runner below, so it must never be reported.
func liveHelper(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		total += i * 2
	}
	return total
}

// deadPrivateFn is referenced by nothing anywhere: the high-confidence tier.
func deadPrivateFn(n int) int {
	acc := 1
	for i := 1; i <= n; i++ {
		acc *= i
	}
	return acc
}

// DeadExportedFn is exported and referenced by nothing: the advisory tier.
func DeadExportedFn(s string) string {
	out := ""
	for _, r := range s {
		out = string(r) + out
	}
	return out
}

// testOnlyFn is production code that only the test file calls.
func testOnlyFn(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// Runner keeps liveHelper alive.
func Runner(n int) int {
	base := liveHelper(n)
	return base + n
}

// init anchors the reachability graph the way a real entry point does:
// init itself is suppressed as an entry point, but its body's reference
// to Runner keeps Runner alive.
func init() {
	_ = Runner(1)
}
