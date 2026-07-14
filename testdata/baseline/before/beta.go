package baselinefix

// Family B — map merge helpers. Three members in the "before"
// snapshot; the "after" tree drops MergeCountsC (drift: member-removed).

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

func MergeCountsC(out map[string]int, in map[string]int) map[string]int {
	if out == nil {
		out = make(map[string]int)
	}
	for k, v := range in {
		if v <= 0 {
			continue
		}
		prev, seen := out[k]
		if seen {
			out[k] = prev + v
		} else {
			out[k] = v
		}
	}
	return out
}
