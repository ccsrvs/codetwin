package tokenizer

import "regexp"

// Ref is one identifier-shaped occurrence in comment-stripped source:
// the word as written and the 1-based line it appears on.
type Ref struct {
	Word string
	Line int
}

var refWordRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// References extracts every identifier-shaped occurrence from the given
// source, attributed to its 1-based line. Comments are stripped first
// (newline-preserving, so line numbers stay accurate); string literals
// and import statements are intentionally KEPT — a name mentioned in a
// string may be a dynamic-dispatch reference (getattr, reflection,
// apply/3), and a name mentioned in an import may be a re-export. Both
// therefore count as references, which biases dead-code analysis toward
// false-alive rather than false-dead.
func References(code string, lang Language) []Ref {
	p, ok := patterns[lang]
	if !ok {
		p = patterns[JavaScript] // same fallback as Normalize
	}
	stripped := replacePreservingNewlines(code, p.comments, " ")

	var refs []Ref
	line := 1
	start := 0
	for i := 0; i <= len(stripped); i++ {
		if i == len(stripped) || stripped[i] == '\n' {
			for _, m := range refWordRe.FindAllString(stripped[start:i], -1) {
				refs = append(refs, Ref{Word: m, Line: line})
			}
			line++
			start = i + 1
		}
	}
	return refs
}
