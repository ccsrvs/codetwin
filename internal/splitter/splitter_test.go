package splitter

import (
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

func TestSplit_PythonTopLevelFunctions(t *testing.T) {
	code := `import os

def foo():
    return 1


def bar(x):
    return x + 1
`
	chunks := Split("a.py", code, tokenizer.Python)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	names := []string{chunks[0].Symbol, chunks[1].Symbol}
	if names[0] != "foo" || names[1] != "bar" {
		t.Errorf("expected symbols [foo bar], got %v", names)
	}
	if chunks[0].StartLine != 3 {
		t.Errorf("foo chunk should start at line 3 (after the import + blank), got %d", chunks[0].StartLine)
	}
	if !strings.Contains(chunks[0].Code, "def foo()") {
		t.Errorf("first chunk code missing def line: %q", chunks[0].Code)
	}
}

func TestSplit_PythonClassMethodsBecomeChunks(t *testing.T) {
	code := `class Foo:
    def __init__(self):
        self.x = 1

    def method(self):
        return self.x

def top_level():
    pass
`
	chunks := Split("a.py", code, tokenizer.Python)
	// Expect: __init__, method, top_level (class itself isn't a def, skipped)
	got := make([]string, len(chunks))
	for i, c := range chunks {
		got[i] = c.Symbol
	}
	want := []string{"__init__", "method", "top_level"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSplit_PythonImportOnlyFileFallsBackToWholeFile(t *testing.T) {
	code := `from pathlib import Path
import pytest
`
	chunks := Split("a.py", code, tokenizer.Python)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 whole-file chunk, got %d", len(chunks))
	}
	if chunks[0].Symbol != "" {
		t.Errorf("whole-file fallback should have empty Symbol, got %q", chunks[0].Symbol)
	}
	if chunks[0].StartLine != 1 {
		t.Errorf("whole-file chunk should start at line 1, got %d", chunks[0].StartLine)
	}
}

func TestSplit_PythonAsyncDef(t *testing.T) {
	code := `async def fetch(url):
    return await get(url)
`
	chunks := Split("a.py", code, tokenizer.Python)
	if len(chunks) != 1 || chunks[0].Symbol != "fetch" {
		t.Errorf("async def not detected: got %+v", chunks)
	}
}

func TestSplit_PythonMultiLineSignatureIncludesBody(t *testing.T) {
	// A Black-formatted multi-line signature puts the closing `):` at the
	// same indent as the def line. The indent-based body-end heuristic used
	// to fire on that line and drop the entire body from the chunk.
	code := `class Foo:
    async def handle(
        self,
        msg: str,
        ctx: dict = {"k": "v"},
    ):
        if msg:
            return ctx[msg]
        return None

    def other(self):
        pass
`
	chunks := Split("a.py", code, tokenizer.Python)
	var handle *Chunk
	for i := range chunks {
		if chunks[i].Symbol == "handle" {
			handle = &chunks[i]
			break
		}
	}
	if handle == nil {
		t.Fatalf("expected a chunk named 'handle', got %+v", chunks)
	}
	for _, want := range []string{"if msg:", "return ctx[msg]", "return None"} {
		if !strings.Contains(handle.Code, want) {
			t.Errorf("body line %q missing from handle chunk:\n%s", want, handle.Code)
		}
	}
	// And the chunk must not bleed into the next method.
	if strings.Contains(handle.Code, "def other") {
		t.Errorf("handle chunk should stop before 'def other':\n%s", handle.Code)
	}
}

func TestSplit_PythonSingleLineDecoratorIncluded(t *testing.T) {
	code := `@cached
def fetch():
    return 42
`
	chunks := Split("a.py", code, tokenizer.Python)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	c := chunks[0]
	if c.Symbol != "fetch" {
		t.Errorf("expected symbol 'fetch', got %q", c.Symbol)
	}
	if c.StartLine != 1 {
		t.Errorf("StartLine should point at the decorator (line 1), got %d", c.StartLine)
	}
	if !strings.Contains(c.Code, "@cached") {
		t.Errorf("decorator missing from chunk Code:\n%s", c.Code)
	}
	if !strings.Contains(c.Code, "return 42") {
		t.Errorf("body missing from chunk Code:\n%s", c.Code)
	}
}

func TestSplit_PythonStackedDecoratorsIncluded(t *testing.T) {
	code := `@cached
@retry(3)
@logged
def handle(x):
    return x
`
	chunks := Split("a.py", code, tokenizer.Python)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	c := chunks[0]
	if c.StartLine != 1 {
		t.Errorf("StartLine should be 1 (the first decorator), got %d", c.StartLine)
	}
	for _, want := range []string{"@cached", "@retry(3)", "@logged", "def handle"} {
		if !strings.Contains(c.Code, want) {
			t.Errorf("expected %q in chunk Code:\n%s", want, c.Code)
		}
	}
}

func TestSplit_PythonMultiLineDecoratorIncluded(t *testing.T) {
	code := `@retry(
    attempts=3,
    backoff=1.5,
)
async def fetch(url: str):
    return await get(url)
`
	chunks := Split("a.py", code, tokenizer.Python)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	c := chunks[0]
	if c.StartLine != 1 {
		t.Errorf("StartLine should point at @retry (line 1), got %d", c.StartLine)
	}
	for _, want := range []string{"@retry(", "attempts=3,", "backoff=1.5,", "async def fetch"} {
		if !strings.Contains(c.Code, want) {
			t.Errorf("expected %q in chunk Code:\n%s", want, c.Code)
		}
	}
}

func TestSplit_PythonDecoratorOnMethod(t *testing.T) {
	code := `class Foo:
    @property
    def value(self):
        return self._value

    @staticmethod
    def helper(x):
        return x + 1
`
	chunks := Split("a.py", code, tokenizer.Python)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "value" {
		t.Errorf("first chunk should be 'value', got %q", chunks[0].Symbol)
	}
	if !strings.Contains(chunks[0].Code, "@property") {
		t.Errorf("@property missing from value chunk:\n%s", chunks[0].Code)
	}
	if chunks[1].Symbol != "helper" {
		t.Errorf("second chunk should be 'helper', got %q", chunks[1].Symbol)
	}
	if !strings.Contains(chunks[1].Code, "@staticmethod") {
		t.Errorf("@staticmethod missing from helper chunk:\n%s", chunks[1].Code)
	}
	// The @property line shouldn't bleed into helper's chunk.
	if strings.Contains(chunks[1].Code, "@property") {
		t.Errorf("helper chunk should not contain @property:\n%s", chunks[1].Code)
	}
}

func TestSplit_PythonDecoratorsDoNotLeakAcrossUnrelatedCode(t *testing.T) {
	// A decorator followed by non-def, non-comment code is invalid Python,
	// but we should be defensive: don't attach the orphaned decorator to
	// the next def we encounter.
	code := `@cached
some_var = 1

def foo():
    return 1
`
	chunks := Split("a.py", code, tokenizer.Python)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	c := chunks[0]
	if c.Symbol != "foo" {
		t.Errorf("expected symbol 'foo', got %q", c.Symbol)
	}
	if strings.Contains(c.Code, "@cached") {
		t.Errorf("orphaned @cached should not attach to foo:\n%s", c.Code)
	}
	if c.StartLine == 1 {
		t.Errorf("StartLine should not be the decorator line; got %d", c.StartLine)
	}
}

func TestSplit_PythonDecoratorAffectsSimilarityTokens(t *testing.T) {
	// Two functions with identical bodies but different decorators must
	// produce different token streams now that decorators are in Code.
	withProperty := `class A:
    @property
    def x(self):
        return self._x
`
	withoutProperty := `class B:
    def x(self):
        return self._x
`
	a := Split("a.py", withProperty, tokenizer.Python)[0]
	b := Split("b.py", withoutProperty, tokenizer.Python)[0]
	if a.Code == b.Code {
		t.Errorf("decorator should be reflected in chunk Code; got identical:\n%s", a.Code)
	}
	if !strings.Contains(a.Code, "@property") {
		t.Errorf("decorator missing from a.Code:\n%s", a.Code)
	}
	if strings.Contains(b.Code, "@property") {
		t.Errorf("b should not contain @property:\n%s", b.Code)
	}
}

func TestSplit_PythonStringsDontFoolSignatureScanner(t *testing.T) {
	// Parens / colons inside string literals or comments must not confuse
	// the multi-line-signature detector into ending the signature early
	// (or running past it into the body).
	code := `def foo(
    s: str = "hello: ((world))",  # default with parens
    t: str = '):',
):
    return (s, t)
`
	chunks := Split("a.py", code, tokenizer.Python)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "foo" {
		t.Errorf("expected symbol 'foo', got %q", chunks[0].Symbol)
	}
	if !strings.Contains(chunks[0].Code, "return (s, t)") {
		t.Errorf("body missing from chunk:\n%s", chunks[0].Code)
	}
}

func TestSplit_GoTopLevelFunctions(t *testing.T) {
	code := `package main

func main() {
	helper(42)
}

func helper(x int) int {
	return x * 2
}
`
	chunks := Split("a.go", code, tokenizer.Go)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "main" || chunks[1].Symbol != "helper" {
		t.Errorf("expected [main helper], got [%s %s]", chunks[0].Symbol, chunks[1].Symbol)
	}
}

func TestSplit_GoMethodReceiver(t *testing.T) {
	code := `package main

func (f *Foo) Bar(x int) int {
	return x + f.y
}
`
	chunks := Split("a.go", code, tokenizer.Go)
	if len(chunks) != 1 || chunks[0].Symbol != "Bar" {
		t.Errorf("method receiver not parsed: got %+v", chunks)
	}
}

func TestSplit_GoInterfaceStubsSkipped(t *testing.T) {
	// Interface method declarations have no body — they should not produce
	// chunks (otherwise we'd emit zero-line chunks for every method line).
	code := `package main

type Reader interface {
	Read(p []byte) (int, error)
}

func Real() {
	x := 1
	_ = x
}
`
	chunks := Split("a.go", code, tokenizer.Go)
	if len(chunks) != 1 || chunks[0].Symbol != "Real" {
		t.Errorf("expected only Real chunk, got %+v", chunks)
	}
}

func TestSplit_GoGoroutineAnonymous(t *testing.T) {
	code := `package main

func Run() {
	go func() {
		doWork()
	}()
}
`
	chunks := Split("a.go", code, tokenizer.Go)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "Run" {
		t.Errorf("outer symbol: want Run, got %q", chunks[0].Symbol)
	}
	if chunks[1].Symbol != "goroutine@L4" {
		t.Errorf("inner symbol: want goroutine@L4, got %q", chunks[1].Symbol)
	}
	if chunks[1].StartLine != 4 || chunks[1].EndLine != 6 {
		t.Errorf("inner range: want 4-6, got %d-%d", chunks[1].StartLine, chunks[1].EndLine)
	}
}

func TestSplit_GoDeferAnonymous(t *testing.T) {
	code := `package main

func Run() {
	defer func() {
		cleanup()
	}()
	work()
}
`
	chunks := Split("a.go", code, tokenizer.Go)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "Run" || chunks[1].Symbol != "defer@L4" {
		t.Errorf("symbols: want [Run defer@L4], got [%s %s]", chunks[0].Symbol, chunks[1].Symbol)
	}
}

func TestSplit_GoAssignmentClosure(t *testing.T) {
	code := `package main

func Run() {
	helper := func(x int) int {
		return x * 2
	}
	_ = helper
}
`
	chunks := Split("a.go", code, tokenizer.Go)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "Run" || chunks[1].Symbol != "helper" {
		t.Errorf("symbols: want [Run helper], got [%s %s]", chunks[0].Symbol, chunks[1].Symbol)
	}
}

func TestSplit_GoVarClosure(t *testing.T) {
	code := `package main

var double = func(x int) int { return x + x }
`
	chunks := Split("a.go", code, tokenizer.Go)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "double" {
		t.Errorf("symbol: want double, got %q", chunks[0].Symbol)
	}
}

func TestSplit_GoIIFE(t *testing.T) {
	code := `package main

func Run() {
	func() {
		println("init")
	}()
}
`
	chunks := Split("a.go", code, tokenizer.Go)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "Run" || chunks[1].Symbol != "anonymous@L4" {
		t.Errorf("symbols: want [Run anonymous@L4], got [%s %s]", chunks[0].Symbol, chunks[1].Symbol)
	}
}

func TestSplit_GoFuncTypeFieldNotChunked(t *testing.T) {
	// `Handler func(...)` is a struct field type declaration, not a function
	// definition — it has no body braces, so findBraceEnd rejects it and no
	// chunk is emitted. The Real func should be the only chunk.
	code := `package main

type Server struct {
	Handler func(http.ResponseWriter, *http.Request)
}

func Real() {
	x := 1
	_ = x
}
`
	chunks := Split("a.go", code, tokenizer.Go)
	if len(chunks) != 1 || chunks[0].Symbol != "Real" {
		t.Errorf("expected only Real chunk, got %+v", chunks)
	}
}

func TestSplit_GoMultilineAnonSignature(t *testing.T) {
	code := `package main

func Run() {
	f := func(
		x int,
		y int,
	) int {
		return x + y
	}
	_ = f
}
`
	chunks := Split("a.go", code, tokenizer.Go)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[1].Symbol != "f" {
		t.Errorf("inner symbol: want f, got %q", chunks[1].Symbol)
	}
	if chunks[1].StartLine != 4 || chunks[1].EndLine != 9 {
		t.Errorf("inner range: want 4-9, got %d-%d", chunks[1].StartLine, chunks[1].EndLine)
	}
}

func TestSplit_JavaScriptFunctionAndArrow(t *testing.T) {
	code := `function App() {
  return 1;
}

const useFoo = () => {
  return 2;
};

class Widget {
  render() { return 3; }
}
`
	chunks := Split("a.js", code, tokenizer.JavaScript)
	got := make([]string, len(chunks))
	for i, c := range chunks {
		got[i] = c.Symbol
	}
	want := []string{"App", "useFoo", "Widget"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSplit_RustTopLevelAndImpl(t *testing.T) {
	code := `pub fn top() -> i32 {
    1
}

impl Foo {
    fn method(&self) -> i32 {
        2
    }
}
`
	chunks := Split("a.rs", code, tokenizer.Rust)
	got := make([]string, 0, len(chunks))
	for _, c := range chunks {
		got = append(got, c.Symbol)
	}
	// We expect "top" and "method"; the impl block itself isn't a fn so it
	// is not emitted as its own chunk.
	if len(got) != 2 || got[0] != "top" || got[1] != "method" {
		t.Errorf("expected [top method], got %v", got)
	}
}

func TestSplit_JavaInterfaceStubsFallBackToWholeFile(t *testing.T) {
	// An interface containing only abstract method stubs (no bodies) has
	// no `{`-balanced chunks for splitJava to extract, so the splitter
	// emits zero chunks and Split's outer fallback returns the whole
	// file as one anonymous chunk.
	code := `package com.foo;
public interface Foo {
  void bar();
  String baz(int n);
}
`
	chunks := Split("Foo.java", code, tokenizer.Java)
	if len(chunks) != 1 || chunks[0].Symbol != "" {
		t.Errorf("Java interface stubs should fall back to a single whole-file chunk, got %+v", chunks)
	}
}

func TestSplit_JavaMethodsBecomeChunks(t *testing.T) {
	code := `package com.foo;
public class Calculator {
    public int add(int a, int b) {
        return a + b;
    }

    private static String describe(int n) {
        if (n < 0) {
            return "negative";
        }
        return "positive";
    }
}
`
	chunks := Split("Calculator.java", code, tokenizer.Java)
	got := make([]string, len(chunks))
	for i, c := range chunks {
		got[i] = c.Symbol
	}
	want := []string{"add", "describe"}
	if len(got) != len(want) {
		t.Fatalf("expected chunks %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk %d: got %q, want %q", i, got[i], want[i])
		}
	}
	// The describe chunk must include its body — the inner `if (n < 0) {`
	// shouldn't be misread as a method header by the keyword filter.
	for _, c := range chunks {
		if c.Symbol == "describe" && !strings.Contains(c.Code, `return "positive"`) {
			t.Errorf("describe chunk missing body:\n%s", c.Code)
		}
	}
}

func TestSplit_JavaConstructorIsAChunk(t *testing.T) {
	code := `package com.foo;
public class Point {
    private final int x;
    private final int y;

    public Point(int x, int y) {
        this.x = x;
        this.y = y;
    }
}
`
	chunks := Split("Point.java", code, tokenizer.Java)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for the constructor, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "Point" {
		t.Errorf("expected constructor symbol 'Point', got %q", chunks[0].Symbol)
	}
}

func TestSplit_JavaGenericMethodAndThrows(t *testing.T) {
	code := `package com.foo;
import java.io.IOException;
import java.util.List;

public class Util {
    public <T> List<T> filter(List<T> input) throws IOException {
        return input;
    }

    public Map<String, Integer> count(Collection<? extends Number> nums) {
        return null;
    }
}
`
	chunks := Split("Util.java", code, tokenizer.Java)
	got := make([]string, len(chunks))
	for i, c := range chunks {
		got[i] = c.Symbol
	}
	want := []string{"filter", "count"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSplit_JavaFieldsAndTypeDeclsNotMatched(t *testing.T) {
	// Fields lack `(`; type declarations are filtered out before the
	// method regex runs. Both kinds of non-method members should yield
	// zero method-chunks here, so the file falls back to a whole-file
	// chunk via Split's outer guard.
	code := `package com.foo;
public class Container {
    private static final int CONSTANT = 42;
    private String name = "default";
    private List<String> items = new ArrayList<>();
}
`
	chunks := Split("Container.java", code, tokenizer.Java)
	if len(chunks) != 1 || chunks[0].Symbol != "" {
		t.Errorf("class with only fields should fall back to whole-file chunk; got %+v", chunks)
	}
}

func TestSplit_JavaControlFlowNotMisreadAsMethods(t *testing.T) {
	// `if (cond) {` and friends have the same surface shape as a method
	// header (name + parens + brace) and would falsely match without
	// the keyword filter. Ensure the only chunk we emit is the
	// enclosing method, not its inner control-flow blocks.
	code := `package com.foo;
public class Loops {
    public void run(int n) {
        if (n > 0) {
            System.out.println("positive");
        }
        while (n > 0) {
            n--;
        }
        for (int i = 0; i < n; i++) {
            System.out.println(i);
        }
        switch (n) {
            case 0:
                break;
        }
    }
}
`
	chunks := Split("Loops.java", code, tokenizer.Java)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (just `run`), got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "run" {
		t.Errorf("expected symbol 'run', got %q", chunks[0].Symbol)
	}
}

func TestSplit_LineRangesArePopulated(t *testing.T) {
	code := `package main

func short() {
	x := 1
	_ = x
}
`
	chunks := Split("a.go", code, tokenizer.Go)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	c := chunks[0]
	if c.StartLine != 3 || c.EndLine != 6 {
		t.Errorf("unexpected line range: got %d-%d, want 3-6", c.StartLine, c.EndLine)
	}
}

func TestSplit_PathPropagated(t *testing.T) {
	chunks := Split("some/path.go", "package main\nfunc f(){}\n", tokenizer.Go)
	for _, c := range chunks {
		if c.Path != "some/path.go" {
			t.Errorf("Path not set on chunk: %+v", c)
		}
	}
}

func TestChunk_Name_WithSymbol(t *testing.T) {
	got := Chunk{Path: "a.go", StartLine: 3, EndLine: 7, Symbol: "Run"}.Name()
	want := "a.go:3-7 Run"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestChunk_Name_NoSymbolHasRange(t *testing.T) {
	got := Chunk{Path: "a.go", StartLine: 10, EndLine: 12}.Name()
	want := "a.go:10-12"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestChunk_Name_WholeFileFallback(t *testing.T) {
	// No symbol AND starts at line 1 → whole-file fallback chunk; just the path.
	got := Chunk{Path: "a.go", StartLine: 1, EndLine: 50}.Name()
	if got != "a.go" {
		t.Errorf("got %q, want %q", got, "a.go")
	}
}

func TestChunk_Name_StartLine1WithSymbol(t *testing.T) {
	// A real top-level def at line 1 must still get the range+symbol form.
	got := Chunk{Path: "a.go", StartLine: 1, EndLine: 5, Symbol: "main"}.Name()
	want := "a.go:1-5 main"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCountNonBlankLines(t *testing.T) {
	cases := []struct {
		name string
		code string
		want int
	}{
		{"empty", "", 0},
		{"all blank", "\n\n   \n\t\n", 0},
		{"mixed", "a\n\nb\n   \nc", 3},
		{"single", "hello", 1},
		{"trailing newline", "a\n", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountNonBlankLines(tc.code); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// ── pythonScanLine direct coverage ─────────────────────────────────────────
//
// pythonScanLine is reachable through pythonSignatureEndLine and
// pythonDecoratorEndLine, but most of its string-state branches
// (triple-quote open/close, escape handling inside single/double
// strings, end-of-line single-line-string reset) are hard to drive
// from real fixtures. Test directly.

func TestPythonScanLine_TripleDoubleOpenAndClose(t *testing.T) {
	state := pyStCode
	depth := 0
	pythonScanLine(`x = """hello`, &state, &depth)
	if state != pyStTripleDouble {
		t.Errorf("after open: state = %v, want pyStTripleDouble", state)
	}
	pythonScanLine(`continued`, &state, &depth)
	if state != pyStTripleDouble {
		t.Errorf("mid-string state must persist across lines, got %v", state)
	}
	pythonScanLine(`done"""`, &state, &depth)
	if state != pyStCode {
		t.Errorf("after triple-double close: state = %v, want pyStCode", state)
	}
}

func TestPythonScanLine_TripleSingleOpenAndClose(t *testing.T) {
	state := pyStCode
	depth := 0
	pythonScanLine(`x = '''hello`, &state, &depth)
	if state != pyStTripleSingle {
		t.Errorf("after open: state = %v, want pyStTripleSingle", state)
	}
	pythonScanLine(`done'''`, &state, &depth)
	if state != pyStCode {
		t.Errorf("after triple-single close: state = %v, want pyStCode", state)
	}
}

func TestPythonScanLine_BackslashEscapeInsideSingleQuotedString(t *testing.T) {
	state := pyStCode
	depth := 0
	// Single quoted string with an escaped quote followed by the closing quote.
	pythonScanLine(`x = 'it\'s ok' # trailing`, &state, &depth)
	if state != pyStCode {
		t.Errorf("string should have closed and returned to code, state = %v", state)
	}
}

func TestPythonScanLine_BackslashEscapeInsideDoubleQuotedString(t *testing.T) {
	state := pyStCode
	depth := 0
	pythonScanLine(`x = "she said \"hi\"" # trailing`, &state, &depth)
	if state != pyStCode {
		t.Errorf("string should have closed and returned to code, state = %v", state)
	}
}

func TestPythonScanLine_SingleLineStringResetsAtNewline(t *testing.T) {
	// An unterminated single-quote shouldn't poison the next line; the
	// reset-on-newline branch (lines 253-255) flips state back to code.
	state := pyStCode
	depth := 0
	pythonScanLine(`x = "unterminated`, &state, &depth)
	if state != pyStCode {
		t.Errorf("single-line string state should reset at newline, got %v", state)
	}
}

func TestPythonScanLine_TopLevelColonReportedOnlyAtDepthZero(t *testing.T) {
	state := pyStCode
	depth := 0
	if !pythonScanLine(`def foo(x):`, &state, &depth) {
		t.Error("expected sawColonAtZero=true on a simple def header")
	}
	// `:` inside a paren stays inside (depth>0) so should NOT be reported.
	if pythonScanLine(`def foo(x: int):`, &state, &depth) != true {
		t.Error("the trailing `):` is at depth 0 and should be reported")
	}
}

// TestPythonSignatureEndLine_NoColonReturnsLastIndex covers the
// end-of-input fallback in pythonSignatureEndLine (line 285).
func TestPythonSignatureEndLine_NoColonReturnsLastIndex(t *testing.T) {
	lines := []string{"def foo(", "    x,"}
	got := pythonSignatureEndLine(lines, 0)
	if got != len(lines)-1 {
		t.Errorf("want %d (last index), got %d", len(lines)-1, got)
	}
}

// TestPythonDecoratorEndLine_NoCloseReturnsLastIndex covers the
// end-of-input fallback in pythonDecoratorEndLine (line 305).
func TestPythonDecoratorEndLine_NoCloseReturnsLastIndex(t *testing.T) {
	// `@retry(` opens a paren that never closes — bottom of the slice
	// is returned.
	lines := []string{"@retry(", "    attempts=3,"}
	got := pythonDecoratorEndLine(lines, 0)
	if got != len(lines)-1 {
		t.Errorf("want %d (last index), got %d", len(lines)-1, got)
	}
}

// TestIndentLen_NonWhitespaceImmediatelyReturns covers the default
// branch (line 316-317) where a non-space/non-tab byte is encountered.
func TestIndentLen_NonWhitespaceImmediatelyReturns(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"no indent", 0},
		{"  two spaces", 2},
		{"\ttab", 4},
		{" \tmix", 5},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := indentLen(c.in); got != c.want {
				t.Errorf("indentLen(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
