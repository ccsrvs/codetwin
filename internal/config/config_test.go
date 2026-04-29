package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ── Load ──────────────────────────────────────────────────────────────────────

func TestLoad_MissingFileReturnsNilNoError(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("missing config should not be an error, got: %v", err)
	}
	if c != nil {
		t.Errorf("missing config should return nil Config, got: %+v", c)
	}
}

func TestLoad_ParsesFullConfig(t *testing.T) {
	dir := t.TempDir()
	body := `{
		"defaults": {
			"threshold": 0.5,
			"preview": true,
			"preview_lines": 15,
			"sort": "size"
		},
		"ignore_paths": ["vendor/**", "*_test.go"],
		"ignore_patterns": ["^\\s*log\\.info\\("]
	}`
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil config")
	}
	if c.Defaults == nil || c.Defaults.Threshold == nil || *c.Defaults.Threshold != 0.5 {
		t.Errorf("defaults.threshold not parsed correctly: %+v", c.Defaults)
	}
	if c.Defaults.Preview == nil || *c.Defaults.Preview != true {
		t.Errorf("defaults.preview not parsed correctly")
	}
	if c.Defaults.PreviewLines == nil || *c.Defaults.PreviewLines != 15 {
		t.Errorf("defaults.preview_lines not parsed correctly")
	}
	if c.Defaults.Sort == nil || *c.Defaults.Sort != "size" {
		t.Errorf("defaults.sort not parsed correctly")
	}
	if got := c.IgnorePaths; len(got) != 2 || got[0] != "vendor/**" || got[1] != "*_test.go" {
		t.Errorf("ignore_paths not parsed correctly: %v", got)
	}
	if got := c.IgnorePatterns; len(got) != 1 {
		t.Errorf("ignore_patterns not parsed correctly: %v", got)
	}
}

func TestLoad_ZeroDistinctFromMissing(t *testing.T) {
	// A user explicitly setting threshold to 0 must produce a non-nil
	// pointer so callers can tell the difference from "not specified".
	dir := t.TempDir()
	body := `{"defaults": {"threshold": 0}}`
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Defaults == nil || c.Defaults.Threshold == nil {
		t.Fatalf("threshold pointer should be non-nil for explicit zero")
	}
	if *c.Defaults.Threshold != 0 {
		t.Errorf("threshold value: got %v, want 0", *c.Defaults.Threshold)
	}
}

func TestLoad_GivenIgnorePairs_WhenParsed_ThenPopulatesField(t *testing.T) {
	dir := t.TempDir()
	body := `{
		"ignore_pairs": [
			{"a": "internal/foo/util.go", "b": "internal/bar/util.go"},
			{"a": "auth/handler.go parseRequest", "b": "api/middleware.go parseRequest"}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(c.IgnorePairs) != 2 {
		t.Fatalf("ignore_pairs: got %d entries, want 2", len(c.IgnorePairs))
	}
	if c.IgnorePairs[0].A != "internal/foo/util.go" || c.IgnorePairs[0].B != "internal/bar/util.go" {
		t.Errorf("entry[0]: got %+v", c.IgnorePairs[0])
	}
	if c.IgnorePairs[1].A != "auth/handler.go parseRequest" {
		t.Errorf("entry[1].A: got %q", c.IgnorePairs[1].A)
	}
}

func TestLoad_BadJSONErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

// ── globToRegex / IgnoreMatcher ───────────────────────────────────────────────

func TestIgnoreMatcher_PlainSubstring(t *testing.T) {
	m, err := CompileIgnorePaths([]string{"vendor"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		path string
		want bool
	}{
		{"vendor/x.go", true},
		{"src/vendor/x.go", true},
		{"vendor", true},
		{"src/lib.go", false},
	}
	for _, c := range cases {
		if got := m.Match(c.path, false); got != c.want {
			t.Errorf("substring %q vs %q: got %v, want %v", "vendor", c.path, got, c.want)
		}
	}
}

func TestIgnoreMatcher_StarGlob(t *testing.T) {
	m, err := CompileIgnorePaths([]string{"*_test.go"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		path string
		want bool
	}{
		{"foo_test.go", true},
		{"src/bar_test.go", true},  // matches via basename
		{"src/baz/qux_test.go", true},
		{"foo.go", false},
		{"test.go", false},
	}
	for _, c := range cases {
		if got := m.Match(c.path, false); got != c.want {
			t.Errorf("*_test.go vs %q: got %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIgnoreMatcher_DoubleStarGlob(t *testing.T) {
	m, err := CompileIgnorePaths([]string{"vendor/**"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		path string
		want bool
	}{
		{"vendor/lib.go", true},
		{"vendor/sub/deep/file.go", true},
		{"src/lib.go", false},
	}
	for _, c := range cases {
		if got := m.Match(c.path, false); got != c.want {
			t.Errorf("vendor/** vs %q: got %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIgnoreMatcher_DirOnly(t *testing.T) {
	m, err := CompileIgnorePaths([]string{"build/"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !m.Match("project/build", true) {
		t.Errorf("build/ should match directory")
	}
	// A file literally named "build" must not match a dir-only rule.
	if m.Match("project/build", false) {
		t.Errorf("build/ should NOT match a non-directory called 'build'")
	}
}

func TestIgnoreMatcher_NilSafe(t *testing.T) {
	var m *IgnoreMatcher
	if m.Match("anything", false) {
		t.Errorf("nil matcher must always return false")
	}
}

func TestIgnoreMatcher_EmptyPatternIsSkipped(t *testing.T) {
	m, err := CompileIgnorePaths([]string{"", "/", "real_pattern"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Empty and "/" patterns must be skipped; only "real_pattern" survives.
	// Plain patterns now match path components, so "real_pattern" hits a
	// path with that as a component but not a similarly-named file.
	if !m.Match("foo/real_pattern/x.go", false) {
		t.Errorf("real_pattern should match a path component")
	}
	if m.Match("foo/real_pattern.go", false) {
		t.Errorf("real_pattern must NOT match real_pattern.go (different name)")
	}
	if m.Match("anything", false) {
		t.Errorf("empty patterns must not cause spurious matches")
	}
}

func TestIgnoreMatcher_MultiComponentLiteral(t *testing.T) {
	m, err := CompileIgnorePaths([]string{"vendor/lib"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		path string
		want bool
	}{
		{"vendor/lib/x.go", true},
		{"src/vendor/lib/x.go", true},
		{"src/vendor/lib", true},
		{"vendor/library/x.go", false}, // boundary check: "lib" must not match "library"
		{"src/lib/x.go", false},
	}
	for _, c := range cases {
		if got := m.Match(c.path, false); got != c.want {
			t.Errorf("vendor/lib vs %q: got %v, want %v", c.path, got, c.want)
		}
	}
}

// ── ignore_patterns regexes ───────────────────────────────────────────────────

func TestCompileIgnorePatterns_Valid(t *testing.T) {
	res, err := CompileIgnorePatterns([]string{`^\s*log\.info\(`, `^println!\(`})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(res) != 2 {
		t.Errorf("expected 2 compiled regexes, got %d", len(res))
	}
	// The (?m) flag must be in effect — these patterns anchor with ^.
	if !res[0].MatchString("    log.info(\"hi\")") {
		t.Errorf("first regex should match an indented log.info call")
	}
}

func TestCompileIgnorePatterns_InvalidReportsAllErrors(t *testing.T) {
	_, err := CompileIgnorePatterns([]string{`(unclosed`, `[noend`, `valid`})
	if err == nil {
		t.Fatal("expected error for invalid regexes")
	}
	// Should mention BOTH bad patterns so the user fixes them in one shot.
	msg := err.Error()
	if !contains(msg, "(unclosed") || !contains(msg, "[noend") {
		t.Errorf("error should mention both invalid patterns; got: %s", msg)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
