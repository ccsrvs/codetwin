package refactor

import (
	"regexp"
	"strings"
)

// Container placement for BuildPatch: Java methods and Elixir defs
// cannot live at file scope, so the emitted helper must be inserted
// inside the innermost container (class/interface/enum/record for
// Java, defmodule for Elixir) that encloses snippet A's chunk. The
// detection here mirrors internal/splitter's scanning rules
// (javaTypeDeclRe, findBraceEnd, exFindMatchingEnd) rather than
// importing them — they're private implementation details of chunking,
// and placement intentionally tolerates the same well-formatted-code
// assumptions the splitter makes.

// placeJavaTypeDeclRe matches lines that declare a Java type. Mirror of
// splitter's javaTypeDeclRe.
var placeJavaTypeDeclRe = regexp.MustCompile(`^[ \t]*(?:(?:public|private|protected|static|final|abstract|sealed|non-sealed)\s+)*(?:class|interface|enum|record|@interface)\s+`)

// placeExModuleRe matches a `defmodule Name do` header, capturing the
// leading indent.
var placeExModuleRe = regexp.MustCompile(`^([ \t]*)defmodule\s`)

// javaEnclosingTypeClose returns the 1-based line number of the closing
// `}` of the innermost type declaration enclosing the (1-based)
// chunkStart line. ok=false when no type declaration encloses it —
// callers fall back to a file-scope append.
//
// Innermost selection: type headers are scanned top-to-bottom; any
// later header that still encloses chunkStart is nested inside every
// earlier enclosing one (siblings closing before chunkStart are
// filtered out by the close-after-chunk check), so the last enclosing
// candidate wins.
func javaEnclosingTypeClose(fileContent string, chunkStart int) (int, bool) {
	lines := strings.Split(fileContent, "\n")
	best := -1
	for i := 0; i < len(lines) && i+1 < chunkStart; i++ {
		if !placeJavaTypeDeclRe.MatchString(lines[i]) {
			continue
		}
		end, ok := braceEndLine(lines, i)
		if !ok {
			continue
		}
		if end+1 > chunkStart {
			best = end
		}
	}
	if best < 0 {
		return 0, false
	}
	return best + 1, true
}

// braceEndLine scans from the start line until the running brace depth
// opens and closes back to 0, returning that 0-based line index.
// Mirror of splitter's findBraceEnd — naive rune counting that ignores
// braces in strings/comments, which matches how the chunk itself was
// delimited.
func braceEndLine(lines []string, start int) (int, bool) {
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
	return 0, false
}

// elixirEnclosingModuleEnd returns the 1-based line number of the
// closing `end` of the innermost defmodule enclosing the (1-based)
// chunkStart line. ok=false when no defmodule encloses it.
func elixirEnclosingModuleEnd(fileContent string, chunkStart int) (int, bool) {
	lines := strings.Split(fileContent, "\n")
	best := -1
	for i := 0; i < len(lines) && i+1 < chunkStart; i++ {
		m := placeExModuleRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		end := exModuleMatchingEnd(lines, i, indentWidth(m[1]))
		if end < 0 {
			continue
		}
		if end+1 > chunkStart {
			best = end
		}
	}
	if best < 0 {
		return 0, false
	}
	return best + 1, true
}

// exModuleMatchingEnd walks forward from the defmodule header looking
// for the first non-blank line at indent <= the header's indent. That
// line closes the module when its trimmed text is `end`; anything else
// means the module is malformed (mirror of splitter's
// exFindMatchingEnd, which makes the same well-formatted-indentation
// assumption). Returns the 0-based line index, or -1.
func exModuleMatchingEnd(lines []string, headerLine, headerIndent int) int {
	for j := headerLine + 1; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			continue
		}
		if indentWidth(lines[j]) <= headerIndent {
			if t == "end" {
				return j
			}
			return -1
		}
	}
	return -1
}

// indentWidth returns the visual indent width of leading whitespace,
// treating each tab as 4 spaces. Mirror of splitter's indentLen.
func indentWidth(s string) int {
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

// lineIndent returns the literal leading whitespace of the (1-based)
// line in fileContent, or "" when the line is out of range. Used to
// indent the helper like a sibling of A's chunk: the chunk's own first
// line already sits at exactly the member indent of its container.
func lineIndent(fileContent string, line int) string {
	lines := strings.Split(fileContent, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	l := lines[line-1]
	i := 0
	for i < len(l) && (l[i] == ' ' || l[i] == '\t') {
		i++
	}
	return l[:i]
}

// indentBlock prefixes every non-empty line of src with indent. Empty
// lines stay empty so the patch adds no trailing whitespace.
func indentBlock(src, indent string) string {
	if indent == "" {
		return src
	}
	lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
	var b strings.Builder
	for _, l := range lines {
		if l != "" {
			b.WriteString(indent)
		}
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}
