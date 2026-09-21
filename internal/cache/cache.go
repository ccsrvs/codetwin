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
//   - cache storage format change (Version constant bump)
//   - algorithm parameter change — fingerprint.DefaultK/DefaultW,
//     fingerprint.SchemaVersion, tokenizer.SchemaVersion,
//     splitter.SchemaVersion — via the SchemaTag stored in the cache
//     file (no Version bump needed)
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
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
// Version is only ONE component of the schema check — algorithm
// parameters (fingerprint k/w, fingerprint hash schema, tokenizer
// schema, splitter schema) are folded in via SchemaTag, so retuning
// any of them invalidates the cache without a manual bump here.
// Reserve Version bumps for changes to the cache's own storage format.
//
// v4: added Chunk.Kind and class-span chunks for Python/Java/JS
// (§5.2 class-level granularity). Entries written by earlier versions
// were split without class chunks, so they must be invalidated
// wholesale — a stale entry would silently drop class findings.
//
// v5: added Chunk.Symbol (definition name for dead-code analysis). Gob
// decodes the missing field in older entries as "" rather than erroring,
// which would silently classify every cached chunk as unnamed — so old
// caches must be invalidated wholesale.
const Version uint32 = 5

// SchemaTag encodes every algorithm parameter whose change makes cached
// per-file output stale: the cache storage version, the fingerprint
// k-gram size and winnowing window, the fingerprint hash schema, the
// tokenizer output schema, and the splitter output schema. Load drops
// any cache whose stored tag differs from the current one, so a retune
// of ANY of these constants auto-invalidates old caches — the
// historical trap was that only a manual Version bump did. The splitter
// component closes the last such gap: a splitter change (new chunk
// kinds or spans, e.g. Elixir defmodule class chunks) alters the chunk
// set for UNCHANGED file content, which no content hash can catch.
func SchemaTag() string {
	return schemaTag(Version, fingerprint.DefaultK, fingerprint.DefaultW,
		fingerprint.SchemaVersion, tokenizer.SchemaVersion, splitter.SchemaVersion)
}

// schemaTag is the parameterized core of SchemaTag, split out so tests
// can prove each component independently changes the tag.
func schemaTag(cacheVersion uint32, k, w, fpSchema, tokSchema, splitSchema int) string {
	return fmt.Sprintf("cache=%d;fp=k%d,w%d,s%d;tok=s%d;split=s%d",
		cacheVersion, k, w, fpSchema, tokSchema, splitSchema)
}

// Chunk mirrors enough of the tokenizer + fingerprint output to reconstruct
// a snippet without rerunning either. Tokens are stored as raw strings;
// fingerprints are stored as a flat uint32 list plus the position map, then
// re-materialized into a Set on load.
type Chunk struct {
	Name       string
	Symbol     string // mirrors splitter.Chunk.Symbol; empty for whole-file chunks
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
	// Schema is the SchemaTag the cache was written under. Load rejects
	// caches whose tag differs from the current build's — this is what
	// makes a fingerprint.DefaultK/DefaultW retune or a tokenizer schema
	// bump auto-invalidate without touching Version. Caches written
	// before this field existed decode with Schema == "" and are
	// likewise rejected (they simply miss and get rebuilt).
	Schema  string
	Entries map[string]Entry
	dirty   bool
}

// Load reads a cache from `dir`. Returns an empty cache when the file is
// missing or its version doesn't match the current code. Any other I/O
// error is returned.
func Load(dir string) (*Cache, error) {
	return NewGobStorage(dir).Load()
}

// New returns a fresh empty cache at the current Version and SchemaTag.
func New() *Cache {
	return &Cache{Version: Version, Schema: SchemaTag(), Entries: map[string]Entry{}}
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
	return NewGobStorage(dir).Save(c)
}

// HashContent returns a hex-encoded SHA-256 of the given byte slice.
// Used to detect file-content changes between runs.
func HashContent(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// PatternsHash hashes regex patterns in application order. Overlapping
// patterns can produce different tokens when reordered, so order must be
// part of the cache key. Length prefixes keep pattern boundaries unambiguous.
func PatternsHash(patterns []string) string {
	if len(patterns) == 0 {
		return ""
	}
	h := sha256.New()
	// Separate these keys from the legacy order-insensitive hash, which
	// may describe tokens produced under a different application order.
	h.Write([]byte("ordered-patterns-v1:"))
	for _, p := range patterns {
		fmt.Fprintf(h, "%d:", len(p))
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Key combines a file's absolute path, its content hash, the active
// ignore_patterns hash, and the chunking granularity into a stable cache
// key. Path is included so two files with identical content but different
// paths don't share an entry (their chunk names differ).
//
// Granularity is part of the key because entries store granularity-shaped
// chunks: a function-level entry holds per-definition chunks, a file-level
// entry holds one whole-file chunk, and serving one to the other mode
// would silently change results. Function-level (or empty, its legacy
// spelling) contributes nothing to the hash, so caches written before the
// granularity dimension existed stay warm; any other granularity gets its
// own key segment, letting both modes coexist in one cache file.
func Key(absPath, contentHash, patternsHash, granularity string) string {
	h := sha256.New()
	h.Write([]byte(absPath))
	h.Write([]byte{0})
	h.Write([]byte(contentHash))
	h.Write([]byte{0})
	h.Write([]byte(patternsHash))
	if granularity != "" && granularity != "function" {
		h.Write([]byte{0})
		h.Write([]byte(granularity))
	}
	return hex.EncodeToString(h.Sum(nil))
}
