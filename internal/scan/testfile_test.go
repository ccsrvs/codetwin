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

		// C: test/tests dirs; test_*, *_test(s), test-*, *-test, tst-*,
		// test/testN, *_unittest basenames; headers follow the same rule.
		{"tests/unit/unit1300.c", true},
		{"test/example.c", true},
		{"src/test_vfs.c", true},
		{"src/test1.c", true},
		{"test.c", true},
		{"lib/sbi_bitmap_test.c", true},
		{"src/zmalloc_tests.c", true},
		{"string/tst-strlcat.c", true},
		{"benchtests/test-memcpy.c", true},
		{"tools/speed-test.c", true},
		{"src/crc64_unittest.c", true},
		{"tests/helpers.h", true},
		{"drivers/base/test/property_kunit.c", true},
		{"lib/list_kunit.c", true},
		{"tools/testing/selftests/net/tcp_mmap.c", true},
		{"tools/testing/radix-tree/main.c", true},
		{"src/latest.c", false}, // "test" inside a word is not a convention
		{"src/contest.c", false},
		{"src/testing_mode.c", false}, // test_ prefix needs the underscore right after "test"
		{"src/attest/x.c", false},
		{"src/server.h", false},

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
