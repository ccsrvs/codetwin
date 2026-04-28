package pathutil

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestContains_TrueForChild(t *testing.T) {
	parent := filepath.Clean("/tmp/codetwin")
	child := filepath.Join(parent, "sub", "file.go")
	if !Contains(parent, child) {
		t.Errorf("expected %q to be contained in %q", child, parent)
	}
}

func TestContains_FalseForSiblingsThatSharePrefix(t *testing.T) {
	a := filepath.Clean("/tmp/codetwin")
	b := filepath.Clean("/tmp/codetwinNEW") // same prefix, different directory
	if Contains(a, b) {
		t.Errorf("%q must not be considered contained in %q (no separator break)", b, a)
	}
}

func TestContains_FalseForIdentical(t *testing.T) {
	p := filepath.Clean("/tmp/codetwin")
	if Contains(p, p) {
		t.Errorf("a path is not 'contained' in itself")
	}
}

func TestContains_FalseForUnrelatedPaths(t *testing.T) {
	if Contains("/foo", "/bar/baz") {
		t.Errorf("unrelated paths must not be reported as contained")
	}
}

func TestDedupe_RemovesExactDuplicates(t *testing.T) {
	dir := t.TempDir()
	out, err := Dedupe([]string{dir, dir, dir})
	if err != nil {
		t.Fatalf("Dedupe returned err: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("expected 1 path after dedup, got %d: %v", len(out), out)
	}
}

func TestDedupe_KeepsOuterDropsInner(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "sub")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	out, err := Dedupe([]string{root, inner})
	if err != nil {
		t.Fatalf("Dedupe returned err: %v", err)
	}
	if len(out) != 1 || out[0] != root {
		t.Errorf("expected only outer path %q kept, got %v", root, out)
	}
}

func TestDedupe_KeepsOuterDropsFileInside(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.go")
	if err := os.WriteFile(file, []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := Dedupe([]string{root, file})
	if err != nil {
		t.Fatalf("Dedupe returned err: %v", err)
	}
	if len(out) != 1 || out[0] != root {
		t.Errorf("file inside dir should be dropped; got %v", out)
	}
}

func TestDedupe_KeepsSiblings(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	out, err := Dedupe([]string{rootA, rootB})
	if err != nil {
		t.Fatalf("Dedupe returned err: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("two unrelated dirs should both be kept, got %v", out)
	}
}

func TestDedupe_PreservesInputOrder(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	c := t.TempDir()
	in := []string{c, a, b}
	out, err := Dedupe(in)
	if err != nil {
		t.Fatalf("Dedupe returned err: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("expected order preserved %v, got %v", in, out)
	}
}

func TestDedupe_EmptyInput(t *testing.T) {
	out, err := Dedupe(nil)
	if err != nil {
		t.Fatalf("Dedupe returned err: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty output, got %v", out)
	}
}
