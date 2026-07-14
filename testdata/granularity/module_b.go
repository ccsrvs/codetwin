package moduleb

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

func renderSummary(recs []Record) string {
	var sb strings.Builder
	sb.WriteString("report:\n")
	for _, rec := range recs {
		if rec.Name == "" {
			continue
		}
		sb.WriteString("  * ")
		sb.WriteString(rec.Name)
		sb.WriteString(" #")
		sb.WriteString(strconv.Itoa(rec.ID))
		if rec.Tag != "" {
			sb.WriteString(" [")
			sb.WriteString(rec.Tag)
			sb.WriteString("]")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("total: ")
	sb.WriteString(strconv.Itoa(len(recs)))
	return sb.String()
}

func readRecords(lines []string) []Record {
	out := make([]Record, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		fields := strings.Split(line, ";")
		if len(fields) != 3 {
			continue
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			id = -1
		}
		out = append(out, Record{ID: id, Name: fields[1], Tag: fields[2]})
	}
	return out
}

func addCounts(dst, src map[string]int) map[string]int {
	if src == nil {
		return dst
	}
	if dst == nil {
		dst = make(map[string]int, len(src))
	}
	for key, n := range src {
		if n < 0 || key == "" {
			continue
		}
		dst[key] += n
	}
	return dst
}
