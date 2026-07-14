package baselinefix

import (
	"fmt"
	"strconv"
	"strings"
)

// Family C — CSV record parsers. The "after" snapshot: ParseRecordB
// grew an empty-label guard (a bug fixed in one copy but not the
// other — the classic drift case), so its body hash changed while it
// still clusters with ParseRecordA.
//
// This comment block is also deliberately longer than the "before"
// version: it shifts every line number below it, proving that member
// identity survives ordinary edits (line ranges are stripped from the
// snapshot keys). ParseRecordA is byte-identical to "before" and must
// NOT read as drift.

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
	if label == "" {
		return "", 0, fmt.Errorf("empty label: %s", row)
	}
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
