package tokenizer

import (
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
	if got := Detect("noext", "package main\nfunc x() {}"); got != Go {
		t.Errorf("expected Go from heuristic, got %v", got)
	}
	if got := Detect("noext", "defmodule Foo do\n  def bar, do: 1\nend"); got != Elixir {
		t.Errorf("expected Elixir from heuristic, got %v", got)
	}
	if got := Detect("noext", "public class Foo { System.out.println(\"hi\"); }"); got != Java {
		t.Errorf("expected Java from heuristic, got %v", got)
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