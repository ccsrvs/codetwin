package splitter

import (
	"regexp"
	"strings"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// ── Assembly ──────────────────────────────────────────────────────────────────
//
// Assembly has no braces or indentation to follow, so each dialect's
// splitter looks for the markers real code uses to delimit routines
// (measured across musl, dav1d, libjpeg-turbo, Go, glibc, the Linux
// kernel, OpenSBI, boost.context, and 7-Zip):
//
//	GAS    a label named by .globl/.global or `.type x, @function`
//	       (anywhere in the file), or a routine macro: ENTRY(x),
//	       SYM_FUNC_START(x), dav1d's `function x`, MIPS LEAF/NESTED and
//	       .ent, ARM .func. Ends at endfunc, END(x), SYM_FUNC_END,
//	       ENDPROC, .size, .cfi_endproc, .endfunc, or .end.
//	NASM   `cglobal x` (x86inc), or a label named by `global x` or
//	       GLOBAL_FUNCTION(x); a cglobal inside %macro ends at %endmacro.
//	MASM   `x PROC` … `ENDP` (also armasm's |x| PROC and bare ENDP,
//	       7-Zip's MY_PROC/MY_ENDP, armasm LEAF_ENTRY/NESTED_ENTRY).
//	Plan 9 `TEXT sym(SB)`, running to the next TEXT, DATA, or GLOBL.
//
// Local labels (.Lx, 1:, .x in NASM, @@ in MASM) and labels nothing
// exports never start a routine. Consecutive exported labels with no
// instruction between them are aliases of one routine. A routine
// without an end marker runs to the next one, minus the trailing blank
// lines and declaration/alignment directives that belong to the next.

type asmRoutine struct {
	start  int // 0-based line
	symbol string
	// macroEnd: this routine started inside a NASM %macro, so it ends
	// at the %endmacro.
	macroEnd bool
}

type asmDialect struct {
	starts func(clean []string) []asmRoutine
	// endAt reports whether line i (comment-stripped) closes routine r.
	endAt func(r asmRoutine, line string) bool
	// stopBefore, when set, reports a line that ends the routine just
	// before it (Plan 9's DATA and GLOBL after a TEXT block).
	stopBefore func(line string) bool
	// trailing reports lines trimmed from the end of a routine that ran
	// into the next one.
	trailing *regexp.Regexp
}

func splitAsm(code string, lang tokenizer.Language) []Chunk {
	var d asmDialect
	switch lang {
	case tokenizer.AsmNASM:
		d = nasmDialect
	case tokenizer.AsmMASM:
		d = masmDialect
	case tokenizer.AsmPlan9:
		d = plan9Dialect
	default:
		d = gasDialect
	}
	lines := strings.Split(code, "\n")
	clean := strings.Split(tokenizer.StripComments(code, lang), "\n")
	routines := d.starts(clean)

	var chunks []Chunk
	for k, r := range routines {
		limit := len(clean) - 1
		if k+1 < len(routines) {
			limit = routines[k+1].start - 1
		}
		end, closed := limit, false
		for j := r.start + 1; j <= limit; j++ {
			if d.endAt(r, clean[j]) {
				end, closed = j, true
				break
			}
			if d.stopBefore != nil && d.stopBefore(clean[j]) {
				end = j - 1
				break
			}
		}
		if !closed {
			for end > r.start && (strings.TrimSpace(clean[end]) == "" || d.trailing.MatchString(clean[end])) {
				end--
			}
		}
		chunks = append(chunks, chunkSpan(lines, r.start, end, r.symbol, ""))
	}
	return chunks
}

// asmLabel matches a label definition and captures its name.
var asmLabel = regexp.MustCompile(`^\s*([A-Za-z_$][\w.$]*)\s*:(?:[^:=]|$)`)

// asmInstruction reports whether a comment-stripped line holds code
// beyond labels and directives — used to tell alias labels (nothing
// between them) from separate routines.
func asmInstruction(line string) bool {
	t := strings.TrimSpace(line)
	for {
		m := asmLabel.FindStringSubmatchIndex(t)
		if m == nil {
			break
		}
		t = strings.TrimSpace(t[m[3]+1:])
	}
	return t != "" && !strings.HasPrefix(t, ".") && !strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "%")
}

// ── GAS ──

var (
	gasGlobalRe     = regexp.MustCompile(`^\s*\.(?:globl|global)\s+(.+)`)
	gasTypeFuncRe   = regexp.MustCompile(`^\s*\.type\s+([\w.$]+)\s*,\s*["@%#]?(?:function|gnu_indirect_function|STT_FUNC)\b`)
	gasTypeObjectRe = regexp.MustCompile(`^\s*\.type\s+([\w.$]+)\s*,\s*["@%#]?(?:object|STT_OBJECT|tls_object|common)\b`)
	// gasCoffFuncRe: a Windows COFF symbol definition of function type
	// (`.def f; .scl 2; .type 32; .endef`).
	gasCoffFuncRe = regexp.MustCompile(`^\s*\.def\s+([\w.$@]+)\s*;.*\.type\s+32\b`)
	// gasLabel also accepts a dot-prefixed name, which only starts a
	// routine when exported (AIX's `.globl .f` entry points); .Lx and
	// other local labels are never exported.
	gasLabel = regexp.MustCompile(`^\s*(\.?[A-Za-z_$][\w.$]*)\s*:(?:[^:=]|$)`)
	// gasWrappedLabel: a label spelled through a name macro, such as
	// cgo's EXT(crosscall1): — declared as .globl EXT(crosscall1).
	gasWrappedLabel = regexp.MustCompile(`^\s*(\w+\(\s*([\w.$]+)\s*\))\s*:`)
	gasMacroStart   = regexp.MustCompile(`^\s*(?:(?:ENTRY|ENTRY_P2ALIGN|ENTRY_CHK|ENTRY_ALIGN|WEAK_ENTRY|LEAF|NESTED|SYM_(?:TYPED_)?(?:FUNC|CODE)_START\w*)\s*\(\s*([\w.$]+)|function\s+([^\s,]+)|\.ent\s+([\w.$]+)|\.func\s+([\w.$]+))`)
	gasEndRe        = regexp.MustCompile(`^\s*(?:endfunc\b|END(?:_\w+)?\s*\(|SYM_(?:FUNC|CODE)_END\w*\s*\(|ENDPROC\s*\(|\.size\s|\.cfi_endproc\b|\.endfunc\b|\.end\s+[\w.$]+)`)
	gasTrailingRe   = regexp.MustCompile(`^\s*(?:\.(?:globl|global|type|hidden|weak|protected|internal|p2align|balign|align|section|text|data|previous|size|local|extern|def|scl|endef)\b|#)`)
	gasMacroDefRe   = regexp.MustCompile(`^\s*\.macro\b`)
)

// gasDeclared splits a .globl operand list into normalized names:
// `a, b` and wrapped names like `EXT( x )` → EXT(x).
func gasDeclared(list string) []string {
	var names []string
	depth, from := 0, 0
	for i := 0; i <= len(list); i++ {
		if i < len(list) {
			switch list[i] {
			case '(':
				depth++
				continue
			case ')':
				depth--
				continue
			case ',':
				if depth > 0 {
					continue
				}
			default:
				continue
			}
		}
		name := strings.Join(strings.Fields(list[from:i]), "")
		if k := strings.IndexByte(name, '['); k > 0 && !strings.Contains(name, "(") {
			name = name[:k] // XCOFF storage-mapping class: jump_fcontext[DS]
		}
		if name != "" {
			names = append(names, name)
		}
		from = i + 1
	}
	return names
}

var gasDialect = asmDialect{
	starts: func(clean []string) []asmRoutine {
		exported, objects := map[string]bool{}, map[string]bool{}
		for _, l := range clean {
			if m := gasGlobalRe.FindStringSubmatch(l); m != nil {
				for _, n := range gasDeclared(m[1]) {
					exported[n] = true
				}
			}
			if m := gasTypeFuncRe.FindStringSubmatch(l); m != nil {
				exported[m[1]] = true
			}
			if m := gasCoffFuncRe.FindStringSubmatch(l); m != nil {
				exported[m[1]] = true
			}
			if m := gasTypeObjectRe.FindStringSubmatch(l); m != nil {
				objects[m[1]] = true
			}
		}
		var out []asmRoutine
		code := false // an instruction since the last routine start
		for i, l := range clean {
			if m := gasMacroStart.FindStringSubmatch(l); m != nil {
				out = append(out, asmRoutine{start: i, symbol: firstNonEmpty(m[1:])})
				code = false
				continue
			}
			symbol := ""
			if m := gasLabel.FindStringSubmatch(l); m != nil && exported[m[1]] && !objects[m[1]] {
				symbol = strings.TrimPrefix(m[1], ".")
			} else if m := gasWrappedLabel.FindStringSubmatch(l); m != nil &&
				exported[strings.Join(strings.Fields(m[1]), "")] {
				symbol = m[2]
			}
			if symbol != "" {
				if len(out) == 0 || code {
					out = append(out, asmRoutine{start: i, symbol: symbol})
				}
				code = asmInstruction(l)
				continue
			}
			if asmInstruction(l) {
				code = true
			}
		}
		return out
	},
	endAt:      func(_ asmRoutine, line string) bool { return gasEndRe.MatchString(line) },
	stopBefore: gasMacroDefRe.MatchString,
	trailing:   gasTrailingRe,
}

func firstNonEmpty(xs []string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// ── NASM ──

var (
	nasmGlobalRe   = regexp.MustCompile(`(?i)^\s*global\s+([\w.$?@]+(?:\s*:\s*\w+)?(?:\s*,\s*[\w.$?@]+(?:\s*:\s*\w+)?)*)`)
	nasmGlobalFnRe = regexp.MustCompile(`^\s*GLOBAL_FUNCTION\s*\(\s*(\w+)\s*\)`)
	nasmGlobalData = regexp.MustCompile(`^\s*GLOBAL_DATA\s*\(\s*(\w+)\s*\)`)
	nasmCglobalRe  = regexp.MustCompile(`^\s*cglobal\s+([^\s,]+)`)
	nasmExtnRe     = regexp.MustCompile(`^\s*EXTN\s*\(\s*(\w+)\s*\)\s*:`)
	nasmMacroRe    = regexp.MustCompile(`(?i)^\s*%i?macro\b`)
	nasmEndMacroRe = regexp.MustCompile(`(?i)^\s*%endmacro\b`)
	nasmTrailingRe = regexp.MustCompile(`(?i)^\s*(?:%i?macro\b|%if|%else|%endif|%define|%undef|%assign|global\b|extern\b|section\b|segment\b|align\b|INIT_[XYZ]MM\b|\[)`)
)

var nasmDialect = asmDialect{
	starts: func(clean []string) []asmRoutine {
		globals, data := map[string]bool{}, map[string]bool{}
		for _, l := range clean {
			if m := nasmGlobalData.FindStringSubmatch(l); m != nil {
				data[m[1]] = true
			}
			if m := nasmGlobalRe.FindStringSubmatch(l); m != nil {
				for _, n := range strings.Split(m[1], ",") {
					name, _, _ := strings.Cut(strings.TrimSpace(n), ":")
					globals[strings.TrimSpace(name)] = true
				}
			}
			if m := nasmGlobalFnRe.FindStringSubmatch(l); m != nil {
				globals[m[1]] = true
			}
		}
		var out []asmRoutine
		depth := 0
		code := false
		for i, l := range clean {
			switch {
			case nasmMacroRe.MatchString(l):
				depth++
			case nasmEndMacroRe.MatchString(l):
				if depth > 0 {
					depth--
				}
			}
			var symbol string
			if m := nasmCglobalRe.FindStringSubmatch(l); m != nil {
				symbol = m[1]
			} else if m := nasmExtnRe.FindStringSubmatch(l); m != nil && !data[m[1]] {
				symbol = m[1]
			} else if m := asmLabel.FindStringSubmatch(l); m != nil && globals[m[1]] && !strings.HasPrefix(m[1], ".") {
				if len(out) > 0 && !code {
					code = asmInstruction(l)
					continue // alias of the routine just started
				}
				symbol = m[1]
			}
			if symbol != "" {
				out = append(out, asmRoutine{start: i, symbol: symbol, macroEnd: depth > 0})
				code = asmInstruction(l) && !nasmCglobalRe.MatchString(l)
				continue
			}
			if asmInstruction(l) {
				code = true
			}
		}
		return out
	},
	endAt: func(r asmRoutine, line string) bool {
		return r.macroEnd && nasmEndMacroRe.MatchString(line)
	},
	// A new macro definition ends a routine written at file level.
	stopBefore: nasmMacroRe.MatchString,
	trailing:   nasmTrailingRe,
}

// ── MASM / armasm ──

var (
	masmProcRe     = regexp.MustCompile(`(?i)^\s*([\w@?$|.]+)\s+PROC\b`)
	masmMyProcRe   = regexp.MustCompile(`^\s*MY_PROC\s+([\w@?$]+)`)
	masmEntryRe    = regexp.MustCompile(`^\s*(?:LEAF|NESTED)_ENTRY\s+([\w@?$|]+)`)
	masmEndRe      = regexp.MustCompile(`(?i)^\s*(?:(?:[\w@?$|.]+\s+)?ENDP\b|MY_ENDP\b|(?:LEAF|NESTED)_END\b)`)
	masmTrailingRe = regexp.MustCompile(`(?i)^\s*(?:ALIGN\b|PUBLIC\b|EXTRN\b|EXPORT\b|IMPORT\b|AREA\b|\.code\b|_TEXT\s+SEGMENT\b)`)
)

var masmDialect = asmDialect{
	starts: func(clean []string) []asmRoutine {
		var out []asmRoutine
		for i, l := range clean {
			for _, re := range []*regexp.Regexp{masmProcRe, masmMyProcRe, masmEntryRe} {
				if m := re.FindStringSubmatch(l); m != nil {
					out = append(out, asmRoutine{start: i, symbol: strings.Trim(m[1], "|")})
					break
				}
			}
		}
		return out
	},
	endAt:    func(_ asmRoutine, line string) bool { return masmEndRe.MatchString(line) },
	trailing: masmTrailingRe,
}

// ── Plan 9 (Go) ──

var (
	plan9TextRe     = regexp.MustCompile(`^\s*TEXT\s+([^\s(,]*)\(SB\)`)
	plan9NextRe     = regexp.MustCompile(`^\s*(?:TEXT|DATA|GLOBL)\s`)
	plan9TrailingRe = regexp.MustCompile(`^\s*#`)
)

var plan9Dialect = asmDialect{
	starts: func(clean []string) []asmRoutine {
		var out []asmRoutine
		for i, l := range clean {
			if m := plan9TextRe.FindStringSubmatch(l); m != nil {
				out = append(out, asmRoutine{start: i, symbol: plan9Symbol(m[1])})
			}
		}
		return out
	},
	endAt: func(_ asmRoutine, line string) bool { return false },
	// A TEXT block runs to the next TEXT (the next routine) or to the
	// DATA/GLOBL declarations that follow it.
	stopBefore: plan9NextRe.MatchString,
	trailing:   plan9TrailingRe,
}

// plan9Symbol reduces a TEXT symbol to the Go name: `·Add`,
// `runtime·memmove<ABIInternal>`, and `helper<>` become Add, memmove,
// and helper.
func plan9Symbol(sym string) string {
	if i := strings.LastIndex(sym, "·"); i >= 0 {
		sym = sym[i+len("·"):]
	}
	if i := strings.IndexByte(sym, '<'); i >= 0 {
		sym = sym[:i]
	}
	return sym
}
