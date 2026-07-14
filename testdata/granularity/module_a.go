package modulea

import (
	"strconv"
	"strings"
)

// Record is one parsed CSV row.
type Record struct {
	ID   int
	Name string
	Tag  string
}

// Options configures parsing and rendering.
type Options struct {
	Delimiter   string
	MaxRecords  int
	SkipInvalid bool
	Header      string
	Footer      string
}

// defaultMaxRecords bounds how many rows a single parse call accepts.
const defaultMaxRecords = 1000

// defaultOptions is the configuration used when the caller passes nil.
var defaultOptions = Options{
	Delimiter:   ",",
	MaxRecords:  defaultMaxRecords,
	SkipInvalid: true,
	Header:      "summary",
	Footer:      "end",
}

func parseRecords(lines []string) []Record {
	out := make([]Record, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, ",")
		if len(fields) < 3 {
			continue
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		rec := Record{ID: id, Name: fields[1], Tag: fields[2]}
		out = append(out, rec)
	}
	return out
}

func mergeCounts(dst, src map[string]int) map[string]int {
	if dst == nil {
		dst = make(map[string]int, len(src))
	}
	for key, n := range src {
		if n <= 0 {
			continue
		}
		cur, ok := dst[key]
		if !ok {
			dst[key] = n
			continue
		}
		dst[key] = cur + n
	}
	return dst
}

func formatSummary(recs []Record) string {
	var sb strings.Builder
	sb.WriteString("summary:\n")
	for _, rec := range recs {
		sb.WriteString("  - ")
		sb.WriteString(strconv.Itoa(rec.ID))
		sb.WriteString(" ")
		sb.WriteString(rec.Name)
		if rec.Tag != "" {
			sb.WriteString(" [")
			sb.WriteString(rec.Tag)
			sb.WriteString("]")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
