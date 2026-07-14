package baselinefix

import (
	"fmt"
	"strconv"
	"strings"
)

// Family C — CSV record parsers. Two members in both snapshots; the
// "after" tree changes ParseRecordB's body while it still clusters
// with ParseRecordA (drift: member-changed).

func ParseRecordA(line string) (string, int, error) {
	fields := strings.Split(line, ",")
	if len(fields) != 3 {
		return "", 0, fmt.Errorf("bad record: %s", line)
	}
	name := strings.TrimSpace(fields[0])
	count, err := strconv.Atoi(strings.TrimSpace(fields[1]))
	if err != nil {
		return "", 0, err
	}
	if count < 0 {
		count = 0
	}
	unit := strings.TrimSpace(fields[2])
	if unit == "" {
		unit = "each"
	}
	return name + "/" + unit, count, nil
}

func ParseRecordB(row string) (string, int, error) {
	cols := strings.Split(row, ",")
	if len(cols) != 3 {
		return "", 0, fmt.Errorf("bad record: %s", row)
	}
	label := strings.TrimSpace(cols[0])
	qty, err := strconv.Atoi(strings.TrimSpace(cols[1]))
	if err != nil {
		return "", 0, err
	}
	if qty < 0 {
		qty = 0
	}
	kind := strings.TrimSpace(cols[2])
	if kind == "" {
		kind = "each"
	}
	return label + "/" + kind, qty, nil
}
