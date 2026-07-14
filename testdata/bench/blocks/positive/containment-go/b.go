// Block-clone fixture (positive/containment-go), review §3.6 + §5.3.
// The small function's entire body (a.go lines 8-19) appears verbatim
// as a block at b.go lines 24-35 inside this large host function.
// The host's own logic is query-parsing/rendering flavored.
package fixture

import (
	"fmt"
	"net/url"
	"strconv"
)

func renderInventoryPage(query url.Values, inv *Inventory, out chan<- string) error {
	offset, err := strconv.Atoi(query.Get("offset"))
	if err != nil {
		offset = 0
	}
	limit, err := strconv.Atoi(query.Get("limit"))
	if err != nil {
		limit = pageSizeHint
	}
	filter := query.Get("filter")
	total := inv.Count(filter)
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
	header := fmt.Sprintf("inventory %d-%d of %d", offset, offset+limit, total)
	out <- header
	emitted := 0
	for _, row := range inv.Slice(filter, offset, limit) {
		if row.Hidden && filter != showHiddenFilter {
			continue
		}
		line := fmt.Sprintf("%s\t%d\t%s", row.Name, row.Stock, row.Location)
		if row.Stock == 0 {
			line += "\t(out of stock)"
		}
		out <- line
		emitted++
	}
	if emitted == 0 {
		out <- emptyPageNotice
	}
	out <- fmt.Sprintf("emitted %d of %d rows", emitted, total)
	return nil
}
