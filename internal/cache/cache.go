// Package cache stores per-file tokenization output between codetwin runs.
//
// The expensive per-file work (split → tokenize → fingerprint with positions)
// is deterministic given the file content + ignore_patterns + tokenizer
// version. We hash those into a key and persist a compact gob blob at
// `.codetwin-cache.bin` in the working directory. On the next run we skip
// any file whose key still matches.
//
// What's NOT cached: TF-IDF vectors (corpus-dependent, must be recomputed)
// and the n² pair matrix (also corpus-dependent). The cache covers the work
// that scales with file count, not the work that scales with pair count.
//
// Cache invalidation is automatic on:
//   - file content change (content hash mismatch)
//   - ignore_patterns change (patterns hash mismatch)
//   - codetwin tokenizer/splitter upgrades (Version constant bump)
package cache

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Filename is the on-disk cache file in the working directory.
const Filename = ".codetwin-cache.bin"

// Version is bumped whenever the cached schema or tokenizer/splitter output
// format changes. Old entries with a different version are dropped on Load.
//
// v3: added Chunk.LexTerms (raw-code lexical vocabulary for the
// structural-twin label gate). Caches written by earlier versions lack
// the field and are invalidated wholesale on Load.
//
// v4: added Chunk.Kind and class-span chunks for Python/Java/JS
// (§5.2 class-level granularity). Entries written by earlier versions
// were split without class chunks, so they must be invalidated
// wholesale — a stale entry would silently drop class findings.
const Version uint32 = 4

// Chunk mirrors enough of the tokenizer + fingerprint output to reconstruct
// a snippet without rerunning either. Tokens are stored as raw strings;
// fingerprints are stored as a flat uint32 list plus the position map, then
// re-materialized into a Set on load.
type Chunk struct {
	Name       string
	Lang       string
	Kind       string // mirrors splitter.Chunk.Kind ("function" or "class")
	StartLine  int
	EndLine    int
	Code       string
	Tokens     []string
	Lines      []int
	NonBlankLn int
	Hashes     []uint32
	Positions  map[uint32][]int
	K          int

	// LexTerms mirrors scan.Snippet.LexTerms: the chunk's sorted
	// raw-code vocabulary (tokenizer.LexicalTerms), persisted so cache
	// hits skip the raw-code pass along with everything else.
	LexTerms []string
}

// Entry is the cached output for one source file: every chunk plus the
// content hash that produced it.
type Entry struct {
	ContentHash string
	Chunks      []Chunk
}

// Cache is a content-addressable map of cache keys to entries. Key
// derivation includes the patterns hash, so changing ignore_patterns
// invalidates all entries automatically.
type Cache struct {
	mu      sync.Mutex
	Version uint32
	Entries map[string]Entry
	dirty   bool
}

// Load reads a cache from `dir`. Returns an empty cache when the file is
// missing or its version doesn't match the current code. Any other I/O
// error is returned.
func Load(dir string) (*Cache, error) {
	path := filepath.Join(dir, Filename)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, fmt.Errorf("cache open: %w", err)
	}
	defer f.Close()

	var c Cache
	if err := gob.NewDecoder(f).Decode(&c); err != nil {
		// Corrupt cache → start fresh rather than fail the run.
		return New(), nil
	}
	if c.Version != Version || c.Entries == nil {
		return New(), nil
	}
	return &c, nil
}

// New returns a fresh empty cache at the current Version.
func New() *Cache {
	return &Cache{Version: Version, Entries: map[string]Entry{}}
}

// Get returns the cached entry for key, if any. The returned bool reports
// hit/miss separately from the entry's content (so a zero-Chunks entry can
// be distinguished from a missing one if needed).
func (c *Cache) Get(key string) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.Entries[key]
	return e, ok
}

// Put stores entry under key. Marks the cache dirty so Save knows to write.
func (c *Cache) Put(key string, e Entry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Entries[key] = e
	c.dirty = true
}

// Save writes the cache to `dir/Filename` atomically (write to .tmp, then
// rename) so a crash mid-write doesn't leave a corrupt file. No-op if
// nothing has been Put since Load.
func (c *Cache) Save(dir string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}

	path := filepath.Join(dir, Filename)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("cache create: %w", err)
	}
	if err := gob.NewEncoder(f).Encode(c); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("cache encode: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("cache close: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("cache rename: %w", err)
	}
	c.dirty = false
	return nil
}

// HashContent returns a hex-encoded SHA-256 of the given byte slice.
// Used to detect file-content changes between runs.
func HashContent(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// PatternsHash returns a stable hex hash for a slice of regex pattern
// strings. Order-insensitive (sorted before hashing) so reordering the
// same patterns doesn't invalidate the cache.
func PatternsHash(patterns []string) string {
	if len(patterns) == 0 {
		return ""
	}
	sorted := make([]string, len(patterns))
	copy(sorted, patterns)
	sort.Strings(sorted)
	h := sha256.New()
	for _, p := range sorted {
		h.Write([]byte(p))
		h.Write([]byte{0}) // separator so "ab" + "c" != "a" + "bc"
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Key combines a file's absolute path, its content hash, and the active
// ignore_patterns hash into a stable cache key. Path is included so two
// files with identical content but different paths don't share an entry
// (their chunk names differ).
func Key(absPath, contentHash, patternsHash string) string {
	h := sha256.New()
	h.Write([]byte(absPath))
	h.Write([]byte{0})
	h.Write([]byte(contentHash))
	h.Write([]byte{0})
	h.Write([]byte(patternsHash))
	return hex.EncodeToString(h.Sum(nil))
}
