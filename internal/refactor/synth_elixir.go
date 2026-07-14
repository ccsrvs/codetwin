package refactor

import (
	"os"
	"regexp"
	"strings"

	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// This file implements the symbol-scoped half of the Elixir emitter:
// re-reading the snippet's source file at synthesis time to recover
// what the clause-level chunk deliberately leaves out — the @doc/@spec
// attribute block above the def and any adjacent sibling clauses of
// the same symbol. Detection chunks stay at clause granularity (see
// splitter.splitElixir); grouping applies only when synthesizing a
// helper for --suggest / --suggest-all.

// exSymbolGroup is the symbol-scoped view of an Elixir def chunk:
// every adjacent sibling clause of the chunk's symbol (same name and
// arity, contiguous in the file apart from blank lines, comments, and
// module attributes) plus the @doc/@spec attribute block immediately
// preceding the first clause. Elixir attaches @spec/@doc to the
// function name, not the individual clause, so both lookups anchor on
// the FIRST clause of the symbol even when the snippet is a later
// clause.
type exSymbolGroup struct {
	clauses []string // verbatim clause sources, in file order
	attrs   []exAttr // @doc/@spec entries above the first clause, dedented
}

// exAttr is one module-attribute entry (`@doc …` / `@spec …`),
// possibly spanning multiple lines (heredoc docs, wrapped specs).
// Lines are dedented by the entry's own leading indent but otherwise
// verbatim, so heredoc content keeps its relative indentation.
type exAttr struct {
	kind  string // attribute name without the `@` (e.g. "doc", "spec")
	lines []string
}

var exAttrNameRe = regexp.MustCompile(`^@(\w+)`)

// exGroupForSnippet resolves the symbol-scoped group for an Elixir
// snippet. Falls back to a single-clause group built from s.Code (no
// attributes) whenever the source file cannot be read or the chunk
// cannot be re-located byte-identically — in-memory snippets, stale
// paths, or files that changed since the scan. The fallback keeps the
// emitter's output byte-identical to the pre-grouping behaviour.
func exGroupForSnippet(s scan.Snippet) exSymbolGroup {
	fallback := exSymbolGroup{clauses: []string{s.Code}}
	sym := SymbolForSnippet(s)
	if sym == "" || s.Path == "" {
		return fallback
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return fallback
	}
	chunks := splitter.Split(s.Path, string(data), tokenizer.Elixir)
	idx := -1
	for i, c := range chunks {
		if c.StartLine == s.StartLine && c.Symbol == sym && c.Code == s.Code {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fallback
	}
	lines := strings.Split(string(data), "\n")
	arity := exClauseArity(chunks[idx].Code)

	lo, hi := idx, idx
	for lo > 0 && exAdjacentSibling(chunks[lo-1], chunks[lo], sym, arity, lines) {
		lo--
	}
	for hi+1 < len(chunks) && exAdjacentSibling(chunks[hi], chunks[hi+1], sym, arity, lines) {
		hi++
	}

	g := exSymbolGroup{}
	for _, c := range chunks[lo : hi+1] {
		g.clauses = append(g.clauses, c.Code)
	}
	g.attrs = exLeadingAttrs(lines, chunks[lo].StartLine-1)
	return g
}

// exAdjacentSibling reports whether prev and next are clauses of the
// same symbol and arity with nothing but trivia (blank lines, `#`
// comments, module attributes) between them in the source file.
func exAdjacentSibling(prev, next splitter.Chunk, sym string, arity int, lines []string) bool {
	if prev.Symbol != sym || next.Symbol != sym {
		return false
	}
	if exClauseArity(prev.Code) != arity || exClauseArity(next.Code) != arity {
		return false
	}
	// Lines strictly between prev's last line and next's first line
	// (both 1-based inclusive) as a 0-based slice.
	if prev.EndLine > next.StartLine-1 {
		return false
	}
	return exGapOnlyTrivia(lines[prev.EndLine : next.StartLine-1])
}

// exGapOnlyTrivia reports whether every line in gap is blank, a `#`
// comment, or part of a module-attribute entry.
func exGapOnlyTrivia(gap []string) bool {
	i := 0
	for i < len(gap) {
		t := strings.TrimSpace(gap[i])
		switch {
		case t == "" || strings.HasPrefix(t, "#"):
			i++
		case exAttrNameRe.MatchString(t):
			i = exAttrEnd(gap, i) + 1
		default:
			return false
		}
	}
	return true
}

// exLeadingAttrs collects the @doc/@spec entries of the contiguous
// attribute block immediately preceding the def at firstClauseIdx
// (0-based). Blank lines and comments are allowed inside the block;
// any other substantive line breaks contiguity and clears what was
// collected so far (attributes attach to the very next def). Entries
// other than @doc/@spec (e.g. @impl) are tracked for contiguity but
// not carried.
func exLeadingAttrs(lines []string, firstClauseIdx int) []exAttr {
	if firstClauseIdx < 0 || firstClauseIdx > len(lines) {
		return nil
	}
	var pending []exAttr
	i := 0
	for i < firstClauseIdx {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			i++
			continue
		}
		if m := exAttrNameRe.FindStringSubmatch(t); m != nil {
			end := exAttrEnd(lines, i)
			if end >= firstClauseIdx {
				// Attribute runs into the def itself — malformed; give up
				// on propagation rather than emit a mangled block.
				return nil
			}
			indent := exLeadingWS(lines[i])
			var dl []string
			for _, l := range lines[i : end+1] {
				dl = append(dl, strings.TrimPrefix(l, indent))
			}
			pending = append(pending, exAttr{kind: m[1], lines: dl})
			i = end + 1
			continue
		}
		pending = nil
		i++
	}
	var out []exAttr
	for _, a := range pending {
		if a.kind == "doc" || a.kind == "spec" {
			out = append(out, a)
		}
	}
	return out
}

// exAttrEnd returns the 0-based index of the last line of the module
// attribute starting at lines[start]. Handles `"""` heredocs (the
// whole heredoc belongs to the entry), bracket-balanced wrapping
// (multi-line @spec parameter lists), and trailing-operator
// continuations (`::`, `|`, `,` at end of line).
func exAttrEnd(lines []string, start int) int {
	heredoc := false
	depth := 0
	for j := start; j < len(lines); j++ {
		line := lines[j]
		k := 0
		for k < len(line) {
			if heredoc {
				if strings.HasPrefix(line[k:], `"""`) {
					heredoc = false
					k += 3
					continue
				}
				k++
				continue
			}
			c := line[k]
			switch c {
			case '#':
				k = len(line) // comment: rest of line is not code
				continue
			case '"':
				if strings.HasPrefix(line[k:], `"""`) {
					heredoc = true
					k += 3
					continue
				}
				k++ // skip to the closing quote on this line
				for k < len(line) && line[k] != '"' {
					if line[k] == '\\' {
						k++
					}
					k++
				}
				if k < len(line) {
					k++
				}
				continue
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				depth--
			}
			k++
		}
		if heredoc || depth > 0 {
			continue
		}
		t := strings.TrimSpace(line)
		if strings.HasSuffix(t, "::") || strings.HasSuffix(t, "|") || strings.HasSuffix(t, ",") {
			continue
		}
		return j
	}
	return len(lines) - 1
}

// exLeadingWS returns the leading space/tab prefix of a line.
func exLeadingWS(l string) string {
	k := 0
	for k < len(l) && (l[k] == ' ' || l[k] == '\t') {
		k++
	}
	return l[:k]
}

// exClauseArity computes the arity of an Elixir def clause from its
// header: the number of top-level parameters inside the parens,
// counting commas at bracket depth 1 (string contents skipped, so
// default args like `\\ "a,b"` don't inflate the count). Headers
// without parens (`def foo do`, `def foo, do: :ok`) are arity 0. The
// paren group may span multiple lines. Returns -1 when no header is
// found, so malformed chunks never group with real clauses.
func exClauseArity(code string) int {
	lines := strings.Split(code, "\n")
	hi := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "@") {
			continue
		}
		hi = i
		break
	}
	if hi < 0 {
		return -1
	}
	t := strings.TrimSpace(lines[hi])
	matched := false
	for _, kw := range []string{"defmacrop ", "defmacro ", "defp ", "def "} {
		if strings.HasPrefix(t, kw) {
			t = t[len(kw):]
			matched = true
			break
		}
	}
	if !matched {
		return -1
	}
	j := 0
	for j < len(t) && isIdentByte(t[j]) {
		j++
	}
	for j < len(t) && (t[j] == '?' || t[j] == '!') {
		j++ // predicate/bang suffix is part of the name
	}
	rest := strings.TrimLeft(t[j:], " \t")
	if !strings.HasPrefix(rest, "(") {
		return 0
	}
	text := rest
	for k := hi + 1; k < len(lines); k++ {
		text += "\n" + lines[k]
	}
	depth := 0
	args := 0
	sawContent := false
	var inStr byte
	for k := 0; k < len(text); k++ {
		c := text[k]
		if inStr != 0 {
			if c == '\\' {
				k++
			} else if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			inStr = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				if sawContent {
					args++
				}
				return args
			}
		case ',':
			if depth == 1 {
				args++
			}
		default:
			if depth >= 1 && c != ' ' && c != '\t' && c != '\n' {
				sawContent = true
			}
		}
	}
	return args
}

// exFirstAttr returns the first attribute of the given kind, or nil.
func exFirstAttr(attrs []exAttr, kind string) *exAttr {
	for i := range attrs {
		if attrs[i].kind == kind {
			return &attrs[i]
		}
	}
	return nil
}

// exRenameSpec rewrites the function name of a `@spec name(...)` (or
// `@spec name ::`) first line to helperName, leaving everything else
// — argument types, return type, `when` constraints — verbatim.
// Arity is untouched because only the name token is replaced.
func exRenameSpec(line, helperName string) string {
	idx := strings.Index(line, "@spec")
	if idx < 0 {
		return line
	}
	rest := line[idx+len("@spec"):]
	ws := 0
	for ws < len(rest) && (rest[ws] == ' ' || rest[ws] == '\t') {
		ws++
	}
	nameEnd := ws
	for nameEnd < len(rest) && isIdentByte(rest[nameEnd]) {
		nameEnd++
	}
	for nameEnd < len(rest) && (rest[nameEnd] == '?' || rest[nameEnd] == '!') {
		nameEnd++
	}
	if nameEnd == ws {
		return line
	}
	return line[:idx] + "@spec " + helperName + rest[nameEnd:]
}

// exSpecKey normalizes a spec entry for A-vs-B comparison: the
// function name is replaced with a fixed placeholder (A and B
// legitimately differ in name) and whitespace is collapsed, so only
// the contract itself is compared.
func exSpecKey(a *exAttr) string {
	if a == nil || len(a.lines) == 0 {
		return ""
	}
	lines := append([]string{exRenameSpec(a.lines[0], "_")}, a.lines[1:]...)
	return strings.Join(strings.Fields(strings.Join(lines, "\n")), " ")
}
