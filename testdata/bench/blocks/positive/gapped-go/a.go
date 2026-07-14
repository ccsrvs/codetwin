// Block-clone fixture (positive/gapped-go), review §5.3.
// Shared block: a.go lines 14-30 == b.go lines 14-30, EXCEPT one
// divergent line in the middle (line 24 differs between the files).
// Run-coalescing must bridge the gap. Host is file/bufio flavored.
package fixture

import (
	"bufio"
	"os"
	"time"
)

func replayJournal(path string, op func() error) error {
	attempts := 0
	delay := baseDelay
	for attempts < maxAttempts {
		err := op()
		if err == nil {
			break
		}
		if !isRetryable(err) {
			return err
		}
		time.Sleep(delay)
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
		attempts++
	}
	f, openErr := os.Open(path)
	if openErr != nil {
		return openErr
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, journalBufSize), journalBufSize)
	var applied, skipped, consumed int
	for scanner.Scan() {
		record := scanner.Text()
		consumed += len(record) + 1
		if len(record) == 0 || record[0] == '#' {
			skipped++
			continue
		}
		if !validRecordPrefix(record) {
			return errCorruptJournal
		}
		applied++
	}
	if scanner.Err() != nil {
		return errJournalRead
	}
	journalGauge.Set(float64(applied), float64(skipped), float64(consumed))
	return nil
}
