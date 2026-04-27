// Package tokenizer provides language-aware lexing and normalization.
// Normalization is the highest-leverage step: rename variables to VAR,
// strings to STR, numbers to NUM, strip comments — so structurally identical
// logic compares equal regardless of naming choices.
package tokenizer

import (
	"regexp"
	"strings"
	"unicode"
)

// Language represents a supported source language.
type Language string

const (
	JavaScript Language = "javascript"
	Python     Language = "python"
	Java       Language = "java"
	Go         Language = "go"
	Rust       Language = "rust"
	Elixir     Language = "elixir"
	Unknown    Language = "unknown"
)

// langPatterns holds the regexes needed to normalize a given language.
type langPatterns struct {
	keywords []string
	comments *regexp.Regexp
	// imports is applied AFTER comments and BEFORE strings/numbers — it strips
	// import/use/alias statements that would otherwise dominate similarity
	// scores for short modules. Each regex in the slice is applied in order;
	// list multi-line patterns first so they consume their content before the
	// single-line patterns try to match a prefix of them.
	imports []*regexp.Regexp
	strings *regexp.Regexp
	numbers *regexp.Regexp
}

var patterns = map[Language]*langPatterns{
	JavaScript: {
		keywords: []string{
			"function", "const", "let", "var", "return", "if", "else",
			"for", "while", "class", "import", "export", "async", "await",
			"new", "this", "typeof", "instanceof", "switch", "case", "break",
			"continue", "throw", "try", "catch", "finally", "null", "undefined",
			"true", "false",
		},
		comments: regexp.MustCompile(`//[^\n]*|/\*[\s\S]*?\*/`),
		imports: []*regexp.Regexp{
			// import { a, b } from 'x' — multi-line braces
			regexp.MustCompile(`(?m)^[ \t]*import\s*\{[\s\S]*?\}\s*from\s+["'][^"']+["']\s*;?`),
			// export { a, b } from 'x' — re-export, multi-line
			regexp.MustCompile(`(?m)^[ \t]*export\s*\{[\s\S]*?\}\s*from\s+["'][^"']+["']\s*;?`),
			// import default / namespace / side-effect / dynamic
			regexp.MustCompile(`(?m)^[ \t]*import[^;\n]*?from\s+["'][^"']+["']\s*;?`),
			regexp.MustCompile(`(?m)^[ \t]*import\s+["'][^"']+["']\s*;?`),
			// export * from 'x'
			regexp.MustCompile(`(?m)^[ \t]*export\s+\*\s+from\s+["'][^"']+["']\s*;?`),
			// const x = require('y')
			regexp.MustCompile(`(?m)^[ \t]*(?:const|let|var)\s+[\w{}\s,]+=\s*require\s*\(\s*["'][^"']+["']\s*\)\s*;?`),
		},
		strings: regexp.MustCompile("`[^`]*`|'(?:[^'\\\\]|\\\\.)*'|\"(?:[^\"\\\\]|\\\\.)*\""),
		numbers: regexp.MustCompile(`\b\d+(\.\d+)?\b`),
	},
	Python: {
		keywords: []string{
			"def", "class", "return", "if", "elif", "else", "for", "while",
			"import", "from", "as", "with", "lambda", "yield", "pass", "break",
			"continue", "and", "or", "not", "in", "is", "True", "False", "None",
			"raise", "try", "except", "finally", "global", "nonlocal", "del",
			"assert", "async", "await",
		},
		comments: regexp.MustCompile(`#[^\n]*`),
		imports: []*regexp.Regexp{
			// from x import (a, b, c) — multi-line
			regexp.MustCompile(`(?m)^[ \t]*from\s+[\w.]+\s+import\s*\([\s\S]*?\)`),
			// from x import a, b
			regexp.MustCompile(`(?m)^[ \t]*from\s+[\w.]+\s+import[^\n]*`),
			// import x, y
			regexp.MustCompile(`(?m)^[ \t]*import\s+[^\n]*`),
		},
		strings: regexp.MustCompile(`'''[\s\S]*?'''|"""[\s\S]*?"""|'[^'\\]*(?:\\.[^'\\]*)*'|"[^"\\]*(?:\\.[^"\\]*)*"`),
		numbers: regexp.MustCompile(`\b\d+(\.\d+)?\b`),
	},
	Java: {
		keywords: []string{
			"public", "private", "protected", "class", "interface", "extends",
			"implements", "return", "if", "else", "for", "while", "new", "this",
			"super", "static", "final", "void", "int", "long", "double", "float",
			"boolean", "String", "import", "package", "throw", "throws", "try",
			"catch", "finally", "null", "true", "false", "instanceof", "abstract",
		},
		comments: regexp.MustCompile(`//[^\n]*|/\*[\s\S]*?\*/`),
		imports: []*regexp.Regexp{
			regexp.MustCompile(`(?m)^[ \t]*import(?:\s+static)?\s+[\w.\*]+\s*;`),
			regexp.MustCompile(`(?m)^[ \t]*package\s+[\w.]+\s*;`),
		},
		strings: regexp.MustCompile(`"(?:[^"\\]|\\.)*"`),
		numbers: regexp.MustCompile(`\b\d+(\.\d+)?[fFdDlL]?\b`),
	},
	Go: {
		keywords: []string{
			"func", "package", "import", "return", "if", "else", "for", "range",
			"struct", "interface", "type", "var", "const", "go", "chan", "select",
			"defer", "make", "new", "map", "append", "len", "cap", "nil", "true",
			"false", "break", "continue", "switch", "case", "default", "fallthrough",
			"goto",
		},
		comments: regexp.MustCompile("//[^\n]*|/\\*[\\s\\S]*?\\*/"),
		imports: []*regexp.Regexp{
			// import ( ... ) — multi-line block
			regexp.MustCompile(`(?m)^[ \t]*import\s*\([\s\S]*?\)`),
			// import "fmt"
			regexp.MustCompile(`(?m)^[ \t]*import\s+(?:[\w.]+\s+)?["][^"]+["]`),
			regexp.MustCompile("(?m)^[ \t]*package\\s+\\w+"),
		},
		strings: regexp.MustCompile("`[^`]*`|\"(?:[^\"\\\\]|\\\\.)*\""),
		numbers: regexp.MustCompile(`\b\d+(\.\d+)?\b`),
	},
	Rust: {
		keywords: []string{
			"fn", "let", "mut", "struct", "impl", "trait", "use", "mod", "pub",
			"return", "if", "else", "for", "while", "match", "enum", "type",
			"where", "async", "await", "move", "ref", "self", "Self", "break",
			"continue", "loop", "crate", "super", "true", "false", "const",
			"static", "unsafe", "extern",
		},
		comments: regexp.MustCompile(`//[^\n]*|/\*[\s\S]*?\*/`),
		imports: []*regexp.Regexp{
			// use foo::bar; or use foo::{a, b}; (semicolon-terminated, allows newlines)
			regexp.MustCompile(`(?m)^[ \t]*(?:pub\s+)?use\s+[^;]+;`),
			regexp.MustCompile(`(?m)^[ \t]*extern\s+crate\s+[^;]+;`),
		},
		strings: regexp.MustCompile(`r#*"[\s\S]*?"#*|"(?:[^"\\]|\\.)*"`),
		numbers: regexp.MustCompile(`\b\d+(\.\d+)?(_\w+)?\b`),
	},
	Elixir: {
		keywords: []string{
			"def", "defp", "defmodule", "defstruct", "defprotocol", "defimpl",
			"defmacro", "defmacrop", "do", "end", "if", "else", "unless",
			"case", "cond", "with", "for", "receive", "send", "spawn",
			"fn", "when", "and", "or", "not", "in", "true", "false", "nil",
			"import", "alias", "use", "require", "raise", "rescue", "after",
			"try", "catch", "throw", "super", "quote", "unquote", "pipe",
		},
		// Elixir comments start with #
		comments: regexp.MustCompile(`#[^\n]*`),
		imports: []*regexp.Regexp{
			// alias Foo.{Bar, Baz} — multi-line group
			regexp.MustCompile(`(?m)^[ \t]*(?:import|alias|require|use)\s+[\w.]+\.\{[\s\S]*?\}`),
			// import Foo / alias Foo / require Foo / use Foo (single line)
			regexp.MustCompile(`(?m)^[ \t]*(?:import|alias|require|use)\s+[^\n]*`),
		},
		// Heredocs (triple-quote), sigils (~s, ~r, etc.), and regular strings
		strings: regexp.MustCompile(`~[a-zA-Z]?\[[\s\S]*?\]|~[a-zA-Z]?"[\s\S]*?"|"""[\s\S]*?"""|'''[\s\S]*?'''|"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`),
		numbers: regexp.MustCompile(`\b\d[\d_]*(\.\d[\d_]*)?\b`),
	},
}

// Detect infers the language from file extension or code heuristics.
func Detect(filename, code string) Language {
	// Extension-based detection first
	switch {
	case strings.HasSuffix(filename, ".js") || strings.HasSuffix(filename, ".ts") ||
		strings.HasSuffix(filename, ".jsx") || strings.HasSuffix(filename, ".tsx"):
		return JavaScript
	case strings.HasSuffix(filename, ".py"):
		return Python
	case strings.HasSuffix(filename, ".java"):
		return Java
	case strings.HasSuffix(filename, ".go"):
		return Go
	case strings.HasSuffix(filename, ".rs"):
		return Rust
	case strings.HasSuffix(filename, ".ex") || strings.HasSuffix(filename, ".exs"):
		return Elixir
	}

	// Heuristic fallback from code content
	switch {
	case strings.Contains(code, "package main") || strings.Contains(code, "func "):
		return Go
	case strings.Contains(code, "fn ") && strings.Contains(code, "let mut"):
		return Rust
	case strings.Contains(code, "defmodule") || strings.Contains(code, "def ") && strings.Contains(code, "do"):
		return Elixir
	case strings.Contains(code, "def ") && strings.Contains(code, ":"):
		return Python
	case strings.Contains(code, "public class") || strings.Contains(code, "System.out"):
		return Java
	case strings.Contains(code, "function") || strings.Contains(code, "const ") ||
		strings.Contains(code, "=>"):
		return JavaScript
	}

	return Unknown
}

// Normalize strips comments, replaces literals and identifiers with canonical
// tokens, and collapses whitespace. Returns the normalized string.
func Normalize(code string, lang Language) string {
	p, ok := patterns[lang]
	if !ok {
		p = patterns[JavaScript] // sensible fallback
	}

	s := code

	// 1. Strip comments
	s = p.comments.ReplaceAllString(s, " ")

	// 1b. Strip import / use / package statements (language-specific). Order
	// matters within the slice — list multi-line patterns first so they consume
	// their full span before the single-line variants get a chance to match
	// only a prefix of the same statement.
	for _, im := range p.imports {
		s = im.ReplaceAllString(s, " ")
	}

	// 2. Normalize string literals
	s = p.strings.ReplaceAllString(s, "STR")

	// 3. Normalize numeric literals
	s = p.numbers.ReplaceAllString(s, "NUM")

	// 4. Collapse whitespace
	s = collapseWhitespace(s)

	// 5. Build keyword set for fast lookup
	kwSet := make(map[string]bool, len(p.keywords))
	for _, kw := range p.keywords {
		kwSet[kw] = true
	}

	// 6. Replace non-keyword identifiers with VAR
	ident := regexp.MustCompile(`\b[a-zA-Z_][a-zA-Z0-9_]*\b`)
	s = ident.ReplaceAllStringFunc(s, func(m string) string {
		if kwSet[m] || m == "STR" || m == "NUM" {
			return m
		}
		return "VAR"
	})

	return strings.TrimSpace(s)
}

// Tokenize normalizes the code then splits on whitespace and punctuation
// returning a clean token slice ready for fingerprinting.
func Tokenize(code string, lang Language) []string {
	tokens, _ := TokenizeWithLines(code, lang)
	return tokens
}

// TokenizeWithLines is Tokenize plus a parallel slice of 1-based line numbers,
// where lines[i] is the originating source line of tokens[i]. Multi-line
// constructs (block comments, multi-line strings) are replaced with their
// canonical token (e.g. "STR") attributed to the line where they opened, with
// downstream blank lines preserved so subsequent token line numbers remain
// accurate.
func TokenizeWithLines(code string, lang Language) ([]string, []int) {
	p, ok := patterns[lang]
	if !ok {
		p = patterns[JavaScript]
	}

	preprocessed := preprocessKeepLines(code, p)

	kwSet := make(map[string]bool, len(p.keywords))
	for _, kw := range p.keywords {
		kwSet[kw] = true
	}
	ident := regexp.MustCompile(`\b[a-zA-Z_][a-zA-Z0-9_]*\b`)

	var tokens []string
	var lineNums []int
	for i, line := range strings.Split(preprocessed, "\n") {
		// Replace non-keyword identifiers with VAR
		normalizedLine := ident.ReplaceAllStringFunc(line, func(m string) string {
			if kwSet[m] || m == "STR" || m == "NUM" {
				return m
			}
			return "VAR"
		})
		raw := strings.FieldsFunc(normalizedLine, func(r rune) bool {
			return unicode.IsSpace(r)
		})
		for _, t := range raw {
			t = strings.TrimFunc(t, func(r rune) bool {
				return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
			})
			if t == "" {
				continue
			}
			tokens = append(tokens, t)
			lineNums = append(lineNums, i+1)
		}
	}
	return tokens, lineNums
}

// preprocessKeepLines is Normalize's comment/import/string/number passes done
// in a way that preserves newline positions: each replacement keeps the same
// number of newlines as the matched span so that downstream line-number
// tracking stays accurate. Identifier replacement is intentionally NOT done
// here — callers handle that per-line so they can attribute each token to a
// source line.
func preprocessKeepLines(code string, p *langPatterns) string {
	s := replacePreservingNewlines(code, p.comments, " ")
	for _, im := range p.imports {
		s = replacePreservingNewlines(s, im, " ")
	}
	s = replacePreservingNewlines(s, p.strings, "STR")
	// Numeric literals are line-internal in every supported language.
	s = p.numbers.ReplaceAllString(s, "NUM")
	return s
}

// replacePreservingNewlines replaces each match of re with `replacement`,
// then re-appends one '\n' per newline that was inside the original match.
// This keeps the total newline count of the document unchanged so that line
// indexes computed downstream still line up with the original source.
func replacePreservingNewlines(s string, re *regexp.Regexp, replacement string) string {
	return re.ReplaceAllStringFunc(s, func(match string) string {
		nl := strings.Count(match, "\n")
		if nl == 0 {
			return replacement
		}
		return replacement + strings.Repeat("\n", nl)
	})
}

func collapseWhitespace(s string) string {
	b := strings.Builder{}
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}
