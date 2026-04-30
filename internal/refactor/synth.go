package refactor

import (
	"fmt"
	"strings"

	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// Suggestion is the output of a per-language emitter: a starter helper
// extracted from snippet A, a comment block summarizing how B diverges,
// and a confidence number. v1 emitters do *not* parameterize the
// helper — they emit the literal copy of A's body and surface the
// divergences as comments. The human (or the Claude skill) finishes
// the refactor. This is a deliberate scope choice: full
// parameterization without a language AST is unsafe.
type Suggestion struct {
	HelperName string
	HelperSrc  string  // full source of the proposed helper, ready to drop into A's file
	Confidence float64 // CommonLines / max(linesA, linesB)
	Note       string  // populated when synthesis cannot proceed; HelperSrc is "" in that case
}

// Synthesize dispatches by language. v1 ships a Go emitter; every
// other language returns a structured "unsupported" Note so the CLI
// can surface a clear message without crashing.
func Synthesize(a, b scan.Snippet, pairID string, al Alignment) Suggestion {
	if a.Lang != b.Lang {
		return Suggestion{Note: "rejected: cross-language extraction not supported in v1"}
	}
	switch a.Lang {
	case tokenizer.Go:
		return synthesizeGo(a, b, pairID, al)
	case tokenizer.Python:
		return synthesizePython(a, b, pairID, al)
	default:
		return Suggestion{Note: fmt.Sprintf(
			"unsupported language: %s (v1 supports Go and Python)", a.Lang)}
	}
}

// SymbolForSnippet recovers the symbol portion of a snippet's name —
// the trailing word after `path:start-end `. Returns "" for whole-file
// chunks that don't have one. (scan.Snippet doesn't expose Symbol
// directly, so we recover it from Name's `path:start-end Symbol`
// format produced by splitter.Chunk.Name.)
func SymbolForSnippet(s scan.Snippet) string {
	parts := strings.SplitN(s.Name, " ", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// synthesizeGo produces a starter helper for two Go function-level
// snippets. Rejection rules (see plan):
//   - Symbol must be a top-level named function (no
//     anonymous/goroutine/defer chunks).
//   - Methods on different receiver types are rejected (the helper has
//     no obvious place to live).
//   - Alignment must have at least one common span (no overlap → no
//     extraction is meaningful).
//   - Holes whose two sides differ in `return`/`break`/`continue`
//     presence are rejected — that asymmetry signals the snippets do
//     meaningfully different things and a starter helper would
//     mislead.
func synthesizeGo(a, b scan.Snippet, pairID string, al Alignment) Suggestion {
	if reason, ok := rejectAnonymousChunk(a, b); !ok {
		return Suggestion{Note: reason}
	}
	if reason, ok := rejectMethodOnDifferentReceivers(a.Code, b.Code); !ok {
		return Suggestion{Note: reason}
	}
	if len(al.Common) == 0 {
		return Suggestion{Note: "rejected: no common lines between snippets"}
	}
	if reason, ok := rejectControlFlowAsymmetry(al.Holes); !ok {
		return Suggestion{Note: reason}
	}

	helperName := goHelperName(a, pairID)
	header := goHelperHeader(a.Code, helperName)
	body := goRebodyAsHelper(a.Code)
	divergence := formatDivergenceComment(al.Holes, "//")

	src := strings.Builder{}
	src.WriteString("// codetwin: starter helper extracted from " +
		nonEmpty(SymbolForSnippet(a), "<anon>") +
		" + " + nonEmpty(SymbolForSnippet(b), "<anon>") +
		" (pair " + pairID + ").\n")
	src.WriteString("// This is a literal copy of the first snippet's body. Review the\n")
	src.WriteString("// divergences below and parameterize as needed before relying on it.\n")
	if divergence != "" {
		src.WriteString(divergence)
	}
	src.WriteString(header)
	src.WriteString("\n")
	src.WriteString(body)

	confidence := 0.0
	aLines := strings.Count(strings.TrimRight(a.Code, "\n"), "\n") + 1
	bLines := strings.Count(strings.TrimRight(b.Code, "\n"), "\n") + 1
	maxLines := aLines
	if bLines > maxLines {
		maxLines = bLines
	}
	if maxLines > 0 {
		confidence = float64(al.CommonLines()) / float64(maxLines)
	}

	return Suggestion{
		HelperName: helperName,
		HelperSrc:  src.String(),
		Confidence: confidence,
	}
}

// synthesizePython produces a starter helper for two Python
// function-level snippets. Modelled on synthesizeGo, adapted for
// Python's indentation-based structure.
//
// Rejection rules:
//   - Alignment must have at least one common span.
//   - Holes must agree on `return`/`break`/`continue`/`raise`/`yield`
//     presence — control-flow asymmetry signals the snippets do
//     meaningfully different things and a starter helper would mislead.
//
// The Python splitter only emits named `def`s (no analogue of Go's
// goroutine/defer chunks), so anonymous-chunk rejection is unnecessary.
// Class methods and free functions are treated uniformly: the helper
// is always emitted as a top-level function, with `self`/`cls` carried
// through as ordinary parameters when the source was a method. The
// human (or Claude skill) decides whether to lift the helper to a
// shared module.
func synthesizePython(a, b scan.Snippet, pairID string, al Alignment) Suggestion {
	if len(al.Common) == 0 {
		return Suggestion{Note: "rejected: no common lines between snippets"}
	}
	if reason, ok := rejectControlFlowAsymmetryWithKeywords(al.Holes,
		[]string{"return", "break", "continue", "raise", "yield"}); !ok {
		return Suggestion{Note: reason}
	}

	helperName := sanitizeHelperName(SymbolForSnippet(a), pairID)
	header, ok := pythonHelperHeader(a.Code, helperName)
	if !ok {
		return Suggestion{Note: "rejected: snippet has no recognisable `def` line"}
	}
	body := pythonRebodyAsHelper(a.Code)
	divergence := formatDivergenceComment(al.Holes, "#")

	src := strings.Builder{}
	src.WriteString("# codetwin: starter helper extracted from " +
		nonEmpty(SymbolForSnippet(a), "<anon>") +
		" + " + nonEmpty(SymbolForSnippet(b), "<anon>") +
		" (pair " + pairID + ").\n")
	src.WriteString("# This is a literal copy of the first snippet's body. Review the\n")
	src.WriteString("# divergences below and parameterize as needed before relying on it.\n")
	if divergence != "" {
		src.WriteString(divergence)
	}
	src.WriteString(header)
	src.WriteString("\n")
	src.WriteString(body)

	confidence := 0.0
	aLines := strings.Count(strings.TrimRight(a.Code, "\n"), "\n") + 1
	bLines := strings.Count(strings.TrimRight(b.Code, "\n"), "\n") + 1
	maxLines := aLines
	if bLines > maxLines {
		maxLines = bLines
	}
	if maxLines > 0 {
		confidence = float64(al.CommonLines()) / float64(maxLines)
	}

	return Suggestion{
		HelperName: helperName,
		HelperSrc:  src.String(),
		Confidence: confidence,
	}
}

// pythonHelperHeader builds the helper's `def` line by finding the
// first non-blank/non-comment/non-decorator line of snippet A,
// stripping its leading whitespace, and replacing the function name
// with helperName. Preserves `async def`. Returns ok=false when no
// recognisable def line is found.
//
// TODO: multi-line `def name(\n    a, b,\n):` signatures aren't
// exercised by v1 fixtures; revisit when one shows up. The single-line
// case is enough for the simple/medium/advanced tiers.
func pythonHelperHeader(aCode, helperName string) (string, bool) {
	for _, l := range strings.Split(aCode, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "@") {
			continue
		}
		rest := t
		prefix := ""
		if strings.HasPrefix(rest, "async ") {
			prefix = "async "
			rest = strings.TrimSpace(rest[len("async "):])
		}
		if !strings.HasPrefix(rest, "def ") {
			return "", false
		}
		afterDef := rest[len("def "):]
		nameEnd := 0
		for nameEnd < len(afterDef) && isIdentByte(afterDef[nameEnd]) {
			nameEnd++
		}
		return prefix + "def " + helperName + afterDef[nameEnd:], true
	}
	return "", false
}

// pythonRebodyAsHelper returns snippet A's body re-indented for a
// top-level helper: the body's minimum non-blank indent is stripped
// and each non-blank line is re-indented at 4 spaces. Decorators on
// the original chunk are dropped — the helper does not carry
// user-defined decorators forward (they may be undefined at the
// helper's scope).
func pythonRebodyAsHelper(aCode string) string {
	lines := strings.Split(strings.TrimRight(aCode, "\n"), "\n")

	defIdx := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "@") {
			continue
		}
		if strings.HasPrefix(t, "def ") || strings.HasPrefix(t, "async def ") {
			defIdx = i
			break
		}
		break
	}
	if defIdx < 0 || defIdx == len(lines)-1 {
		return ""
	}
	body := lines[defIdx+1:]

	minIndent := -1
	for _, l := range body {
		if strings.TrimSpace(l) == "" {
			continue
		}
		ind := 0
		for ind < len(l) && (l[ind] == ' ' || l[ind] == '\t') {
			ind++
		}
		if minIndent == -1 || ind < minIndent {
			minIndent = ind
		}
	}
	if minIndent < 0 {
		minIndent = 0
	}

	out := strings.Builder{}
	for _, l := range body {
		if strings.TrimSpace(l) == "" {
			out.WriteString("\n")
			continue
		}
		stripped := l
		for i := 0; i < minIndent && len(stripped) > 0; i++ {
			if stripped[0] == ' ' || stripped[0] == '\t' {
				stripped = stripped[1:]
			} else {
				break
			}
		}
		out.WriteString("    ")
		out.WriteString(stripped)
		out.WriteString("\n")
	}
	return out.String()
}

// rejectAnonymousChunk fires when either snippet's chunk symbol is one
// of splitter's anonymous-form prefixes. We can't extract a named
// helper from a goroutine/defer/anonymous body without significantly
// more analysis.
func rejectAnonymousChunk(a, b scan.Snippet) (string, bool) {
	for _, s := range []scan.Snippet{a, b} {
		sym := SymbolForSnippet(s)
		if sym == "" {
			return "rejected: snippet is not a top-level named function", false
		}
		for _, prefix := range []string{"goroutine@", "defer@", "anonymous@"} {
			if strings.HasPrefix(sym, prefix) {
				return "rejected: snippet is an anonymous/goroutine/defer chunk", false
			}
		}
	}
	return "", true
}

// rejectMethodOnDifferentReceivers parses the leading `func (r T) ...`
// header of each Go snippet and rejects when the receiver types
// differ. Both `*UserRepo` and `UserRepo` are treated as the same
// underlying type.
func rejectMethodOnDifferentReceivers(aCode, bCode string) (string, bool) {
	aRecv := goReceiverType(aCode)
	bRecv := goReceiverType(bCode)
	if aRecv == "" && bRecv == "" {
		return "", true
	}
	if aRecv != bRecv {
		return fmt.Sprintf("rejected: methods on different receiver types (%q vs %q)", aRecv, bRecv), false
	}
	return "", true
}

// goReceiverType returns the receiver type name (sans pointer star)
// for a Go snippet starting with `func (r T) ...`. Returns "" for
// non-method functions.
func goReceiverType(code string) string {
	first := firstNonBlankLine(code)
	rest := strings.TrimPrefix(first, "func ")
	if !strings.HasPrefix(rest, "(") {
		return ""
	}
	close := strings.IndexByte(rest, ')')
	if close < 0 {
		return ""
	}
	inside := rest[1:close]
	parts := strings.Fields(inside)
	if len(parts) == 0 {
		return ""
	}
	typ := parts[len(parts)-1]
	return strings.TrimPrefix(typ, "*")
}

// rejectControlFlowAsymmetry fires when one side of a hole contains a
// `return`/`break`/`continue` keyword and the other side doesn't.
// Holes where both sides share the same control-flow keyword are
// allowed — the divergence is in the surrounding expression.
func rejectControlFlowAsymmetry(holes []Hole) (string, bool) {
	return rejectControlFlowAsymmetryWithKeywords(holes, []string{"return", "break", "continue"})
}

// rejectControlFlowAsymmetryWithKeywords is the language-parameterised
// form: callers pick the keyword set that's meaningful for their
// language (Python adds `raise`/`yield`, etc.).
func rejectControlFlowAsymmetryWithKeywords(holes []Hole, keywords []string) (string, bool) {
	for _, h := range holes {
		for _, kw := range keywords {
			aHas := containsKeyword(h.AText, kw)
			bHas := containsKeyword(h.BText, kw)
			if aHas != bHas {
				return fmt.Sprintf("rejected: control-flow asymmetry (%q in only one side of a hole)", kw), false
			}
		}
	}
	return "", true
}

// containsKeyword reports whether text contains the given keyword as a
// standalone word (not as part of an identifier like `returnValue`).
func containsKeyword(text, kw string) bool {
	idx := 0
	for {
		i := strings.Index(text[idx:], kw)
		if i < 0 {
			return false
		}
		start := idx + i
		end := start + len(kw)
		if start > 0 && isIdentByte(text[start-1]) {
			idx = end
			continue
		}
		if end < len(text) && isIdentByte(text[end]) {
			idx = end
			continue
		}
		return true
	}
}

func isIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// goHelperName composes a unique helper name. Format:
// extracted_<symbolA>_<pair-id>. Sanitised so the result is a valid Go
// identifier even if the splitter's symbol contained `@` (we already
// reject anonymous chunks, but the sanitisation keeps the name safe
// regardless).
func goHelperName(a scan.Snippet, pairID string) string {
	return sanitizeHelperName(SymbolForSnippet(a), pairID)
}

// sanitizeHelperName composes `extracted_<symbol>_<pairID>`, replacing
// any non-identifier byte with `_`. Used by every per-language emitter
// (Go, Python, …) since both languages share the same identifier
// alphabet for our purposes.
func sanitizeHelperName(symbol, pairID string) string {
	if symbol == "" {
		symbol = "fn"
	}
	out := strings.Builder{}
	for _, r := range symbol {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			out.WriteRune(r)
		default:
			out.WriteByte('_')
		}
	}
	return "extracted_" + out.String() + "_" + pairID
}

// goHelperHeader builds the header line of the helper from snippet A's
// own header, replacing the original function name with helperName.
// Receivers are dropped because the v1 helper is a free function — if
// rejection didn't fire, both snippets share a receiver underlying
// type, but the helper itself doesn't carry one (we can't know which
// receiver it should bind to without more analysis).
func goHelperHeader(aCode, helperName string) string {
	first := firstNonBlankLine(aCode)
	rest := strings.TrimPrefix(first, "func ")
	if strings.HasPrefix(rest, "(") {
		close := strings.IndexByte(rest, ')')
		if close >= 0 {
			rest = strings.TrimLeft(rest[close+1:], " \t")
		}
	}
	nameEnd := 0
	for nameEnd < len(rest) && isIdentByte(rest[nameEnd]) {
		nameEnd++
	}
	return "func " + helperName + rest[nameEnd:]
}

// goRebodyAsHelper returns the body of snippet A — everything from
// after the function header's opening `{` through the closing `}`.
// The header line and its `{` are dropped; goHelperHeader supplies a
// new header (with its own `{`).
func goRebodyAsHelper(aCode string) string {
	lines := strings.Split(strings.TrimRight(aCode, "\n"), "\n")
	openIdx := -1
	for i, l := range lines {
		if strings.Contains(l, "{") {
			openIdx = i
			break
		}
	}
	if openIdx < 0 {
		return strings.Join(lines, "\n") + "\n"
	}
	openLine := lines[openIdx]
	bracePos := strings.IndexByte(openLine, '{')
	afterBrace := strings.TrimSpace(openLine[bracePos+1:])
	var body []string
	if afterBrace != "" {
		body = append(body, afterBrace)
	}
	body = append(body, lines[openIdx+1:]...)
	return strings.Join(body, "\n") + "\n"
}

// formatDivergenceComment produces a comment block summarising the
// holes, using the caller's line-comment prefix (e.g. `//` for Go,
// `#` for Python). Each entry shows A's text and B's text on adjacent
// lines so the reviewer can see the divergence at a glance.
func formatDivergenceComment(holes []Hole, commentPrefix string) string {
	if len(holes) == 0 {
		return ""
	}
	out := strings.Builder{}
	out.WriteString(commentPrefix + " Divergences (B vs A):\n")
	for i, h := range holes {
		out.WriteString(fmt.Sprintf("%s   #%d  A[L%d-%d]: %s\n", commentPrefix, i+1, h.AStart, h.AEnd-1, oneLine(h.AText)))
		out.WriteString(fmt.Sprintf("%s        B[L%d-%d]: %s\n", commentPrefix, h.BStart, h.BEnd-1, oneLine(h.BText)))
	}
	return out.String()
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " | ")
	return s
}

func firstNonBlankLine(code string) string {
	for _, l := range strings.Split(code, "\n") {
		t := strings.TrimSpace(l)
		if t != "" {
			return t
		}
	}
	return ""
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
