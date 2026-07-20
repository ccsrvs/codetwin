// Package deadcode finds definitions that nothing in the scanned corpus
// references: name-based reachability over the same chunks the similarity
// pipeline already extracts.
//
// The analysis is a lexical heuristic, not compiler-grade reachability,
// so every choice biases toward false-alive rather than false-dead:
// a name mentioned anywhere outside its own definition — including in a
// string literal (dynamic dispatch) or an import (re-export) — keeps
// every same-named definition alive, and well-known entry points and
// implicitly-dispatched methods (main, init, TestXxx, dunders, OTP
// callbacks, fmt.Stringer's String, ...) are never reported at all.
package deadcode

import (
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// Verdict classifies an unreferenced definition.
type Verdict string

const (
	// VerdictDead: private/unexported and referenced by nothing in the
	// scan — the highest-confidence tier.
	VerdictDead Verdict = "dead"
	// VerdictUnusedInScan: exported/public and referenced by nothing in
	// the scan. Consumers outside the scanned roots may still use it,
	// so this tier is advisory.
	VerdictUnusedInScan Verdict = "unused-in-scan"
	// VerdictTestOnly: production code referenced only from test files —
	// dead weight in the shipped artifact, alive only for its tests.
	VerdictTestOnly Verdict = "test-only"
)

// Finding is one definition the scan cannot prove alive.
type Finding struct {
	Symbol    string
	Name      string // display name as used by every other section: "path:start-end symbol"
	Path      string // absolute; callers relativize for display
	StartLine int
	EndLine   int
	Kind      splitter.ChunkKind
	Lang      tokenizer.Language
	Exported  bool
	Verdict   Verdict
	ProdRefs  int // name occurrences in non-test files outside all same-name definition spans
	TestRefs  int // same, in test files
}

type span struct {
	start, end int
}

type defSite struct {
	snip *scan.Snippet
}

// Analyze runs name-based reachability over the scanned snippets. Files
// are re-read from disk so references in code outside any chunk (package
// var initializers, top-level registration calls) still count. Returns
// findings sorted by path then line, plus per-file read warnings.
func Analyze(snippets []scan.Snippet) ([]Finding, []string) {
	// Definition index: symbol -> sites, and (path, symbol) -> spans for
	// self-reference exclusion.
	defs := map[string][]defSite{}
	selfSpans := map[string]map[string][]span{} // path -> symbol -> spans
	fileLang := map[string]tokenizer.Language{}
	fileIsTest := map[string]bool{}
	for i := range snippets {
		s := &snippets[i]
		fileLang[s.Path] = s.Lang
		fileIsTest[s.Path] = s.IsTest
		if s.Symbol == "" {
			continue
		}
		// Synthetic symbols for anonymous chunks (goroutine@L41,
		// defer@L12, anonymous@L7) contain '@', which no real identifier
		// can. They are never referenced by name — an anonymous chunk
		// runs when its enclosing function does — so they have no place
		// in name-based reachability.
		if strings.ContainsRune(s.Symbol, '@') {
			continue
		}
		defs[s.Symbol] = append(defs[s.Symbol], defSite{snip: s})
		bySym := selfSpans[s.Path]
		if bySym == nil {
			bySym = map[string][]span{}
			selfSpans[s.Path] = bySym
		}
		bySym[s.Symbol] = append(bySym[s.Symbol], span{s.StartLine, s.EndLine})
	}
	if len(defs) == 0 {
		return nil, nil
	}

	// Reference index: for every defined symbol, count occurrences that
	// fall outside every same-file definition span of that symbol.
	prodRefs := map[string]int{}
	testRefs := map[string]int{}
	var warnings []string
	for path, lang := range fileLang {
		data, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, "dead-code: could not re-read "+path+": "+err.Error())
			continue
		}
		isTest := fileIsTest[path]
		bySym := selfSpans[path]
		for _, ref := range tokenizer.References(string(data), lang) {
			if _, defined := defs[ref.Word]; !defined {
				continue
			}
			if inAnySpan(bySym[ref.Word], ref.Line) {
				continue
			}
			if isTest {
				testRefs[ref.Word]++
			} else {
				prodRefs[ref.Word]++
			}
		}
	}

	var findings []Finding
	for sym, sites := range defs {
		if prodRefs[sym] > 0 {
			continue // alive
		}
		for _, site := range sites {
			s := site.snip
			if suppressed(sym, s) {
				continue
			}
			exported := isExported(sym, s)
			verdict := VerdictDead
			switch {
			case testRefs[sym] > 0 && !s.IsTest:
				verdict = VerdictTestOnly
			case exported && !s.IsTest:
				// Test files have no external consumers (Go test files
				// aren't importable; other languages' test modules are
				// equally terminal), so an exported-but-unreferenced
				// test definition is plain dead, not advisory.
				verdict = VerdictUnusedInScan
			}
			// A test helper referenced by other tests is doing its job;
			// only report test-file definitions nothing references at all.
			if s.IsTest && testRefs[sym] > 0 {
				continue
			}
			findings = append(findings, Finding{
				Symbol:    sym,
				Name:      s.Name,
				Path:      s.Path,
				StartLine: s.StartLine,
				EndLine:   s.EndLine,
				Kind:      s.Kind,
				Lang:      s.Lang,
				Exported:  exported,
				Verdict:   verdict,
				ProdRefs:  prodRefs[sym],
				TestRefs:  testRefs[sym],
			})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		if findings[i].StartLine != findings[j].StartLine {
			return findings[i].StartLine < findings[j].StartLine
		}
		return findings[i].Symbol < findings[j].Symbol
	})
	return findings, warnings
}

func inAnySpan(spans []span, line int) bool {
	for _, sp := range spans {
		if line >= sp.start && line <= sp.end {
			return true
		}
	}
	return false
}

// firstLine returns the first line of a chunk's code, which carries the
// visibility modifiers every supported language puts on the definition
// line (export, pub, public, defp).
func firstLine(code string) string {
	if i := strings.IndexByte(code, '\n'); i >= 0 {
		return code[:i]
	}
	return code
}

// isExported reports whether a definition is visible outside its own
// file/module/package under the language's convention. Exported symbols
// may have consumers outside the scanned roots, so they are reported in
// the advisory unused-in-scan tier instead of dead.
func isExported(sym string, s *scan.Snippet) bool {
	switch s.Lang {
	case tokenizer.Go:
		r, _ := utf8.DecodeRuneInString(sym)
		return unicode.IsUpper(r)
	case tokenizer.Python:
		return !strings.HasPrefix(sym, "_")
	case tokenizer.Rust:
		return rustPubRe.MatchString(firstLine(s.Code))
	case tokenizer.Java:
		return strings.Contains(firstLine(s.Code), "public")
	case tokenizer.JavaScript:
		return strings.Contains(firstLine(s.Code), "export")
	case tokenizer.Elixir:
		return !strings.HasPrefix(strings.TrimSpace(firstLine(s.Code)), "defp")
	}
	return true // unknown language: assume exported, stay advisory
}

// suppressed reports definitions that must never be flagged: entry
// points the runtime calls, and methods dispatched without their name
// ever appearing in user code (interface/trait/magic methods, operator
// overloads, framework lifecycle hooks).
func suppressed(sym string, s *scan.Snippet) bool {
	if names, ok := suppressedNames[s.Lang]; ok && names[sym] {
		return true
	}
	switch s.Lang {
	case tokenizer.Go:
		// Test entry points, only where the toolchain discovers them.
		if s.IsTest {
			for _, p := range []string{"Test", "Benchmark", "Example", "Fuzz"} {
				if strings.HasPrefix(sym, p) {
					return true
				}
			}
		}
	case tokenizer.Python:
		// Dunder methods are dispatched by the runtime (__init__,
		// __repr__, __enter__, ...).
		if strings.HasPrefix(sym, "__") && strings.HasSuffix(sym, "__") {
			return true
		}
	}
	return false
}
