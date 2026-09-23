package tokenizer

import (
	"regexp"
	"strings"
)

// Assembly is four languages, one per syntax family, because comments,
// strings, and function markers differ between them and the extension
// does not tell them apart: .asm holds NASM, MASM, armasm, and even GAS,
// and .s holds GAS or Go's Plan 9 syntax. Detect picks the dialect from
// the file's content. armasm shares MASM's ';' comments and PROC/ENDP
// structure, so it is tokenized as MASM.
const (
	AsmGAS   Language = "asm-gas"   // GNU as: AT&T x86, AArch64, ARM, RISC-V, ...
	AsmNASM  Language = "asm-nasm"  // NASM/YASM, including x86inc (cglobal)
	AsmMASM  Language = "asm-masm"  // MASM and armasm
	AsmPlan9 Language = "asm-plan9" // Go's assembler
)

// IsAssembly reports whether lang is one of the assembly dialects.
func IsAssembly(lang Language) bool {
	switch lang {
	case AsmGAS, AsmNASM, AsmMASM, AsmPlan9, AsmHLASM:
		return true
	}
	return false
}

var (
	asmSniffComments = regexp.MustCompile(`/\*[\s\S]*?\*/|//[^\n]*`)
	plan9TextRe      = regexp.MustCompile(`(?m)^\s*(?:TEXT|GLOBL|DATA)\s+[^\s,]*\(SB\)`)
	plan9IncludeRe   = regexp.MustCompile(`#include\s+"(?:textflag\.h|go_asm\.h|funcdata\.h)"`)
	nasmRe           = regexp.MustCompile(`(?mi)^\s*(?:%(?:macro|imacro|define|xdefine|idefine|assign|include|ifdef|ifndef|if)\b|cglobal\b|INIT_[XYZ]MM\b|(?:section|segment)\s+\.?\w|bits\s+(?:16|32|64)\b|\[bits\b|default\s+rel\b|struc\s+\w|endstruc\b|(?:global|extern)\s+[\w.$]+(?::\s*function)?\s*(?:;.*)?$)`)
	armasmRe         = regexp.MustCompile(`(?m)^\s*(?:AREA\s|EXPORT\s|IMPORT\s|MACRO\s*$|MEND\b|(?:LEAF|NESTED)_ENTRY\b|TEXTAREA\b|\|[^|]+\|\s+PROC\b)`)
	masmRe           = regexp.MustCompile(`(?mi)^\s*(?:[\w@?$]+\s+(?:proc|endp)\b|\.code\b|_TEXT\s+segment\b|(?:public|extrn|externdef)\s+\w|MY_PROC\b)`)
)

// asmDialect picks the dialect of an assembly file from its content.
// .s/.S files are GAS or Plan 9 (ARM GAS's `.code 32` must not read as
// MASM's `.code`); .asm files are checked for NASM, then armasm, then
// MASM, and fall back to GAS. Without content (dead-code analysis of a
// file that yielded no chunks) the extension's usual dialect is used.
func asmDialect(filename, code string) Language {
	dotAsm := strings.HasSuffix(strings.ToLower(filename), ".asm")
	if code == "" {
		if dotAsm {
			return AsmNASM
		}
		return AsmGAS
	}
	c := asmSniffComments.ReplaceAllString(code, " ")
	if plan9TextRe.MatchString(c) || !dotAsm && plan9IncludeRe.MatchString(code) {
		return AsmPlan9
	}
	// HLASM before NASM/armasm/MASM (armasm shares MACRO/MEND, MASM
	// EXTRN) and before GAS (z/OS UNIX .s files).
	if hlasmContent(code) {
		return AsmHLASM
	}
	if dotAsm {
		switch {
		case nasmRe.MatchString(c):
			return AsmNASM
		case armasmRe.MatchString(c), masmRe.MatchString(c):
			return AsmMASM
		}
	}
	return AsmGAS
}

// asmNumbers covers hex (0x1F and NASM/MASM 1Fh), binary, decimal, and
// floats. GAS numeric local-label references (1b, 2f) are rewritten
// before this runs.
var asmNumbers = regexp.MustCompile(`\b(?:0[xX][0-9a-fA-F]+|0[bB][01]+|[0-9][0-9a-fA-F]*[hH]|\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)\b`)

// gasLabelRefRe finds numeric local-label references (`b.ne 1b`,
// `jmp 2f`). The guard keeps AArch64 arrangements like v0.16b intact.
var gasLabelRefRe = regexp.MustCompile(`(^|[^\w.])\d+[bf]\b`)

// gasArrangementRe finds AArch64 vector arrangements (v0.16b, v1.8h,
// v2.4s); ARM32's element types (vadd.i16) are words and already
// normalize.
var gasArrangementRe = regexp.MustCompile(`\.\d+[bhsdq]\b`)

// gasLabelRefs rewrites every numeric local-label reference to the same
// name, which then normalizes like any other label, and every AArch64
// arrangement to one element-size placeholder, so 8- and 16-bit
// variants of a routine match the way ARM32's do.
func gasLabelRefs(s string) string {
	s = gasArrangementRe.ReplaceAllString(s, ".ARR")
	return gasLabelRefRe.ReplaceAllString(s, "${1}LREF")
}

func init() {
	gas := &langPatterns{
		// One rule for every architecture (measured on 1,653 GAS files):
		// a # at line start (comment or cpp directive), a # after
		// whitespace that is followed by whitespace or ends the line
		// (ARM's #imm immediates never are), and ARM32's "@ " — while
		// x86's @function / @PLT operators have no space after the @.
		comments: regexp.MustCompile(`/\*[\s\S]*?\*/|//[^\n]*|(?m:^[ \t]*#[^\n]*$)|(?m:[ \t]#(?:[ \t][^\n]*)?$)|(?m:(?:^|[ \t])@(?:[ \t][^\n]*)?$)`),
		imports:  []*regexp.Regexp{regexp.MustCompile(`(?m)^[ \t]*\.include\b[^\n]*`)},
		strings:  regexp.MustCompile(`"(?:[^"\\\n]|\\.)*"`),
		numbers:  asmNumbers,
		prepare:  gasLabelRefs,
		asm:      &asmSyntax{register: generalRegister, keywords: gasKeywords, statementSep: true},
	}
	nasm := &langPatterns{
		comments: regexp.MustCompile(`;[^\n]*`),
		imports:  []*regexp.Regexp{regexp.MustCompile(`(?mi)^[ \t]*%include\b[^\n]*`)},
		strings:  regexp.MustCompile("\"(?:[^\"\\\\\\n]|\\\\.)*\"|'[^'\\n]*'|`(?:[^`\\\\\\n]|\\\\.)*`"),
		numbers:  asmNumbers,
		asm:      &asmSyntax{register: generalRegister, keywords: nasmKeywords, nameFirst: nasmNameFirst},
	}
	masm := &langPatterns{
		comments: regexp.MustCompile(`;[^\n]*`),
		imports:  []*regexp.Regexp{regexp.MustCompile(`(?mi)^[ \t]*include(?:lib)?\b[^\n]*`)},
		strings:  regexp.MustCompile(`"[^"\n]*"|'[^'\n]*'`),
		numbers:  asmNumbers,
		asm:      &asmSyntax{register: generalRegister, keywords: masmKeywords, nameFirst: masmNameFirst},
	}
	plan9 := &langPatterns{
		comments: regexp.MustCompile(`//[^\n]*|/\*[\s\S]*?\*/`),
		// cpp directives, including continued #define bodies.
		imports: []*regexp.Regexp{regexp.MustCompile(`(?m)^[ \t]*#(?:[^\n\\]|\\[\s\S])*`)},
		strings: regexp.MustCompile(`"(?:[^"\\\n]|\\.)*"`),
		numbers: asmNumbers,
		asm:     &asmSyntax{register: plan9Register, keywords: plan9Keywords, statementSep: true},
	}
	patterns[AsmGAS], patterns[AsmNASM], patterns[AsmMASM], patterns[AsmPlan9] = gas, nasm, masm, plan9
	for _, p := range []*langPatterns{gas, nasm, masm, plan9} {
		p.stringsOrComments = regexp.MustCompile("(" + p.strings.String() + ")|(?:" + p.comments.String() + ")")
	}
}

// asmSyntax drives position-based identifier normalization. Assembly
// has no fixed keyword list worth enumerating — thousands of mnemonics
// across architectures, plus macros used as instructions — so a word is
// kept by where it stands: the first word of a statement (mnemonic,
// directive, or macro), registers, and a few operand keywords. Labels
// and symbols normalize to VAR, so a renamed routine matches.
type asmSyntax struct {
	register func(word string) bool
	// keywords are operand-position words that are syntax in this
	// dialect (compared lowercased).
	keywords map[string]bool
	// nameFirst are keywords that follow the name they define, as in
	// `msg db 'hi'` or `Checksum PROC` (NASM and MASM only). The value
	// reports whether the keyword needs an operand after it, which
	// keeps `jb rest` (a jump to a label named rest) from reading as
	// NASM's `name rest count`.
	nameFirst    map[string]bool
	statementSep bool // ';' separates statements (GAS, Plan 9) instead of starting a comment
	// ident overrides the identifier pattern; its first submatch is the
	// identifier (HLASM symbols contain @ # $).
	ident *regexp.Regexp
	// columnLabels: a statement that starts in column 1 begins with its
	// name, which has no colon (HLASM's name field).
	columnLabels bool
	// operand, when set, classifies operand-field words, given the
	// statement's operation (HLASM).
	operand func(op, seg string, start, end int) asmClass
}

// asmClass is how an identifier is tokenized.
type asmClass int

const (
	asmName asmClass = iota // normalized to VAR
	asmKeep                 // kept, lowercased: an operation, register, or keyword
	asmNum                  // normalized to NUM (HLASM register spellings)
)

var (
	generalRegisterRe = regexp.MustCompile(`^(?:[re]?[abcd]x|[abcd][lh]|[re]?(?:si|di|sp|bp|ip)|(?:si|di|sp|bp)l|r(?:[89]|1[0-5])[dwb]?|[xyz]mm(?:[12]?\d|3[01])|k[0-7]|mm[0-7]|st|[c-gs]s|[xw](?:[12]?\d|30)|[xw]zr|wsp|lr|fp|[bhsdqvz](?:[12]?\d|3[01])|p(?:\d|1[0-5])|r(?:\d|1[0-5])|pc|a[0-7]|v[1-8]|zero|ra|gp|tp|t[0-6]|s(?:\d|1[01])|f[tsa]?\d{1,2}|r\d{1,2}[qdwbh]?|[xyz]?m\d{1,2})$`)
	plan9RegisterRe   = regexp.MustCompile(`^(?:AX|BX|CX|DX|SI|DI|BP|SP|[ABCD][LH]|R\d{1,2}[BWL]?|RSP|[XYZVF]\d{1,2}|K[0-7]|SB|FP|PC|ZR|g)$`)
)

func generalRegister(word string) bool { return generalRegisterRe.MatchString(strings.ToLower(word)) }
func plan9Register(word string) bool   { return plan9RegisterRe.MatchString(word) }

var (
	asmSizeKeywords = []string{"byte", "word", "dword", "qword", "tword", "oword",
		"yword", "zword", "xmmword", "ymmword", "zmmword", "mmword"}
	asmDataDirectives = []string{"db", "dw", "dd", "dq", "dt", "do", "dy", "dz",
		"df", "dp", "resb", "resw", "resd", "resq", "rest", "reso", "resy", "resz"}

	// gasKeywords: Intel-syntax size and address operators, AArch64/ARM
	// shifts, extends, and condition codes, and ELF symbol-type and
	// relocation operators (@function, @PLT, :lo12:).
	gasKeywords = nameSet(append(asmSizeKeywords,
		"ptr", "offset", "flat", "rel",
		"lsl", "lsr", "asr", "ror", "rrx", "msl", "mul", "vl",
		"uxtb", "uxth", "uxtw", "uxtx", "sxtb", "sxth", "sxtw", "sxtx",
		"eq", "ne", "cs", "hs", "cc", "lo", "mi", "pl", "vs", "vc", "hi", "ls",
		"ge", "lt", "gt", "le", "al", "nv",
		"function", "object", "progbits", "nobits", "notype", "note",
		"plt", "got", "gotpcrel", "gotoff", "gotntpoff", "gottpoff", "tpoff",
		"dtpoff", "ntpoff", "tlsgd", "tlsld", "tlsdesc", "lo12", "hi12")...)
	nasmKeywords = nameSet(append(asmSizeKeywords,
		"rel", "abs", "short", "near", "far", "strict", "wrt", "seg")...)
	masmKeywords = nameSet(append(asmSizeKeywords,
		"ptr", "offset", "flat", "short", "near", "far", "dup", "type",
		"sizeof", "length", "lengthof", "addr", "frame", "uses")...)
	// dataDirectives are kept in operand position only after a `times`
	// prefix (`times 16 db 0`); elsewhere a word like `rest` is a label.
	dataDirectives = nameSet(asmDataDirectives...)
	plan9Keywords  = nameSet("nosplit", "noframe", "dupok", "rodata", "noptr",
		"wrapper", "needctxt", "tlsbss", "topframe", "abiinternal", "abi0")

	// nasmNameFirst and masmNameFirst: true when the keyword needs an
	// operand (data, constants), false when it stands alone (PROC).
	nasmNameFirst = nameFirstSet(true, append(asmDataDirectives, "equ")...)
	masmNameFirst = mergeNameFirst(
		nameFirstSet(true, append(asmDataDirectives, "equ", "label", "textequ", "typedef", "record")...),
		nameFirstSet(false, "proc", "endp", "macro", "segment", "ends", "struc", "struct", "union"))
)

func nameFirstSet(needsOperand bool, names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = needsOperand
	}
	return m
}

func mergeNameFirst(a, b map[string]bool) map[string]bool {
	for k, v := range b {
		a[k] = v
	}
	return a
}

func nameSet(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

var (
	asmIdentRe = regexp.MustCompile(`\b[a-zA-Z_][a-zA-Z0-9_]*\b`)
	// asmLabelRe matches a statement's leading label definition:
	// `name:`, `.Lloop:`, NASM `.next:`, `$x:`. A second colon (C++
	// scope) or an = (GAS symbol assignment) is not a label.
	asmLabelRe = regexp.MustCompile(`^\s*[.$]?[A-Za-z_][\w.$]*\s*:`)
)

// identIndexes returns the [start, end) byte ranges of the identifiers
// in seg.
func (a *asmSyntax) identIndexes(seg string) [][]int {
	if a.ident == nil {
		return asmIdentRe.FindAllStringIndex(seg, -1)
	}
	var out [][]int
	for _, m := range a.ident.FindAllStringSubmatchIndex(seg, -1) {
		out = append(out, []int{m[2], m[3]})
	}
	return out
}

// classify reports each identifier on one line with how it is
// tokenized.
func (a *asmSyntax) classify(line string, visit func(start, end int, c asmClass)) {
	if a.columnLabels {
		a.classifyColumns(line, visit)
		return
	}
	stmts := [][2]int{{0, len(line)}}
	if a.statementSep && strings.Contains(line, ";") {
		stmts = stmts[:0]
		from := 0
		for i := 0; i < len(line); i++ {
			if line[i] == ';' {
				stmts = append(stmts, [2]int{from, i})
				from = i + 1
			}
		}
		stmts = append(stmts, [2]int{from, len(line)})
	}
	for _, st := range stmts {
		seg := line[st[0]:st[1]]
		labelEnd := 0
		if loc := asmLabelRe.FindStringIndex(seg); loc != nil &&
			!strings.HasPrefix(seg[loc[1]:], ":") && !strings.HasPrefix(seg[loc[1]:], "=") {
			labelEnd = loc[1]
		}
		idents := a.identIndexes(seg)
		first := -1
		for k, m := range idents {
			if m[0] < labelEnd {
				visit(st[0]+m[0], st[0]+m[1], asmName) // the label's own name
				continue
			}
			if first < 0 {
				first = k
			}
		}
		for k := first; k >= 0 && k < len(idents); k++ {
			m := idents[k]
			word := seg[m[0]:m[1]]
			var kept bool
			switch {
			case k == first:
				// A name-first definition (`msg db 'hi'`): the first
				// word is the name when such a keyword directly follows.
				kept = !(k+1 < len(idents) && a.definesName(seg, m, idents[k+1]))
			case k == first+1 && a.definesName(seg, idents[first], m):
				kept = true
			default:
				lower := strings.ToLower(word)
				kept = a.register(word) || a.keywords[lower] ||
					dataDirectives[lower] && strings.EqualFold(seg[idents[first][0]:idents[first][1]], "times")
			}
			c := asmName
			if kept {
				c = asmKeep
			}
			visit(st[0]+m[0], st[0]+m[1], c)
		}
	}
}

// classifyColumns handles a fixed-format statement ("name op operands",
// a leading blank when there is no name): identifiers in the name field
// are names, the operation is kept unless it is a variable symbol
// (&OP), and operand words go to the dialect's operand classifier.
func (a *asmSyntax) classifyColumns(line string, visit func(start, end int, c asmClass)) {
	nameEnd := 0
	if line != "" && line[0] != ' ' && line[0] != '\t' {
		if k := strings.IndexAny(line, " \t"); k >= 0 {
			nameEnd = k
		} else {
			nameEnd = len(line)
		}
	}
	op := ""
	for _, m := range a.identIndexes(line) {
		switch {
		case m[0] < nameEnd:
			visit(m[0], m[1], asmName)
		case op == "":
			op = line[m[0]:m[1]]
			if m[0] > 0 && line[m[0]-1] == '&' {
				visit(m[0], m[1], asmName) // a variable symbol as the operation
			} else {
				visit(m[0], m[1], asmKeep)
			}
		default:
			visit(m[0], m[1], a.operand(op, line, m[0], m[1]))
		}
	}
}

// definesName reports whether the word at kw directly follows the word
// at name and is a name-first keyword of this dialect — with an operand
// after it when the keyword needs one.
func (a *asmSyntax) definesName(seg string, name, kw []int) bool {
	if a.nameFirst == nil || strings.TrimSpace(seg[name[1]:kw[0]]) != "" {
		return false
	}
	needsOperand, ok := a.nameFirst[strings.ToLower(seg[kw[0]:kw[1]])]
	if !ok {
		return false
	}
	return !needsOperand || strings.TrimSpace(seg[kw[1]:]) != ""
}

// normalizeLine rewrites one line: kept words lowercased, names VAR,
// and the placeholders STR/NUM left alone.
func (a *asmSyntax) normalizeLine(line string) string {
	var b strings.Builder
	last := 0
	a.classify(line, func(start, end int, c asmClass) {
		b.WriteString(line[last:start])
		word := line[start:end]
		switch {
		case word == "STR" || word == "NUM":
			b.WriteString(word)
		case c == asmKeep:
			b.WriteString(strings.ToLower(word))
		case c == asmNum:
			b.WriteString("NUM")
		default:
			b.WriteString("VAR")
		}
		last = end
	})
	b.WriteString(line[last:])
	return b.String()
}

// names returns the words on a line that are names (not kept syntax):
// the content vocabulary LexicalTerms harvests.
func (a *asmSyntax) names(line string) []string {
	var out []string
	a.classify(line, func(start, end int, c asmClass) {
		if w := line[start:end]; c == asmName && w != "STR" && w != "NUM" && w != "LREF" {
			out = append(out, w)
		}
	})
	return out
}
