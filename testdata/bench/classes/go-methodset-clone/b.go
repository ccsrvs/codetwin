package storage

import "fmt"

// BinRegister is StockLedger renamed, with its methods reordered and
// lightly edited — the copied-type shape method-level granularity
// underreports and the §5.2 class chunk must surface.
type BinRegister struct {
	bins    map[string]int
	ceiling int
}

func (r *BinRegister) TotalStored() int {
	total := 0
	for _, count := range r.bins {
		total += count
	}
	return total
}

func (r *BinRegister) StoreBin(sku string, count int) error {
	if count <= 0 {
		return fmt.Errorf("count must be positive")
	}
	current := r.bins[sku]
	if current+count > r.ceiling {
		return fmt.Errorf("ceiling exceeded for %s", sku)
	}
	r.bins[sku] = current + count
	return nil
}

func (r *BinRegister) ReleaseBin(sku string, count int) error {
	current := r.bins[sku]
	if count > current {
		return fmt.Errorf("cannot release more than stored")
	}
	r.bins[sku] = current - count
	return nil
}
