package svcb

import "fmt"

// FormatWebhookPayload is svc-b's unique code: it renders an event into
// the outgoing webhook body. Nothing in svc-a or svc-c resembles it.
func FormatWebhookPayload(event string, fields map[string]string) string {
	body := "event=" + event
	for k, v := range fields {
		if k == "" || v == "" {
			continue
		}
		body += fmt.Sprintf("&%s=%s", k, v)
	}
	return body + "\n"
}
