package cache

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/fingerprint"
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
				// Must be the live constant: Load treats entries whose
				// chunks carry any other K as stale misses.
				K: fingerprint.DefaultK,
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

// TestLoad_ChunkKMismatchIsAMiss guards the DefaultK-retune trap: a cache
// written under an old fingerprint.DefaultK stores per-chunk K values that
// no longer match the current constant. Serving those entries would hand
// downstream code fingerprints built from differently-sized k-grams —
// silently wrong similarity scores. Entries whose chunks carry a stale K
// must be treated as misses on Load (and get rebuilt). We simulate the
// retune by writing a chunk with K = DefaultK+1, as if DefaultK had been
// bumped since the file was written.
func TestLoad_ChunkKMismatchIsAMiss(t *testing.T) {
	dir := t.TempDir()
	c := New()
	c.Put("stale", Entry{
		ContentHash: "x",
		Chunks:      []Chunk{{Name: "f", K: fingerprint.DefaultK + 1, Hashes: []uint32{1}}},
	})
	c.Put("fresh", Entry{
		ContentHash: "y",
		Chunks:      []Chunk{{Name: "g", K: fingerprint.DefaultK, Hashes: []uint32{2}}},
	})
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	c2, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := c2.Get("stale"); ok {
		t.Error("entry with chunk K != fingerprint.DefaultK must be a miss, was served")
	}
	if _, ok := c2.Get("fresh"); !ok {
		t.Error("entry with current K should still be served")
	}
}

// TestLoad_SchemaMismatchReturnsEmpty: a cache written under a different
// algorithm-parameter schema (different k/w, tokenizer schema, or cache
// version) must be dropped wholesale on Load — no manual Version bump
// should ever be required for a parameter retune to invalidate.
func TestLoad_SchemaMismatchReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	old := &Cache{
		Version: Version,
		Schema:  SchemaTag() + ";retuned",
		Entries: map[string]Entry{"k1": {ContentHash: "x"}},
		dirty:   true,
	}
	if err := old.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	c, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(c.Entries) != 0 {
		t.Errorf("schema-mismatched cache should yield empty Entries, got %d", len(c.Entries))
	}
	if c.Schema != SchemaTag() {
		t.Errorf("fresh cache should carry the current schema tag, got %q", c.Schema)
	}
}

// TestSchemaTag_DistinctPerComponent: every algorithm parameter folded
// into the schema tag must change the tag on its own, so a retune of any
// single one auto-invalidates the cache.
func TestSchemaTag_DistinctPerComponent(t *testing.T) {
	base := schemaTag(3, 10, 4, 1, 1)
	variants := map[string]string{
		"cache version":      schemaTag(4, 10, 4, 1, 1),
		"fingerprint k":      schemaTag(3, 11, 4, 1, 1),
		"winnowing w":        schemaTag(3, 10, 5, 1, 1),
		"fingerprint schema": schemaTag(3, 10, 4, 2, 1),
		"tokenizer schema":   schemaTag(3, 10, 4, 1, 2),
	}
	for name, v := range variants {
		if v == base {
			t.Errorf("changing %s did not change the schema tag: %q", name, base)
		}
	}
	if got := schemaTag(3, 10, 4, 1, 1); got != base {
		t.Errorf("schemaTag must be deterministic: %q vs %q", got, base)
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

func TestHashContent_IsStableHexEncoded(t *testing.T) {
	got := HashContent([]byte("hello world"))
	// SHA-256 of "hello world", well-known.
	want := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if got != want {
		t.Errorf("HashContent(\"hello world\") = %q, want %q", got, want)
	}
	if HashContent([]byte("hello world")) != got {
		t.Error("HashContent must be deterministic")
	}
	if HashContent([]byte("hello world!")) == got {
		t.Error("HashContent must differ for different inputs")
	}
	// Empty input should still produce a 64-char hex string (SHA-256 of "").
	if got := HashContent(nil); len(got) != 64 {
		t.Errorf("empty input should produce 64-hex string, got %d chars: %q", len(got), got)
	}
}

// TestNilCache_GetPutSaveAreNoops covers the early-return guards on
// every public method when called against a nil receiver — handy for
// callers that pass a possibly-uninitialised cache without a guard.
func TestNilCache_GetPutSaveAreNoops(t *testing.T) {
	var c *Cache
	if e, ok := c.Get("any"); ok || e.ContentHash != "" || e.Chunks != nil {
		t.Errorf("nil.Get should return zero entry + false, got %+v ok=%v", e, ok)
	}
	c.Put("any", Entry{ContentHash: "x"})
	if err := c.Save(t.TempDir()); err != nil {
		t.Errorf("nil.Save should be a no-op, got error: %v", err)
	}
}

// TestSave_CreateFails_ReturnsWrappedError covers the os.Create error
// branch in Save by pointing at a directory that doesn't exist (so the
// .tmp file cannot be created).
func TestSave_CreateFails_ReturnsWrappedError(t *testing.T) {
	c := New()
	c.Put("k", Entry{ContentHash: "x"})
	err := c.Save("/nonexistent/codetwin/should/not/exist")
	if err == nil {
		t.Fatal("expected error when target dir does not exist")
	}
	if !strings.Contains(err.Error(), "cache create:") {
		t.Errorf("error %q lacks the `cache create:` prefix", err)
	}
}

// TestLoad_OpenError_ReturnsWrappedError covers the non-IsNotExist
// branch of os.Open's error. We point Load at a "directory" that's
// actually a regular file, so joining `Filename` onto it causes
// os.Open to fail with ENOTDIR — distinct from ENOENT and so distinct
// from the "missing → empty cache" path. Works even as root.
func TestLoad_OpenError_ReturnsWrappedError(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "notadir")
	if err := os.WriteFile(notADir, []byte("file, not dir"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(notADir); err == nil {
		t.Fatal("expected open error when dir argument is actually a file")
	} else if !strings.Contains(err.Error(), "cache open:") {
		t.Errorf("error %q lacks the `cache open:` prefix", err)
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
