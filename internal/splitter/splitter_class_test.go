package splitter

// Class-span chunking tests (§5.2 class-level granularity). Class-based
// languages emit a KindClass chunk covering the whole class body IN
// ADDITION to the method chunks inside it; the same-file nesting filter
// and the kind gate downstream keep the container/part overlap out of
// reports. Elixir `defmodule` blocks and Rust `impl` blocks are the
// container equivalents and get the same treatment. Go has no
// contiguous container — methods live outside the type block — so its
// "class" chunk is a SYNTHETIC group: the struct decl plus its in-file
// methodset, joined in file order, under the covering line range.

import (
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// chunksByKind partitions chunks into class-kind and everything else.
func chunksByKind(chunks []Chunk) (classes, funcs []Chunk) {
	for _, c := range chunks {
		if c.Kind == KindClass {
			classes = append(classes, c)
		} else {
			funcs = append(funcs, c)
		}
	}
	return classes, funcs
}

func TestSplit_PythonClassSpanEmittedAlongsideMethods(t *testing.T) {
	code := `import os

class Ledger:
    def add(self, x):
        self.total += x
        return self.total

    def sub(self, x):
        self.total -= x
        return self.total

def top_level():
    return 42
`
	chunks := Split("a.py", code, tokenizer.Python)
	classes, funcs := chunksByKind(chunks)
	if len(classes) != 1 {
		t.Fatalf("expected 1 class chunk, got %d: %+v", len(classes), classes)
	}
	cl := classes[0]
	if cl.Symbol != "Ledger" {
		t.Errorf("class chunk symbol = %q, want Ledger", cl.Symbol)
	}
	// Indent-terminated right before top_level; like def chunks, the
	// trailing blank line (11) is included in the span.
	if cl.StartLine != 3 || cl.EndLine != 11 {
		t.Errorf("class span = %d-%d, want 3-11 (indent-terminated before top_level)", cl.StartLine, cl.EndLine)
	}
	if !strings.Contains(cl.Code, "def sub") {
		t.Errorf("class chunk should contain its methods, got:\n%s", cl.Code)
	}
	// Methods must STILL be emitted individually.
	var symbols []string
	for _, f := range funcs {
		symbols = append(symbols, f.Symbol)
	}
	want := []string{"add", "sub", "top_level"}
	if len(symbols) != len(want) {
		t.Fatalf("function chunks = %v, want %v", symbols, want)
	}
	for i := range want {
		if symbols[i] != want[i] {
			t.Errorf("function chunk %d = %q, want %q", i, symbols[i], want[i])
		}
	}
	if funcs[0].Kind != KindFunction {
		t.Errorf("method chunk kind = %q, want %q", funcs[0].Kind, KindFunction)
	}
	// Name renders normally: path:start-end Symbol.
	if got := cl.Name(); got != "a.py:3-11 Ledger" {
		t.Errorf("class chunk Name() = %q, want %q", got, "a.py:3-11 Ledger")
	}
}

func TestSplit_PythonDecoratedClassIncludesDecoratorBlock(t *testing.T) {
	code := `@dataclasses.dataclass(
    frozen=True,
)
class Point:
    def norm(self):
        return self.x + self.y
`
	chunks := Split("a.py", code, tokenizer.Python)
	classes, _ := chunksByKind(chunks)
	if len(classes) != 1 {
		t.Fatalf("expected 1 class chunk, got %d: %+v", len(classes), chunks)
	}
	if classes[0].StartLine != 1 {
		t.Errorf("decorated class chunk should start at the decorator (line 1), got %d", classes[0].StartLine)
	}
	if !strings.Contains(classes[0].Code, "@dataclasses.dataclass") {
		t.Errorf("class chunk should include the decorator block, got:\n%s", classes[0].Code)
	}
}

func TestSplit_PythonNestedClassesEmitBothSpans(t *testing.T) {
	code := `class Outer:
    class Inner:
        def ping(self):
            return "pong"

    def outer_method(self):
        return 1
`
	chunks := Split("a.py", code, tokenizer.Python)
	classes, _ := chunksByKind(chunks)
	if len(classes) != 2 {
		t.Fatalf("expected 2 class chunks (Outer + Inner), got %d: %+v", len(classes), classes)
	}
	if classes[0].Symbol != "Outer" || classes[1].Symbol != "Inner" {
		t.Errorf("class symbols = %q, %q; want Outer, Inner", classes[0].Symbol, classes[1].Symbol)
	}
	if classes[0].StartLine != 1 || classes[0].EndLine != 8 {
		t.Errorf("Outer span = %d-%d, want 1-8 (runs to EOF)", classes[0].StartLine, classes[0].EndLine)
	}
	// Inner terminates at outer_method (same indent); the trailing
	// blank line 5 is included, mirroring def spans.
	if classes[1].StartLine != 2 || classes[1].EndLine != 5 {
		t.Errorf("Inner span = %d-%d, want 2-5", classes[1].StartLine, classes[1].EndLine)
	}
}

func TestSplit_PythonClassWithBaseAndTrailingComment(t *testing.T) {
	// Indent edge cases: a base-class list in the header, a column-0
	// comment inside the body (carries no indent information), and
	// top-level code after the class.
	code := `class Child(Base, metaclass=Meta):
    def one(self):
        return 1

# free comment at column 0
    def two(self):
        return 2

value = 3
`
	chunks := Split("a.py", code, tokenizer.Python)
	classes, _ := chunksByKind(chunks)
	if len(classes) != 1 {
		t.Fatalf("expected 1 class chunk, got %d: %+v", len(classes), classes)
	}
	if classes[0].Symbol != "Child" {
		t.Errorf("class symbol = %q, want Child", classes[0].Symbol)
	}
	if classes[0].EndLine != 8 {
		t.Errorf("class span should end at line 8 (before `value = 3`, trailing blank included), got %d", classes[0].EndLine)
	}
}

func TestSplit_PythonIdentifiersStartingWithClassNotMatched(t *testing.T) {
	code := `classify = make_classifier()

def classify_all(rows):
    out = []
    for r in rows:
        out.append(classify(r))
    return out
`
	chunks := Split("a.py", code, tokenizer.Python)
	classes, _ := chunksByKind(chunks)
	if len(classes) != 0 {
		t.Errorf("`classify = ...` must not be misread as a class header: %+v", classes)
	}
}

func TestSplit_JavaClassSpanEmittedAlongsideMethods(t *testing.T) {
	code := `package com.foo;

public class Calculator {
    public int add(int a, int b) {
        return a + b;
    }

    public int sub(int a, int b) {
        return a - b;
    }
}
`
	chunks := Split("Calculator.java", code, tokenizer.Java)
	classes, funcs := chunksByKind(chunks)
	if len(classes) != 1 {
		t.Fatalf("expected 1 class chunk, got %d: %+v", len(classes), chunks)
	}
	cl := classes[0]
	if cl.Symbol != "Calculator" {
		t.Errorf("class symbol = %q, want Calculator", cl.Symbol)
	}
	if cl.StartLine != 3 || cl.EndLine != 11 {
		t.Errorf("class span = %d-%d, want 3-11", cl.StartLine, cl.EndLine)
	}
	var symbols []string
	for _, f := range funcs {
		symbols = append(symbols, f.Symbol)
	}
	if len(symbols) != 2 || symbols[0] != "add" || symbols[1] != "sub" {
		t.Errorf("method chunks = %v, want [add sub]", symbols)
	}
}

func TestSplit_JavaInterfaceEnumRecordSpansEmitted(t *testing.T) {
	code := `interface Shape {
    double area();
}

enum Color {
    RED,
    GREEN
}

record Point(int x, int y) {
    int sum() {
        return x + y;
    }
}
`
	chunks := Split("Types.java", code, tokenizer.Java)
	classes, _ := chunksByKind(chunks)
	var symbols []string
	for _, c := range classes {
		symbols = append(symbols, c.Symbol)
	}
	want := []string{"Shape", "Color", "Point"}
	if len(symbols) != len(want) {
		t.Fatalf("class-kind chunks = %v, want %v", symbols, want)
	}
	for i := range want {
		if symbols[i] != want[i] {
			t.Errorf("class chunk %d = %q, want %q", i, symbols[i], want[i])
		}
	}
}

func TestSplit_JavaNestedTypeEmitsBothSpans(t *testing.T) {
	code := `public class Outer {
    private static final class Inner {
        int inner() {
            return 2;
        }
    }

    int outer() {
        return 1;
    }
}
`
	chunks := Split("Outer.java", code, tokenizer.Java)
	classes, funcs := chunksByKind(chunks)
	if len(classes) != 2 {
		t.Fatalf("expected 2 class chunks (Outer + Inner), got %d: %+v", len(classes), classes)
	}
	if classes[0].Symbol != "Outer" || classes[1].Symbol != "Inner" {
		t.Errorf("class symbols = %q, %q; want Outer, Inner", classes[0].Symbol, classes[1].Symbol)
	}
	if classes[0].StartLine != 1 || classes[0].EndLine != 11 {
		t.Errorf("Outer span = %d-%d, want 1-11", classes[0].StartLine, classes[0].EndLine)
	}
	if classes[1].StartLine != 2 || classes[1].EndLine != 6 {
		t.Errorf("Inner span = %d-%d, want 2-6", classes[1].StartLine, classes[1].EndLine)
	}
	var symbols []string
	for _, f := range funcs {
		symbols = append(symbols, f.Symbol)
	}
	if len(symbols) != 2 || symbols[0] != "inner" || symbols[1] != "outer" {
		t.Errorf("method chunks = %v, want [inner outer]", symbols)
	}
}

func TestSplit_JSClassSpanEmittedAlongsideMethods(t *testing.T) {
	code := `export default class Cart {
  add(item) {
    this.items.push(item);
    return this.items.length;
  }

  clear() {
    this.items = [];
    return 0;
  }
}

function helper(x) {
  return x * 2;
}
`
	chunks := Split("cart.js", code, tokenizer.JavaScript)
	classes, funcs := chunksByKind(chunks)
	if len(classes) != 1 {
		t.Fatalf("expected 1 class chunk, got %d: %+v", len(classes), chunks)
	}
	cl := classes[0]
	if cl.Symbol != "Cart" {
		t.Errorf("class symbol = %q, want Cart", cl.Symbol)
	}
	if cl.StartLine != 1 || cl.EndLine != 11 {
		t.Errorf("class span = %d-%d, want 1-11", cl.StartLine, cl.EndLine)
	}
	var symbols []string
	for _, f := range funcs {
		symbols = append(symbols, f.Symbol)
	}
	want := []string{"add", "clear", "helper"}
	if len(symbols) != len(want) {
		t.Fatalf("function chunks = %v, want %v", symbols, want)
	}
	for i := range want {
		if symbols[i] != want[i] {
			t.Errorf("function chunk %d = %q, want %q", i, symbols[i], want[i])
		}
	}
}

func TestSplit_JSClassExtendsEmitted(t *testing.T) {
	// Sibling classes, one with an extends clause. (A class declared
	// inside a FUNCTION body is not emitted — function chunks jump past
	// their bodies, mirroring the existing nested-match skipping.)
	code := `class Base {
  ping() {
    return "base";
  }
}

class Widget extends Base {
  render() {
    return "widget";
  }
}
`
	chunks := Split("w.js", code, tokenizer.JavaScript)
	classes, _ := chunksByKind(chunks)
	if len(classes) != 2 {
		t.Fatalf("expected 2 class chunks (Base + Widget), got %d: %+v", len(classes), classes)
	}
	if classes[0].Symbol != "Base" || classes[1].Symbol != "Widget" {
		t.Errorf("class symbols = %q, %q; want Base, Widget", classes[0].Symbol, classes[1].Symbol)
	}
}

func TestSplit_JSClassExpressionNotEmittedAsClassChunk(t *testing.T) {
	// `const A = class { ... }` (class expression) is deliberately NOT
	// emitted as a class chunk: the header shape overlaps the arrow /
	// function-expression matchers and the payoff is marginal — class
	// expressions are rare as clone containers. Documented follow-up in
	// docs/comparative-algorithms-review.md §5.2. The methods inside
	// are still chunked.
	code := `const A = class {
  greet(name) {
    const msg = "hi " + name;
    return msg;
  }
};
`
	chunks := Split("a.js", code, tokenizer.JavaScript)
	classes, funcs := chunksByKind(chunks)
	if len(classes) != 0 {
		t.Errorf("class expression must not become a class chunk, got %+v", classes)
	}
	if len(funcs) != 1 || funcs[0].Symbol != "greet" {
		t.Errorf("method inside a class expression should still be chunked, got %+v", funcs)
	}
}

// ── Go struct+methodset groups ────────────────────────────────────────────────

func TestSplit_GoMethodsetGroupedAcrossInterleavedFunc(t *testing.T) {
	// Go methods live OUTSIDE the type block, so the class chunk is a
	// SYNTHETIC group: type decl + every in-file method of the type,
	// joined in file order. The covering range [StartLine, EndLine]
	// necessarily CONTAINS the unrelated function interleaved between
	// the methods, but its source is excluded from Code — the range
	// over-approximates (documented: --since/blame degrade gracefully),
	// the text does not. Pointer and value receivers unify into one
	// group.
	code := `package p

type Counter struct {
	n int
}

func (c *Counter) Add(x int) int {
	c.n += x
	return c.n
}

func unrelatedHelper(s string) string {
	return s + s
}

func (c Counter) Total() int {
	return c.n
}
`
	chunks := Split("c.go", code, tokenizer.Go)
	classes, funcs := chunksByKind(chunks)
	if len(classes) != 1 {
		t.Fatalf("expected 1 group chunk, got %d: %+v", len(classes), classes)
	}
	g := classes[0]
	if g.Symbol != "Counter" {
		t.Errorf("group symbol = %q, want Counter", g.Symbol)
	}
	// Covering range: type decl (line 3) through last method end (line 18).
	if g.StartLine != 3 || g.EndLine != 18 {
		t.Errorf("group range = %d-%d, want 3-18 (covering, includes the interleaved helper's lines)", g.StartLine, g.EndLine)
	}
	for _, want := range []string{"type Counter struct", "func (c *Counter) Add", "func (c Counter) Total"} {
		if !strings.Contains(g.Code, want) {
			t.Errorf("group Code must contain %q, got:\n%s", want, g.Code)
		}
	}
	if strings.Contains(g.Code, "unrelatedHelper") {
		t.Errorf("group Code must exclude the interleaved unrelated function, got:\n%s", g.Code)
	}
	if got := g.Name(); got != "c.go:3-18 Counter" {
		t.Errorf("group Name() = %q, want %q", got, "c.go:3-18 Counter")
	}
	// Function chunks are unchanged: methods and the helper all emit.
	var symbols []string
	for _, f := range funcs {
		symbols = append(symbols, f.Symbol)
	}
	want := []string{"Add", "unrelatedHelper", "Total"}
	if len(symbols) != len(want) {
		t.Fatalf("function chunks = %v, want %v", symbols, want)
	}
	for i := range want {
		if symbols[i] != want[i] {
			t.Errorf("function chunk %d = %q, want %q", i, symbols[i], want[i])
		}
	}
}

func TestSplit_GoSingleMethodTypeNotGrouped(t *testing.T) {
	// Mirror of Elixir's ≥2-defs rule: a single-method type's group
	// would just duplicate the method finding plus decl boilerplate.
	goCode := `package p

type Counter struct {
	n int
}

func (c *Counter) Add(x int) int {
	c.n += x
	return c.n
}
`
	classes, _ := chunksByKind(Split("c.go", goCode, tokenizer.Go))
	if len(classes) != 0 {
		t.Errorf("a 1-method type must not be grouped, got %+v", classes)
	}
}

func TestSplit_GoInterfaceTypeNotGrouped(t *testing.T) {
	// Interfaces have no methodset in-file (methods can't be declared
	// on an interface receiver), so no group forms.
	goCode := `package p

type Store interface {
	Get(k string) string
	Put(k, v string)
}

func alpha(x int) int {
	return x + 1
}

func beta(x int) int {
	return x + 2
}
`
	classes, _ := chunksByKind(Split("s.go", goCode, tokenizer.Go))
	if len(classes) != 0 {
		t.Errorf("interface types must not be grouped, got %+v", classes)
	}
}

func TestSplit_GoMethodsWithoutInFileDeclNotGrouped(t *testing.T) {
	// Grouping is decl-anchored: the Go "class" is the struct decl PLUS
	// its in-file methods, so a methods-only file (type declared in a
	// sibling file — a common Go layout) gets no group. Documented
	// limitation: it keeps the chunk count proportional to type
	// definitions rather than to every file that adds a method.
	goCode := `package p

func (c *Counter) Add(x int) int {
	c.n += x
	return c.n
}

func (c *Counter) Sub(x int) int {
	c.n -= x
	return c.n
}
`
	classes, _ := chunksByKind(Split("c.go", goCode, tokenizer.Go))
	if len(classes) != 0 {
		t.Errorf("methods without an in-file type decl must not be grouped, got %+v", classes)
	}
}

func TestSplit_GoGenericTypeAndReceiversGrouped(t *testing.T) {
	code := `package p

type Pair[K comparable, V any] struct {
	k K
	v V
}

func (p *Pair[K, V]) Key() K {
	return p.k
}

func (p Pair[K, V]) Value() V {
	return p.v
}
`
	classes, _ := chunksByKind(Split("p.go", code, tokenizer.Go))
	if len(classes) != 1 || classes[0].Symbol != "Pair" {
		t.Fatalf("expected 1 group chunk for the generic type Pair, got %+v", classes)
	}
	if classes[0].StartLine != 3 || classes[0].EndLine != 14 {
		t.Errorf("group range = %d-%d, want 3-14", classes[0].StartLine, classes[0].EndLine)
	}
}

// ── Rust impl spans ───────────────────────────────────────────────────────────

func TestSplit_RustImplSpanEmittedAlongsideFns(t *testing.T) {
	code := `pub struct Point {
    x: i64,
    y: i64,
}

impl Point {
    pub fn manhattan(&self) -> i64 {
        self.x.abs() + self.y.abs()
    }

    fn scale(&mut self, k: i64) {
        self.x *= k;
        self.y *= k;
    }
}

fn free_fn(v: i64) -> i64 {
    v * 2
}
`
	chunks := Split("p.rs", code, tokenizer.Rust)
	classes, funcs := chunksByKind(chunks)
	if len(classes) != 1 {
		t.Fatalf("expected 1 impl chunk, got %d: %+v", len(classes), chunks)
	}
	cl := classes[0]
	if cl.Symbol != "Point" {
		t.Errorf("impl chunk symbol = %q, want Point", cl.Symbol)
	}
	if cl.StartLine != 6 || cl.EndLine != 15 {
		t.Errorf("impl span = %d-%d, want 6-15 (through the closing brace)", cl.StartLine, cl.EndLine)
	}
	if !strings.Contains(cl.Code, "fn scale") {
		t.Errorf("impl chunk should contain its fns, got:\n%s", cl.Code)
	}
	var symbols []string
	for _, f := range funcs {
		symbols = append(symbols, f.Symbol)
	}
	want := []string{"manhattan", "scale", "free_fn"}
	if len(symbols) != len(want) {
		t.Fatalf("fn chunks = %v, want %v", symbols, want)
	}
	for i := range want {
		if symbols[i] != want[i] {
			t.Errorf("fn chunk %d = %q, want %q", i, symbols[i], want[i])
		}
	}
}

func TestSplit_RustTraitImplSymbolIsTypeName(t *testing.T) {
	// `impl Trait for Foo` carries the TYPE name as symbol — so an
	// inherent impl and a trait impl of the same type share a symbol
	// (their chunk Names stay distinct via the line ranges).
	code := `impl Display for Point {
    fn fmt(&self, f: &mut Formatter) -> Result {
        write!(f, "({}, {})", self.x, self.y)
    }
}

impl Point {
    fn norm(&self) -> i64 {
        self.x + self.y
    }
}
`
	classes, _ := chunksByKind(Split("p.rs", code, tokenizer.Rust))
	if len(classes) != 2 {
		t.Fatalf("expected 2 impl chunks, got %d: %+v", len(classes), classes)
	}
	if classes[0].Symbol != "Point" || classes[1].Symbol != "Point" {
		t.Errorf("impl symbols = %q, %q; want Point, Point (type name, not trait)", classes[0].Symbol, classes[1].Symbol)
	}
	if classes[0].StartLine != 1 || classes[0].EndLine != 5 {
		t.Errorf("trait impl span = %d-%d, want 1-5", classes[0].StartLine, classes[0].EndLine)
	}
}

func TestSplit_RustGenericImplHandled(t *testing.T) {
	code := `impl<T: Clone> Wrapper<T> {
    fn get(&self) -> &T {
        &self.value
    }
}

impl<T> From<T> for Holder<T> {
    fn from(value: T) -> Self {
        Holder { value }
    }
}
`
	classes, _ := chunksByKind(Split("g.rs", code, tokenizer.Rust))
	if len(classes) != 2 {
		t.Fatalf("expected 2 impl chunks, got %d: %+v", len(classes), classes)
	}
	if classes[0].Symbol != "Wrapper" {
		t.Errorf("generic inherent impl symbol = %q, want Wrapper", classes[0].Symbol)
	}
	if classes[1].Symbol != "Holder" {
		t.Errorf("generic trait impl symbol = %q, want Holder (type, not trait)", classes[1].Symbol)
	}
}

func TestSplit_RustImplInsideFnBodyNotEmitted(t *testing.T) {
	// fn chunks jump past their bodies, so an impl declared inside a
	// function is not emitted — same rule as a JS class inside a
	// function body or an Elixir defmodule inside a def.
	code := `fn build() -> i64 {
    struct Local {
        v: i64,
    }
    impl Local {
        fn get(&self) -> i64 {
            self.v
        }
        fn set(&mut self, v: i64) {
            self.v = v;
        }
    }
    let l = Local { v: 3 };
    l.get()
}
`
	chunks := Split("b.rs", code, tokenizer.Rust)
	classes, funcs := chunksByKind(chunks)
	if len(classes) != 0 {
		t.Errorf("impl inside a fn body must not be emitted, got %+v", classes)
	}
	if len(funcs) != 1 || funcs[0].Symbol != "build" {
		t.Errorf("fn chunks = %+v, want just [build]", funcs)
	}
}

// ── Elixir defmodule spans ────────────────────────────────────────────────────

func TestSplit_ElixirModuleSpanEmittedAlongsideDefs(t *testing.T) {
	code := `defmodule Billing.Ledger do
  def add(n, x) do
    n + x
  end

  defp sub(n, x) do
    n - x
  end
end

IO.puts("after")
`
	chunks := Split("ledger.ex", code, tokenizer.Elixir)
	classes, funcs := chunksByKind(chunks)
	if len(classes) != 1 {
		t.Fatalf("expected 1 module chunk, got %d: %+v", len(classes), chunks)
	}
	mod := classes[0]
	// Symbol is the full dotted module name; span includes the closing
	// `end` (line 9) but not the trailing top-level code.
	if mod.Symbol != "Billing.Ledger" {
		t.Errorf("module chunk symbol = %q, want Billing.Ledger", mod.Symbol)
	}
	if mod.StartLine != 1 || mod.EndLine != 9 {
		t.Errorf("module span = %d-%d, want 1-9 (through the closing end)", mod.StartLine, mod.EndLine)
	}
	if !strings.Contains(mod.Code, "defp sub") || !strings.HasSuffix(strings.TrimSpace(mod.Code), "end") {
		t.Errorf("module chunk should contain its defs and the closing end, got:\n%s", mod.Code)
	}
	if got := mod.Name(); got != "ledger.ex:1-9 Billing.Ledger" {
		t.Errorf("module chunk Name() = %q, want %q", got, "ledger.ex:1-9 Billing.Ledger")
	}
	// The defs inside must STILL be emitted exactly as before.
	var symbols []string
	for _, f := range funcs {
		symbols = append(symbols, f.Symbol)
	}
	if len(symbols) != 2 || symbols[0] != "add" || symbols[1] != "sub" {
		t.Fatalf("def chunks = %v, want [add sub]", symbols)
	}
	if funcs[0].StartLine != 2 || funcs[0].EndLine != 4 {
		t.Errorf("add span = %d-%d, want 2-4 (unchanged by module emission)", funcs[0].StartLine, funcs[0].EndLine)
	}
	if funcs[0].Kind != KindFunction {
		t.Errorf("def chunk kind = %q, want %q", funcs[0].Kind, KindFunction)
	}
}

func TestSplit_ElixirNestedModulesEmitBothSpans(t *testing.T) {
	// Mirrors Java's nested-type behavior: both the outer and the inner
	// module get a span, and the defs inside remain individual chunks.
	// (Both modules aggregate ≥ 2 defs; nested defs count toward the
	// outer span too.)
	code := `defmodule Outer do
  defmodule Inner do
    def ping(x) do
      x
    end

    def pong(y) do
      y
    end
  end

  def outer_fun(y) do
    y * 2
  end
end
`
	chunks := Split("o.ex", code, tokenizer.Elixir)
	classes, funcs := chunksByKind(chunks)
	if len(classes) != 2 {
		t.Fatalf("expected 2 module chunks (Outer + Inner), got %d: %+v", len(classes), classes)
	}
	if classes[0].Symbol != "Outer" || classes[1].Symbol != "Inner" {
		t.Errorf("module symbols = %q, %q; want Outer, Inner", classes[0].Symbol, classes[1].Symbol)
	}
	if classes[0].StartLine != 1 || classes[0].EndLine != 15 {
		t.Errorf("Outer span = %d-%d, want 1-15", classes[0].StartLine, classes[0].EndLine)
	}
	if classes[1].StartLine != 2 || classes[1].EndLine != 10 {
		t.Errorf("Inner span = %d-%d, want 2-10", classes[1].StartLine, classes[1].EndLine)
	}
	var symbols []string
	for _, f := range funcs {
		symbols = append(symbols, f.Symbol)
	}
	if len(symbols) != 3 || symbols[0] != "ping" || symbols[1] != "pong" || symbols[2] != "outer_fun" {
		t.Errorf("def chunks = %v, want [ping pong outer_fun]", symbols)
	}
}

func TestSplit_ElixirSingleDefModuleNotSpanChunked(t *testing.T) {
	// The §5.2 value-add is aggregation: a module span matters when a
	// copied module's reordered defs would scatter across method-level
	// pairs. A single-def module aggregates nothing — its span is the
	// def plus defmodule/end boilerplate, which duplicates the def
	// finding while inflating similarity. Elixir's pervasive
	// one-callback modules (`use GenServer` + one handle_*) would
	// otherwise pair up as module↔module near-noise (the
	// negative-short bench fixture pins the report-level contract).
	code := `defmodule MetricsSink do
  use GenServer

  def handle_cast({:record, name, value}, state) do
    {:noreply, Map.update(state, name, value, &(&1 + value))}
  end
end
`
	chunks := Split("m.ex", code, tokenizer.Elixir)
	classes, funcs := chunksByKind(chunks)
	if len(classes) != 0 {
		t.Errorf("single-def module must not be span-chunked, got %+v", classes)
	}
	if len(funcs) != 1 || funcs[0].Symbol != "handle_cast" {
		t.Errorf("the def itself must still be chunked, got %+v", funcs)
	}
}

func TestSplit_ElixirFileWithoutModuleUnchanged(t *testing.T) {
	// A file with no defmodule (a plain .exs script) keeps today's
	// behavior: no definitions → whole-file fallback, function kind.
	code := `IO.puts("hello")
x = 1 + 2
IO.inspect(x)
`
	chunks := Split("script.exs", code, tokenizer.Elixir)
	if len(chunks) != 1 || chunks[0].Symbol != "" || chunks[0].StartLine != 1 {
		t.Fatalf("expected the whole-file fallback chunk, got %+v", chunks)
	}
	if chunks[0].Kind != KindFunction {
		t.Errorf("fallback chunk kind = %q, want %q", chunks[0].Kind, KindFunction)
	}
}

func TestSplit_ElixirModuleInsideDefNotEmitted(t *testing.T) {
	// A defmodule generated inside a def/defmacro body (e.g. inside a
	// `quote` block) is weird-but-legal Elixir. Def chunks jump past
	// their bodies, so the inner defmodule is NOT emitted — mirroring
	// the JS rule that a class declared inside a function body gets no
	// class chunk. The def itself is emitted as usual.
	code := `defmodule Factory do
  defmacro build(name) do
    quote do
      defmodule unquote(name) do
        def make(x) do
          x
        end
      end
    end
  end
end
`
	chunks := Split("f.ex", code, tokenizer.Elixir)
	classes, funcs := chunksByKind(chunks)
	var symbols []string
	for _, c := range classes {
		symbols = append(symbols, c.Symbol)
	}
	if len(classes) != 1 || classes[0].Symbol != "Factory" {
		t.Errorf("only the top-level module should get a span, got %v", symbols)
	}
	if len(funcs) != 1 || funcs[0].Symbol != "build" {
		t.Errorf("def chunks = %+v, want just [build]", funcs)
	}
}

func TestSplit_ElixirModuleShorthandFormNotSpanChunked(t *testing.T) {
	// Only the block form (`defmodule Foo do … end`) is span-chunked.
	// The `defmodule Foo, do: …` shorthand — legal but not a shape that
	// occurs in real-world code — is skipped rather than risking a
	// misparse; the (equally unrealistic) defs it could carry are out of
	// scope. Documented limitation.
	code := `defmodule Tiny, do: :ok

defmodule Real do
  def run(x) do
    x
  end

  def rerun(x) do
    run(x)
  end
end
`
	chunks := Split("t.ex", code, tokenizer.Elixir)
	classes, _ := chunksByKind(chunks)
	if len(classes) != 1 || classes[0].Symbol != "Real" {
		t.Errorf("shorthand defmodule must not be span-chunked, got %+v", classes)
	}
}
