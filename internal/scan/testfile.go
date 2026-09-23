package scan

import (
	"path/filepath"
	"strings"
)

// IsTestFile reports whether path follows the well-known test-file
// convention for its language (determined by extension). Classification
// is purely lexical — no file contents are read — so it can run on
// cached snippets and on paths that no longer exist.
//
// Conventions per language:
//
//	Go       *_test.go
//	Python   test_*.py, *_test.py, or a tests/ or test/ directory component
//	JS/TS    *.spec.*, *.test.*, or a __tests__/ directory component
//	Java     a src/test/ path component sequence
//	Rust     a tests/ directory component
//	Elixir   *_test.exs, or a test/ directory component
//	C        test_*, *_test, *_tests, test-*, *-test, tst-*, *_unittest,
//	         *_kunit, test/testN (test.c, test1.c), or a test/, tests/,
//	         testing/, or selftests/ directory component; .h files follow
//	         the same rule
//
// Paths in unsupported extensions are never classified as tests.
func IsTestFile(path string) bool {
	p := filepath.ToSlash(path)
	base := p
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		base = p[i+1:]
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".go":
		return strings.HasSuffix(base, "_test.go")
	case ".py":
		return strings.HasPrefix(base, "test_") ||
			strings.HasSuffix(base, "_test.py") ||
			hasDirComponent(p, "tests") ||
			hasDirComponent(p, "test")
	case ".js", ".jsx", ".ts", ".tsx":
		return strings.Contains(base, ".spec.") ||
			strings.Contains(base, ".test.") ||
			hasDirComponent(p, "__tests__")
	case ".java":
		return hasDirSequence(p, "src", "test")
	case ".rs":
		return hasDirComponent(p, "tests")
	case ".ex", ".exs":
		return strings.HasSuffix(base, "_test.exs") ||
			hasDirComponent(p, "test")
	case ".c", ".h":
		return cTestStem(strings.TrimSuffix(base, filepath.Ext(base))) ||
			hasDirComponent(p, "test") ||
			hasDirComponent(p, "tests") ||
			hasDirComponent(p, "testing") ||
			hasDirComponent(p, "selftests")
	}
	return false
}

// cTestStem reports whether a C file's name (without extension) follows
// a test naming convention seen across C projects: test_ / _test(s)
// (SQLite, the Linux kernel, OpenSBI), test- / -test and glibc's tst-,
// _unittest, the kernel's _kunit, and test harness drivers named test,
// test1, test2, ...
func cTestStem(stem string) bool {
	for _, prefix := range []string{"test_", "test-", "tst-"} {
		if strings.HasPrefix(stem, prefix) {
			return true
		}
	}
	for _, suffix := range []string{"_test", "_tests", "-test", "_unittest", "_kunit"} {
		if strings.HasSuffix(stem, suffix) {
			return true
		}
	}
	if rest, ok := strings.CutPrefix(stem, "test"); ok {
		return strings.Trim(rest, "0123456789") == ""
	}
	return false
}

// hasDirComponent reports whether name appears as a directory component
// of the slash-normalized path (the basename is excluded — a file named
// "tests" is not a directory).
func hasDirComponent(path, name string) bool {
	comps := strings.Split(path, "/")
	for _, c := range comps[:len(comps)-1] {
		if c == name {
			return true
		}
	}
	return false
}

// hasDirSequence reports whether the directory components of path
// contain first immediately followed by second (e.g. "src", "test" for
// the Maven/Gradle layout src/test/java/...).
func hasDirSequence(path, first, second string) bool {
	comps := strings.Split(path, "/")
	dirs := comps[:len(comps)-1]
	for i := 0; i+1 < len(dirs); i++ {
		if dirs[i] == first && dirs[i+1] == second {
			return true
		}
	}
	return false
}
