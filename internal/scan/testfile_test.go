package scan

import "testing"

func TestIsTestFile_TableDriven(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// Go: only the *_test.go suffix counts.
		{"foo_test.go", true},
		{"internal/report/report_test.go", true},
		{"foo.go", false},
		{"tests/foo.go", false},           // Go has no tests/ convention
		{"my_test.go.bak", false},         // unsupported extension
		{"attest.go", false},              // suffix must be _test.go
		{"/abs/path/pkg/x_test.go", true}, // absolute paths work

		// Python: test_*.py, *_test.py, tests/ or test/ dir component.
		{"test_foo.py", true},
		{"foo_test.py", true},
		{"tests/foo.py", true},
		{"test/foo.py", true},
		{"pkg/tests/deep/foo.py", true},
		{"src/foo.py", false},
		{"latest_run.py", false},  // "test_" must be a prefix
		{"contest.py", false},     // not a test convention
		{"attests/foo.py", false}, // component must match exactly

		// JS/TS: *.spec.*, *.test.*, __tests__/ dir component.
		{"foo.spec.ts", true},
		{"foo.test.js", true},
		{"component.spec.tsx", true},
		{"widget.test.jsx", true},
		{"__tests__/foo.js", true},
		{"src/__tests__/deep/foo.ts", true},
		{"src/foo.ts", false},
		{"detest.js", false},
		{"spec.js", false}, // needs the ".spec." infix

		// Java: src/test/ path component sequence.
		{"src/test/java/FooTest.java", true},
		{"app/src/test/java/FooTest.java", true},
		{"src/main/java/Foo.java", false},
		{"test/Foo.java", false}, // must be src/test, not bare test/
		{"src/testing/F.java", false},

		// Rust: tests/ dir component.
		{"tests/integration.rs", true},
		{"crate/tests/it/main.rs", true},
		{"src/lib.rs", false},
		{"src/tests/helpers.rs", true},

		// Elixir: *_test.exs, test/ dir component.
		{"foo_test.exs", true},
		{"test/foo_test.exs", true},
		{"test/support/conn_case.ex", true},
		{"lib/foo.ex", false},
		{"lib/foo.exs", false},

		// Unsupported extensions never classify.
		{"tests/foo.txt", false},
		{"foo_test", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsTestFile(tc.path); got != tc.want {
			t.Errorf("IsTestFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
