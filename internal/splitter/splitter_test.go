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

func TestSplit_JavaFallsBackToWholeFile(t *testing.T) {
	code := `package com.foo;
public class Foo {
  void bar() {}
}
`
	chunks := Split("Foo.java", code, tokenizer.Java)
	if len(chunks) != 1 || chunks[0].Symbol != "" {
		t.Errorf("Java should fall back to a single whole-file chunk, got %+v", chunks)
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
