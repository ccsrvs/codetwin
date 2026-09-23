package splitter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// ── HLASM ─────────────────────────────────────────────────────────────────────
//
// HLASM programs are organized in control sections and macros, measured
// on 20.7K publicly available HLASM files:
//
//  1. MACRO … MEND (nesting counted) is one routine, named by the macro
//     prototype's operation field — the statement after MACRO.
//  2. `name CSECT|RSECT|START` opens a section routine; the next section,
//     DSECT, COM, LOCTR, END, or MACRO closes it. DSECTs and COM areas are
//     data maps, not routines.
//  3. A section is split at every label that a BAL, BAS, BRAS, BRASL,
//     JAS, or JASL in the same file calls: the internal subroutines
//     (96% of call targets are local). Labels that are only branched to
//     do not split, and returns (BR 14) are not reliable end markers —
//     subroutines return through many registers.
//  4. A file with neither sections nor macros (a COPY member, generated
//     code) is one routine, still split at called labels.
//  5. END ends a program, not the file: a batch assembly holds several
//     programs, each ending in END.
//
// The splitter reads tokenizer.StripComments' normalized statements, so
// comments, remarks, and continuation lines never produce a marker.
// Symbols are uppercased: HLASM is case-insensitive.

var (
	hlasmCallOps     = nameSet("BAL", "BAS", "BRAS", "BRASL", "JAS", "JASL")
	hlasmCallTarget  = regexp.MustCompile(`^[^,]*,([A-Za-z@#$_][A-Za-z0-9@#$_]*)`)
	hlasmSectionOps  = nameSet("CSECT", "RSECT", "START")
	hlasmSectionEnds = nameSet("DSECT", "COM", "LOCTR", "END")
)

type hlasmStmt struct {
	name, op, operands string
}

// parseHLASMStmt splits a normalized statement ("name op operands", or
// " op operands" without a name).
func parseHLASMStmt(line string) (hlasmStmt, bool) {
	if strings.TrimSpace(line) == "" {
		return hlasmStmt{}, false
	}
	var st hlasmStmt
	rest := line
	if line[0] != ' ' {
		name, after, _ := strings.Cut(line, " ")
		st.name, rest = name, after
	}
	fields := strings.SplitN(strings.TrimLeft(rest, " "), " ", 2)
	st.op = fields[0]
	if len(fields) == 2 {
		st.operands = fields[1]
	}
	return st, st.op != ""
}

func splitHLASM(code string) []Chunk {
	lines := strings.Split(code, "\n")
	clean := strings.Split(tokenizer.StripComments(code, tokenizer.AsmHLASM), "\n")
	ends := tokenizer.HLASMStatementEnds(code)
	stmts := make([]hlasmStmt, len(clean))
	ok := make([]bool, len(clean))
	for i, l := range clean {
		stmts[i], ok[i] = parseHLASMStmt(l)
	}

	// Pass 1: call targets outside macro definitions, and whether the
	// file has any structure at all.
	targets := map[string]bool{}
	structured := false
	depth := 0
	for i := range stmts {
		if !ok[i] {
			continue
		}
		op := strings.ToUpper(stmts[i].op)
		switch {
		case op == "MACRO":
			depth++
			structured = true
		case op == "MEND":
			if depth > 0 {
				depth--
			}
		case depth > 0:
		case hlasmSectionOps[op]:
			structured = true
		case hlasmCallOps[op]:
			if m := hlasmCallTarget.FindStringSubmatch(stmts[i].operands); m != nil {
				targets[strings.ToUpper(m[1])] = true
			}
		}
	}

	var chunks []Chunk
	open, openSym := -1, ""
	closeAt := func(end int) { // end is exclusive
		if open < 0 {
			return
		}
		last := end - 1
		for last > open && strings.TrimSpace(clean[last]) == "" {
			last--
		}
		// Blank clean lines after the last statement may be its
		// continuation records.
		last = min(ends[last], end-1)
		chunks = append(chunks, chunkSpan(lines, open, last, openSym, ""))
		open = -1
	}
	start := func(i int, sym string) {
		closeAt(i)
		open, openSym = i, sym
	}

	depth = 0
	macroStart, macroSym := -1, ""
	for i := range stmts {
		if !ok[i] {
			continue
		}
		st := stmts[i]
		op := strings.ToUpper(st.op)
		if depth > 0 {
			switch {
			case op == "MACRO":
				depth++
			case op == "MEND":
				depth--
				if depth == 0 {
					chunks = append(chunks, chunkSpan(lines, macroStart, ends[i], macroSym, ""))
				}
			case macroSym == "":
				// The prototype statement; the tokenizer marks its
				// operation (the macro's name) as a variable symbol.
				macroSym = strings.ToUpper(strings.TrimPrefix(st.op, "&"))
			}
			continue
		}
		name := strings.ToUpper(st.name)
		switch {
		case op == "MACRO":
			closeAt(i)
			depth, macroStart, macroSym = 1, i, ""
		case hlasmSectionOps[op]:
			sym := name
			if sym == "" {
				sym = fmt.Sprintf("%s@L%d", op, i+1) // private (unnamed) code
			}
			start(i, sym)
		case hlasmSectionEnds[op]:
			closeAt(i)
			if op == "END" {
				// One program of a batch ends. The next starts with its
				// own section or macro; anything else (the JCL or
				// linkage-editor statements after a deck) is not code.
				structured = true
			}
		case name != "" && targets[name]:
			start(i, name)
		case open < 0 && !structured:
			start(i, fmt.Sprintf("CODE@L%d", i+1))
		}
	}
	if depth > 0 && macroStart >= 0 {
		// A macro without MEND runs to the end of the file.
		open, openSym = macroStart, macroSym
	}
	closeAt(len(clean))
	return chunks
}
