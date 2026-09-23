package refactor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

func TestSynthesize_CAcceptTiers(t *testing.T) {
	cases := []struct {
		dir           string
		expectInSrc   []string
		minConfidence float64
	}{
		{
			dir: "../../testdata/refactor/c/simple",
			expectInSrc: []string{
				"double extracted_price_with_tax_a_deadbeef(double amount)\n{",
				"// Divergences (B vs A):",
				"0.07",
				"0.085",
			},
			minConfidence: 0.5,
		},
		{
			dir:           "../../testdata/refactor/c/medium",
			expectInSrc:   []string{"int extracted_format_user_a_deadbeef(const struct record *r, char *buf, size_t len)", `"(active)"`, `"(privileged)"`, `"admin:%s %s count=%d"`},
			minConfidence: 0.4,
		},
		{
			dir:           "../../testdata/refactor/c/advanced",
			expectInSrc:   []string{"int extracted_load_config_a_deadbeef(const char *path, char **out)", "goto done;", "done:", `"rb"`, `"r"`, `'\n'`},
			minConfidence: 0.4,
		},
		{
			dir: "../../testdata/refactor/c/realworld-gnu",
			// GNU style: the return type, the wrapped parameter list, and
			// the brace alone on its line all carry over.
			expectInSrc:   []string{"uint32_t\nextracted_header_checksum_a_deadbeef (const unsigned char *buf,\n                   size_t len)\n{", "rotl (sum, 5)"},
			minConfidence: 0.4,
		},
	}
	for _, c := range cases {
		t.Run(filepath.Base(c.dir), func(t *testing.T) {
			a, b := loadSnippets(t, c.dir)
			s := Synthesize(a, b, "deadbeef", Align(a, b))
			if s.Note != "" {
				t.Fatalf("expected accept, got Note=%q", s.Note)
			}
			if s.Confidence < c.minConfidence {
				t.Errorf("Confidence = %.2f, want >= %.2f", s.Confidence, c.minConfidence)
			}
			for _, want := range c.expectInSrc {
				if !strings.Contains(s.HelperSrc, want) {
					t.Errorf("HelperSrc missing %q. Source:\n%s", want, s.HelperSrc)
				}
			}
			if strings.Count(s.HelperSrc, "{") != strings.Count(s.HelperSrc, "}") {
				t.Errorf("unbalanced braces in helper:\n%s", s.HelperSrc)
			}
		})
	}
}

func TestSynthesize_CRejectControlFlowFixture(t *testing.T) {
	a, b := loadSnippets(t, "../../testdata/refactor/c/reject-controlflow")
	s := Synthesize(a, b, "deadbeef", Align(a, b))
	if s.Note == "" || s.HelperSrc != "" {
		t.Fatalf("expected a control-flow rejection, got Note=%q src=%q", s.Note, s.HelperSrc)
	}
}

func TestSynthesize_CRejectsMacroGeneratedAndHeaderless(t *testing.T) {
	al := Alignment{Common: []LineSpan{{AStart: 1, AEnd: 3, BStart: 1, BEnd: 3}}}
	macro := func(name string) scan.Snippet {
		return scan.Snippet{Name: "t.c:1-3 TEST@L1", Lang: tokenizer.C, Code: "TEST(suite, " + name + ") {\n    ASSERT(1);\n}"}
	}
	if s := Synthesize(macro("a"), macro("b"), "deadbeef", al); !strings.Contains(s.Note, "macro-generated") {
		t.Errorf("macro-generated definitions: Note=%q", s.Note)
	}
	headerless := scan.Snippet{Name: "x.c:1-2 f", Lang: tokenizer.C, Code: "/* only a comment */\nint x;"}
	if s := Synthesize(headerless, headerless, "deadbeef", al); !strings.Contains(s.Note, "recognisable C function header") {
		t.Errorf("headerless: Note=%q", s.Note)
	}
}

func TestCHelperHeader(t *testing.T) {
	for _, tc := range []struct{ name, code, want string }{
		{"same-line brace", "int add(int a, int b) {\n\treturn a + b;\n}", "int h(int a, int b) {"},
		{"one-line function", "static int one(void) { return 1; }", "static int h(void) {"},
		{"GNU style", "static int\ncompute (int a,\n         int b)\n{\n  return a * b;\n}", "static int\nh (int a,\n         int b)\n{"},
		{"recursion in the body is untouched", "int fact(int n) {\n\treturn n < 2 ? 1 : n * fact(n - 1);\n}", "int h(int n) {"},
		{"#ifdef header variants are all renamed", "#ifdef WIDE\nint conv(long v)\n#else\nint conv(int v)\n#endif\n{\n    return (int)v;\n}", "#ifdef WIDE\nint h(long v)\n#else\nint h(int v)\n#endif\n{"},
		{"comment mentioning the name is untouched", "int fold(int x) /* fold() doubles */ {\n\treturn 2 * x;\n}", "int h(int x) /* fold() doubles */ {"},
		{"K&R", "int\nold(a, b)\n    int a;\n    int b;\n{\n    return a + b;\n}", "int\nh(a, b)\n    int a;\n    int b;\n{"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := cHelperHeader(tc.code, "h")
			if !ok || got != tc.want {
				t.Errorf("cHelperHeader = %q, %v; want %q", got, ok, tc.want)
			}
		})
	}
}

// The helper must land above the first snippet's function — and above
// that function's doc comment — because C needs a declaration before
// use, and the patched file must still compile.
func TestBuildPatch_CFixtures_PlaceAboveFunctionAndCompile(t *testing.T) {
	for _, fixture := range []string{
		"../../testdata/refactor/c/simple",
		"../../testdata/refactor/c/medium",
		"../../testdata/refactor/c/advanced",
		"../../testdata/refactor/c/realworld-gnu",
	} {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			dir, patched := placeHelper(t, fixture, "a.c")
			helper := strings.Index(patched, "extracted_")
			// The original definition line: the first non-comment line
			// naming the `_a` function (the divergence comments quote it).
			original := -1
			for off, line := 0, ""; off < len(patched); off += len(line) + 1 {
				line, _, _ = strings.Cut(patched[off:], "\n")
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
					continue
				}
				if strings.Contains(line, "_a(") || strings.Contains(line, "_a (") {
					original = off
					break
				}
			}
			if helper < 0 || original < 0 || helper > original {
				t.Fatalf("helper (at %d) must precede the original function (at %d):\n%s", helper, original, patched)
			}
			if doc := strings.Index(patched, "/* Compute the rolling checksum"); doc >= 0 && helper > doc {
				t.Errorf("helper must precede the original function's doc comment:\n%s", patched)
			}
			if !strings.Contains(patched, "}\n\n") {
				t.Errorf("helper must be separated from the next function by a blank line:\n%s", patched)
			}
			if _, err := exec.LookPath("gcc"); err != nil {
				t.Skip("gcc not on PATH, skipping compile check")
			}
			cmd := exec.Command("gcc", "-fsyntax-only", "-Wall", "a.c")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				src, _ := os.ReadFile(filepath.Join(dir, "a.c"))
				t.Errorf("gcc failed on the patched file: %v\n%s\nfile:\n%s", err, out, src)
			}
		})
	}
}

func TestSynthesize_AssemblyExplainsWhyNoHelper(t *testing.T) {
	al := Alignment{Common: []LineSpan{{AStart: 1, AEnd: 3, BStart: 1, BEnd: 3}}}
	for _, lang := range []tokenizer.Language{tokenizer.AsmGAS, tokenizer.AsmNASM, tokenizer.AsmMASM, tokenizer.AsmPlan9, tokenizer.AsmHLASM} {
		a := scan.Snippet{Name: "a.s:1-3 f", Lang: lang, Code: "f:\n\tret\n"}
		s := Synthesize(a, a, "deadbeef", al)
		if s.HelperSrc != "" || !strings.Contains(s.Note, "assembly") || !strings.Contains(s.Note, "macro") {
			t.Errorf("%s: Note=%q HelperSrc=%q", lang, s.Note, s.HelperSrc)
		}
	}
}
