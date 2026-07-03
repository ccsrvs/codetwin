package tokenizer

import (
	"regexp"
	"strings"
	"testing"
)

func TestDetect_FromExtension(t *testing.T) {
	cases := map[string]Language{
		"foo.go":   Go,
		"foo.js":   JavaScript,
		"foo.ts":   JavaScript,
		"foo.tsx":  JavaScript,
		"foo.jsx":  JavaScript,
		"foo.py":   Python,
		"foo.java": Java,
		"foo.rs":   Rust,
		"foo.ex":   Elixir,
		"foo.exs":  Elixir,
	}
	for name, want := range cases {
		if got := Detect(name, ""); got != want {
			t.Errorf("Detect(%q) = %v; want %v", name, got, want)
		}
	}
}

func TestDetect_FromHeuristic(t *testing.T) {
	cases := []struct {
		name string
		code string
		want Language
	}{
		{"go via package main", "package main\nfunc x() {}", Go},
		{"elixir defmodule", "defmodule Foo do\n  def bar, do: 1\nend", Elixir},
		{"java public class", "public class Foo { System.out.println(\"hi\"); }", Java},
		{"rust fn + let mut", "fn main() {\n    let mut x = 1;\n}", Rust},
		{"python def with colon", "def foo(x):\n    return x", Python},
		{"python def mentioning download stays python", "def fetch(url):\n    return download(url)", Python},
		{"python def with docs identifier stays python", "def render(docs):\n    return docs[0]", Python},
		{"elixir def with do block", "def hello(x) do\n  x\nend", Elixir},
		{"elixir shorthand def", "def hello(x), do: x", Elixir},
		{"javascript function keyword", "function foo() { return 1; }", JavaScript},
		{"javascript const + arrow", "const f = () => 1;", JavaScript},
		{"unknown short text", "hello world", Unknown},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := Detect("noext", c.code); got != c.want {
				t.Errorf("Detect(%q) = %v, want %v", c.code, got, c.want)
			}
		})
	}
}

// TestTokenizeWithLines_UnknownLangFallsBackToJSPattern covers the
// `if !ok { p = patterns[JavaScript] }` fallback inside
// TokenizeWithLines (line 285) when an unknown language is requested.
func TestTokenizeWithLines_UnknownLangFallsBackToJSPattern(t *testing.T) {
	tokens, _ := TokenizeWithLines("const x = 1;", Unknown)
	if len(tokens) == 0 {
		t.Errorf("expected JS-pattern tokenization on Unknown lang, got 0 tokens")
	}
}

func TestNormalize_RemovesComments(t *testing.T) {
	code := "// this is a secret comment\nconst x = 1;"
	out := Normalize(code, JavaScript)
	if strings.Contains(out, "secret") {
		t.Errorf("comment not stripped: %q", out)
	}
}

func TestNormalize_ReplacesLiteralsAndIdentifiers(t *testing.T) {
	out := Normalize(`const total = 42;`, JavaScript)
	if !strings.Contains(out, "const") {
		t.Errorf("keyword 'const' missing: %q", out)
	}
	if !strings.Contains(out, "VAR") {
		t.Errorf("identifier not replaced with VAR: %q", out)
	}
	if !strings.Contains(out, "NUM") {
		t.Errorf("number not replaced with NUM: %q", out)
	}
	if strings.Contains(out, "42") {
		t.Errorf("number literal leaked through: %q", out)
	}
	if strings.Contains(out, "total") {
		t.Errorf("identifier 'total' not replaced: %q", out)
	}
}

func TestNormalize_NormalizesStrings(t *testing.T) {
	out := Normalize(`const x = "hello"; const y = 'world';`, JavaScript)
	if !strings.Contains(out, "STR") {
		t.Errorf("string not normalized to STR: %q", out)
	}
	if strings.Contains(out, "hello") || strings.Contains(out, "world") {
		t.Errorf("string content leaked through: %q", out)
	}
}

func TestNormalize_StructurallyEqualCodeNormalizesEqual(t *testing.T) {
	a := `function sumArray(arr) { let total = 0; for (let i = 0; i < arr.length; i++) { total += arr[i]; } return total; }`
	b := `function addNumbers(nums) { let result = 0; for (let i = 0; i < nums.length; i++) { result += nums[i]; } return result; }`
	if Normalize(a, JavaScript) != Normalize(b, JavaScript) {
		t.Errorf("structurally identical JS code did not normalize equal:\n  a=%q\n  b=%q",
			Normalize(a, JavaScript), Normalize(b, JavaScript))
	}
}

func TestNormalize_UnknownLanguageFallsBackToJS(t *testing.T) {
	// Should not panic, should produce some output
	out := Normalize(`const x = 1;`, Unknown)
	if out == "" {
		t.Error("Normalize with Unknown language returned empty string")
	}
}

func TestNormalize_StripsPythonImports(t *testing.T) {
	code := `import os
from pathlib import Path
from foo.bar import (
    Alpha,
    Beta,
)

def hello():
    return Path("/tmp")
`
	out := Normalize(code, Python)
	for _, leaked := range []string{"os", "pathlib", "Path", "foo", "bar", "Alpha", "Beta"} {
		if strings.Contains(out, leaked) {
			t.Errorf("import name %q leaked through Python normalization: %q", leaked, out)
		}
	}
	// Function body should still be there (Path call survives because it's
	// outside an import context — the import statement is gone, but the call
	// inside hello() stays as 'STR' and 'VAR').
	if !strings.Contains(out, "def") || !strings.Contains(out, "return") {
		t.Errorf("function body unexpectedly stripped: %q", out)
	}
}

func TestNormalize_StripsGoImports(t *testing.T) {
	code := `package main

import (
	"fmt"
	"os"
)

import "io"

func main() {
	fmt.Println("hi")
}
`
	out := Normalize(code, Go)
	for _, leaked := range []string{"fmt", "os", "io"} {
		if strings.Contains(out, leaked) {
			t.Errorf("import path %q leaked through Go normalization: %q", leaked, out)
		}
	}
	if !strings.Contains(out, "func") {
		t.Errorf("func keyword unexpectedly stripped: %q", out)
	}
}

func TestNormalize_StripsJavaScriptImports(t *testing.T) {
	code := `import { useState, useEffect } from 'react';
import axios from 'axios';
const fs = require('fs');

function App() {
  return null;
}
`
	out := Normalize(code, JavaScript)
	for _, leaked := range []string{"useState", "useEffect", "react", "axios", "fs"} {
		if strings.Contains(out, leaked) {
			t.Errorf("import name %q leaked through JS normalization: %q", leaked, out)
		}
	}
	if !strings.Contains(out, "function") {
		t.Errorf("function keyword unexpectedly stripped: %q", out)
	}
}

func TestNormalize_StripsRustImports(t *testing.T) {
	code := `use std::collections::HashMap;
use std::io::{self, Read};

fn main() {
    println!("hi");
}
`
	out := Normalize(code, Rust)
	for _, leaked := range []string{"std", "collections", "HashMap", "Read"} {
		if strings.Contains(out, leaked) {
			t.Errorf("use path %q leaked through Rust normalization: %q", leaked, out)
		}
	}
	if !strings.Contains(out, "fn") {
		t.Errorf("fn keyword unexpectedly stripped: %q", out)
	}
}

func TestNormalize_StripsElixirImports(t *testing.T) {
	code := `defmodule Foo do
  alias Bar.Baz
  alias Qux.{Alpha, Beta}
  import Quux
  require Logger

  def hello, do: :world
end`
	out := Normalize(code, Elixir)
	for _, leaked := range []string{"Bar", "Baz", "Qux", "Alpha", "Beta", "Quux", "Logger"} {
		if strings.Contains(out, leaked) {
			t.Errorf("alias/import name %q leaked through Elixir normalization: %q", leaked, out)
		}
	}
	if !strings.Contains(out, "def") {
		t.Errorf("def keyword unexpectedly stripped: %q", out)
	}
}

func TestNormalize_StripsJavaImports(t *testing.T) {
	code := `package com.foo.bar;

import java.util.List;
import java.util.Map;
import static java.lang.Math.PI;

public class Foo {
  void bar() {}
}
`
	out := Normalize(code, Java)
	for _, leaked := range []string{"java", "util", "List", "Map", "Math", "PI", "com", "foo", "bar"} {
		if strings.Contains(out, leaked) {
			// 'bar' could be in `bar()` — that's fine, but the import line shouldn't leak it.
			if leaked == "bar" {
				continue
			}
			t.Errorf("import name %q leaked through Java normalization: %q", leaked, out)
		}
	}
	if !strings.Contains(out, "class") {
		t.Errorf("class keyword unexpectedly stripped: %q", out)
	}
}

func TestNormalize_ImportOnlyFileNormalizesEmpty(t *testing.T) {
	// A Python file that's nothing but imports should normalize to (effectively)
	// empty after import stripping — the regression target for the user's
	// reported import-noise problem.
	code := `from pathlib import Path
import pytest
from scout.testing.cli import invoke
`
	tokens := Tokenize(code, Python)
	if len(tokens) != 0 {
		t.Errorf("import-only Python file should produce zero tokens, got %d: %v", len(tokens), tokens)
	}
}

func TestTokenize_ProducesNonEmptyTokens(t *testing.T) {
	tokens := Tokenize(`function f() { return 1; }`, JavaScript)
	if len(tokens) == 0 {
		t.Fatal("tokens slice was empty")
	}
	for _, tok := range tokens {
		if tok == "" {
			t.Error("empty token in stream")
		}
	}
}

func TestTokenize_ElixirProducesNonEmptyTokens(t *testing.T) {
	code := `defmodule Foo do
  def bar(x) do
    x + 1
  end
end`
	tokens := Tokenize(code, Elixir)
	if len(tokens) == 0 {
		t.Error("Elixir tokenizer produced zero tokens")
	}
}

func TestTokenizeWithLines_AssignsCorrectLineNumbers(t *testing.T) {
	code := `def foo():
    x = 1
    return x
`
	tokens, lines := TokenizeWithLines(code, Python)
	if len(tokens) != len(lines) {
		t.Fatalf("tokens (%d) and lines (%d) length mismatch", len(tokens), len(lines))
	}
	want := []struct {
		tok  string
		line int
	}{
		{"def", 1}, {"VAR", 1}, {"(", 1}, {")", 1}, {":", 1}, // def foo():
		{"VAR", 2}, {"=", 2}, {"NUM", 2}, // x = 1
		{"return", 3}, {"VAR", 3}, // return x
	}
	if len(tokens) != len(want) {
		t.Fatalf("expected %d tokens, got %d: %v on lines %v", len(want), len(tokens), tokens, lines)
	}
	for i, w := range want {
		if tokens[i] != w.tok || lines[i] != w.line {
			t.Errorf("token %d: got (%q, line %d); want (%q, line %d)", i, tokens[i], lines[i], w.tok, w.line)
		}
	}
}

func TestTokenizeWithLines_MatchesTokenize(t *testing.T) {
	// TokenizeWithLines and Tokenize must produce identical token streams —
	// the only difference is the parallel line-number slice.
	cases := []struct {
		name string
		code string
		lang Language
	}{
		{"python", "def f():\n    x = \"hi\"\n    return x\n", Python},
		{"go", "package main\nfunc f() int {\n\treturn 1\n}\n", Go},
		{"js", "function f() { return 'hi'; }\n", JavaScript},
		{"python with docstring", `def f():
    """
    multi-line docstring
    """
    return 1
`, Python},
		{"with imports", "import os\nfrom pathlib import Path\ndef f():\n    return 1\n", Python},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := Tokenize(c.code, c.lang)
			b, _ := TokenizeWithLines(c.code, c.lang)
			if len(a) != len(b) {
				t.Fatalf("token count mismatch: Tokenize=%d, TokenizeWithLines=%d\n  a=%v\n  b=%v",
					len(a), len(b), a, b)
			}
			for i := range a {
				if a[i] != b[i] {
					t.Errorf("token[%d] mismatch: Tokenize=%q, TokenizeWithLines=%q", i, a[i], b[i])
				}
			}
		})
	}
}

func TestTokenizeWithLines_MultilineStringPreservesLines(t *testing.T) {
	// A multi-line string becomes "STR" attributed to the opening-quote line.
	// Tokens AFTER the string must still carry correct line numbers.
	code := `def f():
    msg = """
hello
world
"""
    return msg
`
	tokens, lines := TokenizeWithLines(code, Python)
	// Find the 'return' token and verify its line is 6 (after the 4-line string)
	found := false
	for i, tok := range tokens {
		if tok == "return" {
			found = true
			if lines[i] != 6 {
				t.Errorf("expected 'return' on line 6, got line %d", lines[i])
			}
			break
		}
	}
	if !found {
		t.Errorf("'return' token not found in: %v", tokens)
	}
}

func TestTokenizeWithLines_UserStripPatternsApplied(t *testing.T) {
	// Lines matching a user-provided strip regex should disappear from the
	// token stream entirely (treated like a comment).
	code := `def f():
    log.info("loading config")
    x = 1
    return x
`
	stripLogCalls := regexp.MustCompile(`(?m)^\s*log\.\w+\([^)]*\)`)
	tokens, _ := TokenizeWithLines(code, Python, WithStripPatterns([]*regexp.Regexp{stripLogCalls}))
	for _, tok := range tokens {
		if strings.Contains(tok, "log") {
			t.Errorf("user strip pattern failed; 'log' leaked through: %v", tokens)
			break
		}
	}
}

func TestTokenizeWithLines_UserStripPreservesLineNumbers(t *testing.T) {
	// After stripping a line via user pattern, subsequent tokens must keep
	// the correct source line — i.e. the strip is newline-preserving.
	code := `def f():
    log.info("noise")
    return x
`
	stripLogCalls := regexp.MustCompile(`(?m)^\s*log\.\w+\([^)]*\)`)
	tokens, lines := TokenizeWithLines(code, Python, WithStripPatterns([]*regexp.Regexp{stripLogCalls}))
	// 'return' should be on line 3.
	for i, tok := range tokens {
		if tok == "return" {
			if lines[i] != 3 {
				t.Errorf("expected 'return' on line 3, got line %d", lines[i])
			}
			return
		}
	}
	t.Error("'return' token not found in stream")
}

func TestTokenize_NoOptionsBackCompat(t *testing.T) {
	// Passing no opts must produce the same tokens as before the option
	// signature was added.
	a := Tokenize(`function f() { return 1; }`, JavaScript)
	if len(a) == 0 {
		t.Fatal("Tokenize without options returned no tokens")
	}
}

func TestTokenize_ElixirNormalizesSingleQuotedStrings(t *testing.T) {
	// Single-quoted strings should normalize to STR (regression test for the
	// Elixir string regex bug fixed during initial setup).
	tokens := Tokenize(`x = 'hello'`, Elixir)
	found := false
	for _, tok := range tokens {
		if tok == "STR" {
			found = true
			break
		}
		if strings.Contains(tok, "hello") {
			t.Errorf("single-quoted Elixir string leaked through tokenizer: %v", tokens)
		}
	}
	if !found {
		t.Errorf("expected 'STR' token in Elixir output, got %v", tokens)
	}
}
func TestTokenize_WhitespaceInsensitive(t *testing.T) {
	// Dense and spaced formatting of the same logic must produce the
	// same token stream — otherwise reformatted (or minified) clones
	// fingerprint differently and never match.
	cases := []struct {
		name         string
		dense, roomy string
		lang         Language
	}{
		{"js assignment", "x=a+b;", "x = a + b ;", JavaScript},
		{"js call", "f(x,y)", "f( x , y )", JavaScript},
		{"go define", "x:=len(s)", "x := len( s )", Go},
		{"python def", "def f(x):return x+1", "def f( x ) : return x + 1", Python},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dense := Tokenize(c.dense, c.lang)
			roomy := Tokenize(c.roomy, c.lang)
			if strings.Join(dense, " ") != strings.Join(roomy, " ") {
				t.Errorf("dense %v != roomy %v", dense, roomy)
			}
		})
	}
}

func TestTokenize_PunctuationIsSingleRuneTokens(t *testing.T) {
	// Punctuation splits word runs and each rune stands alone — runs
	// like `):` would re-introduce whitespace sensitivity.
	got := Tokenize("a.b(c)", JavaScript)
	want := []string{"VAR", ".", "VAR", "(", "VAR", ")"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("Tokenize(a.b(c)) = %v, want %v", got, want)
	}
}

func TestTokenize_PunctuationCarriesSignal(t *testing.T) {
	add := Tokenize("x = a + b", JavaScript)
	sub := Tokenize("x = a - b", JavaScript)
	if strings.Join(add, " ") == strings.Join(sub, " ") {
		t.Errorf("a+b and a-b tokenized identically: %v", add)
	}
}
