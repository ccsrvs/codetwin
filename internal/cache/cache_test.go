package cache

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoad_MissingReturnsEmptyCache(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("missing cache file should not be an error, got: %v", err)
	}
	if c == nil {
		t.Fatal("Load must never return nil when no error")
	}
	if c.Version != Version {
		t.Errorf("expected Version=%d, got %d", Version, c.Version)
	}
	if c.Entries == nil {
		t.Error("Entries map should be initialized")
	}
}

func TestRoundTrip_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	c := New()
	c.Put("k1", Entry{
		ContentHash: "deadbeef",
		Chunks: []Chunk{
			{
				Name:       "foo.go:3-9 SumSlice",
				Lang:       "go",
				StartLine:  3,
				EndLine:    9,
				Code:       "func SumSlice() {}",
				Tokens:     []string{"func", "VAR"},
				Lines:      []int{1, 1},
				NonBlankLn: 1,
				Hashes:     []uint32{1, 2, 3},
				Positions:  map[uint32][]int{1: {0}, 2: {1}, 3: {2}},
				K:          5,
			},
		},
	})
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	c2, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, ok := c2.Get("k1")
	if !ok {
		t.Fatal("expected entry k1 to round-trip")
	}
	if !reflect.DeepEqual(got, c.Entries["k1"]) {
		t.Errorf("round-trip mismatch:\n  got:  %+v\n  want: %+v", got, c.Entries["k1"])
	}
}

func TestSave_NoOpWhenClean(t *testing.T) {
	dir := t.TempDir()
	c := New()
	if err := c.Save(dir); err != nil {
		t.Fatalf("save on empty: %v", err)
	}
	// Save with no Puts should NOT create the file.
	if _, err := os.Stat(filepath.Join(dir, Filename)); !os.IsNotExist(err) {
		t.Errorf("Save with nothing to write should not touch the filesystem")
	}
}

func TestLoad_VersionMismatchReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	c := New()
	c.Put("k1", Entry{ContentHash: "x"})
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Manually corrupt the version: rewrite the file with a wrong version.
	bad := &Cache{Version: Version + 99, Entries: map[string]Entry{"k1": {}}}
	bad.dirty = true
	if err := bad.Save(dir); err != nil {
		t.Fatalf("save bad: %v", err)
	}

	c2, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(c2.Entries) != 0 {
		t.Errorf("version-mismatched cache should yield empty Entries, got %d", len(c2.Entries))
	}
}

func TestLoad_CorruptFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte("not a gob blob"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("Load on corrupt should return empty cache, not error: %v", err)
	}
	if len(c.Entries) != 0 {
		t.Errorf("corrupt cache should yield empty Entries, got %d", len(c.Entries))
	}
}

func TestPatternsHash_OrderInsensitive(t *testing.T) {
	a := PatternsHash([]string{"^log\\.", "^debug\\."})
	b := PatternsHash([]string{"^debug\\.", "^log\\."})
	if a != b {
		t.Errorf("PatternsHash should be order-insensitive: %q vs %q", a, b)
	}
}

func TestPatternsHash_DifferentForDifferentSets(t *testing.T) {
	a := PatternsHash([]string{"^log"})
	b := PatternsHash([]string{"^debug"})
	if a == b {
		t.Errorf("PatternsHash collisions are bad: %q", a)
	}
}

func TestPatternsHash_EmptyIsStable(t *testing.T) {
	if got := PatternsHash(nil); got != "" {
		t.Errorf("empty patterns should give empty hash, got %q", got)
	}
	if got := PatternsHash([]string{}); got != "" {
		t.Errorf("empty patterns should give empty hash, got %q", got)
	}
}

func TestKey_DeterministicForSameInputs(t *testing.T) {
	k1 := Key("/a/b/c.go", "abc", "xyz")
	k2 := Key("/a/b/c.go", "abc", "xyz")
	if k1 != k2 {
		t.Errorf("Key should be deterministic: %q vs %q", k1, k2)
	}
}

func TestKey_DistinctForDifferentInputs(t *testing.T) {
	cases := []struct {
		name      string
		a, b      string
		different bool
	}{
		{"different path", Key("/a.go", "h1", "p"), Key("/b.go", "h1", "p"), true},
		{"different content", Key("/a.go", "h1", "p"), Key("/a.go", "h2", "p"), true},
		{"different patterns", Key("/a.go", "h1", "p1"), Key("/a.go", "h1", "p2"), true},
	}
	for _, c := range cases {
		if c.different && c.a == c.b {
			t.Errorf("%s: keys should differ but match: %q", c.name, c.a)
		}
	}
}

func TestSave_AtomicViaTempFile(t *testing.T) {
	// A successful Save should leave the cache file in place but no stray
	// .tmp file behind.
	dir := t.TempDir()
	c := New()
	c.Put("k", Entry{ContentHash: "x"})
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, Filename)); err != nil {
		t.Errorf("expected cache file to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, Filename+".tmp")); !os.IsNotExist(err) {
		t.Errorf(".tmp file must not be left behind after a successful save")
	}
}
