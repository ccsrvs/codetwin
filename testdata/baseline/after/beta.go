package baselinefix

// Family B — map merge helpers. The "after" snapshot: MergeCountsC was
// refactored away, so the cluster lost a member.

func MergeCountsA(dst map[string]int, src map[string]int) map[string]int {
	if dst == nil {
		dst = make(map[string]int)
	}
	for key, val := range src {
		if val <= 0 {
			continue
		}
		existing, ok := dst[key]
		if ok {
			dst[key] = existing + val
		} else {
			dst[key] = val
		}
	}
	return dst
}

func MergeCountsB(target map[string]int, extra map[string]int) map[string]int {
	if target == nil {
		target = make(map[string]int)
	}
	for name, count := range extra {
		if count <= 0 {
			continue
		}
		current, found := target[name]
		if found {
			target[name] = current + count
		} else {
			target[name] = count
		}
	}
	return target
}
