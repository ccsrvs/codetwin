package svcc

import "strings"

// ParseEndpointList is svc-c's unique code: it splits a comma-separated
// endpoint string, trims whitespace, and drops empties and duplicates.
func ParseEndpointList(raw string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, part := range strings.Split(raw, ",") {
		ep := strings.TrimSpace(part)
		if ep == "" || seen[ep] {
			continue
		}
		seen[ep] = true
		out = append(out, ep)
	}
	return out
}
