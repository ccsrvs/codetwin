package inventory

import "fmt"

// StockLedger tracks per-SKU counts against a capacity ceiling. Its
// methods are deliberately interleaved with an unrelated helper so the
// synthetic class chunk exercises the NON-CONTIGUOUS grouping path.
type StockLedger struct {
	items    map[string]int
	capacity int
}

func (l *StockLedger) AddItem(sku string, count int) error {
	if count <= 0 {
		return fmt.Errorf("count must be positive")
	}
	current := l.items[sku]
	if current+count > l.capacity {
		return fmt.Errorf("capacity exceeded for %s", sku)
	}
	l.items[sku] = current + count
	return nil
}

// formatBanner is unrelated to StockLedger: it sits BETWEEN the
// methods so the covering range of the synthetic class chunk contains
// it while its source is excluded from the chunk's Code.
func formatBanner(title string, width int) string {
	banner := "== " + title + " =="
	for len(banner) < width {
		banner = banner + "-"
	}
	return banner
}

func (l *StockLedger) RemoveItem(sku string, count int) error {
	current := l.items[sku]
	if count > current {
		return fmt.Errorf("cannot remove more than stored")
	}
	l.items[sku] = current - count
	return nil
}

func (l *StockLedger) TotalUnits() int {
	total := 0
	for _, count := range l.items {
		total += count
	}
	return total
}
