package tokenizer

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// card builds one fixed-format HLASM line: name in column 1, operation
// in column 10, operands in column 16, the text padded to column 71, an
// optional continuation character in column 72, and a sequence number
// in columns 73-80.
func card(name, op, operands, remarks string, cont byte, seq string) string {
	line := name
	if op != "" {
		line = padTo(line, 9)
		if len(line) > 9 {
			line += " " // a name longer than 8 characters pushes the operation right
		}
		line += op
		if operands != "" {
			if len(line) < 15 {
				line = padTo(line, 15)
			} else {
				line += " " // a long operation pushes the operands right
			}
			line += operands
		}
		if remarks != "" {
			line = padTo(line+" ", len(line)+1) + remarks
		}
	} else {
		line += remarks
	}
	if cont == 0 && seq == "" {
		return line
	}
	line = padTo(line, 71)
	if cont != 0 {
		line += string(cont)
	} else {
		line += " "
	}
	return line + seq
}

// contCard is a continuation line: blanks to column 16, then text.
func contCard(text string, cont byte, seq string) string {
	return card("", "", "", strings.Repeat(" ", 15)+text, cont, seq)
}

func padTo(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

func TestDetect_HLASM(t *testing.T) {
	program := lines(
		"*        SAMPLE",
		card("HELLO", "CSECT", "", "", 0, ""),
		card("", "STM", "R14,R12,12(R13)", "SAVE CALLER'S REGISTERS", 0, ""),
		card("", "BR", "R14", "", 0, ""),
		card("MSG", "DC", "C'IT''S ALIVE'", "", 0, ""),
		card("", "END", "HELLO", "", 0, ""),
	)
	macro := lines(
		card("", "MACRO", "", "", 0, ""),
		card("&LABEL", "CLEARF", "&FIELD", "", 0, ""),
		card("&LABEL", "XC", "&FIELD,&FIELD", "", 0, ""),
		card("", "MEND", "", "", 0, ""),
	)
	for _, tc := range []struct {
		file, code string
		want       Language
	}{
		{"hello.hlasm", "", AsmHLASM},
		{"HELLO.MLC", "", AsmHLASM},
		{"PAYROLL.ASSEMBLE", "", AsmHLASM},
		{"prog.asmpgm", "", AsmHLASM},
		{"HELLO.asm", program, AsmHLASM}, // was misread as MASM (MACRO/MEND) or GAS
		{"HELLO.ASM", program, AsmHLASM},
		{"clearf.asm", macro, AsmHLASM}, // armasm also has MACRO/MEND
		{"CLEARF.MAC", macro, AsmHLASM},
		{"irq.s", program, AsmHLASM},                                  // z/OS UNIX as
		{"util.mac", ".TITLE UTIL\n\tMOV R0,R1\n\tRTS PC\n", Unknown}, // MACRO-11
		{"x86.asm", "global _start\nsection .text\n_start:\n    ret\n", AsmNASM},
		{"cpuid.asm", ".code\nGetCpuid PROC\n    ret\nGetCpuid ENDP\nEND\n", AsmMASM},
	} {
		if got := Detect(tc.file, tc.code); got != tc.want {
			t.Errorf("Detect(%q) = %v, want %v", tc.file, got, tc.want)
		}
	}
	if !IsAssembly(AsmHLASM) || !ClaimedByContentOnly("UTIL.MAC") || ClaimedByContentOnly("x.hlasm") {
		t.Error("IsAssembly/ClaimedByContentOnly misclassify HLASM")
	}
}

// Comments, remarks, sequence numbers, and continuations are layout, not
// code: a program written with all of them tokenizes like the same
// program without any.
func TestTokenize_HLASMLayoutIsNotCode(t *testing.T) {
	dressed := lines(
		"*        COMMENT LINE WITH A QUOTE: CALLER'S",
		".*       MACRO COMMENT",
		card("COPYIT", "CSECT", "", "ENTRY FOR THE COPY", 0, "00010000"),
		card("", "STM", "R14,R12,12(R13)", "SAVE CALLER'S REGS", 0, "00020000"),
		card("", "MVC", "OUT(L'MSG),MSG", "COPY IT'S TEXT", 0, "00030000"),
		// normal continuation: operands fill columns 16-71 and resume
		// at column 16 of the next line, mid-symbol
		card("", "CALL", "SUB,(PARAMETER1,PARAMETER2,PARAMETER3,PARAMETER4,PARAMET", "", 'X', "00040000"),
		contCard("ER5)", 0, "00050000"),
		// alternate format: each line ends its operands with ", " and a remark
		card("", "GETMAIN", "RU,", "FIRST PART", 'X', "00060000"),
		contCard("LV=4096,      LENGTH", 'X', "00070000"),
		contCard("SP=0          SUBPOOL", 0, "00080000"),
		card("", "LTORG", ",", "LITERAL POOL HERE", 0, "00090000"),
		card("", "AIF", "('&A' EQ 'L' AND (K'&B GT 2)).SKIP", "NESTED TEST", 0, ""),
		card("", "EXEC", "CICS SEND MAP('MAP1') MAPSET('SET1') END-EXEC", "", 0, ""),
		card("", "BR", "R14", "RETURN TO CALLER", 0, "00100000"),
		card("MSG", "DC", "C'IT''S ALIVE'", "THE TEXT", 0, "00110000"),
		card("OUT", "DS", "CL20", "", 0, "00120000"),
	)
	plain := lines(
		"",
		"",
		card("COPYIT", "CSECT", "", "", 0, ""),
		card("", "STM", "R14,R12,12(R13)", "", 0, ""),
		card("", "MVC", "OUT(L'MSG),MSG", "", 0, ""),
		card("", "CALL", "SUB,(PARAMETER1,PARAMETER2,PARAMETER3,PARAMETER4,PARAMET", "", 'X', ""),
		contCard("ER5)", 0, ""),
		card("", "GETMAIN", "RU,LV=4096,SP=0", "", 0, ""),
		"",
		"",
		card("", "LTORG", "", "", 0, ""),
		card("", "AIF", "('&A' EQ 'L' AND (K'&B GT 2)).SKIP", "", 0, ""),
		card("", "EXEC", "CICS SEND MAP('MAP1') MAPSET('SET1') END-EXEC", "", 0, ""),
		card("", "BR", "R14", "", 0, ""),
		card("MSG", "DC", "C'IT''S ALIVE'", "", 0, ""),
		card("OUT", "DS", "CL20", "", 0, ""),
	)
	got, gotLines := TokenizeWithLines(dressed, AsmHLASM)
	want, wantLines := TokenizeWithLines(plain, AsmHLASM)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokens differ:\n got %v\nwant %v", got, want)
	}
	if !reflect.DeepEqual(gotLines, wantLines) {
		t.Errorf("a continued statement's tokens belong to its first line:\n got %v\nwant %v", gotLines, wantLines)
	}
	var call []string
	for i, l := range gotLines {
		if l == 7 {
			t.Errorf("token %q attributed to continuation line 7", got[i])
		}
		if l == 6 {
			call = append(call, got[i])
		}
	}
	// CALL SUB,(PARAMETER1,...,PARAMETER5): six names, the last joined
	// across the continuation.
	if strings.Join(call, " ") != "call VAR , ( VAR , VAR , VAR , VAR , VAR )" {
		t.Errorf("continued CALL = %q", strings.Join(call, " "))
	}
	for _, kept := range []string{"csect", "stm", "mvc", "call", "getmain", "ltorg", "aif", "exec", "br", "dc", "ds", "lattr", "eq", "and", "kattr", "gt", "lv", "sp"} {
		if !slices.Contains(got, kept) {
			t.Errorf("%q missing from %v", kept, got)
		}
	}
}

// Register spellings are interchangeable (R14, 14, GR14, @14), a symbol
// may contain @ # $, case does not matter, and renaming labels and
// symbols changes nothing — while changing an operation does.
func TestTokenize_HLASMNormalization(t *testing.T) {
	a := lines(
		card("LOOP", "L", "R1,COUNT$1", "", 0, ""),
		card("", "BCT", "R1,LOOP", "", 0, ""),
		card("", "BAL", "R14,SUB@X", "", 0, ""),
		card("", "BR", "R14", "", 0, ""),
	)
	respelled := lines(
		card("again", "l", "1,total#2", "", 0, ""),
		card("", "bct", "GR1,again", "", 0, ""),
		card("", "bal", "@14,work$x", "", 0, ""),
		card("", "br", "14", "", 0, ""),
	)
	changed := lines(
		card("LOOP", "L", "R1,COUNT$1", "", 0, ""),
		card("", "BCT", "R1,LOOP", "", 0, ""),
		card("", "BAS", "R14,SUB@X", "", 0, ""),
		card("", "BR", "R14", "", 0, ""),
	)
	ta := Tokenize(a, AsmHLASM)
	if tr := Tokenize(respelled, AsmHLASM); !reflect.DeepEqual(ta, tr) {
		t.Errorf("renamed/respelled program differs:\n%v\n%v", ta, tr)
	}
	if tc := Tokenize(changed, AsmHLASM); reflect.DeepEqual(ta, tc) {
		t.Errorf("BAL vs BAS must differ: %v", ta)
	}
	if want := "VAR l NUM , VAR bct NUM , VAR bal NUM , VAR br NUM"; strings.Join(ta, " ") != want {
		t.Errorf("tokens = %q, want %q", strings.Join(ta, " "), want)
	}
	// Strings and attributes: D'1.5' is a long-float string, L'X a
	// length attribute; a DC type designator stays next to its string.
	got := strings.Join(Tokenize(card("", "DC", "D'1.5',CL8'NAME',XL2'00FF'", "", 0, "")+"\n"+card("", "LA", "R1,L'FIELD", "", 0, ""), AsmHLASM), " ")
	if got != "dc d STR , cl8 STR , xl2 STR la NUM , lattr VAR" {
		t.Errorf("strings/attributes = %q", got)
	}
}

func TestTokenize_HLASMDataDefinitions(t *testing.T) {
	norm := func(stmts ...string) string { return strings.Join(Tokenize(lines(stmts...), AsmHLASM), " ") }
	// A duplication factor is a number whatever it is and however the
	// type after it is spelled; the type stays a designator.
	upper := norm(card("SAVE", "DS", "18F", "", 0, ""), card("AREA", "DS", "0CL80", "", 0, ""),
		card("", "CLC", "0(2,R1),=2C'AB'", "", 0, ""))
	lower := norm(card("save", "ds", "72f", "", 0, ""), card("area", "ds", "0cl80", "", 0, ""),
		card("", "clc", "0(2,r1),=3c'XY'", "", 0, ""))
	if upper != lower {
		t.Errorf("case or duplication factor changed the tokens:\n %q\n %q", upper, lower)
	}
	if want := "VAR ds NUM f VAR ds NUM cl80 clc NUM ( NUM , NUM ) , = NUM c STR"; upper != want {
		t.Errorf("tokens = %q, want %q", upper, want)
	}
	// The value of a macro keyword operand is a name, not a literal:
	// DSORG=PS and DSORG=PO are different data sets, like DDNAME=IN/OUT.
	if got, want := norm(card("IN", "DCB", "DSORG=PS,DDNAME=SYSIN,MACRF=GM", "", 0, "")),
		"VAR dcb dsorg = VAR , ddname = VAR , macrf = VAR"; got != want {
		t.Errorf("keyword operands = %q, want %q", got, want)
	}
	// Digits inside a string are content, not a term to normalize.
	terms := LexicalTerms(card("MSG", "DC", "C'TOTAL 2024 OK'", "", 0, ""), AsmHLASM)
	if slices.Contains(terms, "num") {
		t.Errorf("a string's digits were rewritten to NUM: %v", terms)
	}
}

func TestTokenize_HLASMDeclarationsAreNotCode(t *testing.T) {
	// Register equates, EQU constants, linkage declarations, and listing
	// control describe the program; two sections that differ only there
	// are the same code. EQU * is a label and stays.
	dressed := lines(
		card("PROG", "TITLE", "'PROG - UPDATE THE MASTER FILE'", "", 0, ""),
		card("", "PRINT", "NOGEN", "", 0, ""),
		card("PROG", "CSECT", "", "", 0, ""),
		card("", "EXTRN", "RDMAST,WRMAST", "", 0, ""),
		card("", "ENTRY", "PROGALT", "", 0, ""),
		card("", "BALR", "R12,0", "", 0, ""),
		card("", "USING", "*,R12", "", 0, ""),
		card("", "SPACE", "2", "", 0, ""),
		card("NEXT", "EQU", "*", "", 0, ""),
		card("", "BAL", "R14,RDMAST", "", 0, ""),
		card("", "EJECT", "", "", 0, ""),
		card("", "BR", "R14", "", 0, ""),
		card("R12", "EQU", "12", "", 0, ""),
		card("R14", "EQU", "14", "", 0, ""),
		card("RECLEN", "EQU", "*-NEXT", "", 0, ""),
		card("BLANK", "EQU", "C' '", "", 0, ""),
	)
	plain := lines(
		card("PROG", "CSECT", "", "", 0, ""),
		card("", "BALR", "R12,0", "", 0, ""),
		card("", "USING", "*,R12", "", 0, ""),
		card("NEXT", "EQU", "*", "", 0, ""),
		card("", "BAL", "R14,RDMAST", "", 0, ""),
		card("", "BR", "R14", "", 0, ""),
	)
	got, want := Tokenize(dressed, AsmHLASM), Tokenize(plain, AsmHLASM)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("declarations changed the tokens:\n got %v\nwant %v", got, want)
	}
	if w := "VAR csect balr NUM , NUM using * , NUM VAR equ * bal NUM , VAR br NUM"; strings.Join(want, " ") != w {
		t.Errorf("tokens = %q, want %q", strings.Join(want, " "), w)
	}
	// They still reference what they name: an EQU operand is a use.
	refs := map[string]bool{}
	for _, r := range References(dressed, AsmHLASM) {
		refs[r.Word] = true
	}
	if !refs["NEXT"] {
		t.Errorf("RECLEN EQU *-NEXT no longer references NEXT: %v", refs)
	}
}

func TestHLASMStatementEnds(t *testing.T) {
	code := lines(
		card("IN", "DCB", "DDNAME=IN,", "", 'X', ""),            // 0: continued ...
		contCard("MACRF=GM,", 'X', ""),                          // 1: ... twice
		contCard("EODAD=EOF", 0, ""),                            // 2
		padTo("*        A COMMENT MARKED IN COLUMN 72", 71)+"X", // 3
		card("", "BR", "R14", "", 0, ""),                        // 4: not blank in 1-15, so not a continuation
	)
	if got, want := HLASMStatementEnds(code), []int{2, 1, 2, 3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("HLASMStatementEnds = %v, want %v", got, want)
	}
}

func TestHLASMTermScanners(t *testing.T) {
	// A doubled quote is a quote inside the string. A string that never
	// closes on its line ends at the first quote of its last pair, the
	// way a backtracking '(?:[^'\n]|'')*' match does, or is no string.
	for s, want := range map[string]int{
		"'AB''C',X": 7,
		"'AB''":     4,
		"'A''B\nC'": 3,
		"'ABC":      0,
		"'AB\n'":    0,
	} {
		if got := hlasmString(s); got != want {
			t.Errorf("hlasmString(%q) = %d, want %d", s, got, want)
		}
	}
	// Columns count characters, so text with a UTF-8 or a Latin-1 accent
	// still has its continuation marker in column 72.
	for _, accent := range []string{"\u00c9", "\xc9"} {
		first := "         MVC   A,B" + strings.Repeat(" ", 16) + "CAF" + accent + strings.Repeat(" ", 33) + "X"
		code := first + "\n" + contCard("REMARK", 0, "")
		if got := HLASMStatementEnds(code); got[0] != 1 {
			t.Errorf("accent %q: the marker in column 72 was missed: %v", accent, got)
		}
		if text, cont := hlasmCard(first); !cont || !strings.HasSuffix(text, "CAF"+accent+strings.Repeat(" ", 33)) {
			t.Errorf("accent %q: card = %q, %v; the original bytes must survive", accent, text, cont)
		}
	}
}

func TestTokenize_HLASMMacroPrototype(t *testing.T) {
	macro := func(name, parm, key string) string {
		return lines(
			card("", "MACRO", "", "", 0, ""),
			card("&LABEL", name, "&"+parm+",&"+key+"=8", "", 0, ""),
			card("&LABEL", "LA", "&"+parm+",&"+key, "", 0, ""),
			card("", "MEND", "", "", 0, ""),
		)
	}
	a := Tokenize(macro("EDNUM", "REG", "LEN"), AsmHLASM)
	b := Tokenize(macro("CVTDEC", "R", "DIGITS"), AsmHLASM)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("a renamed macro tokenizes differently:\n%v\n%v", a, b)
	}
	// The prototype names the macro and declares its parameters: all
	// names. A later use of the macro keeps its operation.
	if got, want := strings.Join(a, " "), "macro & VAR & VAR & VAR , & VAR = NUM & VAR la & VAR , & VAR mend"; got != want {
		t.Errorf("tokens = %q, want %q", got, want)
	}
	if got := strings.Join(Tokenize(card("", "EDNUM", "R3,FIELD,LEN=8", "", 0, ""), AsmHLASM), " "); got != "ednum NUM , VAR , len = NUM" {
		t.Errorf("macro instruction = %q", got)
	}
}

func TestReferencesAndLexicalTerms_HLASM(t *testing.T) {
	code := lines(
		"*        MENTIONS GHOSTSUB IN A COMMENT",
		card("MAIN", "CSECT", "", "", 0, ""),
		card("", "BAL", "R14,COPY$REC", "CALL GHOSTREM", 0, ""),
		card("", "L", "R15,=V(EXTPGM)", "", 0, ""),
		card("EPNAME", "DC", "CL8'LOADME'", "PROGRAM TO LOAD", 0, ""),
		card("", "mvc", "out,in", "", 0, ""),
	)
	refs := refWords(References(code, AsmHLASM))
	for _, want := range []string{"COPY$REC", "EXTPGM", "LOADME", "OUT", "IN"} {
		if len(refs[want]) == 0 {
			t.Errorf("reference %q missing: %v", want, refs)
		}
	}
	for _, gone := range []string{"GHOSTSUB", "GHOSTREM", "CALL", "PROGRAM"} {
		if len(refs[gone]) != 0 {
			t.Errorf("comment/remark word %q became a reference", gone)
		}
	}
	terms := LexicalTerms(code, AsmHLASM)
	for _, want := range []string{"copy", "rec", "extpgm", "loadme"} {
		if !slices.Contains(terms, want) {
			t.Errorf("term %q missing from %v", want, terms)
		}
	}
	for _, unwanted := range []string{"bal", "csect", "mvc", "ghostrem", "program"} {
		if slices.Contains(terms, unwanted) {
			t.Errorf("operation or remark term %q in %v", unwanted, terms)
		}
	}
}
