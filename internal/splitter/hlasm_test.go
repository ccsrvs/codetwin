package splitter

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// hl builds a fixed-format HLASM statement: name in column 1, operation
// in column 10, operands in column 16 (pushed right by long fields).
func hl(name, op, operands string) string {
	line := name + strings.Repeat(" ", max(0, 9-len(name)))
	if len(name) > 8 {
		line += " "
	}
	line += op
	if operands != "" {
		if len(line) < 15 {
			line += strings.Repeat(" ", 15-len(line))
		} else {
			line += " "
		}
		line += operands
	}
	return line
}

// hlc is hl continued: column 72 marks the next record as more operands.
func hlc(name, op, operands string) string {
	line := hl(name, op, operands)
	return line + strings.Repeat(" ", 71-len(line)) + "X"
}

func TestSplitHLASM(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		want  []span
	}{
		{
			name: "macro definitions are named by their prototype",
			lines: []string{
				"*        CLEAR A FIELD",            // 1
				hl("", "MACRO", ""),                 // 2
				hl("&LABEL", "CLEARF", "&FIELD"),    // 3
				hl("&LABEL", "XC", "&FIELD,&FIELD"), // 4
				hl("", "MEND", ""),                  // 5
				"",                                  // 6
				hl("", "MACRO", ""),                 // 7
				hl("", "@OUTER", "&P"),              // 8
				hl("", "MACRO", ""),                 // 9  nested definition stays inside
				hl("", "INNER", "&Q"),               // 10
				hl("", "MEND", ""),                  // 11
				hl("", "INNER", "&P"),               // 12
				hl("", "MEND", ""),                  // 13
			},
			want: []span{{"CLEARF", 2, 5}, {"@OUTER", 7, 13}},
		},
		{
			name: "sections split at called labels; uncalled labels and data maps do not start routines",
			lines: []string{
				hl("HELLO", "CSECT", ""),                 // 1
				hl("", "STM", "R14,R12,12(R13)"),         // 2
				hl("", "BAL", "R14,SUBA"),                // 3
				hl("", "BAS", "14,SUBB  CALL THE OTHER"), // 4
				hl("", "LM", "R14,R12,12(R13)"),          // 5
				hl("", "BR", "R14"),                      // 6
				hl("SUBA", "DS", "0H"),                   // 7
				hl("", "LA", "R1,1"),                     // 8
				hl("", "BR", "R14"),                      // 9
				hl("SUBB", "ST", "R14,SAVE"),             // 10
				hl("", "LA", "R2,2"),                     // 11
				hl("NOTCALL", "DS", "0H"),                // 12 branched to, never called
				hl("", "L", "R14,SAVE"),                  // 13
				hl("", "BR", "R14"),                      // 14
				hl("SAVE", "DS", "F"),                    // 15
				"",                                       // 16
				hl("WORKD", "DSECT", ""),                 // 17
				hl("FIELD", "DS", "CL8"),                 // 18
				hl("", "END", "HELLO"),                   // 19
			},
			want: []span{{"HELLO", 1, 6}, {"SUBA", 7, 9}, {"SUBB", 10, 15}},
		},
		{
			name: "RSECT, START, unnamed sections, lowercase, and an inline macro before the code",
			lines: []string{
				hl("", "MACRO", ""),         // 1
				hl("", "ZERO", "&R"),        // 2
				hl("", "SR", "&R,&R"),       // 3
				hl("", "MEND", ""),          // 4
				hl("first", "start", "0"),   // 5
				hl("", "zero", "r1"),        // 6
				hl("", "br", "r14"),         // 7
				hl("SECOND", "RSECT", ""),   // 8
				hl("", "BRAS", "R9,HELPER"), // 9
				hl("", "BR", "R14"),         // 10
				hl("HELPER", "EQU", "*"),    // 11
				hl("", "BR", "R9"),          // 12
				hl("", "CSECT", ""),         // 13
				hl("", "BR", "R14"),         // 14
			},
			want: []span{{"ZERO", 1, 4}, {"FIRST", 5, 7}, {"SECOND", 8, 10}, {"HELPER", 11, 12}, {"CSECT@L13", 13, 14}},
		},
		{
			name: "a file with neither sections nor macros is one routine, still split at called labels",
			lines: []string{
				"*        COPY MEMBER",      // 1
				hl("", "BAL", "R14,HELPER"), // 2
				hl("", "BR", "R14"),         // 3
				hl("HELPER", "LA", "R1,0"),  // 4
				hl("", "BR", "R14"),         // 5
			},
			want: []span{{"CODE@L2", 2, 3}, {"HELPER", 4, 5}},
		},
		{
			name: "markers in comments and remarks are ignored",
			lines: []string{
				"*        FAKE     CSECT",             // 1
				hl("REAL", "CSECT", ""),               // 2
				hl("", "BAL", "R14,SUB    SUB CSECT"), // 3 remark mentions CSECT
				hl("", "BR", "R14"),                   // 4
				".*       MACRO",                      // 5
				hl("SUB", "BR", "R14"),                // 6
			},
			want: []span{{"REAL", 2, 4}, {"SUB", 6, 6}},
		},
		{
			name: "a routine that ends on a continued statement keeps its continuation records",
			lines: []string{
				hl("", "MACRO", ""),                   // 1
				hl("&L", "GETBUF", "&LEN"),            // 2
				hlc("&L", "GETMAIN", "RU,LV=&LEN,"),   // 3
				strings.Repeat(" ", 15) + "SP=0",      // 4
				hlc("", "MEND", ""),                   // 5 (a stray marker on MEND)
				hl("FIRST", "CSECT", ""),              // 6
				hl("", "BR", "R14"),                   // 7
				hlc("INDCB", "DCB", "DDNAME=IN,"),     // 8
				strings.Repeat(" ", 15) + "MACRF=GM,", // 9 (alternate format)
				"*        THE LAST OPERAND FOLLOWS",   // 10 (comment ends it)
				hl("SECOND", "CSECT", ""),             // 11
				hl("", "BR", "R14"),                   // 12
				hlc("OUTDCB", "DCB", "DDNAME=OUT,"),   // 13
				strings.Repeat(" ", 15) + "MACRF=PM",  // 14
				"",                                    // 15
				hl("", "END", "FIRST"),                // 16
			},
			want: []span{{"GETBUF", 1, 5}, {"FIRST", 6, 9}, {"SECOND", 11, 14}},
		},
		{
			name: "END ends one program of a batch; JCL after the last one is not code",
			lines: []string{
				hl("ONE", "CSECT", ""),     // 1
				hl("", "BR", "R14"),        // 2
				hl("", "END", "ONE"),       // 3
				"*        SECOND ASSEMBLY", // 4
				hl("TWO", "CSECT", ""),     // 5
				hl("", "BAL", "R14,SUB"),   // 6
				hl("", "BR", "R14"),        // 7
				hl("SUB", "BR", "R14"),     // 8
				hl("", "END", "TWO"),       // 9
				"//LKED.SYSIN DD *",        // 10
				hl("", "NAME", "TWO(R)"),   // 11
			},
			want: []span{{"ONE", 1, 2}, {"TWO", 5, 7}, {"SUB", 8, 8}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := strings.Join(tc.lines, "\n") + "\n"
			got := spansOf(Split("x.hlasm", code, tokenizer.AsmHLASM))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Split =\n  %v\nwant\n  %v", got, tc.want)
			}
		})
	}
}
