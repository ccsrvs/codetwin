// Block-clone test-segregation fixture: same content as
// testdata/bench/blocks/positive/verbatim-go, hosted in _test.go files
// so the resulting partial clone is test↔test and suppressed by
// default (--include-tests restores it).
// Shared verbatim block: b.go lines 13-28 == a.go lines 13-28.
// The surrounding code is goroutine/waitgroup flavored and structurally
// unrelated to a.go's DB/rows/switch host.
package fixture

import (
	"sync"
	"time"
)

func dispatchJobs(req *Request, workers int, results chan<- Result) error {
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
	queue := make(chan Item, len(req.Items))
	for _, item := range req.Items {
		queue <- item
	}
	close(queue)
	deadline := time.After(dispatchTimeout)
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(worker int) {
			defer wg.Done()
			for item := range queue {
				results <- runWorker(worker, item, deadline)
			}
		}(w)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		close(results)
		return nil
	case <-deadline:
		return errDispatchTimeout
	}
}
