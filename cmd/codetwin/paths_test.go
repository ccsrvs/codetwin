package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/ccsrvs/codetwin/internal/config"
)

// ── collectFiles + IgnoreMatcher integration ──────────────────────────────────

func TestCollectFiles_DotPathDoesNotSkipEverything(t *testing.T) {
	// Regression: passing "." as the scan path used to skip the entire
	// walk because d.Name() == "." matched the "skip dotfile dirs" rule.
	root := t.TempDir()
	mustWriteFiles(t, root, map[string]string{
		"a.go": "package x\n",
		"b.py": "x = 1\n",
	})
	// Run from inside root with "." as the path argument.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(cwd) //nolint
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	files, err := collectFiles([]string{"."}, nil)
	if err != nil {
		t.Fatalf("collectFiles: %v", err)
	}
	if len(files) < 2 {
		t.Errorf("expected at least 2 files when scanning '.', got %d: %v", len(files), files)
	}
}

func TestCollectFiles_NoIgnoreMatcher(t *testing.T) {
	root := t.TempDir()
	mustWriteFiles(t, root, map[string]string{
		"a.go":      "package x\n",
		"sub/b.py":  "def foo(): pass\n",
		"sub/c.txt": "not a source file",
	})
	files, err := collectFiles([]string{root}, nil)
	if err != nil {
		t.Fatalf("collectFiles: %v", err)
	}
	got := relPaths(files, root)
	sort.Strings(got)
	want := []string{"a.go", "sub/b.py"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectFiles_IgnoreFileGlob(t *testing.T) {
	root := t.TempDir()
	mustWriteFiles(t, root, map[string]string{
		"foo.go":      "package x\n",
		"foo_test.go": "package x\n",
		"sub/bar_test.go": "package x\n",
	})
	matcher, err := config.CompileIgnorePaths([]string{"*_test.go"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	files, err := collectFiles([]string{root}, matcher)
	if err != nil {
		t.Fatalf("collectFiles: %v", err)
	}
	got := relPaths(files, root)
	sort.Strings(got)
	want := []string{"foo.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectFiles_IgnoreDirectory(t *testing.T) {
	root := t.TempDir()
	mustWriteFiles(t, root, map[string]string{
		"src/a.go":         "package x\n",
		"vendor/lib/x.go":  "package y\n",
		"vendor/lib/y.go":  "package y\n",
	})
	matcher, err := config.CompileIgnorePaths([]string{"vendor/**"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	files, err := collectFiles([]string{root}, matcher)
	if err != nil {
		t.Fatalf("collectFiles: %v", err)
	}
	got := relPaths(files, root)
	sort.Strings(got)
	want := []string{"src/a.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// ── small fs helpers ──────────────────────────────────────────────────────────

func mustWriteFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func relPaths(files []string, root string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		r, err := filepath.Rel(root, f)
		if err != nil {
			r = f
		}
		out = append(out, filepath.ToSlash(r))
	}
	return out
}
