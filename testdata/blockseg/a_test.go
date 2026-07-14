// Block-clone test-segregation fixture: same content as
// testdata/bench/blocks/positive/verbatim-go, hosted in _test.go files
// so the resulting partial clone is test↔test and suppressed by
// default (--include-tests restores it).
// Shared verbatim block: a.go lines 13-28 == b.go lines 13-28.
// The surrounding code is DB/rows/switch flavored and structurally
// unrelated to b.go's goroutine/waitgroup host.
package fixture

import (
	"database/sql"
	"strings"
)

func exportOrderRows(db *sql.DB, req *Request, out *strings.Builder) error {
	if req == nil {
		return errNilRequest
	}
	if req.AccountID == "" || len(req.Items) == 0 {
		return errEmptyRequest
	}
	seen := make(map[string]bool, len(req.Items))
	for _, item := range req.Items {
		if item.SKU == "" || item.Quantity <= 0 {
			return errBadItem
		}
		if seen[item.SKU] {
			return errDuplicateSKU
		}
		seen[item.SKU] = true
	}
	rows, err := db.Query(selectOrders, req.AccountID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var shipped, pending, failed int
	for rows.Next() {
		var id string
		var status, qty int
		if scanErr := rows.Scan(&id, &status, &qty); scanErr != nil {
			return scanErr
		}
		switch status {
		case statusShipped:
			shipped += qty
			out.WriteString(id + "\tshipped\n")
		case statusPending:
			pending += qty
			out.WriteString(id + "\tpending\n")
		default:
			failed += qty
			out.WriteString(id + "\tfailed\n")
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return rowsErr
	}
	out.WriteString(strings.Repeat("-", exportRuleWidth))
	out.WriteString(formatTotals(shipped, pending, failed))
	return nil
}
