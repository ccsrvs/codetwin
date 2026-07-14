package config

import (
	"strings"
	"testing"
)

// ── parseSnippetName ──────────────────────────────────────────────────────────
//
// Snippet names follow splitter.Chunk.Name(): "path", "path:start-end", or
// "path:start-end Symbol". parseSnippetName recovers the stable parts (path,
// symbol) and discards the volatile line range so downstream matching keys
// off identifiers users can copy from output without rotting on every edit.

func TestParseSnippetName_GivenBarePath_WhenParsed_ThenReturnsPathAndEmptySymbol(t *testing.T) {
	path, sym := parseSnippetName("internal/foo/bar.go")

	if path != "internal/foo/bar.go" {
		t.Errorf("path: got %q, want %q", path, "internal/foo/bar.go")
	}
	if sym != "" {
		t.Errorf("symbol: got %q, want empty", sym)
	}
}

func TestParseSnippetName_GivenPathWithLineRange_WhenParsed_ThenStripsRangeAndReturnsEmptySymbol(t *testing.T) {
	path, sym := parseSnippetName("internal/foo/bar.go:15-30")

	if path != "internal/foo/bar.go" {
		t.Errorf("path: got %q, want %q", path, "internal/foo/bar.go")
	}
	if sym != "" {
		t.Errorf("symbol: got %q, want empty", sym)
	}
}

func TestParseSnippetName_GivenPathWithRangeAndSymbol_WhenParsed_ThenReturnsBoth(t *testing.T) {
	path, sym := parseSnippetName("internal/auth/handler.go:15-30 parseRequest")

	if path != "internal/auth/handler.go" {
		t.Errorf("path: got %q, want %q", path, "internal/auth/handler.go")
	}
	if sym != "parseRequest" {
		t.Errorf("symbol: got %q, want %q", sym, "parseRequest")
	}
}

func TestParseSnippetName_GivenSymbolWithSpaces_WhenParsed_ThenSymbolPreservesTrailingTokens(t *testing.T) {
	// Java/JS sometimes produce qualified names like "Foo.bar" or
	// generics. We don't currently emit names with spaces in the symbol,
	// but if we did the splitter format means everything after the FIRST
	// space following the line range is the symbol.
	path, sym := parseSnippetName("Foo.java:1-10 Foo.bar(int)")

	if path != "Foo.java" {
		t.Errorf("path: got %q, want %q", path, "Foo.java")
	}
	if sym != "Foo.bar(int)" {
		t.Errorf("symbol: got %q, want %q", sym, "Foo.bar(int)")
	}
}

func TestParseSnippetName_GivenColonInPathButNoLineRange_WhenParsed_ThenLeavesPathAlone(t *testing.T) {
	// A Windows-style or unusual path shouldn't be misinterpreted as a
	// line range. Only ":N-M" with digits qualifies.
	path, sym := parseSnippetName("weird:name.go")

	if path != "weird:name.go" {
		t.Errorf("path: got %q, want %q (only :digits-digits is a line range)", path, "weird:name.go")
	}
	if sym != "" {
		t.Errorf("symbol: got %q, want empty", sym)
	}
}

func TestParseSnippetName_GivenEmptyString_WhenParsed_ThenReturnsEmptyPathAndSymbol(t *testing.T) {
	path, sym := parseSnippetName("")

	if path != "" || sym != "" {
		t.Errorf("empty input: got (%q, %q), want both empty", path, sym)
	}
}

// TestParseSnippetName_Exported pins the exported wrapper to the internal
// implementation: the baseline package's member identity must stay in
// lockstep with ignore_pairs endpoint normalization.
func TestParseSnippetName_Exported_MatchesInternalNormalization(t *testing.T) {
	for _, name := range []string{
		"internal/auth/handler.go:15-30 parseRequest",
		"internal/foo/bar.go:15-30",
		"internal/foo/bar.go",
		"",
	} {
		wantPath, wantSym := parseSnippetName(name)
		gotPath, gotSym := ParseSnippetName(name)
		if gotPath != wantPath || gotSym != wantSym {
			t.Errorf("ParseSnippetName(%q) = (%q, %q), want (%q, %q)",
				name, gotPath, gotSym, wantPath, wantSym)
		}
	}
}

// ── CompileIgnorePairs / PairIgnoreMatcher ───────────────────────────────────
//
// Endpoint string format mirrors splitter.Chunk.Name() minus the line range:
//
//	"path"          → match any chunk in that file (path is glob)
//	"path SYMBOL"   → match only chunks where the splitter detected SYMBOL
//
// Pairs are matched order-independently: a rule {A, B} fires for both
// (NameA=A, NameB=B) and (NameA=B, NameB=A).

func TestPairIgnoreMatcher_GivenNilMatcher_WhenMatch_ThenReturnsFalse(t *testing.T) {
	var m *PairIgnoreMatcher

	if m.Match("a.go", "b.go") {
		t.Error("nil matcher must always return false")
	}
}

func TestPairIgnoreMatcher_GivenNoRules_WhenMatch_ThenReturnsFalse(t *testing.T) {
	m, err := CompileIgnorePairs(nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if m.Match("a.go", "b.go") {
		t.Error("empty rule set must not match anything")
	}
}

func TestPairIgnoreMatcher_GivenLiteralPathPair_WhenBothEndpointsMatch_ThenReturnsTrue(t *testing.T) {
	m, err := CompileIgnorePairs([]IgnorePair{{A: "internal/foo/util.go", B: "internal/bar/util.go"}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if !m.Match("internal/foo/util.go", "internal/bar/util.go") {
		t.Error("expected literal pair to match in declared order")
	}
}

func TestPairIgnoreMatcher_GivenLiteralPathPair_WhenEndpointsReversed_ThenStillMatches(t *testing.T) {
	m, err := CompileIgnorePairs([]IgnorePair{{A: "x.go", B: "y.go"}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if !m.Match("y.go", "x.go") {
		t.Error("matcher must be order-independent")
	}
}

func TestPairIgnoreMatcher_GivenLineRangedNames_WhenMatching_ThenStripsRangesBeforeCompare(t *testing.T) {
	m, err := CompileIgnorePairs([]IgnorePair{{A: "internal/foo/util.go", B: "internal/bar/util.go"}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Snippet names from real output include line ranges; the matcher must
	// recover the path before comparing, otherwise every edit invalidates
	// the user's ignore entries.
	if !m.Match("internal/foo/util.go:15-30 helper", "internal/bar/util.go:5-20 helper") {
		t.Error("matcher must strip line ranges from snippet names before comparing")
	}
}

func TestPairIgnoreMatcher_GivenSymbolEndpoint_WhenSymbolMatches_ThenReturnsTrue(t *testing.T) {
	m, err := CompileIgnorePairs([]IgnorePair{{
		A: "auth/handler.go parseRequest",
		B: "api/middleware.go parseRequest",
	}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if !m.Match("auth/handler.go:10-40 parseRequest", "api/middleware.go:1-25 parseRequest") {
		t.Error("symbol-qualified endpoints must match same-symbol chunks")
	}
}

func TestPairIgnoreMatcher_GivenSymbolEndpoint_WhenSymbolDiffers_ThenReturnsFalse(t *testing.T) {
	m, err := CompileIgnorePairs([]IgnorePair{{
		A: "auth/handler.go parseRequest",
		B: "api/middleware.go parseRequest",
	}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if m.Match("auth/handler.go:10-40 validateToken", "api/middleware.go:1-25 parseRequest") {
		t.Error("symbol-qualified endpoint must NOT match a chunk with a different symbol")
	}
}

func TestPairIgnoreMatcher_GivenPathOnlyEndpoint_WhenChunkHasSymbol_ThenStillMatches(t *testing.T) {
	// "path" (no symbol) is a wildcard over symbols in that file.
	m, err := CompileIgnorePairs([]IgnorePair{{A: "auth/handler.go", B: "api/middleware.go"}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if !m.Match("auth/handler.go:10-40 parseRequest", "api/middleware.go:1-25 init") {
		t.Error("path-only endpoint must match any symbol in that file")
	}
}

func TestPairIgnoreMatcher_GivenAsymmetricEndpoints_WhenOneSideHasSymbol_ThenMatchesPrecisely(t *testing.T) {
	// Mixing a symbol-qualified endpoint with a path-only endpoint is a
	// realistic user pattern: "any chunk in middleware.go, but only the
	// parseRequest function in handler.go".
	m, err := CompileIgnorePairs([]IgnorePair{{
		A: "auth/handler.go parseRequest",
		B: "api/middleware.go",
	}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if !m.Match("auth/handler.go:10-40 parseRequest", "api/middleware.go:1-25 init") {
		t.Error("symbol-qualified A + path-only B should match")
	}
	if m.Match("auth/handler.go:10-40 validateToken", "api/middleware.go:1-25 init") {
		t.Error("symbol-qualified A must still gate by symbol on its side")
	}
}

func TestPairIgnoreMatcher_GivenGlobPath_WhenPathMatchesGlob_ThenReturnsTrue(t *testing.T) {
	m, err := CompileIgnorePairs([]IgnorePair{{A: "**/*_generated.go", B: "**/*_generated.go"}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if !m.Match("internal/foo/api_generated.go:1-50 Foo", "pkg/bar/types_generated.go:1-80 Bar") {
		t.Error("glob endpoint must match path with same shape on both sides")
	}
}

func TestPairIgnoreMatcher_GivenGlobPath_WhenOneSideOutsideGlob_ThenReturnsFalse(t *testing.T) {
	m, err := CompileIgnorePairs([]IgnorePair{{A: "**/*_generated.go", B: "**/*_generated.go"}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if m.Match("internal/foo/api_generated.go", "pkg/bar/handwritten.go") {
		t.Error("glob must not match when one endpoint falls outside the glob")
	}
}

func TestCompileIgnorePairs_GivenEmptyEndpoint_WhenCompile_ThenReturnsError(t *testing.T) {
	// An empty side is almost certainly a config typo: matching "anything
	// paired with X" is a different feature with very different blast
	// radius. Reject it loudly so users notice.
	_, err := CompileIgnorePairs([]IgnorePair{{A: "", B: "x.go"}})
	if err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}

func TestCompileIgnorePairs_GivenMultipleInvalidEntries_WhenCompile_ThenReportsAllOfThem(t *testing.T) {
	// Validation errors should accumulate so the user fixes every typo
	// in one round-trip, mirroring CompileIgnorePatterns' behaviour.
	_, err := CompileIgnorePairs([]IgnorePair{
		{A: "", B: "ok.go"},
		{A: "ok.go", B: "  "},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ignore_pairs[0]") || !strings.Contains(msg, "ignore_pairs[1]") {
		t.Errorf("error should mention both bad entries by index; got: %s", msg)
	}
}
