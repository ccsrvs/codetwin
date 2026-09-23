package splitter

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// span is a chunk's identity for table comparisons.
type span struct {
	Symbol     string
	Start, End int
}

func spansOf(chunks []Chunk) []span {
	out := make([]span, len(chunks))
	for i, c := range chunks {
		out[i] = span{c.Symbol, c.StartLine, c.EndLine}
	}
	return out
}

func TestSplitC(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
		want []span
	}{
		{
			name: "same-line brace",
			code: "int add(int a, int b) {\n\treturn a + b;\n}\n",
			want: []span{{"add", 1, 3}},
		},
		{
			name: "GNU style: return type on its own line, wrapped parameters, brace alone",
			code: "static int\ncompute (int a,\n         int b)\n{\n  return a * b;\n}\n",
			want: []span{{"compute", 1, 6}},
		},
		{
			name: "prototype before definition is not a chunk and does not start it",
			code: "int foo(int);\nstatic void bar(void);\n\nint foo(int x)\n{\n\treturn x;\n}\n",
			want: []span{{"foo", 4, 7}},
		},
		{
			name: "types, enums, typedefs and initializers are skipped",
			code: "struct point { int x; int y; };\n" +
				"typedef struct { int a; } pair_t;\n" +
				"enum color { RED, GREEN };\n" +
				"static const int table[] = { 1, 2, 3 };\n" +
				"static struct ops my_ops = { .open = my_open, .close = my_close };\n" +
				"union num { int i; float f; } u;\n" +
				"int area(struct point p) { return p.x * p.y; }\n",
			want: []span{{"area", 7, 7}},
		},
		{
			name: "braces inside comments, strings and char literals",
			code: "/* a { brace in a comment */\n" +
				"const char *s = \"}{\";\n" +
				"char c = '{';\n" +
				"int f(void) {\n" +
				"\t// } in a comment\n" +
				"\tprintf(\"{ \\\" }\");\n" +
				"\treturn '}';\n" +
				"}\n",
			want: []span{{"f", 4, 8}},
		},
		{
			name: "multi-line macro with braces is not a function",
			code: "#define SWAP(a, b) do { \\\n    int t = a; a = b; b = t; \\\n} while (0)\n" +
				"#define MAX(a, b) ((a) > (b) ? (a) : (b))\n" +
				"int g(void) { return 1; }\n",
			want: []span{{"g", 5, 5}},
		},
		{
			name: "whole-function platform variants in #ifdef/#else are both chunks",
			code: "#ifdef _WIN32\nint plat(void) { return 1; }\n#else\nint plat(void) { return 2; }\n#endif\nint after(void) { return 3; }\n",
			want: []span{{"plat", 2, 2}, {"plat", 4, 4}, {"after", 6, 6}},
		},
		{
			name: "unbalanced braces across #ifdef branches inside a body",
			code: "int h(int x)\n{\n#ifdef FAST\n    if (x > 0) {\n#else\n    if (x >= 0) {\n#endif\n        x--;\n    }\n    return x;\n}\nint after(void) { return 0; }\n",
			want: []span{{"h", 1, 11}, {"after", 12, 12}},
		},
		{
			name: "function closed separately in each #ifdef branch",
			code: "int split(int x) {\n#ifdef A\n    return x;\n}\n#else\n    return -x;\n}\n#endif\nint after(void) { return 0; }\n",
			want: []span{{"split", 1, 7}, {"after", 9, 9}},
		},
		{
			name: "header variants in #ifdef/#else share one body",
			code: "#ifdef WIDE\nint conv(long v)\n#else\nint conv(int v)\n#endif\n{\n    return (int)v;\n}\n",
			want: []span{{"conv", 2, 8}},
		},
		{
			name: "#if 0 blocks are skipped",
			code: "#if 0\nint dead_code(void) {\n#endif\nint live(void) { return 1; }\n",
			want: []span{{"live", 4, 4}},
		},
		{
			name: "#if 0 with #else keeps the else branch",
			code: "#if 0\nint old(void) { return 0; }\n#else\nint current(void) { return 1; }\n#endif\n",
			want: []span{{"current", 4, 4}},
		},
		{
			name: "extern \"C\" wrapper in a header",
			code: "#ifdef __cplusplus\nextern \"C\" {\n#endif\nstatic inline int sq(int x) { return x * x; }\nint cube(int x);\n#ifdef __cplusplus\n}\n#endif\nint after(void) { return 0; }\n",
			want: []span{{"sq", 4, 4}, {"after", 9, 9}},
		},
		{
			name: "K&R definition",
			code: "int\nold_style(a, b)\n    int a;\n    char *b;\n{\n    return a + *b;\n}\n",
			want: []span{{"old_style", 1, 7}},
		},
		{
			name: "K&R-looking prototype list does not swallow the next definition",
			code: "int legacy();\nextern int f(void), g;\nint next_fn(int x) { return x; }\n",
			want: []span{{"next_fn", 3, 3}},
		},
		{
			name: "function returning a function pointer",
			code: "void (*get_handler(int sig))(int)\n{\n    return handlers[sig];\n}\n",
			want: []span{{"get_handler", 1, 4}},
		},
		{
			name: "attributes and export macros before the name",
			code: "static inline __attribute__((always_inline)) int fast(int x)\n{\n    return x << 1;\n}\n" +
				"LUA_API int lua_gettop (lua_State *L) {\n  return 0;\n}\n" +
				"__declspec(dllexport) int __cdecl exported(void) { return 1; }\n" +
				"LRESULT CALLBACK WndProc(HWND hwnd, UINT msg)\n{\n    return 0;\n}\n",
			want: []span{{"fast", 1, 4}, {"lua_gettop", 5, 7}, {"exported", 8, 8}, {"WndProc", 9, 12}},
		},
		{
			name: "top-level macro invocation without a semicolon is not part of the next function",
			code: "DEFINE_LOCK(global_lock)\nstatic void init(void)\n{\n}\n",
			want: []span{{"init", 2, 4}},
		},
		{
			name: "test-framework macro definitions get a synthetic symbol",
			code: "TEST(parser, handles_empty) {\n    ASSERT(1);\n}\nSHA256_ctx *SHA256(const unsigned char *d, size_t n) {\n    return 0;\n}\n",
			want: []span{{"TEST@L1", 1, 3}, {"SHA256", 4, 6}},
		},
		{
			name: "nested braces, struct literals and statements inside a body",
			code: "int main(int argc, char **argv)\n{\n    struct point p = { 1, 2 };\n    if (argc > 1) {\n        for (int i = 0; i < argc; i++) { puts(argv[i]); }\n    } else {\n        do { argc--; } while (argc);\n    }\n    switch (p.x) { case 1: break; default: break; }\n    return 0;\n}\n",
			want: []span{{"main", 1, 11}},
		},
		{
			name: "static assertion and file-scope declarations between functions",
			code: "int a(void) { return 1; }\n_Static_assert(sizeof(int) == 4, \"int\");\nstatic int counter = 0;\nint b(void) { return counter; }\n",
			want: []span{{"a", 1, 1}, {"b", 4, 4}},
		},
		{
			name: "unterminated function is dropped, not extended to EOF",
			code: "int ok(void) { return 1; }\nint broken(void) {\n    return 2;\n",
			want: []span{{"ok", 1, 1}},
		},
		{
			name: "parenthesized names that suppress macro expansion",
			code: "double (cimag)(double complex z)\n{\n\treturn cimag(z);\n}\n" +
				"LUALIB_API lua_State *(luaL_newstate) (void) {\n  return 0;\n}\n",
			want: []span{{"cimag", 1, 4}, {"luaL_newstate", 5, 7}},
		},
		{
			name: "an unrecognized braced declaration does not swallow the next function",
			code: "WEIRD_MACRO(x) = (y) {\n  1, 2\n}\nint next(void)\n{\n    return 0;\n}\n",
			want: []span{{"next", 4, 7}},
		},
		{
			name: "doc comment before a function is not part of the chunk",
			code: "/*\n * Adds numbers.\n */\nint sum(int a, int b)\n{\n    return a + b;\n}\n",
			want: []span{{"sum", 4, 7}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := spansOf(splitC(tc.code))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitC =\n  %v\nwant\n  %v", got, tc.want)
			}
		})
	}
}

func TestSplit_DispatchesCAndKeepsChunkCode(t *testing.T) {
	code := "#include <stdio.h>\n\nstatic int\ntwice(int x)\n{\n    return 2 * x;\n}\n"
	chunks := Split("m.c", code, tokenizer.C)
	if len(chunks) != 1 || chunks[0].Symbol != "twice" || chunks[0].Path != "m.c" || chunks[0].Kind != KindFunction {
		t.Fatalf("Split = %+v", chunks)
	}
	want := "static int\ntwice(int x)\n{\n    return 2 * x;\n}"
	if chunks[0].Code != want {
		t.Errorf("chunk code = %q, want %q", chunks[0].Code, want)
	}
	if name := chunks[0].Name(); name != "m.c:3-7 twice" {
		t.Errorf("Name() = %q", name)
	}
}

func TestSplit_CHeaderWithoutDefinitionsFallsBackToWholeFile(t *testing.T) {
	code := "#ifndef X_H\n#define X_H\nint f(int);\ntypedef struct { int a; } s_t;\n#endif\n"
	chunks := Split("x.h", code, tokenizer.C)
	if len(chunks) != 1 || chunks[0].Symbol != "" || chunks[0].StartLine != 1 {
		t.Fatalf("expected whole-file fallback, got %+v", chunks)
	}
}

// A long file of many small functions must split in linear time; the
// splitter runs on every C file in a scan.
func TestSplitC_ManyFunctions(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&b, "static int f%d(int x)\n{\n    return x + %d;\n}\n\n", i, i)
	}
	chunks := splitC(b.String())
	if len(chunks) != 2000 || chunks[1999].Symbol != "f1999" || chunks[1999].StartLine != 1999*5+1 {
		t.Fatalf("got %d chunks, last %+v", len(chunks), chunks[len(chunks)-1])
	}
}

func TestCDeclarations(t *testing.T) {
	code := "int proto(int);\n" + // 1
		"static void fwd(void);\n" + // 2
		"extern int a(void), b(int);\n" + // 3
		"typedef int (*handler)(int);\n" + // 4
		"static int (*table[])(int) = { proto };\n" + // 5
		"EXPORT_SYMBOL(proto);\n" + // 6
		"int proto(int x) { return x; }\n" // 7
	got := CDeclarations(code)
	want := map[int]map[string]bool{
		1: {"proto": true},
		2: {"fwd": true},
		3: {"a": true, "b": true},
		6: {"EXPORT_SYMBOL": true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CDeclarations = %v, want %v", got, want)
	}
}

func TestCDefinitionHeader(t *testing.T) {
	code := "#ifdef WIDE\nint conv(long v) /* conv */\n#else\nint conv(int v)\n#endif\n{\n    return conv_inner(v);\n}"
	name, names, brace, ok := CDefinitionHeader(code)
	if !ok || name != "conv" || len(names) != 2 || code[brace] != '{' {
		t.Fatalf("CDefinitionHeader = %q %v %d %v", name, names, brace, ok)
	}
	for _, n := range names {
		if code[n[0]:n[1]] != "conv" || n[0] > brace {
			t.Errorf("occurrence %v is %q", n, code[n[0]:n[1]])
		}
	}
	if _, _, _, ok := CDefinitionHeader("struct point { int x; };"); ok {
		t.Error("a struct body is not a function header")
	}
}
