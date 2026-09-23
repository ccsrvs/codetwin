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

// Test entry points that a harness finds by name — curl's generated
// unit-test dispatcher, pytest collection, pytest fixtures and hooks —
// have no call site in the scanned code, so they must not be reported.
// Other unreferenced definitions in test files still are.
func TestNameDiscoveredTestEntryPointsAreNotDead(t *testing.T) {
	snippets := scanDir(t, map[string]string{
		"tests/unit/unit1300.c": "static void test_Curl_llist_dtor(void *key, void *value)\n{\n  (void)key;\n  (void)value;\n}\n\n" +
			"static int test_unit1300(const char *arg)\n{\n  return arg != 0;\n}\n\n" +
			"static int orphan_c_helper(void)\n{\n  return 1;\n}\n",
		"tests/test_download.py": "import pytest\n\n\nclass TestDownload:\n\n    @pytest.fixture(autouse=True, scope='class')\n    def _class_scope(self, env):\n        env.prepare()\n\n    def test_small_file(self, env):\n        assert env.get('/small')\n\n\ndef test_top_level(env):\n    assert env\n\n\ndef _orphan_py_helper():\n    return 1\n",
		"conftest.py":            "import pytest\n\n\ndef pytest_configure(config):\n    config.addinivalue_line('markers', 'slow')\n\n\n@pytest.fixture\ndef env():\n    return object()\n",
	})
	findings, _ := Analyze(snippets)
	got := findingsBySymbol(findings)
	for _, entry := range []string{"test_Curl_llist_dtor", "test_unit1300", "TestDownload", "_class_scope",
		"test_small_file", "test_top_level", "pytest_configure", "env"} {
		if f, ok := got[entry]; ok {
			t.Errorf("test entry point %s reported: %+v", entry, f)
		}
	}
	for _, orphan := range []string{"orphan_c_helper", "_orphan_py_helper"} {
		if f, ok := got[orphan]; !ok || f.Verdict != VerdictDead {
			t.Errorf("unreferenced test-file helper %s: want dead, got %+v (present=%v)", orphan, f, ok)
		}
	}
}
