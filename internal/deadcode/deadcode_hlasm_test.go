package deadcode

import (
	"strings"
	"testing"
)

func hlLine(name, op, operands string) string {
	line := name + strings.Repeat(" ", max(0, 9-len(name))) + op
	if operands != "" {
		line += strings.Repeat(" ", max(1, 15-len(line))) + operands
	}
	return line
}

func TestHLASMDeadCode(t *testing.T) {
	prog := strings.Join([]string{
		"*        MACROS AND ROUTINES",
		hlLine("", "MACRO", ""),
		hlLine("&L", "CLEARF", "&F"),
		hlLine("&L", "XC", "&F,&F"),
		hlLine("", "MEND", ""),
		hlLine("", "MACRO", ""),
		hlLine("", "UNUSED", "&X"),
		hlLine("", "LA", "R1,&X"),
		hlLine("", "MEND", ""),
		hlLine("MAIN", "CSECT", ""),
		hlLine("", "CLEARF", "WORK      CLEAR IT (NOT LONELY)"),
		hlLine("", "BAL", "R14,SUB@1"),
		hlLine("", "BR", "R14"),
		hlLine("SUB@1", "DS", "0H"),
		hlLine("", "BR", "R14"),
		hlLine("WORK", "DS", "CL8"),
		hlLine("CHK@MOD", "CSECT", ""),
		hlLine("", "BR", "R14"),
		hlLine("LONELY", "CSECT", ""),
		hlLine("", "BR", "R14"),
		hlLine("", "END", ""),
	}, "\n") + "\n"
	caller := strings.Join([]string{
		hlLine("CALLER", "CSECT", ""),
		hlLine("", "EXTRN", "LONELY"),
		hlLine("", "ENTRY", "CHK@MOD"),
		hlLine("", "L", "R15,=v(main)"),
		hlLine("", "BALR", "R14,R15"),
		hlLine("", "BR", "R14"),
		hlLine("", "END", ""),
	}, "\n") + "\n"
	snippets := scanDir(t, map[string]string{"prog.hlasm": prog, "caller.hlasm": caller})
	findings, warnings := Analyze(snippets)
	if len(warnings) > 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	got := findingsBySymbol(findings)
	for _, sym := range []string{"UNUSED", "LONELY", "CHK@MOD", "CALLER"} {
		if f, ok := got[sym]; !ok || f.Verdict != VerdictUnusedInScan {
			t.Errorf("%s: want unused-in-scan, got %+v (present=%v)", sym, f, ok)
		}
	}
	for _, alive := range []string{"CLEARF", "MAIN", "SUB@1"} {
		if f, ok := got[alive]; ok {
			t.Errorf("%s must not be reported, got %+v", alive, f)
		}
	}
}
