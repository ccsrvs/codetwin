package fixture

import "strings"

// splitCSVLine parses one comma-separated line, trimming whitespace
// and skipping empty fields.
func splitCSVLine(line string) []string {
	var fields []string
	for _, raw := range strings.Split(line, ",") {
		field := strings.TrimSpace(raw)
		if field == "" {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}
