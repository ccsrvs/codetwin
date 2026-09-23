package tokenizer

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// AsmHLASM is IBM's High Level Assembler (z/OS, z/VM, z/VSE). Unlike the
// other assembly dialects it is a fixed-format language: a statement's
// name field starts in column 1, text ends at column 71, a non-blank
// column 72 continues the statement at column 16 of the next line, and
// columns 73-80 hold sequence numbers. Everything after the operands is
// a free-text remark with no comment marker, which is why HLASM needs a
// statement normalizer rather than comment regexes. The rules follow a
// study of 20.7K publicly available HLASM files and the Eclipse Che4z
// HLASM language server's margins and attribute rule.
const AsmHLASM Language = "asm-hlasm"

// hlasmExts are claimed by extension; hlasmContentExts only when the
// content looks like HLASM (.mac is also MACRO-11, NASM, and VMS).
var (
	hlasmExts        = []string{".hlasm", ".mlc", ".assemble", ".asmpgm", ".asmmac"}
	hlasmContentExts = []string{".mac", ".macro"}
)

func hasSuffixFold(name string, exts []string) bool {
	lower := strings.ToLower(name)
	for _, e := range exts {
		if strings.HasSuffix(lower, e) {
			return true
		}
	}
	return false
}

// ClaimedByContentOnly reports whether a file with this name is scanned
// only when its content identifies its language (a .mac file is HLASM
// only if it reads as HLASM). Detect returns Unknown for the others,
// and callers skip them.
func ClaimedByContentOnly(filename string) bool {
	return hasSuffixFold(filename, hlasmContentExts)
}

const hlSym = `[A-Za-z@#$_][A-Za-z0-9@#$_]*`

// hlasmDetectRe is one line-anchored HLASM-only construct: a section
// declaration, USING with a base, standard linkage (STM 14,12), SAVE,
// DS 0H, a typed DC/DS, conditional assembly, MNOTE, AMODE/RMODE,
// MVI/CLI with a typed immediate, SETx, a .* comment, or a macro
// prototype. It matched 97.4% of 20,666 HLASM files and 4 of 6,365
// files in other assembly dialects (files that embed HLASM).
var hlasmDetectRe = regexp.MustCompile(`(?mi)^(?:` + hlSym + `|\.` + hlSym + `|&` + hlSym + `)?[ \t]+(?:` +
	`(?:CSECT|RSECT|DSECT)(?:[ \t]|$)` +
	`|USING[ \t]+(?:\*|` + hlSym + `)(?:[+-]\d+)?,` +
	`|STMG?[ \t]+(?:R?14|@14|GR14|RE),(?:R?12|@12|GR12|RC),` +
	`|SAVE[ \t]+\(14,12\)` +
	`|DS[ \t]+0[HFD]\b` +
	`|D[SC][ \t]+\d*[CXFHAVPDBZ](?:L\d+)?(?:['(]|[ \t]|$)` +
	`|(?:AIF|AGO)[ \t]+[(.]` +
	`|MNOTE[ \t]` +
	`|[AR]MODE[ \t]+(?:24|31|64|ANY)\b` +
	`|(?:MVI|CLI)[ \t]+[^,\s]+,(?:X'[0-9A-F]{2}'|C'.'|B'[01]{1,8}')` +
	`)` +
	`|^&` + hlSym + `(?:\([^)\n]*\))?[ \t]+SET[ABC][ \t]` +
	`|^\.\*` +
	`|^[ \t]+MACRO[ \t]*\n(?:[.*][^\n]*\n)*(?:&` + hlSym + `)?[ \t]+` + hlSym + `[ \t]+&`)

// hlasmContent reports whether code reads as HLASM. It tests the raw
// text: stripping // or /* */ first (as the other dialects' sniffing
// does) would cut recall on macro files.
func hlasmContent(code string) bool {
	return hlasmDetectRe.MatchString(strings.ReplaceAll(code, "\r\n", "\n"))
}

// Operations whose statements have no operand field (everything after
// the operation is remarks), and conditional-assembly operations, whose
// parenthesized expressions may contain blanks.
var (
	hlasmZeroOps = nameSet(strings.Fields(`CSECT DSECT RSECT COM LOCTR LTORG EJECT AEJECT ANOP
		MACRO MEND MEXIT REPRO CXD PR SAM24 SAM31 SAM64 TAM UPT CSCH HSCH RSCH XSCH IPK
		PTLB PALB PCKMO PFPO PTFF SCKPF SCHM TEND TRAP2 NNPA PCC RCHP SAL`)...)
	hlasmCAOps = nameSet(strings.Fields(`AIF AGO SETA SETB SETC SETAF SETCF ACTR AIFB AGOB
		GBLA GBLB GBLC LCLA LCLB LCLC AREAD MNOTE`)...)
)

// hlasmString returns the length of the string literal that s starts
// with (a doubled quote is a quote inside it), or 0 when none closes on
// this line. As with a backtracking match, a string that never closes
// but holds a doubled quote ends at the first quote of the last pair.
func hlasmString(s string) int {
	lastPair := -1
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\n':
			return lastPair + 1
		case '\'':
			if i+1 < len(s) && s[i+1] == '\'' {
				lastPair = i
				i++
				continue
			}
			return i + 1
		}
	}
	return lastPair + 1
}

// hlasmAttribute matches an attribute reference such as L'FIELD at the
// start of s, returning its length (0 when there is none).
func hlasmAttribute(s string) int {
	if len(s) < 3 || s[1] != '\'' || !strings.ContainsRune("LTKNDISOltkndiso", rune(s[0])) {
		return 0
	}
	if c := s[2]; !hlasmSymChar(c) && c != '&' && c != '=' && c != '*' || c >= '0' && c <= '9' {
		return 0
	}
	n := 3
	for n < len(s) && hlasmSymChar(s[n]) {
		n++
	}
	return n
}

// hlasmWord returns the length of the run of symbol, &, and . characters
// that s starts with.
func hlasmWord(s string) int {
	n := 0
	for n < len(s) && (hlasmSymChar(s[n]) || s[n] == '&' || s[n] == '.') {
		n++
	}
	return n
}

func hlasmSymChar(c byte) bool {
	return c == '@' || c == '#' || c == '$' || c == '_' ||
		c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// hlasmNormalize rewrites HLASM source into one logical statement per
// line, keeping the line count: comments, remarks, sequence numbers, and
// continuation markers are removed, a continued statement is joined onto
// its first line (the continuation lines become blank), and each
// statement is emitted as "name op operands" — with a leading blank
// when it has no name. String literals are kept (a program name in
// DC CL8'PGM' is a reference); an attribute reference such as L'FIELD,
// which contains a quote but is not a string, becomes LATTR FIELD. The
// operation of a macro prototype (the statement after MACRO) gets a
// leading &: it is the name of the macro being defined, not a use.
func hlasmNormalize(code string) string {
	lines := strings.Split(code, "\n")
	out := make([]string, len(lines))
	prototype := false // the next statement is a macro prototype
	hlasmRecords(lines, func(i, _ int, segs []string) {
		if text := segs[0]; !strings.HasPrefix(text, "*") && !strings.HasPrefix(text, ".*") {
			stmt := hlasmStatement(segs)
			name, op, rest := hlasmFields(stmt)
			switch {
			case op == "":
			case prototype:
				// The prototype's operation is the macro being defined,
				// a name like any other: &op reads as a variable symbol.
				stmt, prototype = name+" &"+op+rest, false
			case strings.EqualFold(op, "MACRO"):
				prototype = true
			}
			out[i] = stmt
		}
	})
	return strings.Join(out, "\n")
}

// hlasmRecords groups physical records into statements, calling fn with
// the first and last line of each and the text of every record in it
// (columns 1-71, then each continuation from column 16).
func hlasmRecords(lines []string, fn func(first, last int, segs []string)) {
	for i := 0; i < len(lines); {
		text, cont := hlasmCard(lines[i])
		last := i
		segs := []string{text}
		for cont && last+1 < len(lines) {
			next, nextCont := hlasmCard(lines[last+1])
			// A continuation resumes in column 16; anything else (comment
			// text that happens to reach column 72) ends the statement.
			if len(next) < 15 || strings.TrimLeft(next[:15], " ") != "" {
				break
			}
			segs = append(segs, next[15:])
			last++
			cont = nextCont
		}
		fn(i, last, segs)
		i = last + 1
	}
}

// HLASMStatementEnds maps each line of HLASM code to the last line of the
// statement that begins on it, so a span ending on a continued statement
// can take in its continuation records. Other lines map to themselves.
func HLASMStatementEnds(code string) []int {
	lines := strings.Split(code, "\n")
	ends := make([]int, len(lines))
	for i := range ends {
		ends[i] = i
	}
	hlasmRecords(lines, func(first, last int, _ []string) { ends[first] = last })
	return ends
}

// hlasmFields splits a statement from hlasmStatement into its name, its
// operation, and the rest (the operands with their leading blank).
func hlasmFields(stmt string) (name, op, rest string) {
	name, after, _ := strings.Cut(stmt, " ")
	op, rest, _ = strings.Cut(after, " ")
	if rest != "" {
		rest = " " + rest
	}
	return name, op, rest
}

// hlasmCard returns a physical line's statement text (columns 1-71) and
// whether column 72 continues it. Columns count characters, and an
// invalid byte (Latin-1 source) is one column.
func hlasmCard(line string) (string, bool) {
	line = strings.TrimSuffix(line, "\r")
	off := 0
	for col := 0; col < 71 && off < len(line); col++ {
		if line[off] < utf8.RuneSelf {
			off++
		} else {
			_, n := utf8.DecodeRuneInString(line[off:])
			off += n
		}
	}
	if off >= len(line) {
		return line, false
	}
	return line[:off], line[off] != ' '
}

// hlasmStatement normalizes one logical statement from its physical
// segments (the first line's columns 1-71, then columns 16-71 of each
// continuation line).
func hlasmStatement(segs []string) string {
	first := segs[0]
	if strings.TrimSpace(first) == "" {
		return ""
	}
	name, rest := "", first
	if first[0] != ' ' && first[0] != '\t' {
		k := strings.IndexAny(first, " \t")
		if k < 0 {
			return first
		}
		name, rest = first[:k], first[k:]
	}
	rest = strings.TrimLeft(rest, " \t")
	k := strings.IndexAny(rest, " \t")
	op := rest
	if k >= 0 {
		op, rest = rest[:k], strings.TrimLeft(rest[k:], " \t")
	} else {
		rest = ""
	}
	opU := strings.ToUpper(op)
	var operands string
	switch {
	case hlasmZeroOps[opU]:
		// No operand field: the rest is remarks.
	case opU == "EXEC" && hlasmExecRe.MatchString(rest):
		// EXEC CICS/SQL/DLI commands have blanks in their operands and
		// no remarks field.
		operands = rest
		for _, s := range segs[1:] {
			operands += s
		}
		operands = strings.TrimRight(operands, " ")
	default:
		operands = hlasmOperands(rest, segs[1:], hlasmCAOps[opU])
	}
	stmt := name + " " + op
	if operands != "" {
		stmt += " " + operands
	}
	return stmt
}

var hlasmExecRe = regexp.MustCompile(`(?i)^(?:CICS|SQL|DLI)\b`)

// hlasmOperands scans the operand field term by term — strings,
// attribute references, words, and single characters — and stops at
// the first blank outside a string: what follows is a remark. On a
// continued statement, a blank right after a top-level comma ends only
// that physical line (HLASM's alternate format), and scanning resumes on
// the next one. Conditional-assembly operations allow blanks inside
// parentheses.
func hlasmOperands(first string, more []string, ca bool) string {
	var b strings.Builder
	segs := append([]string{first}, more...)
	depth := 0
	lastSig := byte(0) // last non-blank operand character emitted
	for s := 0; s < len(segs); s++ {
		text := segs[s]
		for pos := 0; pos < len(text); {
			c := text[pos]
			if c == ' ' || c == '\t' {
				if ca && depth > 0 {
					b.WriteByte(' ')
					pos++
					continue
				}
				if lastSig == ',' && s+1 < len(segs) {
					break // alternate format: the rest of this line is a remark
				}
				return strings.TrimRight(b.String(), " ")
			}
			if c == '\'' {
				if n := hlasmString(text[pos:]); n > 0 {
					m := text[pos : pos+n]
					if prev := b.Len(); prev > 0 && hlasmSymChar(b.String()[prev-1]) {
						b.WriteByte(' ') // CL8'X' -> CL8 'X'
					}
					b.WriteString(m)
					pos += len(m)
					lastSig = '\''
					continue
				}
				// A string continued onto the next line: rejoin the rest of
				// the statement and match it again; if it never closes,
				// it runs to the end of the statement.
				joined := text[pos:] + strings.Join(segs[s+1:], "")
				if hlasmString(joined) > 0 && s+1 < len(segs) {
					segs = []string{"", joined}
					s, text, pos = 0, "", 0
					continue
				}
				b.WriteString(joined)
				return strings.TrimRight(b.String(), " ")
			}
			if n := hlasmAttribute(text[pos:]); n > 0 {
				b.WriteString(strings.ToUpper(text[pos:pos+1]) + "ATTR " + text[pos+2:pos+n])
				pos += n
				lastSig = 'R'
				continue
			}
			if n := hlasmWord(text[pos:]); n > 0 {
				b.WriteString(text[pos : pos+n])
				pos += n
				lastSig = text[pos-1]
				continue
			}
			switch c {
			case '(':
				depth++
			case ')':
				if depth > 0 {
					depth--
				}
			}
			b.WriteByte(c)
			lastSig = c
			pos++
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// hlasmNumbers turns decimal terms outside string literals into NUM: a
// run of digits that no symbol character (or & or .) precedes and no
// symbol character follows, so WORK$1 and .L2 keep theirs. Digits
// directly before a letter are a duplication factor (18F, 0CL8, =2F'1'),
// since no symbol starts with a digit: they become NUM and a blank, so
// 18F, 18f, and 2F all read as NUM followed by the F designator.
func hlasmNumbers(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prev := byte('\n')
	for i := 0; i < len(s); {
		c := s[i]
		if c == '\'' {
			n := max(hlasmString(s[i:]), 1) // an unpaired quote is itself
			b.WriteString(s[i : i+n])
			prev, i = s[i+n-1], i+n
			continue
		}
		if c < '0' || c > '9' || hlasmSymChar(prev) || prev == '&' || prev == '.' {
			b.WriteByte(c)
			prev, i = c, i+1
			continue
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		switch {
		case j < len(s) && (s[j]|0x20 >= 'a' && s[j]|0x20 <= 'z'):
			b.WriteString("NUM ")
		case j == len(s) || !hlasmSymChar(s[j]):
			b.WriteString("NUM")
		default:
			b.WriteString(s[i:j]) // 1$A is not a number
		}
		prev, i = s[j-1], j
	}
	return b.String()
}

var (
	// Registers are written R14, 14, GR14, REG14, or @14 — bare numbers
	// in 40% of operands — so every spelling becomes NUM.
	hlasmRegisterRe = regexp.MustCompile(`(?i)^(?:R|GR|GPR|REG|@|#)(?:0?\d|1[0-5])$`)
	// hlasmDesignatorRe: a DC/DS/literal type with an optional length
	// modifier (C, X, F, A, V, CL8, XL2, AL4).
	hlasmDesignatorRe = regexp.MustCompile(`(?i)^[A-Z]{1,2}(?:L\d+)?$`)
	hlasmIdentRe      = regexp.MustCompile(`(?:^|[^A-Za-z0-9@#$_])([A-Za-z@#$_][A-Za-z0-9@#$_]*)`)
	hlasmKeywords     = nameSet("eq", "ne", "lt", "le", "gt", "ge", "and", "or", "not", "xor",
		"lattr", "tattr", "kattr", "nattr", "dattr", "iattr", "sattr", "oattr")
)

// hlasmOperand classifies a word in the operand field of a statement
// whose operation is op.
func hlasmOperand(op, seg string, start, end int) asmClass {
	word := seg[start:end]
	lower := strings.ToLower(word)
	switch {
	case start > 0 && seg[start-1] == '&':
		if strings.HasPrefix(lower, "sys") {
			return asmKeep // &SYSNDX, &SYSECT, ...
		}
		return asmName // a variable symbol, even as &KEY= in a prototype
	case hlasmRegisterRe.MatchString(word):
		return asmNum
	case hlasmKeywords[lower]:
		return asmKeep
	case end < len(seg) && seg[end] == '=' && !strings.HasPrefix(seg[end:], "=="):
		return asmKeep // macro keyword operand: LV=, SP=, EP=
	case hlasmDesignatorRe.MatchString(word):
		k := start - 1
		opU := strings.ToUpper(op)
		switch {
		case k >= 0 && seg[k] == '=' && (k == 0 || !hlasmSymChar(seg[k-1])):
			return asmKeep // literal: =F'1', =A(X), =CL8'X' (not KEY=PS)
		case (k < 0 || seg[k] == ' ' || seg[k] == ',') && (opU == "DC" || opU == "DS" || opU == "DXD"):
			return asmKeep
		case strings.HasPrefix(strings.TrimLeft(seg[end:], " "), "STR"):
			return asmKeep
		}
	}
	return asmName
}

// hlasmNotCode are the statements that describe the program rather than
// being it, stripped like C's preprocessor lines (PMD CPD drops both):
// COPY; the linkage declarations EXTRN, WXTRN, ENTRY, and ALIAS; EQU
// constants such as register equates and field offsets, which read as
// the same "VAR equ NUM" whatever they define, so unrelated tables
// matched as shared blocks; and listing control (TITLE, EJECT, SPACE,
// PRINT), which is page layout. EQU * is kept: it is a label on the next
// instruction. They run on hlasmNormalize's statements, so remarks and
// continuations are already gone. References() does not strip them, so
// an EQU operand still counts as a use.
var hlasmNotCode = []*regexp.Regexp{
	regexp.MustCompile(`(?mi)^\S*[ \t]+(?:COPY|EXTRN|WXTRN|ENTRY|ALIAS|TITLE|EJECT|AEJECT|SPACE|ASPACE|PRINT)(?:[ \t][^\n]*)?$`),
	regexp.MustCompile(`(?mi)^\S+[ \t]+EQU[ \t]+(?:[^*\n][^\n]*|\*[^\n]+)$`),
}

func init() {
	hlasm := &langPatterns{
		// The normalizer removes comment lines and remarks; this regex
		// only serves the shared strings-or-comments machinery.
		comments: regexp.MustCompile(`(?m)^\.?\*[^\n]*`),
		strip:    hlasmNormalize,
		imports:  hlasmNotCode,
		strings:  regexp.MustCompile(`'(?:[^'\n]|'')*'`),
		// Numbers need HLASM's symbol characters around them, so prepare
		// normalizes them and this pattern never matches.
		numbers: regexp.MustCompile(`[^\s\S]`),
		prepare: hlasmNumbers,
		asm: &asmSyntax{
			ident:        hlasmIdentRe,
			columnLabels: true,
			operand:      hlasmOperand,
		},
	}
	hlasm.stringsOrComments = regexp.MustCompile("(" + hlasm.strings.String() + ")|(?:" + hlasm.comments.String() + ")")
	patterns[AsmHLASM] = hlasm
}
