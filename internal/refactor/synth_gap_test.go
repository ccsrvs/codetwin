package refactor

// Coverage-gap tests for the JS and Rust header / body helpers added in
// commits bffa03c (JS/TS emitter) and 98fc9e6 (Rust emitter). Each
// test pins a defensive branch that the fixture-driven happy-path
// suite doesn't exercise — typically a malformed input shape, a less
// common modifier path, or an early-out rejection branch. Keeping them
// in their own file makes the gap explicit and easy to prune if a
// future emitter rework removes the underlying branch.

import (
	"strings"
	"testing"
)

// ── Rust gap tests ───────────────────────────────────────────────────

// Given a Rust line whose first non-skipped form is not a fn header
// (e.g. a `let` binding), when rsHelperHeader runs, then it returns
// ok=false. Pins the early-rejection path on line 358.
func TestRsHelperHeader_FirstNonSkippedLineNotFn_Rejects(t *testing.T) {
	if got, ok := rsHelperHeader("let x = 1;\n", "extracted_h"); ok {
		t.Errorf("expected ok=false on non-fn first line, got %q", got)
	}
}

// Given `fn ` followed by a non-identifier character (anonymous closure
// shape), when rsHelperHeader runs, then it returns ok=false. Pins the
// nameEnd==0 path on line 366.
func TestRsHelperHeader_FnWithoutName_Rejects(t *testing.T) {
	if got, ok := rsHelperHeader("fn (x: i32) -> i32 { x }\n", "extracted_h"); ok {
		t.Errorf("expected ok=false on anonymous fn, got %q", got)
	}
}

// Given Rust lines using `extern fn` or `const fn` modifiers, when
// rsFindFnKeyword runs, then the offset of `fn` is computed correctly.
// Pins the extern/const switch arms.
func TestRsFindFnKeyword_ExternAndConst(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		expect int
	}{
		{"extern fn", "extern fn raw() {}", len("extern ")},
		{"const fn", "const fn compute() {}", len("const ")},
		{"pub extern fn", "pub extern fn raw() {}", len("pub extern ")},
		{"pub const unsafe fn", "pub const unsafe fn raw() {}", len("pub const unsafe ")},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := rsFindFnKeyword(c.line); got != c.expect {
				t.Errorf("got %d, want %d", got, c.expect)
			}
		})
	}
}

// Given a Rust line that doesn't start with any modifier or `fn`, when
// rsFindFnKeyword runs, then it returns -1. Pins the default switch
// arm on line 412.
func TestRsFindFnKeyword_UnrelatedLineReturnsMinusOne(t *testing.T) {
	if got := rsFindFnKeyword("let x = 1;"); got != -1 {
		t.Errorf("expected -1 for non-fn line, got %d", got)
	}
}

// Given a `pub(` with no closing `)` anywhere in the line, when
// rsFindFnKeyword runs, then it returns -1. Pins the closeParen<0
// path on line 391. (The earlier draft used "pub(crate fn foo()" but
// that string DOES contain a `)` at the end — which made the function
// chew past the `fn` and bottom out at the default arm, not the
// no-`)` arm we want to pin.)
func TestRsFindFnKeyword_UnterminatedPubVisibilityReturnsMinusOne(t *testing.T) {
	if got := rsFindFnKeyword("pub(crate fn foo"); got != -1 {
		t.Errorf("expected -1 for unterminated pub(, got %d", got)
	}
}

// Given a Rust snippet where the header's `{` and inline body appear on
// the same line, when rsRebodyAsHelper runs, then the inline content
// becomes the first body line. Pins the afterBrace!="" path on
// line 460.
func TestRsRebodyAsHelper_InlineFirstStatement(t *testing.T) {
	got := rsRebodyAsHelper("fn f(x: i32) -> i32 { x }")
	if !strings.Contains(got, "x }") {
		t.Errorf("expected inline body to surface; got:\n%q", got)
	}
}

// Given a Rust snippet with no `{`, when rsRebodyAsHelper runs, then
// the source is returned verbatim (with trailing newline). Pins the
// openIdx<0 fallback on line 453.
func TestRsRebodyAsHelper_NoOpenBraceReturnsSourceVerbatim(t *testing.T) {
	got := rsRebodyAsHelper("fn f(x: i32) -> i32;\n")
	if !strings.Contains(got, "fn f(x: i32) -> i32;") {
		t.Errorf("expected source returned when no { present; got:\n%q", got)
	}
}

// Given a Rust snippet led by a crate-level `#![allow(...)]` attribute
// (rare on a chunk but possible), when rsRebodyAsHelper picks the
// header indent, then it skips the attribute and uses the fn line's
// indent. Pins the `#![` skip on line 434.
func TestRsRebodyAsHelper_SkipsCrateLevelAttribute(t *testing.T) {
	got := rsRebodyAsHelper("#![allow(dead_code)]\nfn f(x: i32) -> i32 {\n    x\n}")
	if !strings.Contains(got, "    x") {
		t.Errorf("expected body to dedent off fn line, not the attribute; got:\n%q", got)
	}
}

// ── JS / TS gap tests ────────────────────────────────────────────────

// Given a JS first non-comment line that's neither a function, arrow
// assignment, nor class method (e.g. a top-level `let` binding), when
// jsHelperHeader runs, then it returns ok=false after all three
// rewriters reject. Pins the all-rewriters-failed path on line 560.
func TestJsHelperHeader_NoRewriterMatches_Rejects(t *testing.T) {
	if got, ok := jsHelperHeader("let x = 1;\n", "extracted_h"); ok {
		t.Errorf("expected ok=false on non-fn first line, got %q", got)
	}
}

// Given a class-method line whose name is a JS reserved word (e.g. a
// stray `if (cond) { ... }` mistakenly fed in), when jsRewriteClassMethod
// runs, then it returns ok=false. Pins jsClassMethodReservedNames
// rejection on line 600.
func TestJsRewriteClassMethod_ReservedNameRejects(t *testing.T) {
	if got, ok := jsRewriteClassMethod("  if (cond) {", "extracted_h"); ok {
		t.Errorf("expected ok=false for reserved name, got %q", got)
	}
}

// Given a class-method-shape line whose ident isn't followed by `(`
// (e.g. a property shorthand), when jsRewriteClassMethod runs, then it
// returns ok=false. Pins the no-paren rejection on line 604. Inputs
// arrive trimmed at this layer (jsHelperHeader passes
// strings.TrimSpace), so the test omits leading whitespace.
func TestJsRewriteClassMethod_NoOpenParenRejects(t *testing.T) {
	if got, ok := jsRewriteClassMethod("foo: 1,", "extracted_h"); ok {
		t.Errorf("expected ok=false when no `(` after name, got %q", got)
	}
}

// Given a class-method line with only `async`/`static` modifiers and
// no identifier, when jsRewriteClassMethod runs, then it returns
// ok=false. Pins the nameEnd==0 path on line 596.
func TestJsRewriteClassMethod_OnlyModifiersRejects(t *testing.T) {
	if got, ok := jsRewriteClassMethod("async ", "extracted_h"); ok {
		t.Errorf("expected ok=false on modifier-only line, got %q", got)
	}
}

// Given an arrow-style line lead by `export default`, when
// jsRewriteArrowOrFuncExpr runs, then the prefix is preserved. Pins
// the `export default` path on line 681.
func TestJsRewriteArrowOrFuncExpr_ExportDefaultArrow(t *testing.T) {
	got, ok := jsRewriteArrowOrFuncExpr("export default const f = (x) => {", "extracted_h")
	if !ok {
		t.Fatalf("expected accept, got ok=false")
	}
	if !strings.HasPrefix(got, "export default function extracted_h") {
		t.Errorf("expected `export default function` prefix; got %q", got)
	}
}

// Given a `var name = (...) => {` arrow assignment, when
// jsRewriteArrowOrFuncExpr runs, then it normalises to a free
// function. Pins the `var ` switch arm on line 693.
func TestJsRewriteArrowOrFuncExpr_VarArrow(t *testing.T) {
	got, ok := jsRewriteArrowOrFuncExpr("var f = (x) => {", "extracted_h")
	if !ok {
		t.Fatalf("expected accept, got ok=false")
	}
	if got != "function extracted_h(x) {" {
		t.Errorf("got %q, want %q", got, "function extracted_h(x) {")
	}
}

// Given a `const name = function nested(args) {…}` form (where the
// inner function expression carries its own name), when
// jsRewriteArrowOrFuncExpr runs, then the nested name is stripped and
// the helper takes its place. Pins the named-function-expression path
// on line 720.
func TestJsRewriteArrowOrFuncExpr_NamedFunctionExpression(t *testing.T) {
	got, ok := jsRewriteArrowOrFuncExpr("const f = function nested(x) {", "extracted_h")
	if !ok {
		t.Fatalf("expected accept, got ok=false")
	}
	if got != "function extracted_h(x) {" {
		t.Errorf("got %q, want %q", got, "function extracted_h(x) {")
	}
}

// Given a `const name = function nested<T>` (no parens after the
// nested name's identifier), when jsRewriteArrowOrFuncExpr runs, then
// it rejects. Pins the no-paren path on line 728.
func TestJsRewriteArrowOrFuncExpr_NamedFnExprWithoutParensRejects(t *testing.T) {
	if got, ok := jsRewriteArrowOrFuncExpr("const f = function nested<T>", "extracted_h"); ok {
		t.Errorf("expected ok=false, got %q", got)
	}
}

// Given a malformed `const x =` with nothing after, when
// jsRewriteArrowOrFuncExpr runs, then it rejects. Pins the no-paren
// fallthrough on line 734.
func TestJsRewriteArrowOrFuncExpr_NoFunctionNoParenRejects(t *testing.T) {
	if got, ok := jsRewriteArrowOrFuncExpr("const f = 42", "extracted_h"); ok {
		t.Errorf("expected ok=false, got %q", got)
	}
}

// Given an arrow form whose param list never closes, when
// jsRewriteArrowOrFuncExpr runs, then it rejects. Pins line 738.
func TestJsRewriteArrowOrFuncExpr_UnterminatedParamsRejects(t *testing.T) {
	if got, ok := jsRewriteArrowOrFuncExpr("const f = (x", "extracted_h"); ok {
		t.Errorf("expected ok=false on unterminated params, got %q", got)
	}
}

// Given `const name = (params)` without the `=>` arrow, when
// jsRewriteArrowOrFuncExpr runs, then it rejects. Pins line 743.
func TestJsRewriteArrowOrFuncExpr_NoArrowRejects(t *testing.T) {
	if got, ok := jsRewriteArrowOrFuncExpr("const f = (x) something", "extracted_h"); ok {
		t.Errorf("expected ok=false when no `=>`, got %q", got)
	}
}

// Given `const ` with no identifier after, when
// jsRewriteArrowOrFuncExpr runs, then it rejects. Pins line 703.
func TestJsRewriteArrowOrFuncExpr_NoNameAfterConstRejects(t *testing.T) {
	if got, ok := jsRewriteArrowOrFuncExpr("const  = (x) => {}", "extracted_h"); ok {
		t.Errorf("expected ok=false on missing name, got %q", got)
	}
}

// Given `const name x` — name then non-`=` token — when
// jsRewriteArrowOrFuncExpr runs, then it rejects. Pins line 707.
func TestJsRewriteArrowOrFuncExpr_NoEqualsAfterNameRejects(t *testing.T) {
	if got, ok := jsRewriteArrowOrFuncExpr("const f x", "extracted_h"); ok {
		t.Errorf("expected ok=false when no `=`, got %q", got)
	}
}

// Given anonymous `function (args)` form (no identifier after
// `function`), when jsRewriteFunctionHeader runs, then it rejects.
// Pins the nameEnd==0 path on line 779.
func TestJsRewriteFunctionHeader_AnonymousFunctionRejects(t *testing.T) {
	if got, ok := jsRewriteFunctionHeader("function (x) {", "extracted_h"); ok {
		t.Errorf("expected ok=false on anonymous function, got %q", got)
	}
}

// Given a JS snippet with no `{` at all, when jsRebodyAsHelper runs,
// then the source returns verbatim (with trailing newline). Pins the
// openIdx<0 fallback on line 646.
func TestJsRebodyAsHelper_NoOpenBraceReturnsSourceVerbatim(t *testing.T) {
	got := jsRebodyAsHelper("function f();\n")
	if !strings.Contains(got, "function f();") {
		t.Errorf("expected source returned when no { present; got:\n%q", got)
	}
}

// Given a JS snippet that leads with a blank line before the
// definition, when jsRebodyAsHelper runs, then it skips past the blank
// before deriving the header indent. Pins the blank-line-skip branch
// on line 627.
func TestJsRebodyAsHelper_LeadingBlankLineIsSkipped(t *testing.T) {
	got := jsRebodyAsHelper("\nfunction f(x) {\n  return x;\n}")
	if !strings.Contains(got, "  return x;") {
		t.Errorf("expected body to surface despite leading blank; got:\n%q", got)
	}
}
