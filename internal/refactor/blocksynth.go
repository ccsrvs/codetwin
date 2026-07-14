package refactor

// Block-mode synthesis: turn a verified sub-function block clone
// (internal/blocks) into a starter helper, mirroring the v1 pair-level
// contract. The input is not a whole function but a statement run
// sliced out of each host, so the per-language emitters here do NOT
// look for a `func`/`def` header in the snippet — they wrap the span
// in a fresh helper signature instead. Parameters are deliberately
// left uninferred: a TODO comment lists the free identifiers the
// block appears to use (a lexical heuristic, not scope analysis) and
// the human finishes the extraction. Same "starter, human finishes"
// boundary as v1 --suggest.

import (
	"fmt"
	"strings"

	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// SliceBlock returns a pseudo-snippet containing only the block's line
// range of s.Code. startLine/endLine are 1-based absolute source lines
// (the BlockClone convention); they are clamped to the chunk. The
// result carries the caller-chosen name (usually the block's
// "file:start-end" range name), the original path/language, and the
// block's own StartLine so downstream previews and alignment holes
// keep real line numbers.
func SliceBlock(s scan.Snippet, startLine, endLine int, name string) scan.Snippet {
	lines := strings.Split(strings.TrimSuffix(s.Code, "\n"), "\n")
	first := startLine - s.StartLine
	last := endLine - s.StartLine
	if first < 0 {
		first = 0
	}
	if last > len(lines)-1 {
		last = len(lines) - 1
	}
	if last < first {
		last = first
	}
	return scan.Snippet{
		Name:      name,
		Path:      s.Path,
		Lang:      s.Lang,
		Code:      strings.Join(lines[first:last+1], "\n") + "\n",
		StartLine: s.StartLine + first,
		EndLine:   s.StartLine + last,
	}
}

// SynthesizeBlock dispatches block-mode synthesis by language. Go and
// Python are implemented; every other language returns a structured
// "not implemented" Note so the CLI can reject with a clear message
// (mirroring the v1 pair-level scope boundary).
//
// Before dispatch the block slices are trimmed to the aligned common
// region: the detector's line ranges are token-span bounds, which can
// drag in a divergent boundary line on each end (typically the hosts'
// own function headers) — those lines aren't part of the shared block
// and would poison the helper body, so leading/trailing holes are cut
// and the alignment recomputed. Interior holes (the rare edited line
// inside the block) survive and surface as divergence comments.
func SynthesizeBlock(a, b scan.Snippet, blockID string, al Alignment) Suggestion {
	if a.Lang != b.Lang {
		return Suggestion{Note: "rejected: cross-language extraction not supported"}
	}
	a, b, al = trimBlockToCommon(a, b, al)
	switch a.Lang {
	case tokenizer.Go:
		return synthesizeGoBlock(a, b, blockID, al)
	case tokenizer.Python:
		return synthesizePythonBlock(a, b, blockID, al)
	default:
		return Suggestion{Note: fmt.Sprintf(
			"block extraction not implemented for %s (supported: go, python)", a.Lang)}
	}
}

// maxBoundaryTrim caps how many lines trimBlockToCommon may cut from
// each edge of each side. The detector's token-span rounding drags in
// at most a line or two of the neighboring divergent code (typically
// the hosts' own function headers); anything deeper isn't rounding
// noise.
const maxBoundaryTrim = 2

// trimBlockToCommon drops the leading/trailing hole lines the
// detector's token-span rounding can include in a block's line range,
// then re-aligns the trimmed slices. Two guards keep it from eating
// real content:
//
//   - Trimming only happens when the line alignment is dense (common
//     lines cover at least half the smaller side). Renamed blocks
//     match at the normalized-token level but share few verbatim
//     lines, so their sparse alignment says nothing about boundaries —
//     they pass through untrimmed and the helper stays a literal copy
//     of A's slice.
//   - Each edge of each side loses at most maxBoundaryTrim lines.
//
// No common span → inputs pass through unchanged (the emitters reject
// that case anyway).
func trimBlockToCommon(a, b scan.Snippet, al Alignment) (scan.Snippet, scan.Snippet, Alignment) {
	if len(al.Common) == 0 {
		return a, b, al
	}
	aLines := a.EndLine - a.StartLine + 1
	bLines := b.EndLine - b.StartLine + 1
	minLines := aLines
	if bLines < minLines {
		minLines = bLines
	}
	if al.CommonLines()*2 < minLines {
		return a, b, al // sparse alignment (renamed block): don't trust it
	}
	first := al.Common[0]
	last := al.Common[len(al.Common)-1]
	// Alignment line numbers are 1-based half-open within Code.
	aLead := clampTrim(first.AStart - 1)
	aTail := clampTrim(aLines - (last.AEnd - 1))
	bLead := clampTrim(first.BStart - 1)
	bTail := clampTrim(bLines - (last.BEnd - 1))
	if aLead == 0 && aTail == 0 && bLead == 0 && bTail == 0 {
		return a, b, al // nothing to trim
	}
	ta := SliceBlock(a, a.StartLine+aLead, a.EndLine-aTail, a.Name)
	tb := SliceBlock(b, b.StartLine+bLead, b.EndLine-bTail, b.Name)
	return ta, tb, Align(ta, tb)
}

func clampTrim(n int) int {
	if n < 0 {
		return 0
	}
	if n > maxBoundaryTrim {
		return maxBoundaryTrim
	}
	return n
}

// synthesizeGoBlock wraps side A's block span in a fresh Go helper.
// Rejection rules (a subset of synthesizeGo's — there is no header or
// receiver to check):
//   - Alignment must have at least one common span.
//   - Holes must agree on `return`/`break`/`continue` presence.
func synthesizeGoBlock(a, b scan.Snippet, blockID string, al Alignment) Suggestion {
	if len(al.Common) == 0 {
		return Suggestion{Note: "rejected: no common lines between blocks"}
	}
	if reason, ok := rejectControlFlowAsymmetry(al.Holes); !ok {
		return Suggestion{Note: reason}
	}

	helperName := "extractedBlock_" + blockID
	body := reindentBlock(a.Code, "\t")
	divergence := formatDivergenceComment(al.Holes, "//")

	src := strings.Builder{}
	src.WriteString("// codetwin: starter helper extracted from the shared block\n")
	src.WriteString("// " + nonEmpty(a.Name, "<block A>") +
		" + " + nonEmpty(b.Name, "<block B>") +
		" (block " + blockID + ").\n")
	src.WriteString("// This is a literal copy of the first side's block — a statement\n")
	src.WriteString("// run, not a whole function. Adapt control flow (returns, error\n")
	src.WriteString("// paths) and replace the call sites by hand.\n")
	src.WriteString(freeIdentComment(a.Code, a.Lang, "//"))
	if divergence != "" {
		src.WriteString(divergence)
	}
	src.WriteString("func " + helperName + "() {\n")
	src.WriteString(body)
	src.WriteString("}\n")

	return Suggestion{
		HelperName: helperName,
		HelperSrc:  src.String(),
		Confidence: blockConfidence(a, b, al),
	}
}

// synthesizePythonBlock is synthesizeGoBlock adapted for Python:
// snake_case helper name, `#` comments, 4-space body indent, and the
// Python control-flow keyword set.
func synthesizePythonBlock(a, b scan.Snippet, blockID string, al Alignment) Suggestion {
	if len(al.Common) == 0 {
		return Suggestion{Note: "rejected: no common lines between blocks"}
	}
	if reason, ok := rejectControlFlowAsymmetryWithKeywords(al.Holes,
		[]string{"return", "break", "continue", "raise", "yield"}); !ok {
		return Suggestion{Note: reason}
	}

	helperName := "extracted_block_" + blockID
	body := reindentBlock(a.Code, "    ")
	divergence := formatDivergenceComment(al.Holes, "#")

	src := strings.Builder{}
	src.WriteString("# codetwin: starter helper extracted from the shared block\n")
	src.WriteString("# " + nonEmpty(a.Name, "<block A>") +
		" + " + nonEmpty(b.Name, "<block B>") +
		" (block " + blockID + ").\n")
	src.WriteString("# This is a literal copy of the first side's block — a statement\n")
	src.WriteString("# run, not a whole function. Adapt control flow (returns, error\n")
	src.WriteString("# paths) and replace the call sites by hand.\n")
	src.WriteString(freeIdentComment(a.Code, a.Lang, "#"))
	if divergence != "" {
		src.WriteString(divergence)
	}
	src.WriteString("def " + helperName + "():\n")
	src.WriteString(body)

	return Suggestion{
		HelperName: helperName,
		HelperSrc:  src.String(),
		Confidence: blockConfidence(a, b, al),
	}
}

// blockConfidence mirrors the v1 emitters' confidence formula:
// CommonLines / max(linesA, linesB) over the two block slices.
func blockConfidence(a, b scan.Snippet, al Alignment) float64 {
	aLines := strings.Count(strings.TrimRight(a.Code, "\n"), "\n") + 1
	bLines := strings.Count(strings.TrimRight(b.Code, "\n"), "\n") + 1
	maxLines := aLines
	if bLines > maxLines {
		maxLines = bLines
	}
	if maxLines <= 0 {
		return 0
	}
	return float64(al.CommonLines()) / float64(maxLines)
}

// freeIdentComment renders the parameter TODO line: the free
// identifiers the block appears to use, or "none detected".
func freeIdentComment(code string, lang tokenizer.Language, commentPrefix string) string {
	free := freeIdentifiers(code, lang)
	list := "none detected"
	if len(free) > 0 {
		list = strings.Join(free, ", ")
	}
	return commentPrefix + " TODO(codetwin): parameters not inferred — free identifiers used\n" +
		commentPrefix + " in the block: " + list + "\n"
}

// reindentBlock dedents the block span (stripping the longest common
// leading-whitespace prefix of its non-blank lines) and re-indents
// every non-blank line with indent, yielding a helper body at one
// natural indent level. Blank lines pass through empty.
func reindentBlock(code, indent string) string {
	lines := strings.Split(strings.TrimRight(code, "\n"), "\n")
	prefix := commonIndentPrefix(lines)
	out := strings.Builder{}
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			out.WriteString("\n")
			continue
		}
		out.WriteString(indent)
		out.WriteString(strings.TrimPrefix(l, prefix))
		out.WriteString("\n")
	}
	return out.String()
}

// commonIndentPrefix returns the longest whitespace prefix shared by
// every non-blank line. Works byte-wise, so mixed tabs/spaces dedent
// only as far as the lines literally agree.
func commonIndentPrefix(lines []string) string {
	prefix := ""
	first := true
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		ind := leadingWhitespace(l)
		if first {
			prefix = ind
			first = false
			continue
		}
		max := len(prefix)
		if len(ind) < max {
			max = len(ind)
		}
		k := 0
		for k < max && prefix[k] == ind[k] {
			k++
		}
		prefix = prefix[:k]
	}
	return prefix
}

func leadingWhitespace(l string) string {
	i := 0
	for i < len(l) && (l[i] == ' ' || l[i] == '\t') {
		i++
	}
	return l[:i]
}

// ── Free-identifier heuristic ────────────────────────────────────────

// freeIdentifiers returns, in first-use order, the identifiers a block
// uses without (apparently) defining. This is a lexical heuristic —
// NOT scope analysis:
//
//   - strings and comments are stripped first;
//   - identifiers immediately after `.` are skipped (field/method
//     selectors are not free);
//   - language keywords and common builtins are skipped;
//   - identifiers bound anywhere in the block (`x := …` / `var x` in
//     Go; `x = …`, `for x in`, `with … as x` in Python) are skipped,
//     even when the use precedes the binding.
//
// Package names and imported symbols will appear (they're lexically
// indistinguishable from variables); that's acceptable for a TODO
// comment whose reader finishes the extraction anyway.
func freeIdentifiers(code string, lang tokenizer.Language) []string {
	clean := stripStringsAndComments(code, lang)
	skip := goIdentSkip
	if lang == tokenizer.Python {
		skip = pythonIdentSkip
	}
	defined := definedIdents(clean, lang)

	var free []string
	seen := map[string]bool{}
	for _, id := range scanIdents(clean) {
		if id.afterDot || skip[id.name] || defined[id.name] || seen[id.name] {
			continue
		}
		seen[id.name] = true
		free = append(free, id.name)
	}
	return free
}

type identRef struct {
	name     string
	afterDot bool
}

// scanIdents walks the cleaned source and yields every identifier in
// order, flagging those preceded by `.`.
func scanIdents(code string) []identRef {
	var out []identRef
	i := 0
	for i < len(code) {
		c := code[i]
		if isIdentStartByte(c) {
			j := i
			for j < len(code) && isIdentByte(code[j]) {
				j++
			}
			afterDot := i > 0 && code[i-1] == '.'
			out = append(out, identRef{name: code[i:j], afterDot: afterDot})
			i = j
			continue
		}
		i++
	}
	return out
}

func isIdentStartByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

// definedIdents scans the cleaned block line by line for binding forms
// and returns the set of identifiers the block itself binds.
func definedIdents(code string, lang tokenizer.Language) map[string]bool {
	defined := map[string]bool{}
	for _, raw := range strings.Split(code, "\n") {
		l := strings.TrimSpace(raw)
		if l == "" {
			continue
		}
		if lang == tokenizer.Python {
			definePythonLine(l, defined)
		} else {
			defineGoLine(l, defined)
		}
	}
	return defined
}

// defineGoLine records `x, y := …` short declarations (including the
// `if`/`for`/`switch` init-statement forms) and `var x` / `const x`
// declarations.
func defineGoLine(l string, defined map[string]bool) {
	if idx := strings.Index(l, ":="); idx >= 0 {
		for _, id := range scanIdents(l[:idx]) {
			if !id.afterDot && !goIdentSkip[id.name] {
				defined[id.name] = true
			}
		}
	}
	for _, kw := range []string{"var ", "const "} {
		if strings.HasPrefix(l, kw) {
			rest := l[len(kw):]
			ids := scanIdents(rest)
			if len(ids) > 0 && !ids[0].afterDot {
				defined[ids[0].name] = true
			}
		}
	}
}

// definePythonLine records simple/augmented assignments (`x = …`,
// `x += …` — skipped when the target contains `.` or `[`), loop
// targets (`for x, y in …`), and `with … as x` / `except … as x`.
func definePythonLine(l string, defined map[string]bool) {
	if strings.HasPrefix(l, "for ") {
		if inIdx := strings.Index(l, " in "); inIdx > 0 {
			for _, id := range scanIdents(l[len("for "):inIdx]) {
				if !id.afterDot && !pythonIdentSkip[id.name] {
					defined[id.name] = true
				}
			}
		}
		return
	}
	if asIdx := strings.Index(l, " as "); asIdx >= 0 &&
		(strings.HasPrefix(l, "with ") || strings.HasPrefix(l, "except ")) {
		ids := scanIdents(l[asIdx+len(" as "):])
		if len(ids) > 0 {
			defined[ids[0].name] = true
		}
		return
	}
	if eq := pythonAssignIndex(l); eq >= 0 {
		left := strings.TrimRight(l[:eq], " \t+-*/%&|^@")
		if strings.ContainsAny(left, ".[(") {
			return // attribute/subscript target — uses, not binds
		}
		for _, id := range scanIdents(left) {
			if !pythonIdentSkip[id.name] {
				defined[id.name] = true
			}
		}
	}
}

// pythonAssignIndex returns the index of the first `=` that is an
// assignment (not part of `==`, `!=`, `<=`, `>=`), or -1.
func pythonAssignIndex(l string) int {
	for i := 0; i < len(l); i++ {
		if l[i] != '=' {
			continue
		}
		if i+1 < len(l) && l[i+1] == '=' {
			i++ // skip ==
			continue
		}
		if i > 0 && (l[i-1] == '=' || l[i-1] == '!' || l[i-1] == '<' || l[i-1] == '>') {
			continue
		}
		return i
	}
	return -1
}

// stripStringsAndComments blanks out string literals and comments so
// their contents can't contribute identifiers. Stripped bytes become
// spaces, preserving line structure for the per-line binding scan.
// Go: " ' ` literals, // line comments, /* */ block comments.
// Python: " ' literals (including a naive """ / ''' handling), #
// line comments.
func stripStringsAndComments(code string, lang tokenizer.Language) string {
	b := []byte(code)
	n := len(b)
	blank := func(from, to int) {
		for k := from; k < to && k < n; k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
	}
	i := 0
	for i < n {
		c := b[i]
		switch {
		case lang != tokenizer.Python && c == '/' && i+1 < n && b[i+1] == '/':
			j := i
			for j < n && b[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
		case lang != tokenizer.Python && c == '/' && i+1 < n && b[i+1] == '*':
			j := i + 2
			for j+1 < n && !(b[j] == '*' && b[j+1] == '/') {
				j++
			}
			end := j + 2
			blank(i, end)
			i = end
		case lang == tokenizer.Python && c == '#':
			j := i
			for j < n && b[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
		case c == '"' || c == '\'' || (lang != tokenizer.Python && c == '`'):
			quote := c
			triple := lang == tokenizer.Python && i+2 < n && b[i+1] == quote && b[i+2] == quote
			j := i + 1
			if triple {
				j = i + 3
				for j+2 < n && !(b[j] == quote && b[j+1] == quote && b[j+2] == quote) {
					j++
				}
				j += 3
			} else {
				for j < n && b[j] != quote && b[j] != '\n' {
					if b[j] == '\\' && quote != '`' && j+1 < n {
						j++
					}
					j++
				}
				if j < n && b[j] == quote {
					j++
				}
			}
			blank(i, j)
			i = j
		default:
			i++
		}
	}
	return string(b)
}

// goIdentSkip is Go keywords + predeclared identifiers: never free.
var goIdentSkip = wordSet(
	"break default func interface select case defer go map struct chan else goto package "+
		"switch const fallthrough if range type continue for import return var",
	"append cap close complex copy delete imag len make new panic print println real recover "+
		"min max clear",
	"true false iota nil",
	"any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 "+
		"int64 rune string uint uint8 uint16 uint32 uint64 uintptr",
)

// pythonIdentSkip is Python keywords + ubiquitous builtins: never free.
var pythonIdentSkip = wordSet(
	"False None True and as assert async await break class continue def del elif else except "+
		"finally for from global if import in is lambda nonlocal not or pass raise return try "+
		"while with yield match case self cls",
	"abs all any bool bytes callable chr dict dir divmod enumerate filter float format frozenset "+
		"getattr hasattr hash hex id input int isinstance issubclass iter len list map max min "+
		"next object oct open ord pow print range repr reversed round set setattr slice sorted "+
		"staticmethod str sum super tuple type vars zip",
	"Exception ValueError TypeError KeyError IndexError AttributeError RuntimeError StopIteration "+
		"NotImplementedError OSError IOError ZeroDivisionError",
)

func wordSet(groups ...string) map[string]bool {
	out := map[string]bool{}
	for _, g := range groups {
		for _, w := range strings.Fields(g) {
			out[w] = true
		}
	}
	return out
}
