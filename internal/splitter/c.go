package splitter

import (
	"fmt"
	"strings"
	"unicode"
)

// ── C ─────────────────────────────────────────────────────────────────────────
//
// C definitions cannot be found line by line the way the other brace
// languages are: GNU style puts the return type on its own line, the
// parameter list often wraps, the body brace usually sits alone, K&R
// definitions declare their parameters between `)` and `{`, and
// preprocessor conditionals routinely give one function two headers or
// two different opening lines. splitC therefore works in two steps:
//
//  1. cMask blanks comments, string and char literals, and preprocessor
//     directive lines (with their backslash continuations) to spaces,
//     keeping every newline, so no brace or parenthesis inside them is
//     ever counted.
//  2. A single linear pass over the masked text tracks brace depth and,
//     at file scope, the tokens of the current declaration. Each
//     top-level `{` is classified by the declaration in front of it: a
//     function body (emitted as a chunk when its brace closes), an
//     `extern "C"` wrapper (transparent), or any other block — struct,
//     union, enum, initializer — which is skipped.
//
// Conditional compilation is followed the way a reader would: the
// parser state is saved at `#if`, restored at every `#elif`/`#else`,
// and at `#endif` the state reached by the first processed branch
// wins. Whole-function platform variants therefore become separate
// chunks, while a brace opened differently in each branch (`if (a) {`
// vs `if (b) {`) is counted once. `#if 0` branches are skipped.

// cDirective is one preprocessor directive, recorded on its first line.
type cDirective struct {
	name string // "if", "ifdef", "elif", "else", "endif", ...
	zero bool   // `#if 0` / `#elif 0`: the branch is never compiled
}

// cMask returns code with comments, literals, and directive lines
// replaced by spaces (newlines kept), plus the directives by line.
func cMask(code string) ([]byte, map[int]cDirective) {
	b := []byte(code)
	n := len(b)
	blank := func(i int) {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
	for i := 0; i < n; {
		switch c := b[i]; {
		case c == '/' && i+1 < n && b[i+1] == '/':
			for i < n && b[i] != '\n' {
				if b[i] == '\\' && i+1 < n && b[i+1] == '\n' {
					blank(i)
					i += 2 // a continued line comment goes on
					continue
				}
				blank(i)
				i++
			}
		case c == '/' && i+1 < n && b[i+1] == '*':
			blank(i)
			blank(i + 1)
			i += 2
			for i < n && !(b[i] == '*' && i+1 < n && b[i+1] == '/') {
				blank(i)
				i++
			}
			if i < n {
				blank(i)
				blank(i + 1)
				i += 2
			}
		case c == '"' || c == '\'':
			blank(i)
			i++
			for i < n && b[i] != c && b[i] != '\n' {
				if b[i] == '\\' && i+1 < n {
					blank(i)
					blank(i + 1)
					i += 2
					continue
				}
				blank(i)
				i++
			}
			if i < n && b[i] == c {
				blank(i)
				i++
			}
		default:
			i++
		}
	}

	directives := map[int]cDirective{}
	line := 0
	for start := 0; start < n; line++ {
		end := start
		for end < n && b[end] != '\n' {
			end++
		}
		text := strings.TrimSpace(string(b[start:end]))
		if strings.HasPrefix(text, "#") {
			fields := strings.Fields(strings.TrimPrefix(text, "#"))
			if len(fields) > 0 {
				d := cDirective{name: fields[0]}
				if (d.name == "if" || d.name == "elif") && len(fields) == 2 &&
					(fields[1] == "0" || fields[1] == "(0)") {
					d.zero = true
				}
				directives[line] = d
			}
			// Blank the directive and every backslash-continued line.
			for {
				for k := start; k < end; k++ {
					b[k] = ' '
				}
				// The masked line is blank now; the continuation
				// backslash is read from the original source.
				if !strings.HasSuffix(strings.TrimRight(code[start:end], " \t\r"), "\\") {
					break
				}
				if end >= n {
					break
				}
				start = end + 1
				line++
				end = start
				for end < n && b[end] != '\n' {
					end++
				}
			}
		}
		start = end + 1
	}
	return b, directives
}

// cTok is one file-scope token of the declaration being read.
type cTok struct {
	text string
	line int // 0-based
	off  int // byte offset in the source
}

func (t cTok) isIdent() bool {
	r := rune(t.text[0])
	return r == '_' || unicode.IsLetter(r)
}

// cState is everything a preprocessor branch can change.
type cState struct {
	depth       int
	toks        []cTok // current file-scope declaration
	open        bool   // inside a function body
	openStart   int    // 0-based first line of the open function
	openSymbol  string
	transparent int // open `extern "C" {` wrappers
}

func (s cState) clone() cState {
	s.toks = append([]cTok(nil), s.toks...)
	return s
}

// cCond is one #if…#endif frame.
type cCond struct {
	saved     cState
	firstEnd  *cState // state at the end of the first processed branch
	skipping  bool    // current branch is `#if 0`-style dead code
	processed bool    // some branch of this frame has been processed
	inert     bool    // opened inside a skipped branch
}

func splitC(code string) []Chunk {
	chunks, _ := cParse(code)
	return chunks
}

// CDeclarations returns, by 1-based line, the names of functions that
// C source declares without defining — file-scope prototypes such as
// `int foo(int);` in a header or a forward declaration. A declaration
// mentions a name without using it, so dead-code analysis must not
// count it as a reference.
func CDeclarations(code string) map[int]map[string]bool {
	_, decls := cParse(code)
	return decls
}

// cParse is the single pass behind splitC and CDeclarations.
func cParse(code string) ([]Chunk, map[int]map[string]bool) {
	masked, directives := cMask(code)
	lines := strings.Split(code, "\n")

	var (
		st     cState
		conds  []cCond
		chunks []Chunk
		byLine = map[int]int{} // start line → index in chunks
		decls  = map[int]map[string]bool{}
	)
	skipping := func() bool {
		for i := range conds {
			if conds[i].skipping {
				return true
			}
		}
		return false
	}
	skip := false // skipping(), refreshed after every directive
	applyDirective := func(d cDirective) {
		defer func() { skip = skipping() }()
		switch d.name {
		case "if", "ifdef", "ifndef":
			if skipping() {
				conds = append(conds, cCond{inert: true})
				return
			}
			conds = append(conds, cCond{saved: st.clone(), skipping: d.zero, processed: !d.zero})
		case "elif", "else", "elifdef", "elifndef":
			if len(conds) == 0 {
				return
			}
			top := &conds[len(conds)-1]
			if top.inert {
				return
			}
			if top.processed && top.firstEnd == nil {
				first := st.clone()
				top.firstEnd = &first
			}
			st = top.saved.clone()
			top.skipping = d.zero
			if !d.zero {
				top.processed = true
			}
		case "endif":
			if len(conds) == 0 {
				return
			}
			top := conds[len(conds)-1]
			conds = conds[:len(conds)-1]
			if !top.inert && top.firstEnd != nil {
				st = top.firstEnd.clone()
			}
		}
	}
	emit := func(end int) {
		if prev, ok := byLine[st.openStart]; ok {
			// The same function closed again in another #if branch:
			// keep the widest span.
			if end > chunks[prev].EndLine-1 {
				chunks[prev] = chunkSpan(lines, st.openStart, end, st.openSymbol, "")
			}
			return
		}
		byLine[st.openStart] = len(chunks)
		chunks = append(chunks, chunkSpan(lines, st.openStart, end, st.openSymbol, ""))
	}

	line := 0
	if d, ok := directives[0]; ok {
		applyDirective(d)
	}
	for i := 0; i < len(masked); i++ {
		c := masked[i]
		if c == '\n' {
			line++
			if d, ok := directives[line]; ok {
				applyDirective(d)
			}
			continue
		}
		if skip || c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v' {
			continue
		}
		if st.depth > 0 {
			switch c {
			case '{':
				st.depth++
			case '}':
				st.depth--
				if st.depth == 0 {
					if st.open {
						emit(line)
						st.open = false
						st.toks = nil
					} else {
						st.toks = append(st.toks, cTok{"{}", line, i})
					}
				}
			}
			continue
		}
		switch {
		case c == '{':
			if len(st.toks) == 1 && st.toks[0].text == "extern" {
				st.transparent++ // extern "C" { … }: its contents stay file-scope
				st.toks = nil
				continue
			}
			if symbol, start, ok := cFunctionHeader(st.toks); ok {
				st.open, st.openStart, st.openSymbol = true, start, symbol
			}
			st.depth = 1
		case c == '}':
			if st.transparent > 0 {
				st.transparent--
			}
			st.toks = nil
		case c == ';':
			if cKnRContinues(st.toks) {
				st.toks = append(st.toks, cTok{";", line, i})
			} else {
				for _, t := range cDeclaredFunctions(st.toks) {
					if decls[t.line+1] == nil {
						decls[t.line+1] = map[string]bool{}
					}
					decls[t.line+1][t.text] = true
				}
				st.toks = nil
			}
		case c == '_' || c < 0x80 && unicode.IsLetter(rune(c)) || c >= 0x80:
			j := i
			for j < len(masked) && (masked[j] == '_' || masked[j] >= 0x80 ||
				masked[j] < 0x80 && (unicode.IsLetter(rune(masked[j])) || unicode.IsDigit(rune(masked[j])))) {
				j++
			}
			st.toks = append(st.toks, cTok{string(masked[i:j]), line, i})
			i = j - 1
		case c >= '0' && c <= '9':
			j := i
			for j < len(masked) && (masked[j] == '.' || masked[j] == '_' ||
				unicode.IsLetter(rune(masked[j])) || unicode.IsDigit(rune(masked[j]))) {
				j++
			}
			st.toks = append(st.toks, cTok{"0", line, i})
			i = j - 1
		default:
			st.toks = append(st.toks, cTok{string(c), line, i})
		}
	}
	return chunks, decls
}

// cDeclaredFunctions returns the name tokens of the functions a
// semicolon-terminated file-scope declaration declares: identifiers
// directly followed by "(" outside any parentheses. A declaration with
// an initializer ("=") declares variables, whose initializer may call
// or take the address of functions, so it declares nothing here.
func cDeclaredFunctions(toks []cTok) []cTok {
	toks = cAfterLastBlock(toks)
	paren := 0
	for _, t := range toks {
		switch t.text {
		case "(":
			paren++
		case ")":
			paren--
		case "=":
			if paren == 0 {
				return nil
			}
		}
	}
	var names []cTok
	paren = 0
	for k, t := range toks {
		switch t.text {
		case "(":
			paren++
		case ")":
			paren--
		default:
			if paren == 0 && t.isIdent() && k+1 < len(toks) && toks[k+1].text == "(" &&
				!cReservedNames[t.text] && !cAttributeNames[t.text] {
				names = append(names, t)
			}
		}
	}
	return names
}

// cAfterLastBlock drops everything up to the last closed brace block
// ("{}"). A function header never contains a braced body, so tokens in
// front of one belong to an earlier declaration that was never
// terminated — a struct definition without its ";", or a construct the
// parser did not recognize — and must not affect the next one.
func cAfterLastBlock(toks []cTok) []cTok {
	for k := len(toks) - 1; k >= 0; k-- {
		if toks[k].text == "{}" {
			return toks[k+1:]
		}
	}
	return toks
}

// cMatchOpen returns the index of the "(" matching the ")" at close.
func cMatchOpen(toks []cTok, close int) int {
	depth := 0
	for k := close; k >= 0; k-- {
		switch toks[k].text {
		case ")":
			depth++
		case "(":
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return -1
}

// cIdentList reports whether toks is a non-empty `a, b, c` list: the
// parameter list of a K&R definition.
func cIdentList(toks []cTok) bool {
	if len(toks) == 0 {
		return false
	}
	for k, t := range toks {
		if k%2 == 0 && !t.isIdent() || k%2 == 1 && t.text != "," {
			return false
		}
	}
	return len(toks)%2 == 1
}

// cKnRDecls reports whether toks are K&R parameter declarations
// (`int a; char *b;`) — possibly still missing the final `;`.
func cKnRDecls(toks []cTok) bool {
	if len(toks) == 0 || !toks[0].isIdent() {
		return false
	}
	for _, t := range toks {
		switch t.text {
		case "*", ",", "[", "]", ";", "0":
		default:
			if !t.isIdent() {
				return false
			}
		}
	}
	return true
}

// cKnRContinues reports whether a `;` at file scope ends a K&R
// parameter declaration rather than the declaration itself.
func cKnRContinues(toks []cTok) bool {
	r := -1
	for k := len(toks) - 1; k >= 0; k-- {
		if toks[k].text == ")" {
			r = k
			break
		}
	}
	if r < 0 || r == len(toks)-1 || !cKnRDecls(toks[r+1:]) {
		return false
	}
	l := cMatchOpen(toks, r)
	return l > 0 && toks[l-1].isIdent() && cIdentList(toks[l+1:r])
}

var (
	// cReservedNames can precede "(" without being a function name.
	cReservedNames = nameSet(
		"if", "else", "while", "for", "do", "switch", "case", "return",
		"sizeof", "goto", "break", "continue", "default", "typedef",
		"struct", "union", "enum", "static_assert", "_Static_assert",
		"__attribute__", "__attribute", "__declspec", "typeof", "__typeof__",
		"typeof_unqual", "alignof", "_Alignof", "alignas", "_Alignas",
		"_Generic", "__asm__", "__asm", "asm", "defined", "__extension__",
	)
	// cAttributeNames introduce a parenthesized group that belongs to
	// the declaration specifiers (`__attribute__((noreturn)) void f()`).
	cAttributeNames = nameSet(
		"__attribute__", "__attribute", "__declspec", "_Alignas", "alignas",
		"__asm__", "__asm", "asm",
	)
	// cTypeWords mark a parameter list as real C rather than the
	// argument list of a definition-generating macro like TEST(a, b).
	cTypeWords = nameSet(
		"void", "char", "short", "int", "long", "float", "double",
		"signed", "unsigned", "const", "volatile", "struct", "union",
		"enum", "_Bool", "bool", "restrict", "register",
	)
)

func nameSet(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// cHeader is a parsed function-definition header.
type cHeader struct {
	name   cTok   // the function name
	start  cTok   // first token of the declaration
	params []cTok // tokens inside the parameter list
	knr    bool   // K&R parameter declarations follow the list
}

// cParseHeader decides whether the file-scope declaration in toks,
// which is followed by "{", is a function definition.
func cParseHeader(toks []cTok) (cHeader, bool) {
	toks = cAfterLastBlock(toks)
	if len(toks) == 0 {
		return cHeader{}, false
	}
	// An "=" outside parentheses makes this an initializer.
	paren := 0
	for _, t := range toks {
		switch t.text {
		case "(":
			paren++
		case ")":
			paren--
		case "=":
			if paren == 0 {
				return cHeader{}, false
			}
		}
	}

	// The parameter list closes at the last ")" — directly before the
	// body, or before K&R parameter declarations.
	r := len(toks) - 1
	knr := false
	if toks[r].text != ")" {
		for r = len(toks) - 1; r >= 0 && toks[r].text != ")"; r-- {
		}
		if r < 0 || !cKnRDecls(toks[r+1:]) || toks[len(toks)-1].text != ";" {
			return cHeader{}, false
		}
		knr = true
	}
	l := cMatchOpen(toks, r)
	if l <= 0 {
		return cHeader{}, false
	}
	params := toks[l+1 : r]
	if knr && !cIdentList(params) {
		return cHeader{}, false
	}

	nameIdx := l - 1
	switch {
	case toks[nameIdx].isIdent():
	case toks[nameIdx].text == ")" && nameIdx >= 2 && toks[nameIdx-2].text == "(" && toks[nameIdx-1].isIdent():
		// `double (cimag)(double complex z)`: a parenthesized name keeps
		// a same-named function-like macro from expanding.
		nameIdx--
	case toks[nameIdx].text == ")":
		// A function returning a function pointer:
		// `void (*name(int sig))(int)` — the name sits after "( *".
		nameIdx = -1
		for k := 0; k+3 < l; k++ {
			if toks[k].text == "(" && toks[k+1].text == "*" && toks[k+2].isIdent() && toks[k+3].text == "(" {
				nameIdx = k + 2
				break
			}
		}
		if nameIdx < 0 {
			return cHeader{}, false
		}
	default:
		return cHeader{}, false
	}
	if cReservedNames[toks[nameIdx].text] {
		return cHeader{}, false
	}

	// Walk back over the declaration specifiers — words, pointer stars,
	// the "(" of a function-pointer declarator, and attribute groups —
	// to find where the declaration starts. Anything else (a
	// semicolon-less macro invocation on an earlier line, a struct body)
	// belongs to what came before.
	start := nameIdx
	for k := nameIdx - 1; k >= 0; k-- {
		t := toks[k]
		if t.isIdent() || t.text == "*" || t.text == "(" {
			start = k
			continue
		}
		if t.text == ")" {
			if m := cMatchOpen(toks, k); m > 0 && cAttributeNames[toks[m-1].text] {
				start = m - 1
				k = m - 1
				continue
			}
		}
		break
	}
	return cHeader{name: toks[nameIdx], start: toks[start], params: params, knr: knr}, true
}

// cFunctionHeader returns the symbol and 0-based first line of the
// function definition whose header is toks, if it is one.
func cFunctionHeader(toks []cTok) (string, int, bool) {
	h, ok := cParseHeader(toks)
	if !ok {
		return "", 0, false
	}
	symbol := h.name.text
	if !h.knr && cMacroName(symbol) && !cHasTypeWords(h.params) {
		// TEST(suite, name) { … } — a definition generated by a macro.
		// The macro name is shared by every such definition and never
		// called directly, so it gets a synthetic, line-qualified symbol
		// that dead-code analysis skips.
		symbol = fmt.Sprintf("%s@L%d", symbol, h.name.line+1)
	}
	return symbol, h.start.line, true
}

// CDefinitionHeader parses the source of one C function definition (a
// chunk's code). It returns the function's name, the byte ranges of
// every occurrence of that name in the header — a header split across
// #ifdef variants names it more than once — and the byte offset of the
// body's opening brace. Comments and literals are never matched.
func CDefinitionHeader(code string) (string, [][2]int, int, bool) {
	masked, _ := cMask(code)
	var toks []cTok
	line, paren := 0, 0
	for i := 0; i < len(masked); i++ {
		c := masked[i]
		switch {
		case c == '\n':
			line++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
		case c == '{' && paren == 0:
			h, ok := cParseHeader(toks)
			if !ok {
				return "", nil, 0, false
			}
			var occ [][2]int
			for _, t := range toks {
				if t.text == h.name.text {
					occ = append(occ, [2]int{t.off, t.off + len(t.text)})
				}
			}
			return h.name.text, occ, i, true
		case c == '_' || c >= 0x80 || unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c)):
			j := i
			for j < len(masked) && (masked[j] == '_' || masked[j] >= 0x80 ||
				unicode.IsLetter(rune(masked[j])) || unicode.IsDigit(rune(masked[j]))) {
				j++
			}
			text := string(masked[i:j])
			if unicode.IsDigit(rune(c)) {
				text = "0"
			}
			toks = append(toks, cTok{text, line, i})
			i = j - 1
		default:
			if c == '(' {
				paren++
			} else if c == ')' {
				paren--
			}
			toks = append(toks, cTok{string(c), line, i})
		}
	}
	return "", nil, 0, false
}

// cMacroName reports whether name is spelled like a macro: upper-case
// letters, digits, and underscores, with at least one letter.
func cMacroName(name string) bool {
	letter := false
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			letter = true
		case r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return letter
}

func cHasTypeWords(toks []cTok) bool {
	for _, t := range toks {
		if t.text == "*" || cTypeWords[t.text] {
			return true
		}
	}
	return false
}
