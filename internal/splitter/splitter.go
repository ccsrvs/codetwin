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

// Split breaks code into per-definition chunks. The returned slice always
// contains at least one chunk: when no definitions are found the whole file
// is returned as a single anonymous chunk.
func Split(path, code string, lang tokenizer.Language) []Chunk {
	var chunks []Chunk
	switch lang {
	case tokenizer.Python:
		chunks = splitPython(code)
	case tokenizer.Go:
		chunks = splitBraceLang(code, goFuncRe)
	case tokenizer.JavaScript:
		chunks = splitJavaScript(code)
	case tokenizer.Rust:
		chunks = splitBraceLang(code, rustFnRe)
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

var pyDefRe = regexp.MustCompile(`^(\s*)(?:async\s+)?def\s+(\w+)`)

func splitPython(code string) []Chunk {
	lines := strings.Split(code, "\n")
	type defStart struct {
		line, indent int
		name         string
	}
	var defs []defStart
	for i, line := range lines {
		m := pyDefRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		defs = append(defs, defStart{
			line:   i,
			indent: indentLen(m[1]),
			name:   m[2],
		})
	}
	if len(defs) == 0 {
		return nil
	}
	var chunks []Chunk
	for _, d := range defs {
		end := len(lines) - 1
		for j := d.line + 1; j < len(lines); j++ {
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
			StartLine: d.line + 1,
			EndLine:   end + 1,
			Symbol:    d.name,
			Code:      strings.Join(lines[d.line:end+1], "\n"),
		})
	}
	return chunks
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
)

// splitBraceLang chunks code using a "find a definition header, then
// brace-balance to its closer" strategy. Works for Go and Rust.
func splitBraceLang(code string, headerRe *regexp.Regexp) []Chunk {
	lines := strings.Split(code, "\n")
	var chunks []Chunk
	i := 0
	for i < len(lines) {
		m := headerRe.FindStringSubmatch(lines[i])
		if m == nil {
			i++
			continue
		}
		end, ok := findBraceEnd(lines, i)
		if !ok {
			// No body braces (e.g. interface method stub) — skip without
			// emitting a chunk.
			i++
			continue
		}
		chunks = append(chunks, Chunk{
			StartLine: i + 1,
			EndLine:   end + 1,
			Symbol:    m[1],
			Code:      strings.Join(lines[i:end+1], "\n"),
		})
		i = end + 1
	}
	return chunks
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
	jsFuncRe  = regexp.MustCompile(`^(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s+(\w+)`)
	jsArrowRe = regexp.MustCompile(`^(?:export\s+(?:default\s+)?)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:function\b|\([^)]*\)\s*=>|\w+\s*=>)`)
	jsClassRe = regexp.MustCompile(`^(?:export\s+(?:default\s+)?)?class\s+(\w+)`)
)

func splitJavaScript(code string) []Chunk {
	lines := strings.Split(code, "\n")
	var chunks []Chunk
	i := 0
	for i < len(lines) {
		var symbol string
		for _, re := range []*regexp.Regexp{jsFuncRe, jsArrowRe, jsClassRe} {
			if m := re.FindStringSubmatch(lines[i]); m != nil {
				symbol = m[1]
				break
			}
		}
		if symbol == "" {
			i++
			continue
		}
		end, ok := findBraceEnd(lines, i)
		if !ok {
			// Body-less arrow (single-expression) — emit just that line.
			chunks = append(chunks, Chunk{
				StartLine: i + 1,
				EndLine:   i + 1,
				Symbol:    symbol,
				Code:      lines[i],
			})
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
