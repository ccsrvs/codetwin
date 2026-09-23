package deadcode

import "testing"

func TestCVerdicts(t *testing.T) {
	snippets := scanDir(t, map[string]string{
		"util.h": "#ifndef UTIL_H\n#define UTIL_H\n" +
			"int dead_exported(int x);\n" +
			"int api_used(int x);\n" +
			"static inline int header_helper(int x) { return x; }\n" +
			"#endif\n",
		"util.c": "#include \"util.h\"\n" +
			"static int dead_static(int x) { return x; }\n" +
			"static int live_static(int x) { return x + 1; }\n" +
			"int dead_exported(int x) { return x * 2; }\n" +
			"int api_used(int x) { return live_static(x); }\n" +
			"static int via_pointer(int x) { return x; }\n" +
			"static int (*table[])(int) = { via_pointer };\n" +
			"/* mentions comment_only */\n" +
			"static int comment_only(int x) { return x; }\n",
		"main.c": "#include \"util.h\"\n" +
			"static int forward_only(int);\n" +
			"int main(int argc, char **argv) { return api_used(argc); }\n" +
			"static int forward_only(int x) { return x; }\n" +
			"__attribute__((constructor)) static void setup(void) { }\n" +
			"int LLVMFuzzerTestOneInput(const unsigned char *d, unsigned long n) { return 0; }\n",
	})
	findings, warnings := Analyze(snippets)
	if len(warnings) > 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	got := findingsBySymbol(findings)
	for sym, want := range map[string]Verdict{
		"dead_static":   VerdictDead,
		"forward_only":  VerdictDead,         // its forward prototype is not a use
		"comment_only":  VerdictDead,         // nor is a comment
		"dead_exported": VerdictUnusedInScan, // external linkage; header prototype is not a use
		"header_helper": VerdictUnusedInScan, // static inline in a header is includable API
	} {
		if f, ok := got[sym]; !ok || f.Verdict != want {
			t.Errorf("%s: want %s, got %+v (present=%v)", sym, want, f, ok)
		}
	}
	for _, alive := range []string{"api_used", "live_static", "via_pointer", "main", "setup", "LLVMFuzzerTestOneInput"} {
		if f, ok := got[alive]; ok {
			t.Errorf("%s must not be reported, got %+v", alive, f)
		}
	}
}

func TestCTestMacroDefinitionsAreNotDeadCode(t *testing.T) {
	snippets := scanDir(t, map[string]string{
		"tests/test_parser.c": "TEST(parser, empty) {\n    ASSERT(1);\n}\nTEST(parser, full) {\n    ASSERT(2);\n}\n",
	})
	findings, _ := Analyze(snippets)
	if len(findings) != 0 {
		t.Errorf("macro-generated test definitions must not be findings: %+v", findings)
	}
}
