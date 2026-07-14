package refactor

import (
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// Given a file with nested Java types, when the chunk sits inside the
// inner type, then javaEnclosingTypeClose picks the INNERMOST type's
// closing brace; when it sits between the inner type and the outer
// close, the outer type wins; and when no type encloses it, ok=false.
func TestJavaEnclosingTypeClose(t *testing.T) {
	file := strings.Join([]string{
		"public class Outer {", // 1
		"    int x;",           // 2
		"    class Inner {",    // 3
		"        void m() {",   // 4
		"        }",            // 5
		"    }",                // 6
		"    void n() {",       // 7
		"    }",                // 8
		"}",                    // 9
	}, "\n") + "\n"

	cases := []struct {
		name       string
		chunkStart int
		wantLine   int
		wantOK     bool
	}{
		{"inside inner type", 4, 6, true},
		{"inside outer type only", 7, 9, true},
		{"no enclosing type (line 1 is the header itself)", 1, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line, ok := javaEnclosingTypeClose(file, c.chunkStart)
			if ok != c.wantOK || line != c.wantLine {
				t.Errorf("javaEnclosingTypeClose(chunkStart=%d) = (%d, %v), want (%d, %v)",
					c.chunkStart, line, ok, c.wantLine, c.wantOK)
			}
		})
	}

	if _, ok := javaEnclosingTypeClose("int x;\nint y() { return 0; }\n", 2); ok {
		t.Error("expected ok=false for a file without any type declaration")
	}
}

// Given nested defmodules, when the chunk sits inside the inner
// module, then elixirEnclosingModuleEnd picks the INNERMOST module's
// closing `end`; a def outside any module gets ok=false.
func TestElixirEnclosingModuleEnd(t *testing.T) {
	file := strings.Join([]string{
		"defmodule Outer do",   // 1
		"  defmodule Inner do", // 2
		"    def m(x) do",      // 3
		"      x",              // 4
		"    end",              // 5
		"  end",                // 6
		"  def n(y) do",        // 7
		"    y",                // 8
		"  end",                // 9
		"end",                  // 10
	}, "\n") + "\n"

	cases := []struct {
		name       string
		chunkStart int
		wantLine   int
		wantOK     bool
	}{
		{"inside inner module", 3, 6, true},
		{"inside outer module only", 7, 10, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line, ok := elixirEnclosingModuleEnd(file, c.chunkStart)
			if ok != c.wantOK || line != c.wantLine {
				t.Errorf("elixirEnclosingModuleEnd(chunkStart=%d) = (%d, %v), want (%d, %v)",
					c.chunkStart, line, ok, c.wantLine, c.wantOK)
			}
		})
	}

	if _, ok := elixirEnclosingModuleEnd("def free(x) do\n  x\nend\n", 1); ok {
		t.Error("expected ok=false for a def outside any defmodule")
	}
}

// buildInsertBeforePatch: the hunk must anchor on the context around
// the insertion point, add a blank separator when the preceding line
// is non-blank, and keep the pushed-down line as trailing context.
func TestBuildInsertBeforePatch_ShapesHunkAroundInsertionPoint(t *testing.T) {
	file := "class A {\n    void m() {\n    }\n}\n"
	got := buildInsertBeforePatch("A.java", file, "    void h() {\n    }\n", 4)
	want := "--- a/A.java\n" +
		"+++ b/A.java\n" +
		"@@ -1,4 +1,7 @@\n" +
		" class A {\n" +
		"     void m() {\n" +
		"     }\n" +
		"+\n" +
		"+    void h() {\n" +
		"+    }\n" +
		" }\n"
	if got != want {
		t.Errorf("diff mismatch.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// buildInsertBeforePatch with an out-of-range insertion point degrades
// to the plain append shape rather than emitting a broken hunk.
func TestBuildInsertBeforePatch_PastEOFFallsBackToAppend(t *testing.T) {
	file := "one\ntwo\n"
	got := buildInsertBeforePatch("x.txt", file, "new\n", 99)
	if !strings.Contains(got, "+new\n") || !strings.Contains(got, " two\n") {
		t.Errorf("expected append-shaped diff, got:\n%s", got)
	}
}

// Given a Java suggestion whose chunk has no enclosing type (not legal
// Java, but defensive), when buildPlacedPatch runs, then it falls back
// to the file-scope append and prepends the placement NOTE.
func TestBuildPlacedPatch_JavaNoEnclosingType_FallsBackWithNote(t *testing.T) {
	content := "static int one() {\n    return 1;\n}\n"
	diff := buildPlacedPatch("X.java", content, Suggestion{
		HelperSrc:       "static int extracted_one_deadbeef() {\n    return 1;\n}\n",
		Lang:            tokenizer.Java,
		SourceStartLine: 1,
	})
	if !strings.Contains(diff, "+// NOTE: appended at file scope") {
		t.Errorf("fallback diff missing the placement NOTE:\n%s", diff)
	}
	if !strings.Contains(diff, "+static int extracted_one_deadbeef() {") {
		t.Errorf("fallback diff missing the helper:\n%s", diff)
	}
}

// Given an Elixir suggestion whose chunk has no enclosing defmodule,
// when buildPlacedPatch runs, then it falls back to the file-scope
// append and prepends the defmodule placement NOTE.
func TestBuildPlacedPatch_ElixirNoModule_FallsBackWithNote(t *testing.T) {
	content := "def one(x) do\n  x\nend\n"
	diff := buildPlacedPatch("x.ex", content, Suggestion{
		HelperSrc:       "def extracted_one_deadbeef(x) do\n  x\nend\n",
		Lang:            tokenizer.Elixir,
		SourceStartLine: 1,
	})
	if !strings.Contains(diff, "+# NOTE: appended at file scope") {
		t.Errorf("fallback diff missing the placement NOTE:\n%s", diff)
	}
	if !strings.Contains(diff, "+def extracted_one_deadbeef(x) do") {
		t.Errorf("fallback diff missing the helper:\n%s", diff)
	}
}

// Non-Java/Elixir languages keep the plain file-scope append with no
// placement NOTE — placement only changes for container languages.
func TestBuildPlacedPatch_GoAppendsWithoutNote(t *testing.T) {
	content := "package x\n\nfunc Foo() {}\n"
	diff := buildPlacedPatch("x.go", content, Suggestion{
		HelperSrc:       "func Helper() {}\n",
		Lang:            tokenizer.Go,
		SourceStartLine: 3,
	})
	if !strings.Contains(diff, "+func Helper() {}") {
		t.Errorf("diff missing helper:\n%s", diff)
	}
	if strings.Contains(diff, "NOTE") {
		t.Errorf("Go append must not carry a placement NOTE:\n%s", diff)
	}
}

// indentBlock prefixes non-empty lines only, so the patch never adds
// trailing whitespace on blank helper lines.
func TestIndentBlock_SkipsEmptyLines(t *testing.T) {
	got := indentBlock("a\n\nb\n", "  ")
	want := "  a\n\n  b\n"
	if got != want {
		t.Errorf("indentBlock = %q, want %q", got, want)
	}
}
