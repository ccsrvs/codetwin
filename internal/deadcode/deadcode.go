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
	"path/filepath"
	"regexp"
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
// findings sorted by path then line, plus per-file read warnings. Optional
// files extend the reference corpus to files that yielded no eligible snippets.
func Analyze(snippets []scan.Snippet, files ...string) ([]Finding, []string) {
	// Definition index: symbol -> sites, and (path, symbol) -> spans for
	// self-reference exclusion.
	defs := map[string][]defSite{}
	selfSpans := map[string]map[string][]span{} // path -> symbol -> spans
	fileLang := map[string]tokenizer.Language{}
	fileIsTest := map[string]bool{}
	for _, file := range files {
		abs, err := filepath.Abs(file)
		if err != nil {
			continue
		}
		fileLang[abs] = tokenizer.Detect(file, "")
		fileIsTest[abs] = scan.IsTestFile(file)
	}
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
		if syntheticSymbolRe.MatchString(s.Symbol) {
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
	// underscoreAlias maps foo to an assembly definition _foo.
	underscoreAlias := map[string]string{}
	for sym, sites := range defs {
		for _, site := range sites {
			if tokenizer.IsAssembly(site.snip.Lang) && strings.HasPrefix(sym, "_") && len(sym) > 1 {
				underscoreAlias[sym[1:]] = sym
			}
		}
	}
	var warnings []string
	for path, lang := range fileLang {
		data, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, "dead-code: could not re-read "+path+": "+err.Error())
			continue
		}
		if tokenizer.IsAssembly(lang) {
			// A file with no chunks got its dialect from the extension
			// alone; its content decides.
			lang = tokenizer.Detect(path, string(data))
			if lang == tokenizer.Unknown {
				continue // a .mac file that is not HLASM is not scanned
			}
		}
		isTest := fileIsTest[path]
		bySym := selfSpans[path]
		declared := declarationSites(string(data), lang)
		for _, ref := range tokenizer.References(string(data), lang) {
			if declared[ref.Line][ref.Word] {
				continue // a prototype or directive names it without using it
			}
			// Mach-O and 32-bit Windows prefix C symbols with "_": C's
			// foo is _foo in assembly, and assembly calls _foo for it.
			if alt, ok := underscoreAlias[ref.Word]; ok && !inAnySpan(bySym[alt], ref.Line) {
				count(isTest, alt, prodRefs, testRefs)
			}
			if tokenizer.IsAssembly(lang) && strings.HasPrefix(ref.Word, "_") {
				if base := ref.Word[1:]; defs[base] != nil && !inAnySpan(bySym[base], ref.Line) {
					count(isTest, base, prodRefs, testRefs)
				}
			}
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

// syntheticSymbolRe matches the line-qualified symbols splitters give
// chunks that have no name of their own (goroutine@L41, TEST@L12,
// CSECT@L7). HLASM names may contain "@" themselves (SUB@1), so only
// the @L<line> suffix marks a symbol as synthetic.
var syntheticSymbolRe = regexp.MustCompile(`@L\d+$`)

// count records one reference to sym from a test or production file.
func count(isTest bool, sym string, prodRefs, testRefs map[string]int) {
	if isTest {
		testRefs[sym]++
	} else {
		prodRefs[sym]++
	}
}

// declarationSites returns, by 1-based line, the words a file mentions
// without using them: C prototypes and forward declarations.
func declarationSites(code string, lang tokenizer.Language) map[int]map[string]bool {
	switch {
	case lang == tokenizer.C:
		return splitter.CDeclarations(code)
	case tokenizer.IsAssembly(lang):
		return asmDeclarations(code, lang)
	}
	return nil
}

// asmDeclRe matches, per dialect, the lines that declare symbols —
// export and visibility directives, symbol types and sizes, routine
// start/end markers — which name a symbol without calling it.
var asmDeclRe = map[tokenizer.Language]*regexp.Regexp{
	tokenizer.AsmGAS:   regexp.MustCompile(`^\s*(?:\.(?:globl|global|type|size|hidden|weak|protected|internal|local|extern|def|scl|endef|func|endfunc|ent|end)\b|(?:ENTRY\w*|END\w*|ENDPROC|WEAK_ENTRY|LEAF|NESTED|SYM_\w+)\s*\()`),
	tokenizer.AsmNASM:  regexp.MustCompile(`(?i)^\s*(?:global|extern|cextern|cglobal|common|GLOBAL_FUNCTION|GLOBAL_DATA)\b`),
	tokenizer.AsmMASM:  regexp.MustCompile(`(?i)^\s*(?:PUBLIC|EXTRN|EXTERNDEF|EXPORT|IMPORT)\b`),
	tokenizer.AsmPlan9: regexp.MustCompile(`^\s*GLOBL\b`),
	// HLASM statements are normalized to "name op operands".
	tokenizer.AsmHLASM: regexp.MustCompile(`(?i)^\S*[ \t]+(?:EXTRN|WXTRN|ENTRY|ALIAS)\b`),
}

func asmDeclarations(code string, lang tokenizer.Language) map[int]map[string]bool {
	re := asmDeclRe[lang]
	decls := map[int]map[string]bool{}
	for i, line := range strings.Split(tokenizer.StripComments(code, lang), "\n") {
		if !re.MatchString(line) {
			continue
		}
		words := map[string]bool{}
		for _, w := range tokenizer.ReferenceWords(line, lang) {
			words[w] = true
		}
		decls[i+1] = words
	}
	return decls
}

var (
	asmPastedNameRe = regexp.MustCompile(`^\s*(?:cglobal|function)\s`)
	cStaticRe       = regexp.MustCompile(`\bstatic\b`)
	cCtorDtorRe     = regexp.MustCompile(`__attribute__\s*\(\(\s*(?:constructor|destructor)\b`)
)

// pyFixtureRe matches a pytest fixture decorator: @pytest.fixture,
// @pytest_asyncio.fixture, or a bare imported @fixture, with or without
// arguments.
var pyFixtureRe = regexp.MustCompile(`^@(?:pytest\.|pytest_asyncio\.)?fixture\b`)

// pyFixtureDecorated reports whether a Python definition's own
// decorators — the lines above its def/class line — include a pytest
// fixture decorator. Decorators on methods inside a class chunk do not
// count for the class.
func pyFixtureDecorated(code string) bool {
	for _, line := range strings.Split(code, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "@") {
			return false
		}
		if pyFixtureRe.MatchString(t) {
			return true
		}
	}
	return false
}

// cSpecifiers returns the part of a C definition in front of its name:
// storage class, attributes, and return type. It falls back to the
// first line when the name cannot be found.
func cSpecifiers(code, sym string) string {
	for from := 0; ; {
		k := strings.Index(code[from:], sym)
		if k < 0 {
			return firstLine(code)
		}
		k += from
		end := k + len(sym)
		before := k == 0 || !isIdentByte(code[k-1])
		rest := strings.TrimLeft(code[end:], " \t\r\n")
		if before && (end == len(code) || !isIdentByte(code[end])) && strings.HasPrefix(rest, "(") {
			return code[:k]
		}
		from = k + 1
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
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
	case tokenizer.C:
		// Only `static` gives a C function internal linkage — except in
		// a header, where a static inline helper is part of what every
		// includer (possibly outside the scan) can call.
		if strings.EqualFold(filepath.Ext(s.Path), ".h") {
			return true
		}
		return !cStaticRe.MatchString(cSpecifiers(s.Code, sym))
	}
	// Assembly (and any unknown language): exported symbols may be
	// called from outside the scan, so findings stay advisory.
	return true
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
		// pytest and unittest collect tests by name, call pytest_* hooks,
		// and inject fixtures — none of them has a call site. Collection
		// only happens in test files; hooks and fixtures also live in a
		// root conftest.py, which is not one.
		if s.IsTest && (strings.HasPrefix(sym, "test") || strings.HasPrefix(sym, "Test")) {
			return true
		}
		if strings.HasPrefix(sym, "pytest_") || pyFixtureDecorated(s.Code) {
			return true
		}
	case tokenizer.C:
		// __attribute__((constructor/destructor)) functions run around
		// main without ever being called by name.
		if cCtorDtorRe.MatchString(cSpecifiers(s.Code, sym)) {
			return true
		}
		// Unit-test harnesses reach test functions through a dispatcher
		// that is often generated (curl's tests/unit) or outside the scan.
		if s.IsTest && strings.HasPrefix(sym, "test") {
			return true
		}
	}
	if tokenizer.IsAssembly(s.Lang) {
		// x86inc's cglobal and dav1d's `function` macro paste a prefix
		// and CPU suffix onto the name (dav1d_put_bilin_8bpc_ssse3), so
		// the literal name never appears at a call site; names built
		// from macro parameters (%1, \w) are not names at all.
		if asmPastedNameRe.MatchString(firstLine(s.Code)) || strings.ContainsAny(sym, "%\\") {
			return true
		}
		switch sym {
		case "_start", "start", "main", "_main", "DllMain", "WinMain":
			return true
		}
	}
	return false
}
