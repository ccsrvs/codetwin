// Package splitter breaks a source file into function/class-level chunks so
// similarity comparisons happen at the granularity of individual definitions
// rather than entire files. A 500-line module with one duplicated 20-line
// helper now scores high on that helper instead of being washed out by 480
// lines of unrelated code.
//
// When a language splitter cannot identify any definitions in a file (or
// when the language is unsupported), the whole file is returned as a single
// chunk with Symbol == "".
package splitter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// Chunk is a contiguous span of source code, optionally named after the
// definition that opens it.
type Chunk struct {
	Path      string
	StartLine int    // 1-based, inclusive
	EndLine   int    // 1-based, inclusive
	Symbol    string // best-effort symbol name (function/class), may be empty
	Code      string
}

// Name produces a unique, human-readable identifier for a chunk. The format
// is "path:start-end Symbol" when the symbol is known, "path:start-end" when
// it isn't, and just "path" for whole-file fallback chunks (those have no
// symbol and start at line 1).
func (c Chunk) Name() string {
	if c.Symbol == "" && c.StartLine == 1 {
		return c.Path
	}
	if c.Symbol != "" {
		return fmt.Sprintf("%s:%d-%d %s", c.Path, c.StartLine, c.EndLine, c.Symbol)
	}
	return fmt.Sprintf("%s:%d-%d", c.Path, c.StartLine, c.EndLine)
}

// CountNonBlankLines reports how many newline-separated lines in code have
// non-whitespace content. Used to gate display of tiny matches.
func CountNonBlankLines(code string) int {
	n := 0
	for _, line := range strings.Split(code, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// Split breaks code into per-definition chunks. The returned slice always
// contains at least one chunk: when no definitions are found the whole file
// is returned as a single anonymous chunk.
func Split(path, code string, lang tokenizer.Language) []Chunk {
	var chunks []Chunk
	switch lang {
	case tokenizer.Python:
		chunks = splitPython(code)
	case tokenizer.Go:
		chunks = splitGo(code)
	case tokenizer.JavaScript:
		chunks = splitJavaScript(code)
	case tokenizer.Rust:
		chunks = splitBraceLang(code, rustFnRe)
	case tokenizer.Java:
		chunks = splitJava(code)
	case tokenizer.Elixir:
		chunks = splitElixir(code)
	}
	if len(chunks) == 0 {
		lines := strings.Split(code, "\n")
		chunks = []Chunk{{StartLine: 1, EndLine: len(lines), Code: code}}
	}
	for i := range chunks {
		chunks[i].Path = path
	}
	return chunks
}

// ── Python ────────────────────────────────────────────────────────────────────

var (
	pyDefRe  = regexp.MustCompile(`^(\s*)(?:async\s+)?def\s+(\w+)`)
	pyDecoRe = regexp.MustCompile(`^([ \t]*)@`)
)

func splitPython(code string) []Chunk {
	lines := strings.Split(code, "\n")

	type defInfo struct {
		defLine    int // 0-based index of the `def` line
		chunkStart int // 0-based index where the chunk starts (defLine, or earlier if decorated)
		indent     int
		name       string
	}
	type pendingDeco struct {
		startLine int // 0-based start of the decorator block
		indent    int
	}

	var defs []defInfo
	var pending []pendingDeco

	i := 0
	for i < len(lines) {
		line := lines[i]

		// Decorator? Capture its start line and skip past any multi-line
		// continuation so we don't misread `attempts=3,` etc. as their own
		// statements. A decorator block belongs to the very next def at the
		// same indent — pending is cleared the moment we see anything that
		// breaks that contiguity (non-comment, non-decorator, non-def code).
		if m := pyDecoRe.FindStringSubmatch(line); m != nil {
			decoIndent := indentLen(m[1])
			decoStart := i
			decoEnd := pythonDecoratorEndLine(lines, i)
			pending = append(pending, pendingDeco{startLine: decoStart, indent: decoIndent})
			i = decoEnd + 1
			continue
		}

		// Def? Attach the earliest pending decorator at the same indent.
		if m := pyDefRe.FindStringSubmatch(line); m != nil {
			defIndent := indentLen(m[1])
			chunkStart := i
			for _, p := range pending {
				if p.indent == defIndent {
					chunkStart = p.startLine
					break
				}
			}
			defs = append(defs, defInfo{
				defLine: i, chunkStart: chunkStart, indent: defIndent, name: m[2],
			})
			pending = nil
			i++
			continue
		}

		// Anything else: blank lines and comments preserve pending decorators
		// (PEP 8 tolerates them between decorator and def). Anything substantive
		// breaks the chain.
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			pending = nil
		}
		i++
	}

	if len(defs) == 0 {
		return nil
	}
	var chunks []Chunk
	for _, d := range defs {
		end := len(lines) - 1
		// Skip past a multi-line signature so the closing `):` (which sits
		// at the def line's indent and would otherwise look like the end of
		// the function) doesn't fool the indent-based termination below.
		sigEnd := pythonSignatureEndLine(lines, d.defLine)
		for j := sigEnd + 1; j < len(lines); j++ {
			line := lines[j]
			if strings.TrimSpace(line) == "" {
				continue
			}
			if indentLen(line) <= d.indent {
				end = j - 1
				break
			}
		}
		chunks = append(chunks, Chunk{
			StartLine: d.chunkStart + 1,
			EndLine:   end + 1,
			Symbol:    d.name,
			Code:      strings.Join(lines[d.chunkStart:end+1], "\n"),
		})
	}
	return chunks
}

// pythonStringState tracks whether the scanner is inside a string literal.
// Carried across line boundaries so triple-quoted strings work correctly;
// single-line strings reset at newline (Python doesn't allow them to span).
type pythonStringState int

const (
	pyStCode pythonStringState = iota
	pyStSingle
	pyStDouble
	pyStTripleSingle
	pyStTripleDouble
)

// pythonScanLine walks one line, updating *state and *depth for code-mode
// brackets/parens/braces, ignoring whitespace inside string literals and
// `#` comments. Returns true when a top-level `:` (depth == 0, in code
// state) was seen on this line — used by the signature scanner.
func pythonScanLine(line string, state *pythonStringState, depth *int) (sawColonAtZero bool) {
	commentHit := false
	for k := 0; k < len(line) && !commentHit; k++ {
		c := line[k]
		switch *state {
		case pyStCode:
			switch {
			case c == '#':
				commentHit = true
			case c == '"' && k+2 < len(line) && line[k+1] == '"' && line[k+2] == '"':
				*state = pyStTripleDouble
				k += 2
			case c == '\'' && k+2 < len(line) && line[k+1] == '\'' && line[k+2] == '\'':
				*state = pyStTripleSingle
				k += 2
			case c == '"':
				*state = pyStDouble
			case c == '\'':
				*state = pyStSingle
			case c == '(', c == '[', c == '{':
				*depth++
			case c == ')', c == ']', c == '}':
				*depth--
			case c == ':' && *depth == 0:
				sawColonAtZero = true
			}
		case pyStSingle:
			if c == '\\' {
				k++ // skip escaped next char
			} else if c == '\'' {
				*state = pyStCode
			}
		case pyStDouble:
			if c == '\\' {
				k++
			} else if c == '"' {
				*state = pyStCode
			}
		case pyStTripleSingle:
			if c == '\'' && k+2 < len(line) && line[k+1] == '\'' && line[k+2] == '\'' {
				*state = pyStCode
				k += 2
			}
		case pyStTripleDouble:
			if c == '"' && k+2 < len(line) && line[k+1] == '"' && line[k+2] == '"' {
				*state = pyStCode
				k += 2
			}
		}
	}
	// Single-line strings cannot span newlines in Python; reset their
	// state at line boundaries so an unterminated quote doesn't poison
	// subsequent lines.
	if *state == pyStSingle || *state == pyStDouble {
		*state = pyStCode
	}
	return sawColonAtZero
}

// pythonSignatureEndLine returns the 0-based index of the line containing
// the `:` that closes a Python def's signature, starting from defLine. A
// single-line signature like `def foo(x):` returns defLine itself.
//
// Without this, a Black-formatted signature like
//
//	async def f(
//	    x,
//	    y,
//	):
//	    body
//
// gets mis-chunked: the closing `):` sits at the def's indent, so the
// indent-based body-end heuristic would fire *before* the body is captured.
//
// On malformed input (no closing `:` ever found) returns the last line
// index, which causes the caller to emit an empty body — preferable to
// reading past EOF.
func pythonSignatureEndLine(lines []string, defLine int) int {
	state := pyStCode
	depth := 0
	for i := defLine; i < len(lines); i++ {
		if pythonScanLine(lines[i], &state, &depth) {
			return i
		}
	}
	return len(lines) - 1
}

// pythonDecoratorEndLine returns the 0-based index of the last line of a
// decorator block beginning at decoLine. A simple `@cached` returns
// decoLine itself; a multi-line `@retry(\n    attempts=3,\n)` returns the
// line containing the closing `)`.
//
// Used to advance past a decorator's continuation lines so the main loop
// doesn't misread `attempts=3,` as a free-standing statement that would
// clear pending decorators.
func pythonDecoratorEndLine(lines []string, decoLine int) int {
	state := pyStCode
	depth := 0
	for i := decoLine; i < len(lines); i++ {
		pythonScanLine(lines[i], &state, &depth)
		if depth == 0 {
			return i
		}
	}
	return len(lines) - 1
}

// indentLen returns the visual indent width of leading whitespace, treating
// each tab as 4 spaces. Stops at the first non-whitespace rune.
func indentLen(s string) int {
	n := 0
	for _, r := range s {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// ── Brace-counting languages (Go, Rust) ───────────────────────────────────────

var (
	goFuncRe = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s+)?(\w+)`)
	rustFnRe = regexp.MustCompile(`^[ \t]*(?:pub(?:\s*\([^)]*\))?\s+)?(?:async\s+)?(?:unsafe\s+)?fn\s+(\w+)`)

	// Anonymous-func forms in Go. Together with goFuncRe these let splitGo
	// emit closures, goroutines, and defers as their own chunks; the
	// downstream nested-pair filter (chunksNestedSameFile) suppresses
	// outer/inner overlap so emitting nested chunks is safe.
	goAssignFuncRe = regexp.MustCompile(`^[ \t]*(\w+)\s*(?::=|=)\s*func\s*\(`)
	goVarFuncRe    = regexp.MustCompile(`^[ \t]*var\s+(\w+)\b[^=]*=\s*func\s*\(`)
	goGoroutineRe  = regexp.MustCompile(`^[ \t]*go\s+func\s*\(`)
	goDeferFuncRe  = regexp.MustCompile(`^[ \t]*defer\s+func\s*\(`)
	goBareFuncRe   = regexp.MustCompile(`^[ \t]*func\s*\(`)
)

// splitGo extracts top-level named funcs/methods plus anonymous closures
// (assignment, var, goroutine, defer, bare/IIFE). The loop advances by one
// line per iteration — not past the matched body — so anonymous funcs
// nested inside an outer function body are also visited and emitted.
func splitGo(code string) []Chunk {
	lines := strings.Split(code, "\n")
	var chunks []Chunk
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		var symbol string
		switch {
		case goFuncRe.MatchString(line):
			symbol = goFuncRe.FindStringSubmatch(line)[1]
		case goGoroutineRe.MatchString(line):
			symbol = fmt.Sprintf("goroutine@L%d", i+1)
		case goDeferFuncRe.MatchString(line):
			symbol = fmt.Sprintf("defer@L%d", i+1)
		case goVarFuncRe.MatchString(line):
			symbol = goVarFuncRe.FindStringSubmatch(line)[1]
		case goAssignFuncRe.MatchString(line):
			symbol = goAssignFuncRe.FindStringSubmatch(line)[1]
		case goBareFuncRe.MatchString(line):
			symbol = fmt.Sprintf("anonymous@L%d", i+1)
		default:
			continue
		}
		end, ok := findBraceEnd(lines, i)
		if !ok {
			continue
		}
		chunks = append(chunks, Chunk{
			StartLine: i + 1,
			EndLine:   end + 1,
			Symbol:    symbol,
			Code:      strings.Join(lines[i:end+1], "\n"),
		})
	}
	return chunks
}

// braceMatcher inspects a single header line and reports whether it opens a
// chunkable definition. Returns the symbol name when ok=true; the symbol is
// ignored when ok=false. Per-language adapters (Java type-decl skipping,
// JS multi-regex try-each) are encapsulated inside the matcher.
type braceMatcher func(line string) (symbol string, ok bool)

// splitBraceBased drives the shared "match header, brace-balance to closer,
// emit chunk, jump past body" loop used by Go/Rust, Java, and JS/TS. The
// matcher decides what counts as a header; emitBodyless controls whether a
// matched header without a `{...}` block becomes a single-line chunk
// (true for JS arrow shorthands) or is skipped (false for everything else).
func splitBraceBased(code string, match braceMatcher, emitBodyless bool) []Chunk {
	lines := strings.Split(code, "\n")
	var chunks []Chunk
	i := 0
	for i < len(lines) {
		symbol, ok := match(lines[i])
		if !ok {
			i++
			continue
		}
		end, hasBody := findBraceEnd(lines, i)
		if !hasBody {
			if emitBodyless {
				chunks = append(chunks, Chunk{
					StartLine: i + 1,
					EndLine:   i + 1,
					Symbol:    symbol,
					Code:      lines[i],
				})
			}
			i++
			continue
		}
		chunks = append(chunks, Chunk{
			StartLine: i + 1,
			EndLine:   end + 1,
			Symbol:    symbol,
			Code:      strings.Join(lines[i:end+1], "\n"),
		})
		i = end + 1
	}
	return chunks
}

// splitBraceLang chunks code using a "find a definition header, then
// brace-balance to its closer" strategy. Works for Go and Rust.
func splitBraceLang(code string, headerRe *regexp.Regexp) []Chunk {
	return splitBraceBased(code, func(line string) (string, bool) {
		m := headerRe.FindStringSubmatch(line)
		if m == nil {
			return "", false
		}
		return m[1], true
	}, false)
}

// ── Java ──────────────────────────────────────────────────────────────────────

var (
	// javaTypeDeclRe matches lines that declare a type (class/interface/enum/
	// record). Methods inside these types are what we want to extract; the
	// type header itself would otherwise pull in the entire class body as a
	// single chunk and dominate the report.
	javaTypeDeclRe = regexp.MustCompile(`^[ \t]*(?:(?:public|private|protected|static|final|abstract|sealed|non-sealed)\s+)*(?:class|interface|enum|record|@interface)\s+`)

	// javaMethodRe matches plausible method or constructor headers. Allows
	// any combination of access/non-access modifiers, an optional generic
	// type parameter, an optional return type (constructors omit it), then
	// `name(...)`. The trailing `\{?` is loose so we still match headers
	// where `{` lives on the next line; findBraceEnd handles the body.
	javaMethodRe = regexp.MustCompile(`^[ \t]*(?:(?:public|private|protected|static|final|synchronized|abstract|native|default|strictfp)\s+)*(?:<[^>]+>\s+)?(?:[\w<>\[\],\s\?\.\$]+?\s+)?(\w+)\s*\([^)]*\)\s*(?:throws\s+[\w,\s\.]+)?\s*\{?\s*$`)
)

// splitJava chunks a Java compilation unit into method- and constructor-
// level chunks. Class/interface/enum/record headers are deliberately not
// matched — they'd swallow the whole body. Interface method stubs (no
// `{`) and field declarations both naturally fall out: the former because
// findBraceEnd reports no body, the latter because the regex requires
// `name(`.
func splitJava(code string) []Chunk {
	return splitBraceBased(code, javaHeaderMatch, false)
}

// javaHeaderMatch reports whether a line opens a method or constructor
// definition. Type declarations and Java keywords with method-like
// shape (`if (...) {`, `while (...) {`, etc.) are explicitly rejected.
func javaHeaderMatch(line string) (string, bool) {
	if javaTypeDeclRe.MatchString(line) {
		return "", false
	}
	m := javaMethodRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	switch m[1] {
	case "if", "while", "for", "switch", "synchronized", "catch", "try", "do", "return":
		return "", false
	}
	return m[1], true
}

// findBraceEnd scans from the start line until the first time the running
// brace depth opens and then closes back to 0, returning that line index.
// Naive byte-level counting — does not understand braces inside strings or
// comments. Acceptable for v1; the tokenizer normalizes anyway.
func findBraceEnd(lines []string, start int) (int, bool) {
	depth := 0
	opened := false
	for j := start; j < len(lines); j++ {
		for _, r := range lines[j] {
			switch r {
			case '{':
				depth++
				opened = true
			case '}':
				depth--
			}
		}
		if opened && depth <= 0 {
			return j, true
		}
	}
	return start, false
}

// ── JavaScript / TypeScript ───────────────────────────────────────────────────

var (
	jsFuncRe   = regexp.MustCompile(`^(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s+(\w+)`)
	jsArrowRe  = regexp.MustCompile(`^(?:export\s+(?:default\s+)?)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:function\b|\([^)]*\)\s*=>|\w+\s*=>)`)
	jsClassRe  = regexp.MustCompile(`^(?:export\s+(?:default\s+)?)?class\s+\w+`)
	jsMethodRe = regexp.MustCompile(`^[ \t]+(?:(?:async|static|get|set)\s+)*(\w+)\s*\([^)]*\)\s*\{`)
)

// jsMethodReservedNames are control-flow / language keywords whose
// `name(...)` shape would otherwise look like a method header. Mirrors
// the Java splitter's reserved-name rejection.
var jsMethodReservedNames = map[string]bool{
	"if": true, "while": true, "for": true, "switch": true, "catch": true,
	"return": true, "do": true, "else": true, "try": true, "finally": true,
	"function": true, "class": true, "throw": true,
}

func splitJavaScript(code string) []Chunk {
	return splitBraceBased(code, jsHeaderMatch, true)
}

// jsHeaderMatch tries each JavaScript/TypeScript header pattern in order
// (named function, arrow assigned to const/let/var, class method) and
// returns the first symbol found. Class declarations are deliberately
// rejected so the loop falls through into the class body, where each
// method is extracted as its own chunk — matching Python's and Java's
// method-level granularity. emitBodyless=true at the splitBraceBased
// call site captures single-expression arrow shorthands that have no
// `{...}` body.
func jsHeaderMatch(line string) (string, bool) {
	if jsClassRe.MatchString(line) {
		return "", false
	}
	for _, re := range []*regexp.Regexp{jsFuncRe, jsArrowRe} {
		if m := re.FindStringSubmatch(line); m != nil {
			return m[1], true
		}
	}
	if m := jsMethodRe.FindStringSubmatch(line); m != nil {
		if !jsMethodReservedNames[m[1]] {
			return m[1], true
		}
	}
	return "", false
}

// ── Elixir ────────────────────────────────────────────────────────────────────

var exDefRe = regexp.MustCompile(`^([ \t]*)(?:def|defp)\s+(\w+)`)

// splitElixir chunks an Elixir source file into per-`def`/`defp` blocks.
// Each chunk runs from the def's header line through the matching `end`
// keyword at the same indent level. Module wrappers (`defmodule`) are
// not chunked; their inner defs are. v1 only supports the
// `def name(args) do ... end` block form — the `do:` shorthand
// (`def add(a, b), do: a + b`) and multi-clause guarded forms aren't
// matched. Header lines must end with the bare `do` keyword.
func splitElixir(code string) []Chunk {
	lines := strings.Split(code, "\n")
	var chunks []Chunk
	i := 0
	for i < len(lines) {
		m := exDefRe.FindStringSubmatch(lines[i])
		if m == nil {
			i++
			continue
		}
		if !exHeaderEndsWithDo(lines[i]) {
			i++
			continue
		}
		defIndent := indentLen(m[1])
		end := exFindMatchingEnd(lines, i, defIndent)
		if end < 0 {
			i++
			continue
		}
		chunks = append(chunks, Chunk{
			StartLine: i + 1,
			EndLine:   end + 1,
			Symbol:    m[2],
			Code:      strings.Join(lines[i:end+1], "\n"),
		})
		i = end + 1
	}
	return chunks
}

// exHeaderEndsWithDo reports whether a `def` header line terminates
// with the bare `do` keyword (block form). Skips the `do:` shorthand
// and any continuation form that doesn't open a do/end block.
func exHeaderEndsWithDo(line string) bool {
	t := strings.TrimRight(line, " \t")
	if !strings.HasSuffix(t, " do") && t != "do" {
		return false
	}
	if strings.HasSuffix(t, ", do") {
		return false
	}
	return true
}

// exFindMatchingEnd walks forward from defLine looking for the first
// non-blank line at indent <= defIndent whose trimmed text is `end`.
// That line closes the def's block. Returns -1 if no closing `end` is
// found at the matching indent — the caller should skip this def.
func exFindMatchingEnd(lines []string, defLine, defIndent int) int {
	for j := defLine + 1; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			continue
		}
		ind := indentLen(lines[j])
		if ind <= defIndent {
			if t == "end" {
				return j
			}
			return -1
		}
	}
	return -1
}
