// Block-clone fixture (positive/containment-go), review §3.6 + §5.3.
// The entire body of clampWindow (a.go lines 8-19) appears verbatim as
// a block inside b.go's large host function (b.go lines 24-35).
// Union-normalized Jaccard is blind to this containment by design.
package fixture

func clampWindow(offset, limit, total int) (int, int) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	return offset, limit
}
