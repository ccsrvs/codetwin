// Package scan turns source files into Snippets — the per-definition unit
// downstream similarity scoring operates on. ProcessFiles is the parallel
// orchestrator; ProcessFile is the per-file pipeline (cache lookup,
// splitter, tokenizer, fingerprint).
package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"

	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// Snippet is one analyzable unit (typically a function- or class-level
// chunk produced by splitter.Split).
type Snippet struct {
	Name       string
	Path       string // absolute file path, used for same-file containment checks
	Lang       tokenizer.Language
	Code       string
	StartLine  int
	EndLine    int
	NonBlankLn int
	Tokens     []string
	Lines      []int // parallel to Tokens; 1-based source line of each token
	Fps        fingerprint.PositionalSet

	// LexTerms is the sorted, deduplicated vocabulary of the chunk's
	// RAW code (identifier + string-literal words, camel/snake split,
	// lowercased, keywords skipped) — see tokenizer.LexicalTerms. Used
	// only by the report's structural-twin label gate; never part of
	// the numeric score.
	LexTerms []string

	// IsTest marks snippets whose file path follows the language's
	// test-file convention (see IsTestFile). Computed from the path as
	// given by the caller (usually relative to the scan root) so
	// unrelated directory names above the repo can't misclassify.
	IsTest bool

	// Repo is the repo label the snippet belongs to in a multi-root
	// ("cross-repo") scan: the base name of the directory root the file
	// was collected under. Empty on single-root and file-argument
	// invocations — the scan package never sets it; cmd/codetwin
	// assigns it (and prefixes Name with "repo:") only when the CLI was
	// given two or more directory roots.
	Repo string
}

// ProcessFiles runs the per-file split → tokenize → fingerprint pipeline
// across runtime.NumCPU() goroutines. Each worker pulls from a channel
// of file paths and accumulates snippets into a per-worker buffer to
// avoid lock contention on a shared slice. Cache.Get/Put are already
// internally synchronized so the cache is shared safely.
//
// Errors per file (e.g. a file we can't read) are collected as warnings
// and returned alongside the snippet list, rather than printed inline.
//
// onFileDone, if non-nil, is invoked exactly once per processed file
// (success or warning). Used by callers that want to drive a progress
// indicator without coupling this package to a presentation layer.
func ProcessFiles(
	files []string,
	minLines int,
	stripPatterns []*regexp.Regexp,
	cacheState *cache.Cache,
	patternsHash string,
	onFileDone func(),
) ([]Snippet, []string) {
	n := len(files)
	if n == 0 {
		return nil, nil
	}
	workers := runtime.NumCPU()
	if workers > n {
		workers = n
	}
	if workers < 1 {
		workers = 1
	}

	workCh := make(chan string, n)
	for _, p := range files {
		workCh <- p
	}
	close(workCh)

	type result struct {
		snippets []Snippet
		warnings []string
	}
	resultsCh := make(chan result, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var local result
			for path := range workCh {
				snips, warn := ProcessFile(path, minLines, stripPatterns, cacheState, patternsHash)
				local.snippets = append(local.snippets, snips...)
				if warn != "" {
					local.warnings = append(local.warnings, warn)
				}
				if onFileDone != nil {
					onFileDone()
				}
			}
			resultsCh <- local
		}()
	}
	wg.Wait()
	close(resultsCh)

	var allSnippets []Snippet
	var allWarnings []string
	for r := range resultsCh {
		allSnippets = append(allSnippets, r.snippets...)
		allWarnings = append(allWarnings, r.warnings...)
	}
	return allSnippets, allWarnings
}

// ProcessFile is the per-file pipeline (cache lookup, splitter, tokenizer,
// fingerprint), safe to call concurrently. Returns the snippets that
// survive minLines plus an optional warning string for read errors.
// Cache state is shared and thread-safe via its internal mutex.
func ProcessFile(
	path string,
	minLines int,
	stripPatterns []*regexp.Regexp,
	cacheState *cache.Cache,
	patternsHash string,
) ([]Snippet, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Sprintf("could not read %s: %v", path, err)
	}

	absPath, _ := filepath.Abs(path)
	contentHash := cache.HashContent(data)
	key := cache.Key(absPath, contentHash, patternsHash)
	isTest := IsTestFile(path)

	if entry, ok := cacheState.Get(key); ok {
		var out []Snippet
		for _, c := range entry.Chunks {
			if c.NonBlankLn < minLines {
				continue
			}
			out = append(out, Snippet{
				Name:       c.Name,
				Path:       absPath,
				Lang:       tokenizer.Language(c.Lang),
				Code:       c.Code,
				StartLine:  c.StartLine,
				EndLine:    c.EndLine,
				NonBlankLn: c.NonBlankLn,
				Tokens:     c.Tokens,
				Lines:      c.Lines,
				Fps:        positionalFromCache(c),
				LexTerms:   c.LexTerms,
				IsTest:     isTest,
			})
		}
		return out, ""
	}

	code := string(data)
	lang := tokenizer.Detect(path, code)
	var out []Snippet
	var entryChunks []cache.Chunk
	for _, ch := range splitter.Split(path, code, lang) {
		tokens, lines := tokenizer.TokenizeWithLines(ch.Code, lang,
			tokenizer.WithStripPatterns(stripPatterns))
		if len(tokens) == 0 {
			continue
		}
		nonBlank := splitter.CountNonBlankLines(ch.Code)
		ps := fingerprint.GeneratePositional(tokens, fingerprint.DefaultK, fingerprint.DefaultW)
		lexTerms := tokenizer.LexicalTerms(ch.Code, lang)
		name := ch.Name()

		entryChunks = append(entryChunks, cache.Chunk{
			Name:       name,
			Lang:       string(lang),
			StartLine:  ch.StartLine,
			EndLine:    ch.EndLine,
			Code:       ch.Code,
			Tokens:     tokens,
			Lines:      lines,
			NonBlankLn: nonBlank,
			Hashes:     fingerprint.Hashes(ps.Set),
			Positions:  ps.Positions,
			K:          ps.K,
			LexTerms:   lexTerms,
		})

		if nonBlank < minLines {
			continue
		}
		out = append(out, Snippet{
			Name:       name,
			Path:       absPath,
			Lang:       lang,
			Code:       ch.Code,
			StartLine:  ch.StartLine,
			EndLine:    ch.EndLine,
			NonBlankLn: nonBlank,
			Tokens:     tokens,
			Lines:      lines,
			Fps:        ps,
			LexTerms:   lexTerms,
			IsTest:     isTest,
		})
	}

	cacheState.Put(key, cache.Entry{
		ContentHash: contentHash,
		Chunks:      entryChunks,
	})
	return out, ""
}

// positionalFromCache reconstructs a fingerprint.PositionalSet from a
// cached chunk. The Set is rebuilt from the flat hash list; Positions
// and K survive serialization unchanged.
func positionalFromCache(c cache.Chunk) fingerprint.PositionalSet {
	set := make(fingerprint.Set, len(c.Hashes))
	for _, h := range c.Hashes {
		set[h] = struct{}{}
	}
	return fingerprint.PositionalSet{Set: set, Positions: c.Positions, K: c.K}
}
