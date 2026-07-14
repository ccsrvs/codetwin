package config

import (
	"fmt"
	"regexp"
	"strings"
)

// IgnorePair is one entry in the .codetwin.json `ignore_pairs` array. Both
// endpoints follow the splitter chunk-name shape, minus the line range:
//
//	"path"           → match any chunk in that file (path is a glob)
//	"path SYMBOL"    → match only chunks whose splitter symbol equals SYMBOL
//
// A pair of snippets is suppressed when the two snippets match A and B in
// either order.
type IgnorePair struct {
	A string `json:"a"`
	B string `json:"b"`
}

// PairIgnoreMatcher decides whether a (NameA, NameB) pair should be ignored
// based on user-supplied rules. Build via CompileIgnorePairs and reuse for
// every pair.
type PairIgnoreMatcher struct {
	rules []pairRule
}

type pairRule struct {
	a, b endpoint
}

type endpoint struct {
	pathRE  *regexp.Regexp // non-nil when path uses wildcards
	pathLit string         // non-empty when path is a plain string
	symbol  string         // empty → match any symbol
}

// snippetNameRange matches the ":start-end" portion of a chunk name produced
// by splitter.Chunk.Name(). The optional " Symbol" tail (everything after the
// first whitespace following the range) is captured separately.
var snippetNameRange = regexp.MustCompile(`:\d+-\d+(?:\s+(.*))?$`)

// parseSnippetName decomposes a snippet identifier into its stable parts.
// It accepts the three forms produced by splitter.Chunk.Name():
//
//	"path"                       → ("path", "")
//	"path:start-end"             → ("path", "")
//	"path:start-end Symbol"      → ("path", "Symbol")
//
// Line numbers are deliberately discarded so users can write durable
// ignore_pairs entries that survive ordinary edits.
func parseSnippetName(name string) (path, symbol string) {
	loc := snippetNameRange.FindStringSubmatchIndex(name)
	if loc == nil {
		return name, ""
	}
	path = name[:loc[0]]
	if loc[2] >= 0 {
		symbol = name[loc[2]:loc[3]]
	}
	return path, symbol
}

// ParseSnippetName is the exported form of parseSnippetName. The baseline
// package reuses it so clone-watchlist member identity and ignore_pairs
// endpoints share exactly one normalization: the ":start-end" line range is
// discarded (routine edits shift line numbers) and the optional splitter
// symbol is returned separately.
func ParseSnippetName(name string) (path, symbol string) {
	return parseSnippetName(name)
}

// CompileIgnorePairs turns the `ignore_pairs` config slice into a matcher.
// Both endpoints are validated; invalid globs and empty endpoints are
// collected and reported together so the user fixes every problem in one
// pass instead of one error per run.
func CompileIgnorePairs(pairs []IgnorePair) (*PairIgnoreMatcher, error) {
	if len(pairs) == 0 {
		return &PairIgnoreMatcher{}, nil
	}
	rules := make([]pairRule, 0, len(pairs))
	var errs []string
	for i, p := range pairs {
		ea, errA := compileEndpoint(p.A)
		eb, errB := compileEndpoint(p.B)
		if errA != nil {
			errs = append(errs, fmt.Sprintf("ignore_pairs[%d].a %q: %v", i, p.A, errA))
		}
		if errB != nil {
			errs = append(errs, fmt.Sprintf("ignore_pairs[%d].b %q: %v", i, p.B, errB))
		}
		if errA == nil && errB == nil {
			rules = append(rules, pairRule{a: ea, b: eb})
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid ignore_pairs: %s", strings.Join(errs, "; "))
	}
	return &PairIgnoreMatcher{rules: rules}, nil
}

// compileEndpoint parses one side of an IgnorePair. The leading path is the
// glob (or literal); a single space separates an optional symbol tail.
// compileAnchoredGlob (used by ignore_paths) is reused so syntax stays
// consistent.
func compileEndpoint(s string) (endpoint, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return endpoint{}, fmt.Errorf("endpoint must not be empty")
	}
	pathPart, symPart := s, ""
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		pathPart = strings.TrimSpace(s[:i])
		symPart = strings.TrimSpace(s[i+1:])
	}
	if pathPart == "" {
		return endpoint{}, fmt.Errorf("path part must not be empty")
	}
	e := endpoint{symbol: symPart}
	if strings.ContainsAny(pathPart, "*?") {
		re, err := compileAnchoredGlob(pathPart)
		if err != nil {
			return endpoint{}, err
		}
		e.pathRE = re
	} else {
		e.pathLit = pathPart
	}
	return e, nil
}

// match reports whether the endpoint matches a parsed snippet path/symbol.
// A symbol of "" on the endpoint means "any symbol in that file".
func (e endpoint) match(path, symbol string) bool {
	if e.symbol != "" && e.symbol != symbol {
		return false
	}
	if e.pathRE != nil {
		return e.pathRE.MatchString(path)
	}
	return matchPathComponent(path, e.pathLit)
}

// Match reports whether the (nameA, nameB) pair is ignored. Order is
// irrelevant: a rule with endpoints (X, Y) fires for both (A=X, B=Y) and
// (A=Y, B=X). nil receivers are safe and always return false so callers
// don't need to special-case "no rules configured".
func (m *PairIgnoreMatcher) Match(nameA, nameB string) bool {
	if m == nil || len(m.rules) == 0 {
		return false
	}
	pa, sa := parseSnippetName(nameA)
	pb, sb := parseSnippetName(nameB)
	for _, r := range m.rules {
		if r.a.match(pa, sa) && r.b.match(pb, sb) {
			return true
		}
		if r.a.match(pb, sb) && r.b.match(pa, sa) {
			return true
		}
	}
	return false
}
