package splitter

// Class-span chunking tests (§5.2 class-level granularity). Class-based
// languages emit a KindClass chunk covering the whole class body IN
// ADDITION to the method chunks inside it; the same-file nesting filter
// and the kind gate downstream keep the container/part overlap out of
// reports. Go/Rust (struct+methodset grouping — methods live outside
// the type block) and Elixir defmodule are out of scope; see
// docs/comparative-algorithms-review.md §5.2.

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

func TestSplit_GoAndElixirEmitNoClassChunks(t *testing.T) {
	// Go "class-level" would mean struct+methodset symbol grouping
	// (methods live outside the type block) and Elixir defmodule
	// grouping — both out of scope for span-based class chunks; noted
	// as follow-ups in docs/comparative-algorithms-review.md §5.2.
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
		t.Errorf("Go must not emit class chunks, got %+v", classes)
	}
	exCode := `defmodule Counter do
  def add(n, x) do
    n + x
  end
end
`
	classes, _ = chunksByKind(Split("c.ex", exCode, tokenizer.Elixir))
	if len(classes) != 0 {
		t.Errorf("Elixir must not emit class chunks, got %+v", classes)
	}
}
