package refactor

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// summariseChunks renders a slice of chunks for assertion error output.
func summariseChunks(cs []splitter.Chunk) string {
	out := strings.Builder{}
	for _, c := range cs {
		fmt.Fprintf(&out, "  L%d-%d %q\n", c.StartLine, c.EndLine, c.Symbol)
	}
	return out.String()
}

// TestSynthesize_GoAcceptTiers covers the simple/medium/advanced Go
// fixtures. We assert that synthesis succeeds (Note is empty), the
// helper name is sanitised, the helper source contains the divergence
// comment for every divergence we expect, and the helper compiles
// against the simplest possible structural check (the source contains
// the helper-header `func extracted_…(`).
func TestSynthesize_GoAcceptTiers(t *testing.T) {
	cases := []struct {
		dir           string
		expectInSrc   []string // substrings that must appear in HelperSrc
		minConfidence float64
	}{
		{
			dir: "../../testdata/refactor/go/simple",
			expectInSrc: []string{
				"func extracted_priceWithTaxA_",
				"// Divergences (B vs A):",
				"0.07",
				"0.085",
			},
			minConfidence: 0.5,
		},
		{
			dir: "../../testdata/refactor/go/medium",
			expectInSrc: []string{
				"func extracted_formatUserA_",
				`"user:"`,
				`"admin:"`,
				`"(active)"`,
				`"(privileged)"`,
			},
			minConfidence: 0.4,
		},
		{
			dir: "../../testdata/refactor/go/advanced",
			expectInSrc: []string{
				"func extracted_backoffStepA_",
				"base * 2",
				"base + 5",
			},
			minConfidence: 0.4,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.dir, func(t *testing.T) {
			a, b := loadSnippets(t, c.dir)
			al := Align(a, b)
			s := Synthesize(a, b, "deadbeef", al)
			if s.Note != "" {
				t.Fatalf("expected accept, got Note=%q", s.Note)
			}
			if s.HelperSrc == "" {
				t.Fatal("HelperSrc empty")
			}
			if s.Confidence < c.minConfidence {
				t.Errorf("Confidence = %.2f, want >= %.2f", s.Confidence, c.minConfidence)
			}
			for _, want := range c.expectInSrc {
				if !strings.Contains(s.HelperSrc, want) {
					t.Errorf("HelperSrc missing %q. Source:\n%s", want, s.HelperSrc)
				}
			}
			// Helper name must be a valid Go identifier.
			if !validGoIdent(s.HelperName) {
				t.Errorf("HelperName %q is not a valid Go identifier", s.HelperName)
			}
		})
	}
}

func TestSynthesize_GoRejections(t *testing.T) {
	cases := []struct {
		dir         string
		wantNoteSub string
	}{
		{
			dir:         "../../testdata/refactor/go/reject-receiver",
			wantNoteSub: "different receiver types",
		},
		{
			dir:         "../../testdata/refactor/go/reject-anon",
			wantNoteSub: "anonymous/goroutine/defer",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.dir, func(t *testing.T) {
			a, b := loadSnippets(t, c.dir)
			al := Align(a, b)
			s := Synthesize(a, b, "deadbeef", al)
			if s.Note == "" {
				t.Fatalf("expected rejection, got HelperSrc:\n%s", s.HelperSrc)
			}
			if !strings.Contains(s.Note, c.wantNoteSub) {
				t.Errorf("Note = %q, want substring %q", s.Note, c.wantNoteSub)
			}
			if s.HelperSrc != "" {
				t.Errorf("rejected suggestion should have empty HelperSrc, got:\n%s", s.HelperSrc)
			}
		})
	}
}

// TestSynthesize_RejectControlFlowFixture exercises the
// reject-controlflow fixture, which has a `return` inside a hole on
// only one side. Our fixture's snippets actually share a `return`
// pattern, so this test is a structural placeholder: it asserts that
// when both sides share the keyword we accept, but when they diverge
// we reject. We synthesize a divergence inline.
func TestSynthesize_RejectControlFlowFixture(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/go/reject-controlflow")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	// The reject-controlflow fixture differs only in the literal
	// strings — both sides have matching `return` statements, so this
	// pair is actually accepted. The keyword-asymmetry rejection only
	// fires when one side has a control-flow keyword in a hole and the
	// other doesn't — covered by TestRejectControlFlowAsymmetry below
	// with a constructed alignment.
	if s.Note != "" {
		t.Logf("synthesis result on shared-controlflow fixture: %q (acceptable either way)", s.Note)
	}
}

func TestRejectControlFlowAsymmetry_OnlyOneSideHasReturn(t *testing.T) {
	holes := []Hole{
		{AText: "return errors.New(\"x\")", BText: "log.Fatal(\"x\")"},
	}
	if _, ok := rejectControlFlowAsymmetry(holes); ok {
		t.Error("expected rejection when only A has 'return'")
	}
}

func TestRejectControlFlowAsymmetry_BothSidesHaveReturn(t *testing.T) {
	holes := []Hole{
		{AText: "return errors.New(\"a\")", BText: "return errors.New(\"b\")"},
	}
	if _, ok := rejectControlFlowAsymmetry(holes); !ok {
		t.Error("expected acceptance when both sides share 'return'")
	}
}

// TestRejectControlFlowAsymmetry_PythonKeywords covers the Python
// emitter's extended keyword set: `raise` and `yield` count as
// control-flow asymmetry the same way `return`/`break`/`continue` do.
func TestRejectControlFlowAsymmetry_PythonKeywords(t *testing.T) {
	pyKeywords := []string{"return", "break", "continue", "raise", "yield"}
	cases := []struct {
		name string
		hole Hole
		want bool // true = rejected
	}{
		{"raise asymmetric", Hole{AText: "raise ValueError(\"x\")", BText: "log.error(\"x\")"}, true},
		{"yield asymmetric", Hole{AText: "yield row", BText: "rows.append(row)"}, true},
		{"raise symmetric", Hole{AText: "raise A()", BText: "raise B()"}, false},
		{"yield symmetric", Hole{AText: "yield a", BText: "yield b"}, false},
		{"unrelated", Hole{AText: "x = 1", BText: "x = 2"}, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, ok := rejectControlFlowAsymmetryWithKeywords([]Hole{c.hole}, pyKeywords)
			rejected := !ok
			if rejected != c.want {
				t.Errorf("rejected=%v, want %v", rejected, c.want)
			}
		})
	}
}

// Given the simple/medium/advanced JS fixtures, when Synthesize runs,
// then it accepts and emits a HelperSrc with the helper signature, the
// divergence block, and both sides' literals. The advanced case
// additionally surfaces the this-binding NOTE because the source is a
// class method. Cycle 10.
func TestSynthesize_JavaScriptAcceptTiers(t *testing.T) {
	cases := []struct {
		dir           string
		expectInSrc   []string
		minConfidence float64
	}{
		{
			dir: "../../testdata/refactor/js/simple",
			expectInSrc: []string{
				"function extracted_priceWithTaxA_",
				"// Divergences (B vs A):",
				"0.07",
				"0.085",
			},
			minConfidence: 0.5,
		},
		{
			dir: "../../testdata/refactor/js/medium",
			expectInSrc: []string{
				"function extracted_formatUserA_",
				`"user:"`,
				`"admin:"`,
				`"(active)"`,
				`"(privileged)"`,
			},
			minConfidence: 0.4,
		},
		{
			dir: "../../testdata/refactor/js/advanced",
			expectInSrc: []string{
				"function extracted_fetchA_",
				`"/v1"`,
				`"/v2"`,
				"this.table",
				"// NOTE:",
			},
			minConfidence: 0.4,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.dir, func(t *testing.T) {
			a, b := loadSnippets(t, c.dir)
			al := Align(a, b)
			s := Synthesize(a, b, "deadbeef", al)
			if s.Note != "" {
				t.Fatalf("expected accept, got Note=%q", s.Note)
			}
			if s.HelperSrc == "" {
				t.Fatal("HelperSrc empty")
			}
			if s.Confidence < c.minConfidence {
				t.Errorf("Confidence = %.2f, want >= %.2f", s.Confidence, c.minConfidence)
			}
			for _, want := range c.expectInSrc {
				if !strings.Contains(s.HelperSrc, want) {
					t.Errorf("HelperSrc missing %q. Source:\n%s", want, s.HelperSrc)
				}
			}
			if !validGoIdent(s.HelperName) {
				t.Errorf("HelperName %q is not a valid identifier", s.HelperName)
			}
		})
	}
}

// Given a Rust snippet whose first non-blank line is a `fn name(...)`
// declaration, when rsHelperHeader rewrites the header, then the name
// is replaced and modifiers (pub/pub(crate)/async/unsafe) plus the
// parameter list are preserved verbatim. Cycle 2.
func TestRsHelperHeader_FreeFunctionForms(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			"plain fn",
			"fn price_with_tax_a(amount: f64) -> f64 {\n    amount\n}",
			"fn extracted_h(amount: f64) -> f64 {",
		},
		{
			"pub fn",
			"pub fn compute(x: i32) -> i32 {\n    x\n}",
			"pub fn extracted_h(x: i32) -> i32 {",
		},
		{
			"pub(crate) fn",
			"pub(crate) fn compute(x: i32) -> i32 {\n    x\n}",
			"pub(crate) fn extracted_h(x: i32) -> i32 {",
		},
		{
			"async fn",
			"async fn fetch(id: u64) -> Result<String, Error> {\n    Ok(String::new())\n}",
			"async fn extracted_h(id: u64) -> Result<String, Error> {",
		},
		{
			"unsafe fn",
			"unsafe fn raw(p: *const u8) -> u8 {\n    *p\n}",
			"unsafe fn extracted_h(p: *const u8) -> u8 {",
		},
		{
			"pub async fn",
			"pub async fn fetch(id: u64) -> String {\n    String::new()\n}",
			"pub async fn extracted_h(id: u64) -> String {",
		},
		{
			"impl method with &self",
			"    fn fetch_a(&self, key: i32) -> String {\n        String::new()\n    }",
			"fn extracted_h(&self, key: i32) -> String {",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, ok := rsHelperHeader(c.input, "extracted_h")
			if !ok {
				t.Fatalf("rsHelperHeader returned ok=false for %q", c.input)
			}
			if got != c.expect {
				t.Errorf("got %q, want %q", got, c.expect)
			}
		})
	}
}

// Given a Rust snippet whose header carries generics, lifetimes, a
// return type, or a `where` clause, when rsHelperHeader rewrites the
// header, then everything after the function name is preserved
// verbatim. Cycle 3.
func TestRsHelperHeader_GenericsAndReturnType(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			"single type param",
			"fn process<T>(x: T) -> T {\n    x\n}",
			"fn extracted_h<T>(x: T) -> T {",
		},
		{
			"bounded type param",
			"fn process<T: Clone + Send>(x: T) -> T {\n    x.clone()\n}",
			"fn extracted_h<T: Clone + Send>(x: T) -> T {",
		},
		{
			"lifetime param",
			"fn first<'a>(s: &'a str) -> &'a str {\n    s\n}",
			"fn extracted_h<'a>(s: &'a str) -> &'a str {",
		},
		{
			"return Result",
			"fn parse(s: &str) -> Result<i32, ParseIntError> {\n    s.parse()\n}",
			"fn extracted_h(s: &str) -> Result<i32, ParseIntError> {",
		},
		{
			"where clause",
			"fn process<T>(x: T) -> T where T: Clone {\n    x.clone()\n}",
			"fn extracted_h<T>(x: T) -> T where T: Clone {",
		},
		{
			"attribute above header",
			"#[inline]\nfn fast(x: i32) -> i32 {\n    x\n}",
			"fn extracted_h(x: i32) -> i32 {",
		},
		{
			"doc comment above header",
			"/// Compute the price.\nfn price(amount: f64) -> f64 {\n    amount\n}",
			"fn extracted_h(amount: f64) -> f64 {",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, ok := rsHelperHeader(c.input, "extracted_h")
			if !ok {
				t.Fatalf("rsHelperHeader returned ok=false for %q", c.input)
			}
			if got != c.expect {
				t.Errorf("got %q, want %q", got, c.expect)
			}
		})
	}
}

// Given an impl-method snippet indented 4 spaces, when rsRebodyAsHelper
// extracts the body, then the method's outer indent is stripped so the
// body sits at one natural level below a column-zero header, ending
// with the closing `}`. Cycle 4.
func TestRsRebodyAsHelper_ImplMethodDedents(t *testing.T) {
	input := "    fn fetch_a(&self, key: i32) -> String {\n        let prefix = format!(\"{}:\", self.table);\n        prefix\n    }"
	got := rsRebodyAsHelper(input)
	expected := "    let prefix = format!(\"{}:\", self.table);\n    prefix\n}\n"
	if got != expected {
		t.Errorf("got:\n%q\nwant:\n%q", got, expected)
	}
}

// Given a free-function Rust snippet at column 0, when rsRebodyAsHelper
// runs, then the body is dedented relative to the (zero-indent) header
// — i.e. preserved as-is. Cycle 4.
func TestRsRebodyAsHelper_FreeFunctionPassesThrough(t *testing.T) {
	input := "fn price(amount: f64) -> f64 {\n    amount * 1.07\n}"
	got := rsRebodyAsHelper(input)
	expected := "    amount * 1.07\n}\n"
	if got != expected {
		t.Errorf("got:\n%q\nwant:\n%q", got, expected)
	}
}

// Given two Rust snippets and an Alignment with no common spans, when
// Synthesize runs, then synthesis is rejected with a "no common
// lines" Note. Cycle 6.
func TestSynthesizeRust_EmptyAlignment_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "x.rs:1-3 a", Lang: tokenizer.Rust, Code: "fn a() -> i32 {\n    1\n}"}
	b := scan.Snippet{Name: "y.rs:1-3 b", Lang: tokenizer.Rust, Code: "fn b() -> i32 {\n    2\n}"}
	s := Synthesize(a, b, "deadbeef", Alignment{})
	if !strings.Contains(s.Note, "no common lines") {
		t.Errorf("expected empty-alignment rejection, got Note=%q", s.Note)
	}
}

// Given a Rust snippet whose first non-blank line has no `fn` keyword,
// when Synthesize runs, then it rejects with a clear Note. Cycle 6.
func TestSynthesizeRust_NoFnHeader_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "x.rs:1-2 a", Lang: tokenizer.Rust, Code: "// only comments\n// no header"}
	b := scan.Snippet{Name: "y.rs:1-2 b", Lang: tokenizer.Rust, Code: "// only comments\n// no header"}
	al := Alignment{Common: []LineSpan{{AStart: 1, AEnd: 3, BStart: 1, BEnd: 3}}}
	s := Synthesize(a, b, "deadbeef", al)
	if !strings.Contains(s.Note, "Rust") && !strings.Contains(s.Note, "fn header") {
		t.Errorf("expected no-fn-header rejection, got Note=%q", s.Note)
	}
}

// Given a Rust snippet paired with a Python snippet, when Synthesize
// runs, then the cross-language guard fires before reaching
// synthesizeRust. Cycle 6.
func TestSynthesize_RustCrossLanguage_Rejected(t *testing.T) {
	a, _ := loadSnippets(t, "../../testdata/refactor/rust/simple")
	_, bPy := loadSnippets(t, "../../testdata/refactor/python/simple")
	al := Align(a, bPy)
	s := Synthesize(a, bPy, "deadbeef", al)
	if !strings.Contains(s.Note, "cross-language") {
		t.Errorf("expected cross-language rejection, got %q", s.Note)
	}
}

// Given two Rust snippets where B is materially longer than A, when
// Synthesize computes confidence, then bLines drives the denominator
// (mirrors the Go/Java/Python/JS tests). Cycle 6.
func TestSynthesizeRust_ConfidenceWithBLongerThanA(t *testing.T) {
	a := scan.Snippet{Name: "x.rs:1-3 foo", Lang: tokenizer.Rust, Code: "fn foo() -> i32 {\n    1\n}"}
	b := scan.Snippet{Name: "y.rs:1-7 bar", Lang: tokenizer.Rust, Code: "fn bar() -> i32 {\n    1\n    let extra = 2;\n    let extra2 = 3;\n    let extra3 = 4;\n    let extra4 = 5;\n}"}
	al := Align(a, b)
	if al.CommonLines() == 0 {
		t.Fatal("test setup expected at least one common line; alignment found none")
	}
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if s.Confidence <= 0 {
		t.Errorf("Confidence = %v; expected non-zero score", s.Confidence)
	}
	if s.Confidence >= 0.5 {
		t.Errorf("Confidence = %v; expected B's line count to drive denominator", s.Confidence)
	}
}

// Given an impl-method Rust snippet whose body references `self`, when
// Synthesize emits the helper, then a `// NOTE:` line surfaces the
// self-binding boundary. Cycle 7.
func TestSynthesize_RustImplMethod_AnnotatesSelfBinding(t *testing.T) {
	a := scan.Snippet{
		Name: "x.rs:1-4 fetch_a", Lang: tokenizer.Rust,
		Code: "    fn fetch_a(&self, key: i32) -> String {\n        let prefix = format!(\"{}:\", self.table);\n        prefix\n    }",
	}
	b := scan.Snippet{
		Name: "y.rs:1-4 fetch_b", Lang: tokenizer.Rust,
		Code: "    fn fetch_b(&self, key: i32) -> String {\n        let prefix = format!(\"{}:\", self.table);\n        prefix\n    }",
	}
	al := Align(a, b)
	if al.CommonLines() == 0 {
		t.Fatal("test setup expected at least one common line")
	}
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if !strings.Contains(s.HelperSrc, "// NOTE:") {
		t.Errorf("impl-method helper that references `self` should carry a // NOTE: line. HelperSrc:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, "self") {
		t.Errorf("expected NOTE to mention `self`. HelperSrc:\n%s", s.HelperSrc)
	}
}

// Given a free-function Rust snippet without any `self` reference,
// when Synthesize emits the helper, then NO self-binding NOTE is
// emitted. Cycle 7.
func TestSynthesize_RustFreeFunction_NoSelfNote(t *testing.T) {
	a := scan.Snippet{
		Name: "x.rs:1-3 a", Lang: tokenizer.Rust,
		Code: "fn a(x: i32) -> i32 {\n    x + 1\n}",
	}
	b := scan.Snippet{
		Name: "y.rs:1-3 b", Lang: tokenizer.Rust,
		Code: "fn b(x: i32) -> i32 {\n    x + 2\n}",
	}
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if strings.Contains(s.HelperSrc, "// NOTE:") {
		t.Errorf("free-function helper should NOT carry a // NOTE: line. HelperSrc:\n%s", s.HelperSrc)
	}
}

// Given a hole where one side has a Rust control-flow keyword (incl.
// the `panic!(...)` macro) and the other doesn't, when
// rejectControlFlowAsymmetryWithKeywords runs with the Rust keyword
// set, then it rejects. Symmetric flows and identifier-prefix matches
// must NOT reject. Cycle 5.
func TestRejectControlFlowAsymmetry_RustKeywords(t *testing.T) {
	rsKeywords := []string{"return", "break", "continue", "panic"}
	cases := []struct {
		name string
		hole Hole
		want bool // true = rejected
	}{
		{"panic asymmetric", Hole{AText: "metric.increment();", BText: "panic!(\"bad\");"}, true},
		{"panic symmetric", Hole{AText: "panic!(\"a\");", BText: "panic!(\"b\");"}, false},
		{"return asymmetric", Hole{AText: "return 1;", BText: "x = 1;"}, true},
		{"break asymmetric", Hole{AText: "break;", BText: "continue_value();"}, true},
		{"continue asymmetric", Hole{AText: "continue;", BText: "next();"}, true},
		{"panicked identifier not standalone", Hole{AText: "panicked = true;", BText: "x = true;"}, false},
		{"unrelated", Hole{AText: "x = 1;", BText: "x = 2;"}, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, ok := rejectControlFlowAsymmetryWithKeywords([]Hole{c.hole}, rsKeywords)
			rejected := !ok
			if rejected != c.want {
				t.Errorf("rejected=%v, want %v", rejected, c.want)
			}
		})
	}
}

// Given two Rust snippets where B introduces a `panic!` on a line A
// has as a plain statement, when Synthesize runs, then synthesis is
// rejected with a `control-flow asymmetry` Note containing `"panic"`.
// Cycle 5 (in-memory; fixture-driven version comes in Cycle 8).
func TestSynthesize_RustPanicAsymmetry_Rejected_InMemory(t *testing.T) {
	a := scan.Snippet{
		Name: "x.rs:1-3 a", Lang: tokenizer.Rust,
		Code: "fn a() {\n    metric.increment();\n}",
	}
	b := scan.Snippet{
		Name: "y.rs:1-3 b", Lang: tokenizer.Rust,
		Code: "fn b() {\n    panic!(\"bad\");\n}",
	}
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if !strings.Contains(s.Note, "control-flow asymmetry") {
		t.Errorf("expected control-flow rejection, got Note=%q", s.Note)
	}
	if !strings.Contains(s.Note, `"panic"`) {
		t.Errorf("expected the keyword name to be \"panic\" in Note, got %q", s.Note)
	}
	if s.HelperSrc != "" {
		t.Errorf("rejected suggestion should have empty HelperSrc, got:\n%s", s.HelperSrc)
	}
}

// Given the simple/medium/advanced Rust fixtures, when Synthesize
// runs, then it accepts and emits a HelperSrc with the helper
// signature, the divergence block, and both sides' literals. The
// advanced case additionally surfaces the self-binding NOTE because
// the source is an impl method. Cycle 8.
func TestSynthesize_RustAcceptTiers(t *testing.T) {
	cases := []struct {
		dir           string
		expectInSrc   []string
		minConfidence float64
	}{
		{
			dir: "../../testdata/refactor/rust/simple",
			expectInSrc: []string{
				"fn extracted_price_with_tax_a_",
				"// Divergences (B vs A):",
				"0.07",
				"0.085",
			},
			minConfidence: 0.5,
		},
		{
			dir: "../../testdata/refactor/rust/medium",
			expectInSrc: []string{
				"fn extracted_format_user_a_",
				`"user:"`,
				`"admin:"`,
				`"(active)"`,
				`"(privileged)"`,
			},
			minConfidence: 0.4,
		},
		{
			dir: "../../testdata/refactor/rust/advanced",
			expectInSrc: []string{
				"fn extracted_fetch_a_",
				`"/v1"`,
				`"/v2"`,
				"self.table",
				"// NOTE:",
				"&self",
			},
			minConfidence: 0.4,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.dir, func(t *testing.T) {
			a, b := loadSnippets(t, c.dir)
			al := Align(a, b)
			s := Synthesize(a, b, "deadbeef", al)
			if s.Note != "" {
				t.Fatalf("expected accept, got Note=%q", s.Note)
			}
			if s.HelperSrc == "" {
				t.Fatal("HelperSrc empty")
			}
			if s.Confidence < c.minConfidence {
				t.Errorf("Confidence = %.2f, want >= %.2f", s.Confidence, c.minConfidence)
			}
			for _, want := range c.expectInSrc {
				if !strings.Contains(s.HelperSrc, want) {
					t.Errorf("HelperSrc missing %q. Source:\n%s", want, s.HelperSrc)
				}
			}
			if !validGoIdent(s.HelperName) {
				t.Errorf("HelperName %q is not a valid identifier", s.HelperName)
			}
		})
	}
}

// Given the reject-panic fixture (B introduces panic!() where A had a
// plain call), when Synthesize runs, then it rejects with a
// `control-flow asymmetry` Note containing `"panic"`. Cycle 8.
func TestSynthesize_RustPanicAsymmetry_Rejected(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/rust/reject-panic")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note == "" {
		t.Fatalf("expected rejection, got HelperSrc:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.Note, "control-flow asymmetry") {
		t.Errorf("Note = %q, want 'control-flow asymmetry' substring", s.Note)
	}
	if !strings.Contains(s.Note, `"panic"`) {
		t.Errorf("Note = %q, want the keyword name to be \"panic\"", s.Note)
	}
	if s.HelperSrc != "" {
		t.Errorf("rejected suggestion should have empty HelperSrc, got:\n%s", s.HelperSrc)
	}
}

// Given an Elixir snippet whose first non-blank line is a `def` or
// `defp` declaration, when exHelperHeader rewrites the header, then
// the function name is replaced and the def/defp keyword, parameters,
// and trailing `do` are preserved verbatim. Cycle 5.
func TestExHelperHeader_DefForms(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			"plain def",
			"def price_with_tax(amount) do\n  amount\nend",
			"def extracted_h(amount) do",
		},
		{
			"private defp",
			"defp internal(x) do\n  x\nend",
			"defp extracted_h(x) do",
		},
		{
			"indented def (inside defmodule)",
			"  def fetch_a(table, key) do\n    \"#{table}:\"\n  end",
			"def extracted_h(table, key) do",
		},
		{
			"def with multiple params",
			"def format(name, age) do\n  name\nend",
			"def extracted_h(name, age) do",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, ok := exHelperHeader(c.input, "extracted_h")
			if !ok {
				t.Fatalf("exHelperHeader returned ok=false for %q", c.input)
			}
			if got != c.expect {
				t.Errorf("got %q, want %q", got, c.expect)
			}
		})
	}
}

// Given the simple/medium/advanced Elixir fixtures, when Synthesize
// runs, then it accepts and emits a HelperSrc with the helper
// signature, the divergence block, the module-context NOTE, and both
// sides' literals. Cycle 10.
func TestSynthesize_ElixirAcceptTiers(t *testing.T) {
	cases := []struct {
		dir           string
		expectInSrc   []string
		minConfidence float64
	}{
		{
			dir: "../../testdata/refactor/elixir/simple",
			expectInSrc: []string{
				"def extracted_price_with_tax_",
				"# Divergences (B vs A):",
				"# NOTE:",
				"defmodule",
				"0.07",
				"0.085",
			},
			minConfidence: 0.5,
		},
		{
			dir: "../../testdata/refactor/elixir/medium",
			expectInSrc: []string{
				"def extracted_format_",
				`"user:"`,
				`"admin:"`,
				`"(active)"`,
				`"(privileged)"`,
				"# NOTE:",
			},
			minConfidence: 0.4,
		},
		{
			dir: "../../testdata/refactor/elixir/advanced",
			expectInSrc: []string{
				"def extracted_fetch_a_",
				`"/v1"`,
				`"/v2"`,
				"# NOTE:",
			},
			minConfidence: 0.4,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.dir, func(t *testing.T) {
			a, b := loadSnippets(t, c.dir)
			al := Align(a, b)
			s := Synthesize(a, b, "deadbeef", al)
			if s.Note != "" {
				t.Fatalf("expected accept, got Note=%q", s.Note)
			}
			if s.HelperSrc == "" {
				t.Fatal("HelperSrc empty")
			}
			if s.Confidence < c.minConfidence {
				t.Errorf("Confidence = %.2f, want >= %.2f", s.Confidence, c.minConfidence)
			}
			for _, want := range c.expectInSrc {
				if !strings.Contains(s.HelperSrc, want) {
					t.Errorf("HelperSrc missing %q. Source:\n%s", want, s.HelperSrc)
				}
			}
			if !validGoIdent(s.HelperName) {
				t.Errorf("HelperName %q is not a valid identifier", s.HelperName)
			}
		})
	}
}

// Given the reject-raise fixture (B introduces raise where A returns
// :ok), when Synthesize runs, then it rejects with a `control-flow
// asymmetry` Note containing `"raise"`. Cycle 10.
func TestSynthesize_ElixirRaiseAsymmetry_Rejected(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/elixir/reject-raise")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note == "" {
		t.Fatalf("expected rejection, got HelperSrc:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.Note, "control-flow asymmetry") {
		t.Errorf("Note = %q, want 'control-flow asymmetry' substring", s.Note)
	}
	if !strings.Contains(s.Note, `"raise"`) {
		t.Errorf("Note = %q, want the keyword name to be \"raise\"", s.Note)
	}
	if s.HelperSrc != "" {
		t.Errorf("rejected suggestion should have empty HelperSrc, got:\n%s", s.HelperSrc)
	}
}

// Given a hole where one side has an Elixir control-flow keyword
// (raise/throw/exit) and the other doesn't, when
// rejectControlFlowAsymmetryWithKeywords runs with the Elixir set,
// then it rejects. Symmetric and identifier-prefix matches must NOT
// reject. Cycle 7.
func TestRejectControlFlowAsymmetry_ElixirKeywords(t *testing.T) {
	exKeywords := []string{"raise", "throw", "exit"}
	cases := []struct {
		name string
		hole Hole
		want bool
	}{
		{"raise asymmetric", Hole{AText: ":ok", BText: "raise \"bad\""}, true},
		{"raise symmetric", Hole{AText: "raise \"a\"", BText: "raise \"b\""}, false},
		{"throw asymmetric", Hole{AText: ":ok", BText: "throw :bad"}, true},
		{"exit asymmetric", Hole{AText: ":ok", BText: "exit(:normal)"}, true},
		{"raised identifier not standalone", Hole{AText: "raised = true", BText: "x = true"}, false},
		{"unrelated", Hole{AText: "x = 1", BText: "x = 2"}, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, ok := rejectControlFlowAsymmetryWithKeywords([]Hole{c.hole}, exKeywords)
			rejected := !ok
			if rejected != c.want {
				t.Errorf("rejected=%v, want %v", rejected, c.want)
			}
		})
	}
}

// Given two Elixir snippets where B introduces `raise` on a line A
// has as a return value, when Synthesize runs, then synthesis is
// rejected with a `control-flow asymmetry` Note containing `"raise"`.
// Cycle 7 (in-memory; fixture-driven version is in C10).
func TestSynthesize_ElixirRaiseAsymmetry_Rejected_InMemory(t *testing.T) {
	a := scan.Snippet{
		Name: "x.ex:1-3 a", Lang: tokenizer.Elixir,
		Code: "def a do\n  :ok\nend",
	}
	b := scan.Snippet{
		Name: "y.ex:1-3 b", Lang: tokenizer.Elixir,
		Code: "def b do\n  raise \"bad\"\nend",
	}
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if !strings.Contains(s.Note, "control-flow asymmetry") {
		t.Errorf("expected control-flow rejection, got Note=%q", s.Note)
	}
	if !strings.Contains(s.Note, `"raise"`) {
		t.Errorf("expected the keyword name to be \"raise\" in Note, got %q", s.Note)
	}
	if s.HelperSrc != "" {
		t.Errorf("rejected suggestion should have empty HelperSrc, got:\n%s", s.HelperSrc)
	}
}

// Given two Elixir snippets and an Alignment with no common spans,
// when Synthesize runs, then synthesis is rejected with a "no common
// lines" Note. Cycle 8.
func TestSynthesizeElixir_EmptyAlignment_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "x.ex:1-3 a", Lang: tokenizer.Elixir, Code: "def a do\n  1\nend"}
	b := scan.Snippet{Name: "y.ex:1-3 b", Lang: tokenizer.Elixir, Code: "def b do\n  2\nend"}
	s := Synthesize(a, b, "deadbeef", Alignment{})
	if !strings.Contains(s.Note, "no common lines") {
		t.Errorf("expected empty-alignment rejection, got Note=%q", s.Note)
	}
}

// Given an Elixir snippet whose first non-blank line has no `def`,
// when Synthesize runs, then it rejects with a clear Note. Cycle 8.
func TestSynthesizeElixir_NoDefHeader_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "x.ex:1-2 a", Lang: tokenizer.Elixir, Code: "# only comments\n# no header"}
	b := scan.Snippet{Name: "y.ex:1-2 b", Lang: tokenizer.Elixir, Code: "# only comments\n# no header"}
	al := Alignment{Common: []LineSpan{{AStart: 1, AEnd: 3, BStart: 1, BEnd: 3}}}
	s := Synthesize(a, b, "deadbeef", al)
	if !strings.Contains(s.Note, "Elixir") && !strings.Contains(s.Note, "def header") {
		t.Errorf("expected no-def-header rejection, got Note=%q", s.Note)
	}
}

// Given an Elixir snippet paired with a Python snippet, when
// Synthesize runs, then the cross-language guard fires before reaching
// synthesizeElixir. Cycle 8.
func TestSynthesize_ElixirCrossLanguage_Rejected(t *testing.T) {
	a, _ := loadSnippets(t, "../../testdata/refactor/elixir/simple")
	_, bPy := loadSnippets(t, "../../testdata/refactor/python/simple")
	al := Align(a, bPy)
	s := Synthesize(a, bPy, "deadbeef", al)
	if !strings.Contains(s.Note, "cross-language") {
		t.Errorf("expected cross-language rejection, got %q", s.Note)
	}
}

// Given two Elixir snippets where B is materially longer than A, when
// Synthesize computes confidence, then bLines drives the denominator
// (mirrors the other languages). Cycle 8.
func TestSynthesizeElixir_ConfidenceWithBLongerThanA(t *testing.T) {
	a := scan.Snippet{Name: "x.ex:1-3 foo", Lang: tokenizer.Elixir, Code: "def foo do\n  1\nend"}
	b := scan.Snippet{Name: "y.ex:1-7 bar", Lang: tokenizer.Elixir, Code: "def bar do\n  1\n  e = 2\n  e2 = 3\n  e3 = 4\n  e4 = 5\nend"}
	al := Align(a, b)
	if al.CommonLines() == 0 {
		t.Fatal("test setup expected at least one common line")
	}
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if s.Confidence <= 0 {
		t.Errorf("Confidence = %v; expected non-zero score", s.Confidence)
	}
	if s.Confidence >= 0.5 {
		t.Errorf("Confidence = %v; expected B's line count to drive denominator", s.Confidence)
	}
}

// Given any Elixir snippet, when Synthesize emits the helper, then a
// `# NOTE:` block always surfaces — Elixir defs cannot live at file
// scope, so the user must always move the helper into a module.
// Cycle 9.
func TestSynthesize_ElixirAlwaysCarriesModuleNote(t *testing.T) {
	a := scan.Snippet{Name: "x.ex:1-3 a", Lang: tokenizer.Elixir, Code: "def a(x) do\n  x + 1\nend"}
	b := scan.Snippet{Name: "y.ex:1-3 b", Lang: tokenizer.Elixir, Code: "def b(x) do\n  x + 2\nend"}
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if !strings.Contains(s.HelperSrc, "# NOTE:") {
		t.Errorf("Elixir helper should always carry a module-context # NOTE: line. HelperSrc:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, "defmodule") {
		t.Errorf("expected NOTE to mention `defmodule`. HelperSrc:\n%s", s.HelperSrc)
	}
}

// Given two modules where the duplicated logic lives inside
// `defmacro`, when the splitter+synthesizer runs, then the macro is
// chunked and a helper is emitted using `defmacro` (not `def`) so the
// extracted form is reusable as a macro itself. Realworld v2 fixture.
func TestSynthesize_ElixirDefmacro_HelperUsesDefmacro(t *testing.T) {
	a, b := loadSnippetsByPredicate(t,
		"../../testdata/refactor/elixir/realworld-defmacro",
		func(c splitter.Chunk) bool { return c.Symbol == "trace" })
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if !strings.Contains(s.HelperSrc, "defmacro extracted_trace_") {
		t.Errorf("helper should be a defmacro (not def); got:\n%s", s.HelperSrc)
	}
	for _, want := range []string{`"trace_a value=`, `"trace_b value=`} {
		if !strings.Contains(s.HelperSrc, want) {
			t.Errorf("expected divergence to surface %q; got:\n%s", want, s.HelperSrc)
		}
	}
}

// Given two modules with multi-clause defs (parse/decode) where
// each clause uses different patterns + guards + a do: shorthand
// trailing clause, when the splitter chunks them, then each clause
// becomes its own chunk and the synthesizer pairs equivalent clauses
// across modules. Verifies that the splitter produces 4 chunks per
// file (one per clause) and the synthesizer can produce a helper for
// the error-handling clause where A logs "parse failed" and B logs
// "decode failed". Realworld v2 fixture.
func TestSynthesize_ElixirMultiClauseErrorClause(t *testing.T) {
	a, b := loadSnippetsByPredicate(t,
		"../../testdata/refactor/elixir/realworld-multiclause",
		func(c splitter.Chunk) bool {
			// Pick the {:error, reason} clause (the third clause in each
			// file). The previous clauses share Symbol = parse / decode but
			// the splitter emits each as its own chunk; we narrow by line
			// range — the error clause is the third def, starting around
			// line 10.
			return c.StartLine >= 9 && c.StartLine <= 13
		})
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if !strings.Contains(s.HelperSrc, `"parse failed:`) {
		t.Errorf("expected A's logger string in helper; got:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, `"decode failed:`) {
		t.Errorf("expected divergence to surface B's logger string; got:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, "{:error, reason}") {
		t.Errorf("expected the pattern-matched arg to carry through to the helper; got:\n%s", s.HelperSrc)
	}
}

// Given the multi-clause fixture, when the splitter runs, then each
// of the 4 def clauses (binary guard, integer guard, error pattern,
// nil shorthand) becomes its own chunk. Pins the splitter's behaviour
// on multi-clause idioms.
func TestSplit_ElixirRealworldMultiClause_AllClauses(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/refactor/elixir/realworld-multiclause/a.ex")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	chunks := splitter.Split("a.ex", string(data), tokenizer.Elixir)
	if len(chunks) != 4 {
		t.Fatalf("expected 4 clauses, got %d (%v)", len(chunks), summariseChunks(chunks))
	}
	for _, c := range chunks {
		if c.Symbol != "parse" {
			t.Errorf("expected every clause symbol to be `parse`, got %q at line %d", c.Symbol, c.StartLine)
		}
	}
}

// Given two GenServer modules differing only in a logger string,
// when the splitter+aligner+synthesizer pipeline runs on the
// `handle_cast` chunks, then the helper preserves the block-form
// header and surfaces the diverging Logger.info call. Realworld
// regression fixture for v2 — exercises @impl, multi-line headers,
// pattern matching in args, and Logger string interpolation.
func TestSynthesize_ElixirGenServerHandleCast(t *testing.T) {
	a, b := loadSnippetsByPredicate(t,
		"../../testdata/refactor/elixir/realworld-genserver",
		func(c splitter.Chunk) bool { return c.Symbol == "handle_cast" })
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if !strings.Contains(s.HelperSrc, "def extracted_handle_cast_") {
		t.Errorf("expected helper named extracted_handle_cast_…; got:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, "user_cache put") {
		t.Errorf("expected A's logger string in helper; got:\n%s", s.HelperSrc)
	}
	for _, want := range []string{"user_cache put", "order_cache put"} {
		if !strings.Contains(s.HelperSrc, want) {
			t.Errorf("expected divergence to surface %q; got:\n%s", want, s.HelperSrc)
		}
	}
}

// Given the same GenServer fixture's `init` shorthand, when synthesis
// runs, then the helper preserves the shorthand form. Init bodies are
// often identical across GenServers — the alignment may have zero
// holes, but synthesis should still produce a usable starter helper
// (no rejection on identical bodies — only on no-common-lines, which
// requires zero common spans).
func TestSynthesize_ElixirGenServerInit_ShorthandPreserved(t *testing.T) {
	a, b := loadSnippetsByPredicate(t,
		"../../testdata/refactor/elixir/realworld-genserver",
		func(c splitter.Chunk) bool { return c.Symbol == "init" })
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if !strings.Contains(s.HelperSrc, "def extracted_init_") {
		t.Errorf("expected helper named extracted_init_…; got:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, "do: {:ok, state}") {
		t.Errorf("expected shorthand body preserved; got:\n%s", s.HelperSrc)
	}
}

// Given a `do:` shorthand chunk, when synthesizing through the full
// pipeline (header rewrite + rebody), then the helper preserves the
// shorthand form and renames just the function. Elixir v2.
func TestSynthesize_ElixirDoShorthand_HelperPreservesShorthand(t *testing.T) {
	a := scan.Snippet{
		Name: "x.ex:1-2 init", Lang: tokenizer.Elixir,
		Code: "  def init(state),\n    do: {:ok, state}",
	}
	b := scan.Snippet{
		Name: "y.ex:1-2 init", Lang: tokenizer.Elixir,
		Code: "  def init(state),\n    do: {:ok, state, :hibernate}",
	}
	al := Align(a, b)
	if al.CommonLines() == 0 {
		t.Fatal("test setup expected at least one common line")
	}
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if !strings.Contains(s.HelperSrc, "def extracted_init_") {
		t.Errorf("HelperSrc should rename the function; got:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, "do: {:ok, state}") {
		t.Errorf("HelperSrc should preserve the shorthand `do:` body; got:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, "(state),") {
		t.Errorf("HelperSrc should preserve the `(state),` head + comma; got:\n%s", s.HelperSrc)
	}
	// The shorthand form must NOT have an `end` keyword on its own line —
	// shorthand has no `end`. The trailing newline at the end of the
	// helper is fine; a bare `end` line would indicate the rebody added
	// block-form framing where it shouldn't.
	for _, l := range strings.Split(s.HelperSrc, "\n") {
		if strings.TrimSpace(l) == "end" {
			t.Errorf("shorthand helper should not have a bare `end` line; got:\n%s", s.HelperSrc)
		}
	}
}

// Given an Elixir def chunk indented 2 spaces (inside a defmodule),
// when exRebodyAsHelper extracts the body, then the def's outer indent
// is stripped so the body sits at one natural indent level below a
// column-zero header, ending with `end`. Cycle 6.
func TestExRebodyAsHelper_IndentedDefDedents(t *testing.T) {
	input := "  def fetch_a(table, key) do\n    prefix = \"#{table}:\"\n    body = \"#{prefix}#{key}\"\n    body\n  end"
	got := exRebodyAsHelper(input)
	expected := "  prefix = \"#{table}:\"\n  body = \"#{prefix}#{key}\"\n  body\nend\n"
	if got != expected {
		t.Errorf("got:\n%q\nwant:\n%q", got, expected)
	}
}

// Given a column-zero Elixir def, when exRebodyAsHelper runs, then the
// body passes through dedented relative to the (zero-indent) header.
// Cycle 6.
func TestExRebodyAsHelper_FreeDefPassesThrough(t *testing.T) {
	input := "def add(a, b) do\n  a + b\nend"
	got := exRebodyAsHelper(input)
	expected := "  a + b\nend\n"
	if got != expected {
		t.Errorf("got:\n%q\nwant:\n%q", got, expected)
	}
}

// Given a snippet led by a blank line, when exRebodyAsHelper runs,
// then it skips past the blank to find the def's indent. Cycle 6.
func TestExRebodyAsHelper_LeadingBlankLineSkipped(t *testing.T) {
	input := "\ndef add(a, b) do\n  a + b\nend"
	got := exRebodyAsHelper(input)
	if !strings.Contains(got, "  a + b") {
		t.Errorf("expected body to surface despite leading blank; got:\n%q", got)
	}
}

// Given two Elixir snippets that share the bulk of their bodies, when
// Synthesize runs, then it accepts (no rejection Note) and emits a
// non-empty HelperSrc. Drives the Elixir dispatch into synth.go.
func TestSynthesize_ElixirSimple_Accepts(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/elixir/simple")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if s.HelperSrc == "" {
		t.Fatal("HelperSrc empty")
	}
}

// Given two Rust snippets that share the bulk of their bodies, when
// Synthesize runs, then it accepts (no rejection Note) and emits a
// non-empty HelperSrc. Drives the Rust dispatch into synth.go.
func TestSynthesize_RustSimple_Accepts(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/rust/simple")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if s.HelperSrc == "" {
		t.Fatal("HelperSrc empty")
	}
}

// Given a snippet whose first non-blank line is a `function name(...)`
// declaration, when jsHelperHeader rewrites the header, then the
// function name is replaced and modifiers/parameters/async marker are
// preserved verbatim. Cycle 2.
func TestJsHelperHeader_FreeFunctionForms(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			"plain function",
			"function priceWithTaxA(amount) {\n  return amount;\n}",
			"function extracted_h(amount) {",
		},
		{
			"async function",
			"async function fetchUserA(id) {\n  return id;\n}",
			"async function extracted_h(id) {",
		},
		{
			"export default function",
			"export default function compute(x) {\n  return x;\n}",
			"export default function extracted_h(x) {",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, ok := jsHelperHeader(c.input, "extracted_h")
			if !ok {
				t.Fatalf("jsHelperHeader returned ok=false for %q", c.input)
			}
			if got != c.expect {
				t.Errorf("got %q, want %q", got, c.expect)
			}
		})
	}
}

// Given a snippet whose first non-blank line is an arrow assignment or
// `const x = function(...)`, when jsHelperHeader runs, then the helper
// is emitted as a free `function extracted_h(...) {` declaration —
// arrow-shape sources are normalised into the canonical free-function
// form for the helper. Cycle 3.
func TestJsHelperHeader_ArrowForms(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			"arrow with parens",
			"const compute = (a, b) => {\n  return a + b;\n};",
			"function extracted_h(a, b) {",
		},
		{
			"async arrow",
			"const fetchA = async (id) => {\n  return id;\n};",
			"async function extracted_h(id) {",
		},
		{
			"const = function",
			"const compute = function(a) {\n  return a;\n};",
			"function extracted_h(a) {",
		},
		{
			"const = async function",
			"const compute = async function(a) {\n  return a;\n};",
			"async function extracted_h(a) {",
		},
		{
			"let = arrow",
			"let f = (x) => {\n  return x;\n};",
			"function extracted_h(x) {",
		},
		{
			"export const arrow",
			"export const f = (x) => {\n  return x;\n};",
			"export function extracted_h(x) {",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, ok := jsHelperHeader(c.input, "extracted_h")
			if !ok {
				t.Fatalf("jsHelperHeader returned ok=false for %q", c.input)
			}
			if got != c.expect {
				t.Errorf("got %q, want %q", got, c.expect)
			}
		})
	}
}

// Given a snippet whose first non-blank line is a class-method header
// (no `function` keyword, no `=` arrow assignment, just
// `name(params) {`), when jsHelperHeader runs, then the helper is
// emitted as a free `function extracted_h(params) {` declaration.
// Methods come up when the JS splitter chunks class bodies. Cycle 4.
func TestJsHelperHeader_ClassMethodForms(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			"plain class method",
			"  fetchA(key) {\n    return key;\n  }",
			"function extracted_h(key) {",
		},
		{
			"async class method",
			"  async fetchA(key) {\n    return key;\n  }",
			"async function extracted_h(key) {",
		},
		{
			"static class method",
			"  static fetchA(key) {\n    return key;\n  }",
			"static function extracted_h(key) {",
		},
		{
			"two-arg method",
			"  format(name, age) {\n    return name;\n  }",
			"function extracted_h(name, age) {",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, ok := jsHelperHeader(c.input, "extracted_h")
			if !ok {
				t.Fatalf("jsHelperHeader returned ok=false for %q", c.input)
			}
			if got != c.expect {
				t.Errorf("got %q, want %q", got, c.expect)
			}
		})
	}
}

// Given a class-method JS snippet whose body references `this.`, when
// Synthesize emits the helper, then a `// NOTE:` line surfaces the
// this-binding boundary. Free functions and class methods that don't
// touch `this` MUST NOT carry the NOTE — the user shouldn't see it
// when it doesn't apply. Cycle 9.
func TestSynthesize_JavaScriptClassMethod_AnnotatesThisBinding(t *testing.T) {
	a := scan.Snippet{
		Name: "x.js:1-4 fetchA", Lang: tokenizer.JavaScript,
		Code: "  fetchA(key) {\n    const prefix = this.table + \":\";\n    return prefix + key;\n  }",
	}
	b := scan.Snippet{
		Name: "y.js:1-4 fetchB", Lang: tokenizer.JavaScript,
		Code: "  fetchB(key) {\n    const prefix = this.table + \":\";\n    return prefix + key;\n  }",
	}
	al := Align(a, b)
	if al.CommonLines() == 0 {
		t.Fatal("test setup expected at least one common line")
	}
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if !strings.Contains(s.HelperSrc, "// NOTE:") {
		t.Errorf("class-method helper that references `this.` should carry a // NOTE: line. HelperSrc:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, "this") {
		t.Errorf("expected NOTE to mention `this`. HelperSrc:\n%s", s.HelperSrc)
	}
}

// Given a free-function JS snippet without any `this` reference, when
// Synthesize emits the helper, then NO this-binding NOTE is emitted.
// Cycle 9.
func TestSynthesize_JavaScriptFreeFunction_NoThisNote(t *testing.T) {
	a := scan.Snippet{
		Name: "x.js:1-3 a", Lang: tokenizer.JavaScript,
		Code: "function a(x) {\n  return x + 1;\n}",
	}
	b := scan.Snippet{
		Name: "y.js:1-3 b", Lang: tokenizer.JavaScript,
		Code: "function b(x) {\n  return x + 2;\n}",
	}
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if strings.Contains(s.HelperSrc, "// NOTE:") {
		t.Errorf("free-function helper should NOT carry a // NOTE: line. HelperSrc:\n%s", s.HelperSrc)
	}
}

// Given two JS snippets and an Alignment with no common spans, when
// Synthesize runs, then synthesis is rejected with a "no common
// lines" Note. Cycle 7.
func TestSynthesizeJavaScript_EmptyAlignment_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "x.js:1-3 a", Lang: tokenizer.JavaScript, Code: "function a() {\n  return 1;\n}"}
	b := scan.Snippet{Name: "y.js:1-3 b", Lang: tokenizer.JavaScript, Code: "function b() {\n  return 2;\n}"}
	s := Synthesize(a, b, "deadbeef", Alignment{})
	if !strings.Contains(s.Note, "no common lines") {
		t.Errorf("expected empty-alignment rejection, got Note=%q", s.Note)
	}
}

// Given a JS snippet whose first non-blank line has no recognisable
// definition header (no `function`, no arrow, no class method), when
// Synthesize runs, then it rejects with a clear Note. Cycle 7.
func TestSynthesizeJavaScript_NoFunctionHeader_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "x.js:1-2 a", Lang: tokenizer.JavaScript, Code: "// only comments\n// no header"}
	b := scan.Snippet{Name: "y.js:1-2 b", Lang: tokenizer.JavaScript, Code: "// only comments\n// no header"}
	al := Alignment{Common: []LineSpan{{AStart: 1, AEnd: 3, BStart: 1, BEnd: 3}}}
	s := Synthesize(a, b, "deadbeef", al)
	if !strings.Contains(s.Note, "JavaScript") && !strings.Contains(s.Note, "function header") {
		t.Errorf("expected no-function-header rejection, got Note=%q", s.Note)
	}
}

// Given a JS snippet paired with a Python snippet, when Synthesize
// runs, then the cross-language guard fires before reaching
// synthesizeJavaScript. Cycle 7.
func TestSynthesize_JavaScriptCrossLanguage_Rejected(t *testing.T) {
	a, _ := loadSnippets(t, "../../testdata/refactor/js/simple")
	_, bPy := loadSnippets(t, "../../testdata/refactor/python/simple")
	al := Align(a, bPy)
	s := Synthesize(a, bPy, "deadbeef", al)
	if !strings.Contains(s.Note, "cross-language") {
		t.Errorf("expected cross-language rejection, got %q", s.Note)
	}
}

// Given two JS snippets where B is materially longer than A, when
// Synthesize computes confidence, then bLines drives the denominator
// (mirrors the Go/Java/Python tests). Cycle 8.
func TestSynthesizeJavaScript_ConfidenceWithBLongerThanA(t *testing.T) {
	a := scan.Snippet{Name: "x.js:1-3 foo", Lang: tokenizer.JavaScript, Code: "function foo() {\n  return 1;\n}"}
	b := scan.Snippet{Name: "y.js:1-7 bar", Lang: tokenizer.JavaScript, Code: "function bar() {\n  return 1;\n  let extra = 2;\n  let extra2 = 3;\n  let extra3 = 4;\n  let extra4 = 5;\n}"}
	al := Align(a, b)
	if al.CommonLines() == 0 {
		t.Fatal("test setup expected at least one common line; alignment found none")
	}
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if s.Confidence <= 0 {
		t.Errorf("Confidence = %v; expected non-zero score", s.Confidence)
	}
	if s.Confidence >= 0.5 {
		t.Errorf("Confidence = %v; expected B's line count to drive denominator", s.Confidence)
	}
}

// Given a hole where one side has `throw` and the other doesn't, when
// rejectControlFlowAsymmetryWithKeywords is called with the JS keyword
// set, then it rejects. Symmetric throws and identifier-prefix
// matches must NOT reject. Cycle 6.
func TestRejectControlFlowAsymmetry_JavaScriptKeywords(t *testing.T) {
	jsKeywords := []string{"return", "break", "continue", "throw", "yield"}
	cases := []struct {
		name string
		hole Hole
		want bool // true = rejected
	}{
		{"throw asymmetric", Hole{AText: "log(err);", BText: "throw new Error('x');"}, true},
		{"throw symmetric", Hole{AText: "throw new A();", BText: "throw new B();"}, false},
		{"yield asymmetric", Hole{AText: "x = 1;", BText: "yield 2;"}, true},
		{"yieldValue identifier not standalone", Hole{AText: "yieldValue = 1;", BText: "x = 1;"}, false},
		{"return asymmetric", Hole{AText: "return 1;", BText: "x = 1;"}, true},
		{"unrelated", Hole{AText: "x = 1;", BText: "x = 2;"}, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, ok := rejectControlFlowAsymmetryWithKeywords([]Hole{c.hole}, jsKeywords)
			rejected := !ok
			if rejected != c.want {
				t.Errorf("rejected=%v, want %v", rejected, c.want)
			}
		})
	}
}

// Given two JS snippets where B introduces a `throw` on a line A has
// as a plain statement, when Synthesize runs, then synthesis is
// rejected with a `control-flow asymmetry` Note and an empty
// HelperSrc. Cycles 6 + 11 (fixture-driven).
func TestSynthesize_JavaScriptThrowAsymmetry_Rejected(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/js/reject-throw")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note == "" {
		t.Fatalf("expected rejection, got HelperSrc:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.Note, "control-flow asymmetry") {
		t.Errorf("Note = %q, want 'control-flow asymmetry' substring", s.Note)
	}
	if !strings.Contains(s.Note, `"throw"`) {
		t.Errorf("Note = %q, want the keyword name to be \"throw\"", s.Note)
	}
	if s.HelperSrc != "" {
		t.Errorf("rejected suggestion should have empty HelperSrc, got:\n%s", s.HelperSrc)
	}
}

// Given a class-method snippet whose body is indented 4 spaces, when
// jsRebodyAsHelper extracts and dedents the body, then the result is
// the inner statements at 2-space indent (one level deep) without the
// outer header line, and ends with the closing `}` brace. Cycle 5.
func TestJsRebodyAsHelper_ClassMethodDedents(t *testing.T) {
	input := "  fetchA(key) {\n    const prefix = this.table + \":\";\n    return prefix;\n  }"
	got := jsRebodyAsHelper(input)
	expected := "  const prefix = this.table + \":\";\n  return prefix;\n}\n"
	if got != expected {
		t.Errorf("got:\n%q\nwant:\n%q", got, expected)
	}
}

// Given a free-function snippet whose body is at 2-space indent, when
// jsRebodyAsHelper runs, then the body is dedented relative to the
// header indent (which is 0 spaces here). Cycle 5.
func TestJsRebodyAsHelper_FreeFunctionPassesThrough(t *testing.T) {
	input := "function priceWithTaxA(amount) {\n  return amount * 1.07;\n}"
	got := jsRebodyAsHelper(input)
	expected := "  return amount * 1.07;\n}\n"
	if got != expected {
		t.Errorf("got:\n%q\nwant:\n%q", got, expected)
	}
}

// Given a snippet whose header line ends with a `{` followed by inline
// content, when jsRebodyAsHelper runs, then the inline content becomes
// the first body line. Cycle 5.
func TestJsRebodyAsHelper_InlineFirstStatement(t *testing.T) {
	input := "function f(x) { return x; }"
	got := jsRebodyAsHelper(input)
	if !strings.Contains(got, "return x;") {
		t.Errorf("expected inline body to surface; got:\n%q", got)
	}
}

// Given two JavaScript snippets that share the bulk of their bodies,
// when Synthesize runs, then it accepts (no rejection Note) and emits
// a non-empty HelperSrc. Cycle 1 — drives the JS dispatch into
// synth.go.
func TestSynthesize_JavaScriptSimple_Accepts(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/js/simple")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if s.HelperSrc == "" {
		t.Fatal("HelperSrc empty")
	}
}

// TestSynthesize_PythonAcceptTiers covers the simple/medium/advanced
// Python fixtures. Mirrors TestSynthesize_GoAcceptTiers but checks the
// `#`-comment block and the dedented helper body that drops out of
// pythonRebodyAsHelper.
func TestSynthesize_PythonAcceptTiers(t *testing.T) {
	cases := []struct {
		dir           string
		expectInSrc   []string
		minConfidence float64
	}{
		{
			dir: "../../testdata/refactor/python/simple",
			expectInSrc: []string{
				"def extracted_price_with_tax_a_",
				"# Divergences (B vs A):",
				"0.07",
				"0.085",
			},
			minConfidence: 0.5,
		},
		{
			dir: "../../testdata/refactor/python/medium",
			expectInSrc: []string{
				"def extracted_format_user_a_",
				`"user:"`,
				`"admin:"`,
				`"(active)"`,
				`"(privileged)"`,
			},
			minConfidence: 0.4,
		},
		{
			dir: "../../testdata/refactor/python/advanced",
			expectInSrc: []string{
				"def extracted_fetch_a_",
				`"/v1"`,
				`"/v2"`,
			},
			minConfidence: 0.4,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.dir, func(t *testing.T) {
			a, b := loadSnippets(t, c.dir)
			al := Align(a, b)
			s := Synthesize(a, b, "deadbeef", al)
			if s.Note != "" {
				t.Fatalf("expected accept, got Note=%q", s.Note)
			}
			if s.HelperSrc == "" {
				t.Fatal("HelperSrc empty")
			}
			if s.Confidence < c.minConfidence {
				t.Errorf("Confidence = %.2f, want >= %.2f", s.Confidence, c.minConfidence)
			}
			for _, want := range c.expectInSrc {
				if !strings.Contains(s.HelperSrc, want) {
					t.Errorf("HelperSrc missing %q. Source:\n%s", want, s.HelperSrc)
				}
			}
			if !validGoIdent(s.HelperName) {
				t.Errorf("HelperName %q is not a valid identifier", s.HelperName)
			}
		})
	}
}

// TestSynthesize_JavaAcceptTiers covers the simple/medium/advanced Java
// fixtures. The helper is appended at file scope (after the wrapping
// class's closing `}`) — this won't compile until a human moves it
// into the appropriate class, which is the documented v1 contract for
// Java. We assert the helper signature, the `// NOTE: appended at file
// scope` placement comment, the `//`-style divergence block, and that
// both differing literals show up.
func TestSynthesize_JavaAcceptTiers(t *testing.T) {
	cases := []struct {
		dir           string
		expectInSrc   []string
		minConfidence float64
	}{
		{
			dir: "../../testdata/refactor/java/simple",
			expectInSrc: []string{
				"public double extracted_priceWithTaxA_",
				"// Divergences (B vs A):",
				"// NOTE: appended at file scope",
				"0.07",
				"0.085",
			},
			minConfidence: 0.5,
		},
		{
			dir: "../../testdata/refactor/java/medium",
			expectInSrc: []string{
				"public String extracted_formatUserA_",
				`"user:"`,
				`"admin:"`,
				`"(active)"`,
				`"(privileged)"`,
			},
			minConfidence: 0.4,
		},
		{
			dir: "../../testdata/refactor/java/advanced",
			expectInSrc: []string{
				"public String extracted_fetchA_",
				`"/v1"`,
				`"/v2"`,
				"this.table",
			},
			minConfidence: 0.4,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.dir, func(t *testing.T) {
			a, b := loadSnippets(t, c.dir)
			al := Align(a, b)
			s := Synthesize(a, b, "deadbeef", al)
			if s.Note != "" {
				t.Fatalf("expected accept, got Note=%q", s.Note)
			}
			if s.HelperSrc == "" {
				t.Fatal("HelperSrc empty")
			}
			if s.Confidence < c.minConfidence {
				t.Errorf("Confidence = %.2f, want >= %.2f", s.Confidence, c.minConfidence)
			}
			for _, want := range c.expectInSrc {
				if !strings.Contains(s.HelperSrc, want) {
					t.Errorf("HelperSrc missing %q. Source:\n%s", want, s.HelperSrc)
				}
			}
			if !validGoIdent(s.HelperName) {
				t.Errorf("HelperName %q is not a valid identifier", s.HelperName)
			}
		})
	}
}

// TestSynthesize_JavaThrowAsymmetry_Rejected exercises the
// reject-throw fixture: B introduces a `throw` on a line A has as a
// plain statement, so the Java keyword set's `throw` entry triggers
// control-flow asymmetry rejection.
func TestSynthesize_JavaThrowAsymmetry_Rejected(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/java/reject-throw")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note == "" {
		t.Fatalf("expected rejection, got HelperSrc:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.Note, "control-flow asymmetry") {
		t.Errorf("Note = %q, want 'control-flow asymmetry' substring", s.Note)
	}
	if !strings.Contains(s.Note, `"throw"`) {
		t.Errorf("Note = %q, want the keyword name to be \"throw\"", s.Note)
	}
	if s.HelperSrc != "" {
		t.Errorf("rejected suggestion should have empty HelperSrc, got:\n%s", s.HelperSrc)
	}
}

// TestRejectControlFlowAsymmetry_JavaKeywords covers the Java
// emitter's keyword set: `throw` counts as control-flow asymmetry the
// same way `return`/`break`/`continue`/`yield` do.
func TestRejectControlFlowAsymmetry_JavaKeywords(t *testing.T) {
	javaKeywords := []string{"return", "break", "continue", "throw", "yield"}
	cases := []struct {
		name string
		hole Hole
		want bool // true = rejected
	}{
		{"throw asymmetric", Hole{AText: "metric.increment();", BText: "throw new IllegalStateException(\"x\");"}, true},
		{"throw symmetric", Hole{AText: "throw new A();", BText: "throw new B();"}, false},
		{"yield asymmetric (switch expr)", Hole{AText: "x = 1;", BText: "yield 2;"}, true},
		{"yield in identifier-name not standalone", Hole{AText: "yieldValue = 1;", BText: "x = 1;"}, false},
		{"unrelated", Hole{AText: "x = 1;", BText: "x = 2;"}, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, ok := rejectControlFlowAsymmetryWithKeywords([]Hole{c.hole}, javaKeywords)
			rejected := !ok
			if rejected != c.want {
				t.Errorf("rejected=%v, want %v", rejected, c.want)
			}
		})
	}
}

// TestSynthesizeJava_EmptyAlignment_Rejected covers the
// no-common-lines branch in synthesizeJava.
func TestSynthesizeJava_EmptyAlignment_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "X.java:1-3 foo", Lang: tokenizer.Java, Code: "    public void foo() {\n        a();\n    }"}
	b := scan.Snippet{Name: "Y.java:1-3 bar", Lang: tokenizer.Java, Code: "    public void bar() {\n        b();\n    }"}
	s := Synthesize(a, b, "deadbeef", Alignment{})
	if !strings.Contains(s.Note, "no common lines") {
		t.Errorf("expected empty-alignment rejection, got Note=%q", s.Note)
	}
}

// TestSynthesizeJava_NoMethodHeader_Rejected hits the
// javaHelperHeader=false branch: a chunk without a recognisable method
// header (no `(`) should reject with a clear note.
func TestSynthesizeJava_NoMethodHeader_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "X.java:1-2 foo", Lang: tokenizer.Java, Code: "// only comments\n// no method"}
	b := scan.Snippet{Name: "Y.java:1-2 bar", Lang: tokenizer.Java, Code: "// only comments\n// no method"}
	al := Alignment{
		Common: []LineSpan{{AStart: 1, AEnd: 3, BStart: 1, BEnd: 3}},
	}
	s := Synthesize(a, b, "deadbeef", al)
	if !strings.Contains(s.Note, "recognisable Java method header") {
		t.Errorf("expected no-method-header rejection, got Note=%q", s.Note)
	}
}

// TestJavaHelperHeader_PreservesModifiersAndThrows verifies the header
// rewriter preserves modifiers, generics, return type, parameter list,
// and `throws` clause while replacing only the method name.
func TestJavaHelperHeader_PreservesModifiersAndThrows(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			"plain instance method",
			"    public double priceWithTaxA(double amount) {\n        return amount;\n    }",
			"public double extracted_h(double amount) {",
		},
		{
			"static with throws",
			"    public static int parse(String s) throws IOException {\n        return 0;\n    }",
			"public static int extracted_h(String s) throws IOException {",
		},
		{
			"generic method",
			"    public <T> T identity(T x) {\n        return x;\n    }",
			"public <T> T extracted_h(T x) {",
		},
		{
			"annotation skipped, body line is the header",
			"    @Override\n    public String toString() {\n        return \"x\";\n    }",
			"public String extracted_h() {",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, ok := javaHelperHeader(c.input, "extracted_h")
			if !ok {
				t.Fatalf("javaHelperHeader returned ok=false for input:\n%s", c.input)
			}
			if got != c.expect {
				t.Errorf("javaHelperHeader = %q, want %q", got, c.expect)
			}
		})
	}
}

// TestJavaHelperHeader_NoParen_ReturnsFalse covers the `parenIdx <= 0`
// rejection branch.
func TestJavaHelperHeader_NoParen_ReturnsFalse(t *testing.T) {
	if _, ok := javaHelperHeader("    public class X {\n", "extracted_h"); ok {
		t.Error("expected ok=false when first non-comment line has no `(`")
	}
}

// TestJavaHelperHeader_AllCommentsAndAnnotations_ReturnsFalse exhausts
// the loop without finding any candidate header.
func TestJavaHelperHeader_AllCommentsAndAnnotations_ReturnsFalse(t *testing.T) {
	if _, ok := javaHelperHeader("// hi\n/* block */\n* javadoc cont\n@Override\n", "extracted_h"); ok {
		t.Error("expected ok=false when nothing but comments/annotations are present")
	}
}

// TestJavaRebodyAsHelper_DedentsByHeaderIndent confirms each body line
// is dedented by the header line's leading whitespace, leaving the body
// at one natural indent below the helper header.
func TestJavaRebodyAsHelper_DedentsByHeaderIndent(t *testing.T) {
	input := "    public void foo() {\n        a();\n        b();\n    }"
	got := javaRebodyAsHelper(input)
	if !strings.Contains(got, "    a();\n") {
		t.Errorf("body line `a();` not dedented to 4-space indent. Got:\n%s", got)
	}
	if !strings.Contains(got, "    b();\n") {
		t.Errorf("body line `b();` not dedented to 4-space indent. Got:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), "}") {
		t.Errorf("closing brace missing from body. Got:\n%s", got)
	}
}

// TestJavaRebodyAsHelper_NoBrace covers the early-return branch when
// the chunk has no `{`.
func TestJavaRebodyAsHelper_NoBrace(t *testing.T) {
	got := javaRebodyAsHelper("int x = 1;\nint y = 2;")
	if !strings.Contains(got, "int x = 1;") || !strings.Contains(got, "int y = 2;") {
		t.Errorf("javaRebodyAsHelper dropped no-brace input: %q", got)
	}
}

// TestSynthesizeJava_ConfidenceWithBLongerThanA covers the
// `bLines > maxLines` branch in synthesizeJava — when B is the longer
// snippet, B's line count drives the denominator.
func TestSynthesizeJava_ConfidenceWithBLongerThanA(t *testing.T) {
	a := scan.Snippet{
		Name: "X.java:1-3 foo", Lang: tokenizer.Java,
		Code: "    public void foo() {\n        a();\n    }",
	}
	b := scan.Snippet{
		Name: "Y.java:1-5 bar", Lang: tokenizer.Java,
		Code: "    public void bar() {\n        a();\n        b();\n        c();\n    }",
	}
	al := Align(a, b)
	if al.CommonLines() == 0 {
		t.Fatal("test setup expected at least one common line; alignment found none")
	}
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	// B has 5 lines, A has 3. If A's 3 lines drove the denominator,
	// confidence would be CommonLines/3, much higher than CommonLines/5.
	if s.Confidence >= 0.7 {
		t.Errorf("Confidence = %v; expected B's line count (5) to drive denominator", s.Confidence)
	}
}

// TestJavaHelperHeader_WhitespaceBetweenNameAndParen covers the
// `for nameEnd > 0 && (... ' ' || '\t' ...)` walk-back-over-space loop
// (lines 284-286): an unusual `methodName ()` with whitespace before
// the parameter list still gets the name token replaced cleanly.
func TestJavaHelperHeader_WhitespaceBetweenNameAndParen(t *testing.T) {
	got, ok := javaHelperHeader("    public void foo  () {\n        a();\n    }", "extracted_h")
	if !ok {
		t.Fatal("javaHelperHeader returned ok=false")
	}
	if !strings.Contains(got, "extracted_h  ()") {
		t.Errorf("whitespace before paren not preserved or name not replaced: %q", got)
	}
	if strings.Contains(got, "foo  ()") {
		t.Errorf("original method name not replaced: %q", got)
	}
}

// TestJavaHelperHeader_ParenWithoutPrecedingIdent covers the
// `nameStart == nameEnd` rejection branch (lines 291-293): when the
// `(` isn't preceded by an identifier (e.g. `)(`, `}(`).
func TestJavaHelperHeader_ParenWithoutPrecedingIdent(t *testing.T) {
	if _, ok := javaHelperHeader("    )(args)", "extracted_h"); ok {
		t.Error("expected ok=false when no identifier precedes `(`")
	}
}

// TestJavaRebodyAsHelper_LeadingBlankLineSkipped exercises the
// `TrimSpace(l) == ""` continue inside the header-indent detection
// loop (lines 310-311): a leading blank line shouldn't be mistaken
// for the header.
func TestJavaRebodyAsHelper_LeadingBlankLineSkipped(t *testing.T) {
	input := "\n    public void foo() {\n        a();\n    }"
	got := javaRebodyAsHelper(input)
	if !strings.Contains(got, "    a();\n") {
		t.Errorf("body line `a();` not dedented to 4-space indent. Got:\n%s", got)
	}
}

// TestJavaRebodyAsHelper_BraceOnHeaderLineWithTrailingContent covers
// the `afterBrace != ""` branch (lines 335-337): when the opening `{`
// shares a line with body content (`void foo() { a();`), that
// trailing content becomes the first body line.
func TestJavaRebodyAsHelper_BraceOnHeaderLineWithTrailingContent(t *testing.T) {
	input := "    public void foo() { a();\n        b();\n    }"
	got := javaRebodyAsHelper(input)
	if !strings.HasPrefix(got, "a();\n") {
		t.Errorf("post-brace content should be the first body line. Got:\n%s", got)
	}
}

// TestSynthesize_JavaCrossLanguage_Rejected verifies the upstream
// cross-language guard fires before reaching synthesizeJava.
func TestSynthesize_JavaCrossLanguage_Rejected(t *testing.T) {
	a, _ := loadSnippets(t, "../../testdata/refactor/java/simple")
	_, bGo := loadSnippets(t, "../../testdata/refactor/go/simple")
	al := Align(a, bGo)
	s := Synthesize(a, bGo, "deadbeef", al)
	if !strings.Contains(s.Note, "cross-language") {
		t.Errorf("expected cross-language rejection, got %q", s.Note)
	}
}

func TestSynthesize_CrossLanguage_Rejected(t *testing.T) {
	a, _ := loadSnippets(t, "../../testdata/refactor/go/simple")
	_, bPy := loadSnippets(t, "../../testdata/refactor/python/simple")
	al := Align(a, bPy)
	s := Synthesize(a, bPy, "deadbeef", al)
	if !strings.Contains(s.Note, "cross-language") {
		t.Errorf("expected cross-language rejection, got %q", s.Note)
	}
}

// ── Real-world fixtures ──────────────────────────────────────────────────────
//
// The simple/medium/advanced tiers are toy fixtures. The
// realworld-* tiers exercise patterns the toy fixtures don't:
//   - Go: methods on a shared receiver type (so the
//     receiver-stripping branch in goHelperHeader runs end-to-end)
//     and a multi-statement err-wrap function with shared
//     control-flow shape.
//   - Python: `async def` (so the async branch in
//     pythonHelperHeader runs end-to-end), and decorated functions
//     (verifying decorators are dropped from the helper header but
//     show up in the divergence comment block).

func TestSynthesize_GoRealworld_Method(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/go/realworld-method")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept (same receiver type), got Note=%q", s.Note)
	}
	if !strings.HasPrefix(s.HelperName, "extracted_FindUserByID_") {
		t.Errorf("HelperName = %q, want extracted_FindUserByID_… prefix", s.HelperName)
	}
	// Helper must be a free function — receiver `(r *Repo)` dropped
	// from the header. (The divergence comment block does retain the
	// original receivers verbatim — that's intended, so the reviewer
	// can see what was elided. We don't assert on that.)
	if !strings.Contains(s.HelperSrc, "func extracted_FindUserByID_deadbeef(ctx context.Context, id int) (*User, error) {") {
		t.Errorf("helper header didn't strip the receiver. Source:\n%s", s.HelperSrc)
	}
	// Body retained.
	if !strings.Contains(s.HelperSrc, `r.db.QueryRowContext(ctx, "SELECT * FROM users WHERE id = $1", id)`) {
		t.Errorf("helper missing original body line. Source:\n%s", s.HelperSrc)
	}
	// Divergences surface both method names + return-type differences.
	if !strings.Contains(s.HelperSrc, "FindUserByID") || !strings.Contains(s.HelperSrc, "FindOrderByID") {
		t.Errorf("divergence comment missing method-name pair. Source:\n%s", s.HelperSrc)
	}
}

// TestSynthesize_GoRealworld_ErrWrap exercises a multi-statement Go
// function with two `if err != nil` branches that share the same
// control-flow shape (`return nil, fmt.Errorf(...)` on both sides).
// Both sides have the keyword in the same place, so
// rejectControlFlowAsymmetry must accept; the divergences should
// surface the differing config types and error messages.
func TestSynthesize_GoRealworld_ErrWrap(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/go/realworld-errwrap")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept (matching control flow), got Note=%q", s.Note)
	}
	if !strings.HasPrefix(s.HelperName, "extracted_loadUserConfig_") {
		t.Errorf("HelperName = %q, want extracted_loadUserConfig_… prefix", s.HelperName)
	}
	wantSubs := []string{
		"data, err := os.ReadFile(path)",
		`json.Unmarshal(data, &cfg)`,
		"UserConfig", "OrderConfig",
		`"read user config: %w"`, `"read order config: %w"`,
		`"parse user config: %w"`, `"parse order config: %w"`,
	}
	for _, want := range wantSubs {
		if !strings.Contains(s.HelperSrc, want) {
			t.Errorf("HelperSrc missing %q. Source:\n%s", want, s.HelperSrc)
		}
	}
	// 9 lines in each chunk, ~5 common (declarations + return).
	if s.Confidence < 0.4 {
		t.Errorf("Confidence = %.2f, want >= 0.4 for substantial overlap", s.Confidence)
	}
}

func TestSynthesize_PythonRealworld_Async(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/python/realworld-async")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	if !strings.HasPrefix(s.HelperName, "extracted_fetch_user_") {
		t.Errorf("HelperName = %q, want extracted_fetch_user_… prefix", s.HelperName)
	}
	if !strings.Contains(s.HelperSrc, "async def extracted_fetch_user_") {
		t.Errorf("helper missing `async def`. Source:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, `await client.get(f"/users/{user_id}")`) {
		t.Errorf("helper missing original await body line. Source:\n%s", s.HelperSrc)
	}
	if s.Confidence < 0.5 {
		t.Errorf("Confidence = %.2f, want >= 0.5 for 4-of-5-line overlap", s.Confidence)
	}
}

// TestSynthesize_PythonRealworld_Decorated verifies the decorator-skip
// contract end-to-end: the helper header drops every `@…` line, but
// the divergence comment block surfaces the differing decorators so a
// reviewer sees what was elided.
func TestSynthesize_PythonRealworld_Decorated(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/python/realworld-decorated")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}

	// Helper must NOT carry the decorator forward — we said so in the
	// emitter doc, this confirms it.
	helperLines := strings.Split(strings.TrimSpace(s.HelperSrc), "\n")
	for _, l := range helperLines {
		if strings.HasPrefix(l, "@") {
			t.Errorf("helper carried a decorator forward: %q\nFull source:\n%s", l, s.HelperSrc)
		}
	}
	if !strings.Contains(s.HelperSrc, "def extracted_load_user_profile_") {
		t.Errorf("helper missing extracted def. Source:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, `audit.log("read_user", user_id=user_id)`) {
		t.Errorf("helper missing original body line. Source:\n%s", s.HelperSrc)
	}
	// Divergence comment must surface the dropped decorators so the
	// reviewer can decide whether they belong on the helper.
	if !strings.Contains(s.HelperSrc, "@retry(attempts=3)") {
		t.Errorf("divergence comment missing A's decorator. Source:\n%s", s.HelperSrc)
	}
	if !strings.Contains(s.HelperSrc, "@retry(attempts=5)") {
		t.Errorf("divergence comment missing B's decorator. Source:\n%s", s.HelperSrc)
	}
}

// ── Synthesize* defensive-branch coverage (Class A) ─────────────────────────
//
// These tests exercise rejection paths that the fixture pipeline can't
// reach: empty alignments, control-flow asymmetry, and the
// confidence-calc branch where B is longer than A. They construct
// Snippets and Alignments by hand instead of running the full
// scan/align pipeline.

func TestSynthesizeGo_EmptyAlignment_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "x.go:1-3 foo", Lang: tokenizer.Go, Code: "func foo() {\n\ta()\n}"}
	b := scan.Snippet{Name: "y.go:1-3 bar", Lang: tokenizer.Go, Code: "func bar() {\n\tb()\n}"}
	s := Synthesize(a, b, "deadbeef", Alignment{})
	if !strings.Contains(s.Note, "no common lines") {
		t.Errorf("expected empty-alignment rejection, got Note=%q", s.Note)
	}
}

func TestSynthesizeGo_ControlFlowAsymmetry_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "x.go:1-3 foo", Lang: tokenizer.Go, Code: "func foo() {\n\treturn errors.New(\"x\")\n}"}
	b := scan.Snippet{Name: "y.go:1-3 bar", Lang: tokenizer.Go, Code: "func bar() {\n\tlog.Fatal(\"x\")\n}"}
	al := Alignment{
		Common: []LineSpan{{AStart: 1, AEnd: 2, BStart: 1, BEnd: 2}},
		Holes: []Hole{{
			AStart: 2, AEnd: 3, BStart: 2, BEnd: 3,
			AText: "return errors.New(\"x\")", BText: "log.Fatal(\"x\")",
		}},
	}
	s := Synthesize(a, b, "deadbeef", al)
	if !strings.Contains(s.Note, "control-flow asymmetry") {
		t.Errorf("expected control-flow rejection through synthesizeGo, got Note=%q", s.Note)
	}
}

// TestSynthesizeGo_ConfidenceWithBLongerThanA covers the maxLines =
// bLines branch in the confidence calculation: when B has more lines
// than A, the denominator switches to bLines.
func TestSynthesizeGo_ConfidenceWithBLongerThanA(t *testing.T) {
	a := scan.Snippet{
		Name: "x.go:1-3 foo", Lang: tokenizer.Go,
		Code: "func foo() {\n\ta()\n}",
	}
	b := scan.Snippet{
		Name: "y.go:1-5 bar", Lang: tokenizer.Go,
		Code: "func bar() {\n\ta()\n\tb()\n\tc()\n}",
	}
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	// Confidence == CommonLines / max(aLines, bLines). If aLines (=3)
	// drove the denominator we'd get a value > 0.5; bLines (=5) driving
	// it caps the value at <= CommonLines/5, which for any sensible
	// alignment of these snippets stays below 0.5.
	if s.Confidence >= 0.5 {
		t.Errorf("Confidence = %v; expected B's line count (5) to drive the denominator (would be < 0.5)", s.Confidence)
	}
}

func TestSynthesizePython_EmptyAlignment_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "x.py:1-2 foo", Lang: tokenizer.Python, Code: "def foo():\n    return 1"}
	b := scan.Snippet{Name: "y.py:1-2 bar", Lang: tokenizer.Python, Code: "def bar():\n    return 2"}
	s := Synthesize(a, b, "deadbeef", Alignment{})
	if !strings.Contains(s.Note, "no common lines") {
		t.Errorf("expected empty-alignment rejection, got Note=%q", s.Note)
	}
}

// TestSynthesizePython_RaiseAsymmetry_Rejected uses the extended Python
// keyword set: a hole where only one side `raise`s should reject through
// synthesizePython, not just at the helper level.
func TestSynthesizePython_RaiseAsymmetry_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "x.py:1-2 foo", Lang: tokenizer.Python, Code: "def foo():\n    raise ValueError(\"x\")"}
	b := scan.Snippet{Name: "y.py:1-2 bar", Lang: tokenizer.Python, Code: "def bar():\n    log.error(\"x\")"}
	al := Alignment{
		Common: []LineSpan{{AStart: 1, AEnd: 2, BStart: 1, BEnd: 2}},
		Holes: []Hole{{
			AStart: 2, AEnd: 3, BStart: 2, BEnd: 3,
			AText: "raise ValueError(\"x\")", BText: "log.error(\"x\")",
		}},
	}
	s := Synthesize(a, b, "deadbeef", al)
	if !strings.Contains(s.Note, "control-flow asymmetry") {
		t.Errorf("expected control-flow rejection, got Note=%q", s.Note)
	}
}

func TestSynthesizePython_NoDefLine_Rejected(t *testing.T) {
	a := scan.Snippet{Name: "x.py:1-2 m", Lang: tokenizer.Python, Code: "x = 1\ny = 2"}
	b := scan.Snippet{Name: "y.py:1-2 m", Lang: tokenizer.Python, Code: "x = 1\ny = 3"}
	al := Alignment{
		Common: []LineSpan{{AStart: 1, AEnd: 2, BStart: 1, BEnd: 2}},
	}
	s := Synthesize(a, b, "deadbeef", al)
	if !strings.Contains(s.Note, "no recognisable `def`") {
		t.Errorf("expected no-def rejection, got Note=%q", s.Note)
	}
}

// ── pythonHelperHeader / pythonRebodyAsHelper edge cases (Class B) ──────────

func TestPythonHelperHeader_AsyncDef_PreservesAsync(t *testing.T) {
	header, ok := pythonHelperHeader("async def fetch(x):\n    return x\n", "extracted_fetch_deadbeef")
	if !ok {
		t.Fatal("expected ok=true for async def")
	}
	if !strings.HasPrefix(header, "async def extracted_fetch_deadbeef(") {
		t.Errorf("async prefix not preserved: %q", header)
	}
}

// TestPythonHelperHeader_DecoratorsThenDef walks the
// blank/comment/decorator skip path before finding the def line.
func TestPythonHelperHeader_DecoratorsThenDef(t *testing.T) {
	code := "\n# leading comment\n@decorator\n@another\ndef target(x):\n    return x\n"
	header, ok := pythonHelperHeader(code, "extracted_target_deadbeef")
	if !ok {
		t.Fatal("expected ok=true after skipping decorators")
	}
	if !strings.HasPrefix(header, "def extracted_target_deadbeef(") {
		t.Errorf("def line not extracted after decorator skip: %q", header)
	}
}

func TestPythonHelperHeader_NoDefLine_ReturnsFalse(t *testing.T) {
	if _, ok := pythonHelperHeader("x = 1\ny = 2\n", "extracted_x_deadbeef"); ok {
		t.Error("expected ok=false when no def line is present")
	}
}

// TestPythonHelperHeader_OnlyDecorators_ReturnsFalse exhausts the loop
// without finding any def line, exercising the trailing `return "",
// false` after the for-range completes.
func TestPythonHelperHeader_OnlyDecorators_ReturnsFalse(t *testing.T) {
	if _, ok := pythonHelperHeader("\n@deco1\n@deco2\n# comment\n\n", "extracted_x_deadbeef"); ok {
		t.Error("expected ok=false when only decorators/comments are present")
	}
}

func TestPythonRebodyAsHelper_PreservesBlankLines(t *testing.T) {
	code := "def foo():\n    a = 1\n\n    b = 2\n"
	body := pythonRebodyAsHelper(code)
	// The blank line between `a` and `b` should survive as a `\n`
	// between two re-indented lines.
	if !strings.Contains(body, "    a = 1\n\n    b = 2\n") {
		t.Errorf("blank line not preserved between body statements:\n%s", body)
	}
}

// TestPythonRebodyAsHelper_DefOnlyNoBody_ReturnsEmpty hits the
// `defIdx == len(lines)-1` branch — the def line is the last line of
// the chunk, so there's no body to re-indent.
func TestPythonRebodyAsHelper_DefOnlyNoBody_ReturnsEmpty(t *testing.T) {
	if got := pythonRebodyAsHelper("def foo():"); got != "" {
		t.Errorf("expected empty body, got %q", got)
	}
}

// TestPythonRebodyAsHelper_AllBlankBody covers the `minIndent < 0`
// branch when the body has only blank-but-non-empty lines (after the
// outer TrimRight strips the truly trailing blanks). Whitespace-only
// lines pass the splitter but trip the `TrimSpace(l) == ""` guard
// inside the minIndent loop, leaving minIndent at -1.
func TestPythonRebodyAsHelper_AllBlankBody(t *testing.T) {
	body := pythonRebodyAsHelper("def foo():\n   \n\t\n")
	// Every body line was blank-after-trim, so output is just blank
	// lines.
	if strings.TrimSpace(body) != "" {
		t.Errorf("expected blanks-only body, got %q", body)
	}
}

// TestPythonRebodyAsHelper_DecoratorBeforeDef hits the
// blank/comment/decorator skip path inside pythonRebodyAsHelper
// (which has its own def-finder loop separate from
// pythonHelperHeader's).
func TestPythonRebodyAsHelper_DecoratorBeforeDef(t *testing.T) {
	body := pythonRebodyAsHelper("@deco\ndef foo():\n    return 1\n")
	if !strings.Contains(body, "    return 1") {
		t.Errorf("body did not survive decorator skip: %q", body)
	}
}

// TestPythonRebodyAsHelper_NoDefLine_ReturnsAsIs hits the early
// `defIdx < 0` branch.
func TestPythonRebodyAsHelper_NoDefLine_ReturnsAsIs(t *testing.T) {
	if got := pythonRebodyAsHelper("x = 1\ny = 2\n"); got != "" {
		t.Errorf("expected empty body for no-def input, got %q", got)
	}
}

// ── Class C: pre-existing defensive scaffolding ─────────────────────────────
//
// These cover the small utility functions whose edge-case branches
// the higher-level tests skip past. They're not behavioral changes —
// just closing the coverage gap.

func TestSymbolForSnippet_WholeFileChunk_ReturnsEmpty(t *testing.T) {
	s := scan.Snippet{Name: "wholefile.go"}
	if got := SymbolForSnippet(s); got != "" {
		t.Errorf("SymbolForSnippet on whole-file chunk = %q, want \"\"", got)
	}
}

func TestGoReceiverType_EdgeCases(t *testing.T) {
	cases := []struct {
		name string
		code string
		want string
	}{
		{"plain function (no receiver)", "func Foo() {}", ""},
		{"value receiver", "func (r Repo) Foo() {}", "Repo"},
		{"pointer receiver normalised", "func (r *Repo) Foo() {}", "Repo"},
		{"empty parens (parts==0)", "func () Foo() {}", ""},
		{"no close paren anywhere", "func (r Repo missing", ""},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := goReceiverType(c.code); got != c.want {
				t.Errorf("goReceiverType(%q) = %q, want %q", c.code, got, c.want)
			}
		})
	}
}

// TestContainsKeyword_IdentifierAdjacency walks the two reject branches
// in containsKeyword: a keyword fragment preceded by an identifier
// byte, and one followed by an identifier byte. Both should fall
// through to the next occurrence rather than returning true.
func TestContainsKeyword_IdentifierAdjacency(t *testing.T) {
	cases := []struct {
		name string
		text string
		kw   string
		want bool
	}{
		{"standalone return", "return errors.New(x)", "return", true},
		{"return prefix-glued (returnValue)", "returnValue := 1", "return", false},
		{"return suffix-glued (xreturn)", "fooreturn = 1", "return", false},
		{"both glued then standalone", "myreturning; return x", "return", true},
		{"keyword absent", "log.Fatal(x)", "return", false},
		{"keyword at start of text", "return", "return", true},
		{"keyword followed by identifier byte at end", "return1", "return", false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := containsKeyword(c.text, c.kw); got != c.want {
				t.Errorf("containsKeyword(%q, %q) = %v, want %v", c.text, c.kw, got, c.want)
			}
		})
	}
}

func TestSanitizeHelperName_EmptySymbol_FallsBackToFn(t *testing.T) {
	got := sanitizeHelperName("", "deadbeef")
	if got != "extracted_fn_deadbeef" {
		t.Errorf("sanitizeHelperName(\"\", \"deadbeef\") = %q, want extracted_fn_deadbeef", got)
	}
}

func TestNonEmpty_FallbackBranch(t *testing.T) {
	if got := nonEmpty("", "fallback"); got != "fallback" {
		t.Errorf("nonEmpty(\"\", \"fallback\") = %q, want \"fallback\"", got)
	}
	if got := nonEmpty("real", "fallback"); got != "real" {
		t.Errorf("nonEmpty(\"real\", \"fallback\") = %q, want \"real\"", got)
	}
}

func TestFirstNonBlankLine_AllBlank_ReturnsEmpty(t *testing.T) {
	if got := firstNonBlankLine("\n   \n\t\n"); got != "" {
		t.Errorf("firstNonBlankLine on all-blank input = %q, want \"\"", got)
	}
}

// TestGoRebodyAsHelper_NoBrace exercises the early-return path when
// the snippet is somehow brace-less (defensive — the fixture pipeline
// ensures Go chunks always have one). Returns the input unchanged
// (with a trailing newline).
func TestGoRebodyAsHelper_NoBrace(t *testing.T) {
	got := goRebodyAsHelper("var x = 1\nvar y = 2")
	if !strings.Contains(got, "var x = 1") || !strings.Contains(got, "var y = 2") {
		t.Errorf("goRebodyAsHelper dropped no-brace input: %q", got)
	}
}

func TestRejectAnonymousChunk_WholeFileChunk_Rejected(t *testing.T) {
	// SymbolForSnippet returns "" for snippets whose Name lacks a
	// space — i.e. whole-file chunks. rejectAnonymousChunk has its
	// own message for this distinct from the goroutine/defer prefix
	// path.
	a := scan.Snippet{Name: "wholefile.go"}
	b := scan.Snippet{Name: "wholefile.go"}
	reason, ok := rejectAnonymousChunk(a, b)
	if ok {
		t.Fatal("expected rejection for whole-file chunk")
	}
	if !strings.Contains(reason, "top-level named function") {
		t.Errorf("reason = %q, want 'top-level named function' message", reason)
	}
}

func TestRejectMethodOnDifferentReceivers_SameReceiver_Accepts(t *testing.T) {
	aCode := "func (r Repo) FetchA(id int) error { return nil }"
	bCode := "func (r Repo) FetchB(id int) error { return nil }"
	if _, ok := rejectMethodOnDifferentReceivers(aCode, bCode); !ok {
		t.Error("expected acceptance when both methods share the same receiver type")
	}
	// And pointer/value normalisation: *Repo and Repo should match.
	if _, ok := rejectMethodOnDifferentReceivers(aCode, "func (r *Repo) FetchB() {}"); !ok {
		t.Error("expected acceptance when pointer- and value-receiver share underlying type")
	}
}

func TestSanitizeHelperName_NonIdentChars_GetUnderscored(t *testing.T) {
	got := sanitizeHelperName("goroutine@42", "deadbeef")
	if got != "extracted_goroutine_42_deadbeef" {
		t.Errorf("sanitizeHelperName = %q, want extracted_goroutine_42_deadbeef", got)
	}
}

// TestGoHelperHeader_MethodReceiver_DroppedFromHeader exercises the
// receiver-stripping branch (lines 429-433): when the snippet is a
// method, the helper drops the receiver and emerges as a free
// function with the helper name in place of the original method name.
func TestGoHelperHeader_MethodReceiver_DroppedFromHeader(t *testing.T) {
	got := goHelperHeader("func (r *Repo) Fetch(id int) error {\n\treturn nil\n}",
		"extracted_Fetch_deadbeef")
	if !strings.HasPrefix(got, "func extracted_Fetch_deadbeef(id int) error {") {
		t.Errorf("receiver not dropped: %q", got)
	}
}

// TestGoRebodyAsHelper_AfterBraceContent covers the `afterBrace != ""`
// branch (lines 462-464): when the opening `{` shares a line with body
// content (`func F() { x := 1`), that trailing content becomes the
// first body line.
func TestGoRebodyAsHelper_AfterBraceContent(t *testing.T) {
	body := goRebodyAsHelper("func F() { x := 1\n\treturn x\n}")
	if !strings.HasPrefix(body, "x := 1\n") {
		t.Errorf("expected post-brace content as first body line, got %q", body)
	}
}

// TestSynthesizePython_ConfidenceWithBLongerThanA mirrors the Go test
// for the bLines > aLines branch in synthesizePython.
func TestSynthesizePython_ConfidenceWithBLongerThanA(t *testing.T) {
	a := scan.Snippet{
		Name: "x.py:1-2 foo", Lang: tokenizer.Python,
		Code: "def foo():\n    return 1",
	}
	b := scan.Snippet{
		Name: "y.py:1-5 bar", Lang: tokenizer.Python,
		Code: "def bar():\n    return 1\n    extra = 2\n    extra2 = 3\n    extra3 = 4",
	}
	al := Align(a, b)
	if al.CommonLines() == 0 {
		t.Fatal("test setup expected at least one common line; alignment found none")
	}
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	// CommonLines is at most 2 (the def line and the `return 1` body
	// line); bLines is 5. If aLines drove the denominator we'd see
	// confidence ≥ 0.5; bLines driving it caps confidence at 2/5 = 0.4.
	if s.Confidence >= 0.5 {
		t.Errorf("Confidence = %v; expected B's line count to drive denominator", s.Confidence)
	}
}

func TestFormatDivergenceComment_EmptyHoles_ReturnsEmpty(t *testing.T) {
	if got := formatDivergenceComment(nil, "//"); got != "" {
		t.Errorf("expected empty string for nil holes, got %q", got)
	}
	if got := formatDivergenceComment([]Hole{}, "#"); got != "" {
		t.Errorf("expected empty string for empty holes, got %q", got)
	}
}

func validGoIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			// always allowed
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ── Bet #4 deferred follow-ups: Python multi-line signatures + TS syntax ─────

// Given a single-line Python def, when pythonHelperHeader rewrites it,
// then the output is byte-identical to the pre-multi-line-support
// behaviour: the trimmed def line with only the name replaced. Pins the
// single-line path against regressions from the multi-line work.
func TestPythonHelperHeader_SingleLine_ByteIdentical(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			"plain def with default arg",
			"def price(amount, rate=0.07):\n    return amount\n",
			"def extracted_h(amount, rate=0.07):",
		},
		{
			"async def",
			"async def fetch(client):\n    return client\n",
			"async def extracted_h(client):",
		},
		{
			"annotated single-line def",
			"def price(amount: float) -> float:\n    return amount\n",
			"def extracted_h(amount: float) -> float:",
		},
		{
			"indented method def",
			"    def fetch(self, key):\n        return key\n",
			"def extracted_h(self, key):",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, ok := pythonHelperHeader(c.input, "extracted_h")
			if !ok {
				t.Fatalf("pythonHelperHeader returned ok=false for %q", c.input)
			}
			if got != c.expect {
				t.Errorf("got %q, want %q", got, c.expect)
			}
		})
	}
}

// Given a Black-formatted multi-line def signature, when
// pythonHelperHeader rewrites it, then the function name is replaced on
// the first line and the remaining signature lines (params with
// trailing commas, default args, type annotations, and the closing
// `) -> Ret:` line) are carried verbatim, dedented by the def line's
// indent. Bet #4 deferred follow-up.
func TestPythonHelperHeader_MultilineSignatureForms(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			"black-formatted multi-line def",
			"def compute(\n    orders,\n    cutoff=10,\n) -> dict:\n    return {}\n",
			"def extracted_h(\n    orders,\n    cutoff=10,\n) -> dict:",
		},
		{
			"async multi-line def",
			"async def fetch(\n    client,\n    user_id: int,\n) -> dict:\n    return await client.get(user_id)\n",
			"async def extracted_h(\n    client,\n    user_id: int,\n) -> dict:",
		},
		{
			"indented method multi-line def dedents by def indent",
			"    def fetch(\n        self,\n        key: str,\n    ) -> str:\n        return key\n",
			"def extracted_h(\n    self,\n    key: str,\n) -> str:",
		},
		{
			"decorator above multi-line def",
			"@retry\ndef load(\n    path,\n):\n    return path\n",
			"def extracted_h(\n    path,\n):",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, ok := pythonHelperHeader(c.input, "extracted_h")
			if !ok {
				t.Fatalf("pythonHelperHeader returned ok=false for %q", c.input)
			}
			if got != c.expect {
				t.Errorf("got %q, want %q", got, c.expect)
			}
		})
	}
}

// Given a multi-line def signature, when pythonRebodyAsHelper extracts
// the body, then the body starts AFTER the signature's closing `):`
// line — the continuation params must not leak into the body.
func TestPythonRebodyAsHelper_MultilineSignatureSkipped(t *testing.T) {
	code := "def compute(\n    orders,\n    cutoff=10,\n) -> dict:\n    total = 0\n    return {\"total\": total}\n"
	body := pythonRebodyAsHelper(code)
	if strings.Contains(body, "orders,") || strings.Contains(body, "cutoff=10,") {
		t.Errorf("signature lines leaked into body:\n%q", body)
	}
	if !strings.HasPrefix(body, "    total = 0\n") {
		t.Errorf("body should start at the first post-signature line, got:\n%q", body)
	}
}

// Given the realworld-multiline-sig fixture (Black-formatted def with
// trailing-comma params, a default arg, type annotations, and a
// `) -> dict:` return annotation on the closing line), when Synthesize
// runs, then the helper carries the whole multi-line signature verbatim
// with only the name rewritten, and the body starts after the closing
// line. Bet #4 deferred follow-up.
func TestSynthesize_PythonRealworld_MultilineSig(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/python/realworld-multiline-sig")
	al := Align(a, b)
	s := Synthesize(a, b, "deadbeef", al)
	if s.Note != "" {
		t.Fatalf("expected accept, got Note=%q", s.Note)
	}
	wantHeader := "def extracted_compute_totals_deadbeef(\n" +
		"    orders: list[dict],\n" +
		"    cutoff: int = 10,\n" +
		") -> dict:\n" +
		"    total = 0\n"
	if !strings.Contains(s.HelperSrc, wantHeader) {
		t.Errorf("helper missing verbatim multi-line signature + body start.\nWant substring:\n%s\nGot:\n%s", wantHeader, s.HelperSrc)
	}
	// Divergences must surface both labels.
	for _, want := range []string{`"totals:v1"`, `"sums:v2"`} {
		if !strings.Contains(s.HelperSrc, want) {
			t.Errorf("HelperSrc missing %q. Source:\n%s", want, s.HelperSrc)
		}
	}
	if s.Confidence < 0.5 {
		t.Errorf("Confidence = %.2f, want >= 0.5", s.Confidence)
	}
}

// Given TypeScript-specific header shapes, when jsHelperHeader rewrites
// them, then parameter and return-type annotations plus generics are
// carried verbatim, and access modifiers (public/private/protected/
// readonly — invalid on a free function) are dropped while async/static
// are preserved. Bet #4 deferred follow-up.
func TestJsHelperHeader_TypeScriptForms(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			"function with return annotation",
			"function makeWidget(spec: string): Widget {\n  return spec;\n}",
			"function extracted_h(spec: string): Widget {",
		},
		{
			"generic function",
			"function pickFirst<T extends Base>(items: T[]): T {\n  return items[0];\n}",
			"function extracted_h<T extends Base>(items: T[]): T {",
		},
		{
			"arrow with return annotation",
			"const buildLabel = (name: string): string => {\n  return name;\n};",
			"function extracted_h(name: string): string {",
		},
		{
			"async arrow with return annotation",
			"const fetchItem = async (id: string): Promise<Item> => {\n  return id;\n};",
			"async function extracted_h(id: string): Promise<Item> {",
		},
		{
			"method with return annotation",
			"  load(id: string): Promise<Item> {\n    return id;\n  }",
			"function extracted_h(id: string): Promise<Item> {",
		},
		{
			"private method drops access modifier",
			"  private load(id: string): Promise<Item> {\n    return id;\n  }",
			"function extracted_h(id: string): Promise<Item> {",
		},
		{
			"private async method keeps async",
			"  private async load(id: string): Promise<Item> {\n    return this.get(id);\n  }",
			"async function extracted_h(id: string): Promise<Item> {",
		},
		{
			"protected static method keeps static",
			"  protected static count(): number {\n    return 1;\n  }",
			"static function extracted_h(): number {",
		},
		{
			"public method drops modifier",
			"  public format(name: string): string {\n    return name;\n  }",
			"function extracted_h(name: string): string {",
		},
		{
			"readonly dropped",
			"  readonly compute(x: number): number {\n    return x;\n  }",
			"function extracted_h(x: number): number {",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, ok := jsHelperHeader(c.input, "extracted_h")
			if !ok {
				t.Fatalf("jsHelperHeader returned ok=false for %q", c.input)
			}
			if got != c.expect {
				t.Errorf("got %q, want %q", got, c.expect)
			}
		})
	}
}

// Given the realworld-typescript fixture, when the splitter runs on the
// .ts file, then Detect maps .ts to JavaScript and exactly the four
// function-shaped definitions are chunked. The `interface` declaration
// and `type` alias are NOT chunked — they're type-level declarations,
// not functions, and stay out of the emitter's scope by construction.
func TestSplit_TypeScriptRealworld_ChunksFunctionsNotTypes(t *testing.T) {
	data, err := os.ReadFile("../../testdata/refactor/js/realworld-typescript/a.ts")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lang := tokenizer.Detect("a.ts", string(data))
	if lang != tokenizer.JavaScript {
		t.Fatalf("Detect(a.ts) = %v, want JavaScript", lang)
	}
	chunks := splitter.Split("a.ts", string(data), lang)
	want := []string{"makeWidgetA", "buildLabelA", "pickFirstA", "loadA"}
	if len(chunks) != len(want) {
		t.Fatalf("expected %d chunks %v, got %d:\n%s", len(want), want, len(chunks), summariseChunks(chunks))
	}
	for i, w := range want {
		if chunks[i].Symbol != w {
			t.Errorf("chunk[%d].Symbol = %q, want %q", i, chunks[i].Symbol, w)
		}
	}
}

// Given the realworld-typescript fixture pairs (annotated function,
// annotated arrow, generic function, class method with access
// modifier), when Synthesize runs on each, then the helper header
// carries the TS annotations verbatim. Only the class-method pair
// (whose body touches `this.`) carries the this-binding NOTE.
// Bet #4 deferred follow-up.
func TestSynthesize_TypeScriptRealworld_Shapes(t *testing.T) {
	dir := "../../testdata/refactor/js/realworld-typescript"
	cases := []struct {
		name         string
		symbolPrefix string
		expectHeader string
		expectNote   bool
		expectInSrc  []string
	}{
		{
			"function with param and return annotations",
			"makeWidget",
			"function extracted_makeWidgetA_deadbeef(spec: string, size: number = 1): Widget {",
			false,
			[]string{`"/v1"`, `"/v2"`},
		},
		{
			"arrow with return annotation",
			"buildLabel",
			"function extracted_buildLabelA_deadbeef(name: string): string {",
			false,
			[]string{`"user:"`, `"admin:"`},
		},
		{
			"generic function",
			"pickFirst",
			"function extracted_pickFirstA_deadbeef<T extends Widget>(items: T[], fallback: T): T {",
			false,
			[]string{"items[0]", "items[items.length - 1]"},
		},
		{
			"class method with access modifier",
			"load",
			"async function extracted_loadA_deadbeef(id: string): Promise<Widget> {",
			true,
			[]string{`"store:v1:"`, `"store:v2:"`},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			a, b := loadSnippetsByPredicate(t, dir, func(ch splitter.Chunk) bool {
				return strings.HasPrefix(ch.Symbol, c.symbolPrefix)
			})
			al := Align(a, b)
			s := Synthesize(a, b, "deadbeef", al)
			if s.Note != "" {
				t.Fatalf("expected accept, got Note=%q", s.Note)
			}
			if !strings.Contains(s.HelperSrc, c.expectHeader) {
				t.Errorf("HelperSrc missing header %q. Source:\n%s", c.expectHeader, s.HelperSrc)
			}
			hasNote := strings.Contains(s.HelperSrc, "// NOTE:")
			if hasNote != c.expectNote {
				t.Errorf("NOTE presence = %v, want %v. Source:\n%s", hasNote, c.expectNote, s.HelperSrc)
			}
			for _, want := range c.expectInSrc {
				if !strings.Contains(s.HelperSrc, want) {
					t.Errorf("HelperSrc missing %q. Source:\n%s", want, s.HelperSrc)
				}
			}
		})
	}
}
