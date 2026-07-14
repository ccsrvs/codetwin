// Block-clone fixture (positive/gapped-go), review §5.3.
// Shared block: b.go lines 14-30 == a.go lines 14-30, EXCEPT one
// divergent line in the middle (line 24 differs between the files).
// Run-coalescing must bridge the gap. Host is HTTP-client flavored.
package fixture

import (
	"io"
	"net/http"
	"time"
)

func pushNodeStatus(node string, epoch int64, op func() error) error {
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
		time.Sleep(delay + jitterFor(attempts, node))
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
		attempts++
	}
	reqBody := buildStatusPayload(node, epoch)
	httpReq, reqErr := http.NewRequest(http.MethodPost, statusURL, reqBody)
	if reqErr != nil {
		return reqErr
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Node-ID", node)
	httpReq.Header.Set("X-Epoch", formatEpoch(epoch))
	resp, doErr := client.Do(httpReq)
	if doErr != nil {
		return doErr
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 500:
		return errUpstreamDown
	case resp.StatusCode == http.StatusTooManyRequests:
		return errThrottled
	case resp.StatusCode != http.StatusOK:
		return errStatusRejected
	}
	ack, _ := io.ReadAll(resp.Body)
	if string(ack) != expectedAck {
		return errBadAck
	}
	return nil
}
