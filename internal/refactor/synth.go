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

// Synthesize dispatches by language. v1 ships Go, Python, and Java
// emitters; every other language returns a structured "unsupported"
// Note so the CLI can surface a clear message without crashing.
func Synthesize(a, b scan.Snippet, pairID string, al Alignment) Suggestion {
	if a.Lang != b.Lang {
		return Suggestion{Note: "rejected: cross-language extraction not supported in v1"}
	}
	switch a.Lang {
	case tokenizer.Go:
		return synthesizeGo(a, b, pairID, al)
	case tokenizer.Python:
		return synthesizePython(a, b, pairID, al)
	case tokenizer.Java:
		return synthesizeJava(a, b, pairID, al)
	case tokenizer.JavaScript:
		return synthesizeJavaScript(a, b, pairID, al)
	case tokenizer.Rust:
		return synthesizeRust(a, b, pairID, al)
	case tokenizer.Elixir:
		return synthesizeElixir(a, b, pairID, al)
	default:
		return Suggestion{Note: fmt.Sprintf(
			"unsupported language: %s", a.Lang)}
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

// synthesizeJava produces a starter helper for two Java method-level
// snippets. Java has no top-level functions: the helper is appended at
// the end of A's file (matching Go/Python convention) but lands after
// the wrapping class's closing `}`, so the file won't compile until a
// human moves the helper inside the appropriate class. The helper
// header carries a `// NOTE:` comment flagging this. This is the v1
// "starter, human finishes" contract.
//
// Rejection rules (Java splitter only emits methods/constructors, so no
// anonymous-chunk handling is needed):
//   - Alignment must have at least one common span.
//   - Holes must agree on `return`/`break`/`continue`/`throw`/`yield`
//     presence — control-flow asymmetry signals semantically different
//     snippets.
//
// Modifiers (`public`, `static`, `final`, `synchronized`, generics,
// return type, `throws` clauses) are copied verbatim from A. Different
// wrapping classes are NOT rejected (the advanced fixture has
// UserStore.fetchA + OrderStore.fetchB and is meant to accept).
func synthesizeJava(a, b scan.Snippet, pairID string, al Alignment) Suggestion {
	if len(al.Common) == 0 {
		return Suggestion{Note: "rejected: no common lines between snippets"}
	}
	if reason, ok := rejectControlFlowAsymmetryWithKeywords(al.Holes,
		[]string{"return", "break", "continue", "throw", "yield"}); !ok {
		return Suggestion{Note: reason}
	}

	helperName := sanitizeHelperName(SymbolForSnippet(a), pairID)
	header, ok := javaHelperHeader(a.Code, helperName)
	if !ok {
		return Suggestion{Note: "rejected: snippet has no recognisable Java method header"}
	}
	body := javaRebodyAsHelper(a.Code)
	divergence := formatDivergenceComment(al.Holes, "//")

	src := strings.Builder{}
	src.WriteString("// codetwin: starter helper extracted from " +
		nonEmpty(SymbolForSnippet(a), "<anon>") +
		" + " + nonEmpty(SymbolForSnippet(b), "<anon>") +
		" (pair " + pairID + ").\n")
	src.WriteString("// This is a literal copy of the first snippet's body. Review the\n")
	src.WriteString("// divergences below and parameterize as needed before relying on it.\n")
	src.WriteString("// NOTE: appended at file scope; move it into the appropriate Java\n")
	src.WriteString("// class (or extract to a utility class) before compiling.\n")
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

// synthesizeRust produces a starter helper for two Rust snippets. The
// emitter targets the same v1 contract as Go/Python/Java/JS: literal
// copy of A's body, divergence comments, no parameterization, human
// finishes the refactor.
//
// Rejection rules (mirroring synthesizeJavaScript):
//   - Alignment must have at least one common span.
//   - Holes must agree on `return`/`break`/`continue`/`panic` presence
//     — the `panic!(...)` macro counts as control-flow asymmetry the
//     same way Java's `throw` does.
//   - The chunk must have a recognisable Rust definition header.
func synthesizeRust(a, b scan.Snippet, pairID string, al Alignment) Suggestion {
	if len(al.Common) == 0 {
		return Suggestion{Note: "rejected: no common lines between snippets"}
	}
	if reason, ok := rejectControlFlowAsymmetryWithKeywords(al.Holes,
		[]string{"return", "break", "continue", "panic"}); !ok {
		return Suggestion{Note: reason}
	}

	helperName := sanitizeHelperName(SymbolForSnippet(a), pairID)
	header, ok := rsHelperHeader(a.Code, helperName)
	if !ok {
		return Suggestion{Note: "rejected: snippet has no recognisable Rust fn header"}
	}
	body := rsRebodyAsHelper(a.Code)
	divergence := formatDivergenceComment(al.Holes, "//")

	src := strings.Builder{}
	src.WriteString("// codetwin: starter helper extracted from " +
		nonEmpty(SymbolForSnippet(a), "<anon>") +
		" + " + nonEmpty(SymbolForSnippet(b), "<anon>") +
		" (pair " + pairID + ").\n")
	src.WriteString("// This is a literal copy of the first snippet's body. Review the\n")
	src.WriteString("// divergences below and parameterize as needed before relying on it.\n")
	if rsBodyReferencesSelf(a.Code) {
		src.WriteString("// NOTE: extracted as a free function with &self carried as an\n")
		src.WriteString("// explicit parameter; bind a receiver at call sites (e.g.\n")
		src.WriteString("// extracted_helper(&store, key)) or move the fn into an impl\n")
		src.WriteString("// block to restore method-call syntax.\n")
	}
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

// rsBodyReferencesSelf reports whether snippet code contains a
// standalone `self` token. Used to decide whether to emit the
// self-binding NOTE after lifting a method body to a free function.
// `Self` (capital S — the implementing-type alias) is also a method
// idiom but `containsKeyword` is case-sensitive, so this only matches
// the receiver `self`.
func rsBodyReferencesSelf(code string) bool {
	return containsKeyword(code, "self")
}

// synthesizeElixir produces a starter helper for two Elixir snippets.
// The emitter targets the same v1 contract as Go/Python/Java/JS/Rust:
// literal copy of A's body, divergence comments, no parameterization,
// human finishes the refactor.
//
// Elixir defs must live inside a `defmodule`, so the helper is
// emitted as a free `def` block and ALWAYS carries a `# NOTE:` line
// flagging that the user must move it into an appropriate module
// before running. (Mirrors Java's "appended at file scope" contract,
// adapted for Elixir's module-context requirement.)
//
// Rejection rules:
//   - Alignment must have at least one common span.
//   - Holes must agree on `raise`/`throw`/`exit` presence — Elixir
//     has no `return`/`break`/`continue` (functions return their last
//     expression; iteration is recursive).
//   - The chunk must have a recognisable `def`/`defp` header.
func synthesizeElixir(a, b scan.Snippet, pairID string, al Alignment) Suggestion {
	if len(al.Common) == 0 {
		return Suggestion{Note: "rejected: no common lines between snippets"}
	}
	if reason, ok := rejectControlFlowAsymmetryWithKeywords(al.Holes,
		[]string{"raise", "throw", "exit"}); !ok {
		return Suggestion{Note: reason}
	}

	helperName := sanitizeHelperName(SymbolForSnippet(a), pairID)
	header, ok := exHelperHeader(a.Code, helperName)
	if !ok {
		return Suggestion{Note: "rejected: snippet has no recognisable Elixir def header"}
	}
	body := exRebodyAsHelper(a.Code)
	divergence := formatDivergenceComment(al.Holes, "#")

	src := strings.Builder{}
	src.WriteString("# codetwin: starter helper extracted from " +
		nonEmpty(SymbolForSnippet(a), "<anon>") +
		" + " + nonEmpty(SymbolForSnippet(b), "<anon>") +
		" (pair " + pairID + ").\n")
	src.WriteString("# This is a literal copy of the first snippet's body. Review the\n")
	src.WriteString("# divergences below and parameterize as needed before relying on it.\n")
	src.WriteString("# NOTE: appended at file scope; Elixir defs must live inside a\n")
	src.WriteString("# defmodule — move this def into the appropriate module (or\n")
	src.WriteString("# extract to a shared helper module) before compiling.\n")
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

// exRebodyAsHelper returns the body of an Elixir def chunk —
// everything between the header line's trailing `do` and the
// closing `end`. Each body line is dedented by the header line's
// leading whitespace so the helper renders at column 0 with body lines
// at one natural indent level. The closing `end` of the def passes
// through. Mirrors rsRebodyAsHelper / javaRebodyAsHelper in shape,
// adapted for `do`/`end` framing.
func exRebodyAsHelper(aCode string) string {
	lines := strings.Split(strings.TrimRight(aCode, "\n"), "\n")

	headerIdx := -1
	headerIndent := ""
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "@") {
			continue
		}
		headerIdx = i
		k := 0
		for k < len(l) && (l[k] == ' ' || l[k] == '\t') {
			k++
		}
		headerIndent = l[:k]
		break
	}
	if headerIdx < 0 {
		return strings.Join(lines, "\n") + "\n"
	}
	var body []string
	for _, l := range lines[headerIdx+1:] {
		body = append(body, strings.TrimPrefix(l, headerIndent))
	}
	return strings.Join(body, "\n") + "\n"
}

// exHelperHeader rewrites the first recognisable Elixir `def`/`defp`
// header of aCode to use helperName. Skips blank lines, line comments
// (`#`), and module attributes (`@spec`, `@doc`, etc.) before locating
// the def keyword. The `def` or `defp` keyword, parameter list, and
// trailing `do` keyword are preserved verbatim. Returns ok=false when
// no def header is found.
func exHelperHeader(aCode, helperName string) (string, bool) {
	for _, l := range strings.Split(aCode, "\n") {
		t := strings.TrimSpace(l)
		if t == "" ||
			strings.HasPrefix(t, "#") ||
			strings.HasPrefix(t, "@") {
			continue
		}
		var keyword string
		var rest string
		switch {
		case strings.HasPrefix(t, "def "):
			keyword = "def "
			rest = t[len("def "):]
		case strings.HasPrefix(t, "defp "):
			keyword = "defp "
			rest = t[len("defp "):]
		default:
			return "", false
		}
		nameEnd := 0
		for nameEnd < len(rest) && isIdentByte(rest[nameEnd]) {
			nameEnd++
		}
		if nameEnd == 0 {
			return "", false
		}
		return keyword + helperName + rest[nameEnd:], true
	}
	return "", false
}

// rsHelperHeader rewrites the first recognisable Rust definition
// header of aCode to use helperName. Skips blank lines, line comments
// (`//`), block comment fragments (`/*`, `*`), and attributes (`#[...]`)
// before locating the `fn` keyword. Modifiers (`pub`, `pub(crate)`,
// `async`, `unsafe`) before `fn` and everything after the function
// name (generics, parameter list, return type, `where` clause, opening
// brace) are preserved verbatim. Returns ok=false when no `fn` is
// found.
func rsHelperHeader(aCode, helperName string) (string, bool) {
	for _, l := range strings.Split(aCode, "\n") {
		t := strings.TrimSpace(l)
		if t == "" ||
			strings.HasPrefix(t, "//") ||
			strings.HasPrefix(t, "/*") ||
			strings.HasPrefix(t, "*") ||
			strings.HasPrefix(t, "#[") ||
			strings.HasPrefix(t, "#![") {
			continue
		}
		fnIdx := rsFindFnKeyword(t)
		if fnIdx < 0 {
			return "", false
		}
		afterFn := t[fnIdx+len("fn "):]
		nameEnd := 0
		for nameEnd < len(afterFn) && isIdentByte(afterFn[nameEnd]) {
			nameEnd++
		}
		if nameEnd == 0 {
			return "", false
		}
		return t[:fnIdx] + "fn " + helperName + afterFn[nameEnd:], true
	}
	return "", false
}

// rsFindFnKeyword locates the `fn ` token at the start of a Rust
// header line, after any leading modifiers (`pub`, `pub(crate)`,
// `async`, `unsafe`). Returns the byte offset of the `f` in `fn`, or
// -1 when not found. Modifiers may appear in any order — Rust
// canonicalises them but rustfmt accepts e.g. `pub async fn`.
func rsFindFnKeyword(line string) int {
	rest := line
	consumed := 0
	for {
		switch {
		case strings.HasPrefix(rest, "fn "):
			return consumed
		case strings.HasPrefix(rest, "pub "):
			rest = rest[len("pub "):]
			consumed += len("pub ")
		case strings.HasPrefix(rest, "pub("):
			closeParen := strings.IndexByte(rest, ')')
			if closeParen < 0 {
				return -1
			}
			skip := closeParen + 1
			for skip < len(rest) && (rest[skip] == ' ' || rest[skip] == '\t') {
				skip++
			}
			rest = rest[skip:]
			consumed += skip
		case strings.HasPrefix(rest, "async "):
			rest = rest[len("async "):]
			consumed += len("async ")
		case strings.HasPrefix(rest, "unsafe "):
			rest = rest[len("unsafe "):]
			consumed += len("unsafe ")
		case strings.HasPrefix(rest, "extern "):
			rest = rest[len("extern "):]
			consumed += len("extern ")
		case strings.HasPrefix(rest, "const "):
			rest = rest[len("const "):]
			consumed += len("const ")
		default:
			return -1
		}
	}
}

// rsRebodyAsHelper returns the body of snippet A — everything inside
// the outermost `{ ... }`. Each body line is dedented by the header
// line's leading whitespace so the helper renders at column 0 and the
// body sits at one natural indent level below it. Mirrors
// jsRebodyAsHelper / javaRebodyAsHelper; the closing `}` of the
// function passes through.
func rsRebodyAsHelper(aCode string) string {
	lines := strings.Split(strings.TrimRight(aCode, "\n"), "\n")

	headerIndent := ""
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" ||
			strings.HasPrefix(t, "//") ||
			strings.HasPrefix(t, "/*") ||
			strings.HasPrefix(t, "*") ||
			strings.HasPrefix(t, "#[") ||
			strings.HasPrefix(t, "#![") {
			continue
		}
		i := 0
		for i < len(l) && (l[i] == ' ' || l[i] == '\t') {
			i++
		}
		headerIndent = l[:i]
		break
	}

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
	for _, l := range lines[openIdx+1:] {
		body = append(body, strings.TrimPrefix(l, headerIndent))
	}
	return strings.Join(body, "\n") + "\n"
}

// synthesizeJavaScript produces a starter helper for two JavaScript or
// TypeScript snippets. The emitter targets the same v1 contract as
// Go/Python/Java: literal copy of A's body, divergence comments, no
// parameterization, human finishes the refactor.
//
// Rejection rules (mirroring synthesizeJava):
//   - Alignment must have at least one common span.
//   - Holes must agree on `return`/`break`/`continue`/`throw`/`yield`
//     presence — control-flow asymmetry signals semantically different
//     snippets.
//   - The chunk must have a recognisable JS/TS definition header
//     (free function, arrow assignment, or class method).
func synthesizeJavaScript(a, b scan.Snippet, pairID string, al Alignment) Suggestion {
	if len(al.Common) == 0 {
		return Suggestion{Note: "rejected: no common lines between snippets"}
	}
	if reason, ok := rejectControlFlowAsymmetryWithKeywords(al.Holes,
		[]string{"return", "break", "continue", "throw", "yield"}); !ok {
		return Suggestion{Note: reason}
	}

	helperName := sanitizeHelperName(SymbolForSnippet(a), pairID)
	header, ok := jsHelperHeader(a.Code, helperName)
	if !ok {
		return Suggestion{Note: "rejected: snippet has no recognisable JavaScript function header"}
	}
	body := jsRebodyAsHelper(a.Code)
	divergence := formatDivergenceComment(al.Holes, "//")

	src := strings.Builder{}
	src.WriteString("// codetwin: starter helper extracted from " +
		nonEmpty(SymbolForSnippet(a), "<anon>") +
		" + " + nonEmpty(SymbolForSnippet(b), "<anon>") +
		" (pair " + pairID + ").\n")
	src.WriteString("// This is a literal copy of the first snippet's body. Review the\n")
	src.WriteString("// divergences below and parameterize as needed before relying on it.\n")
	if jsBodyReferencesThis(a.Code) {
		src.WriteString("// NOTE: extracted as a free function from a class-method context;\n")
		src.WriteString("// `this` references must be wired at call sites (e.g. via\n")
		src.WriteString("// helper.call(this, …)) before relying on the helper.\n")
	}
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

// jsHelperHeader rewrites the first recognisable JS/TS definition
// header of aCode to use helperName. Supported forms:
//   - `function name(...)`, `async function name(...)`, optionally
//     prefixed with `export` / `export default`.
//
// Subsequent cycles add arrow-assignment and class-method forms.
// Returns ok=false when no recognisable form is found.
func jsHelperHeader(aCode, helperName string) (string, bool) {
	for _, l := range strings.Split(aCode, "\n") {
		t := strings.TrimSpace(l)
		if t == "" ||
			strings.HasPrefix(t, "//") ||
			strings.HasPrefix(t, "/*") ||
			strings.HasPrefix(t, "*") ||
			strings.HasPrefix(t, "@") {
			continue
		}
		if h, ok := jsRewriteFunctionHeader(t, helperName); ok {
			return h, true
		}
		if h, ok := jsRewriteArrowOrFuncExpr(t, helperName); ok {
			return h, true
		}
		if h, ok := jsRewriteClassMethod(t, helperName); ok {
			return h, true
		}
		return "", false
	}
	return "", false
}

// jsRewriteClassMethod handles ES6+ class-method headers — a bare
// `name(params) {` line, optionally prefixed with `async`, `static`, or
// both. The method is normalised into a free-function declaration so
// the helper can drop into module scope. `static` is preserved because
// it reads naturally on the helper if a human chooses to lift it back
// inside a class.
//
// Control-flow keywords like `if`, `while`, `for`, `switch`, `catch`,
// `return` happen to share the `keyword(...)` shape with method
// headers; jsClassMethodReservedNames rejects them.
func jsRewriteClassMethod(line, helperName string) (string, bool) {
	rest := line
	prefix := ""
	for {
		switch {
		case strings.HasPrefix(rest, "async "):
			prefix += "async "
			rest = rest[len("async "):]
		case strings.HasPrefix(rest, "static "):
			prefix += "static "
			rest = rest[len("static "):]
		default:
			goto done
		}
	}
done:
	nameEnd := 0
	for nameEnd < len(rest) && isIdentByte(rest[nameEnd]) {
		nameEnd++
	}
	if nameEnd == 0 {
		return "", false
	}
	name := rest[:nameEnd]
	if jsClassMethodReservedNames[name] {
		return "", false
	}
	afterName := rest[nameEnd:]
	if !strings.HasPrefix(afterName, "(") {
		return "", false
	}
	return prefix + "function " + helperName + afterName, true
}

// jsBodyReferencesThis reports whether snippet code contains a
// standalone `this` token (not as part of an identifier like
// `thisYear`). Used to decide whether to emit the this-binding NOTE
// after lifting a class-method body to a free-function helper.
func jsBodyReferencesThis(code string) bool {
	return containsKeyword(code, "this")
}

// jsRebodyAsHelper returns the body of snippet A — everything inside
// the outermost `{ ... }`. Each body line is dedented by the header
// line's leading whitespace so the helper renders at column 0 and the
// body sits at one natural indent level below it. Mirrors
// javaRebodyAsHelper; the closing `}` of the function passes through.
func jsRebodyAsHelper(aCode string) string {
	lines := strings.Split(strings.TrimRight(aCode, "\n"), "\n")

	headerIndent := ""
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		i := 0
		for i < len(l) && (l[i] == ' ' || l[i] == '\t') {
			i++
		}
		headerIndent = l[:i]
		break
	}

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
	for _, l := range lines[openIdx+1:] {
		body = append(body, strings.TrimPrefix(l, headerIndent))
	}
	return strings.Join(body, "\n") + "\n"
}

var jsClassMethodReservedNames = map[string]bool{
	"if": true, "while": true, "for": true, "switch": true, "catch": true,
	"return": true, "do": true, "function": true, "class": true,
	"const": true, "let": true, "var": true, "new": true, "typeof": true,
	"yield": true, "await": true, "throw": true, "try": true, "else": true,
}

// jsRewriteArrowOrFuncExpr handles assignment-style definitions:
//   - `const|let|var name = (params) => {…}`
//   - `const|let|var name = async (params) => {…}`
//   - `const|let|var name = function(params) {…}`
//   - `const|let|var name = async function(params) {…}`
//
// Each is normalised into a free-function header: `[export ]function
// extracted_h(params) {`. Arrow shorthands without parens around a
// single parameter and without a `{}` body are deliberately not
// matched — they require body lifting that v1 doesn't tackle.
func jsRewriteArrowOrFuncExpr(line, helperName string) (string, bool) {
	rest := line
	exportPrefix := ""
	if strings.HasPrefix(rest, "export default ") {
		exportPrefix = "export default "
		rest = rest[len("export default "):]
	} else if strings.HasPrefix(rest, "export ") {
		exportPrefix = "export "
		rest = rest[len("export "):]
	}
	switch {
	case strings.HasPrefix(rest, "const "):
		rest = rest[len("const "):]
	case strings.HasPrefix(rest, "let "):
		rest = rest[len("let "):]
	case strings.HasPrefix(rest, "var "):
		rest = rest[len("var "):]
	default:
		return "", false
	}
	nameEnd := 0
	for nameEnd < len(rest) && isIdentByte(rest[nameEnd]) {
		nameEnd++
	}
	if nameEnd == 0 {
		return "", false
	}
	rest = strings.TrimLeft(rest[nameEnd:], " \t")
	if !strings.HasPrefix(rest, "=") {
		return "", false
	}
	rest = strings.TrimLeft(rest[1:], " \t")
	asyncPrefix := ""
	if strings.HasPrefix(rest, "async ") {
		asyncPrefix = "async "
		rest = strings.TrimLeft(rest[len("async "):], " \t")
	}
	if strings.HasPrefix(rest, "function") {
		afterFunc := rest[len("function"):]
		if afterFunc == "" || afterFunc[0] == '(' || afterFunc[0] == ' ' || afterFunc[0] == '\t' {
			afterFunc = strings.TrimLeft(afterFunc, " \t")
			afterName := afterFunc
			if len(afterName) > 0 && isIdentByte(afterName[0]) {
				k := 0
				for k < len(afterName) && isIdentByte(afterName[k]) {
					k++
				}
				afterName = afterName[k:]
			}
			if !strings.HasPrefix(afterName, "(") {
				return "", false
			}
			return exportPrefix + asyncPrefix + "function " + helperName + afterName, true
		}
	}
	if !strings.HasPrefix(rest, "(") {
		return "", false
	}
	closeParen := strings.IndexByte(rest, ')')
	if closeParen < 0 {
		return "", false
	}
	params := rest[:closeParen+1]
	tail := strings.TrimLeft(rest[closeParen+1:], " \t")
	if !strings.HasPrefix(tail, "=>") {
		return "", false
	}
	afterArrow := strings.TrimLeft(tail[len("=>"):], " \t")
	headerSuffix := ""
	if strings.HasPrefix(afterArrow, "{") {
		headerSuffix = " {"
	}
	return exportPrefix + asyncPrefix + "function " + helperName + params + headerSuffix, true
}

// jsRewriteFunctionHeader handles `function name(...)` / `async
// function name(...)` (with optional `export` / `export default`
// prefix). Returns ok=false when the line isn't of that shape.
func jsRewriteFunctionHeader(line, helperName string) (string, bool) {
	rest := line
	prefix := ""
	if strings.HasPrefix(rest, "export default ") {
		prefix += "export default "
		rest = rest[len("export default "):]
	} else if strings.HasPrefix(rest, "export ") {
		prefix += "export "
		rest = rest[len("export "):]
	}
	if strings.HasPrefix(rest, "async ") {
		prefix += "async "
		rest = rest[len("async "):]
	}
	if !strings.HasPrefix(rest, "function ") {
		return "", false
	}
	afterKeyword := rest[len("function "):]
	nameEnd := 0
	for nameEnd < len(afterKeyword) && isIdentByte(afterKeyword[nameEnd]) {
		nameEnd++
	}
	if nameEnd == 0 {
		return "", false
	}
	return prefix + "function " + helperName + afterKeyword[nameEnd:], true
}

// javaHelperHeader builds the helper's method-header line by finding
// the first non-blank/non-comment/non-annotation line of snippet A,
// stripping its leading whitespace, and replacing the method-name token
// (the identifier immediately preceding the `(` of the parameter list)
// with helperName. Modifiers, generic type parameters, return type,
// parameter list, optional `throws` clause, and an optional trailing
// `{` are preserved verbatim. Returns ok=false when no `(` is found
// (e.g. a malformed chunk) or when no identifier precedes it.
//
// Multi-line method headers (where the parameter list wraps) aren't
// exercised by v1 fixtures and aren't supported here; the splitter also
// requires the header to fit on one line for `javaMethodRe` to match.
func javaHelperHeader(aCode, helperName string) (string, bool) {
	for _, l := range strings.Split(aCode, "\n") {
		t := strings.TrimSpace(l)
		if t == "" ||
			strings.HasPrefix(t, "//") ||
			strings.HasPrefix(t, "/*") ||
			strings.HasPrefix(t, "*") ||
			strings.HasPrefix(t, "@") {
			continue
		}
		parenIdx := strings.IndexByte(t, '(')
		if parenIdx <= 0 {
			return "", false
		}
		nameEnd := parenIdx
		for nameEnd > 0 && (t[nameEnd-1] == ' ' || t[nameEnd-1] == '\t') {
			nameEnd--
		}
		nameStart := nameEnd
		for nameStart > 0 && isIdentByte(t[nameStart-1]) {
			nameStart--
		}
		if nameStart == nameEnd {
			return "", false
		}
		return t[:nameStart] + helperName + t[nameEnd:], true
	}
	return "", false
}

// javaRebodyAsHelper returns the body of snippet A — everything inside
// the method's outermost `{ ... }`. Each body line is dedented by the
// header line's leading whitespace (typically 4 spaces for class
// methods) so the helper renders at column 0 with body lines at one
// natural indent level, ready to drop into a class. The closing `}` of
// the method passes through.
func javaRebodyAsHelper(aCode string) string {
	lines := strings.Split(strings.TrimRight(aCode, "\n"), "\n")

	headerIndent := ""
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		i := 0
		for i < len(l) && (l[i] == ' ' || l[i] == '\t') {
			i++
		}
		headerIndent = l[:i]
		break
	}

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
	for _, l := range lines[openIdx+1:] {
		body = append(body, strings.TrimPrefix(l, headerIndent))
	}
	return strings.Join(body, "\n") + "\n"
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
