package tokenizer

import (
	"regexp"
	"sort"
	"strings"
)

// LexicalTerms extracts a snippet's content vocabulary from its RAW
// code: identifier and string-literal words, split on camelCase /
// snake_case / digit boundaries and lowercased. This is exactly the
// signal normalization erases (identifiers → VAR, strings → STR), kept
// separately so the report can tell "same shape, same content" apart
// from "same shape, different content" without touching the score.
//
// Language awareness mirrors the normalizer: comments and import/use
// statements are stripped first, and the per-language keyword set is
// skipped — keywords are grammar, not content. Single-character terms
// (loop variables, format verbs) and pure numbers are dropped: they're
// ubiquitous and would dilute both sides of a Jaccard equally.
//
// The result is a sorted, deduplicated term SET. Set semantics (rather
// than multiset) is deliberate: the question the structural-twin label
// asks is "do these two snippets talk about the same things?", and
// repetition counts track snippet length and style, not content
// identity — a multiset would penalize a rename clone for repeating
// its (renamed) accumulator more often than the original, which is
// exactly the rename-sensitivity this measure must not have.
func LexicalTerms(code string, lang Language) []string {
	p, ok := patterns[lang]
	if !ok {
		p = patterns[JavaScript] // same fallback as Normalize
	}
	kw := make(map[string]bool, len(p.keywords))
	for _, k := range p.keywords {
		kw[strings.ToLower(k)] = true
	}

	s := p.comments.ReplaceAllString(code, " ")
	for _, im := range p.imports {
		s = im.ReplaceAllString(s, " ")
	}

	set := make(map[string]struct{})
	harvest := func(chunk string) {
		for _, w := range lexWordRun.FindAllString(chunk, -1) {
			if kw[strings.ToLower(w)] {
				continue
			}
			for _, part := range splitLexicalWord(w) {
				if kw[part] {
					continue
				}
				set[part] = struct{}{}
			}
		}
	}
	// String-literal contents are content too ("insufficient funds" vs
	// "not enough stock" is a real divergence): harvest each literal's
	// words, then blank the literal so its delimiters don't leak.
	s = p.strings.ReplaceAllStringFunc(s, func(m string) string {
		harvest(m)
		return " "
	})
	harvest(s)

	if len(set) == 0 {
		return nil
	}
	terms := make([]string, 0, len(set))
	for t := range set {
		terms = append(terms, t)
	}
	sort.Strings(terms)
	return terms
}

// lexWordRun matches identifier-shaped runs; the camel/snake splitting
// happens per run in splitLexicalWord.
var lexWordRun = regexp.MustCompile(`[A-Za-z0-9_]+`)

// splitLexicalWord splits one identifier-shaped run into lowercase
// terms on underscores, lower→Upper camel boundaries, acronym ends
// (HTTPServer → http, server), and letter↔digit boundaries. Terms
// shorter than two characters and pure numbers are dropped.
func splitLexicalWord(w string) []string {
	var parts []string
	var cur []rune
	flush := func() {
		if isLexicalTerm(cur) {
			parts = append(parts, strings.ToLower(string(cur)))
		}
		cur = cur[:0]
	}
	for _, r := range w {
		c := lexClass(r)
		if c == lexOther { // underscore (regexp admits nothing else)
			flush()
			continue
		}
		if len(cur) > 0 {
			prev := lexClass(cur[len(cur)-1])
			switch {
			case prev == lexLower && c == lexUpper:
				flush() // fooBar → foo | Bar
			case (prev == lexDigit) != (c == lexDigit):
				flush() // float64 → float | 64
			case prev == lexUpper && c == lexLower && len(cur) > 1:
				// HTTPServer → HTTP|Server: the last upper rune starts
				// the next word.
				last := cur[len(cur)-1]
				cur = cur[:len(cur)-1]
				flush()
				cur = append(cur, last)
			}
		}
		cur = append(cur, r)
	}
	flush()
	return parts
}

type lexRuneClass int

const (
	lexLower lexRuneClass = iota
	lexUpper
	lexDigit
	lexOther
)

func lexClass(r rune) lexRuneClass {
	switch {
	case r >= 'a' && r <= 'z':
		return lexLower
	case r >= 'A' && r <= 'Z':
		return lexUpper
	case r >= '0' && r <= '9':
		return lexDigit
	default:
		return lexOther
	}
}

// isLexicalTerm reports whether a split word carries content signal:
// at least two runes and not purely numeric. Two-rune terms stay in —
// measured on the bench fixtures, dropping them ("be", "at", "ok")
// lowers renamed-clone overlap more than it lowers twin overlap,
// shrinking exactly the separation the floor depends on.
func isLexicalTerm(rs []rune) bool {
	if len(rs) < 2 {
		return false
	}
	for _, r := range rs {
		if lexClass(r) != lexDigit {
			return true
		}
	}
	return false
}
