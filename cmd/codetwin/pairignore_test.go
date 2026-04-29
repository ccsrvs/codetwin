package main

import (
	"testing"

	"github.com/ccsrvs/codetwin/internal/config"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
)

// applyPairIgnores is the post-BuildMatrix filter that drops pairs matching
// the user's ignore_pairs config and zeros the corresponding matrix entries
// so DBSCAN cannot link the two snippets afterwards.

func mkSnippet(name string) scan.Snippet { return scan.Snippet{Name: name} }

func TestApplyPairIgnores_GivenNilMatcher_WhenInvoked_ThenLeavesPairsAndMatrixIntact(t *testing.T) {
	pairs := []report.Pair{{NameA: "a.go", NameB: "b.go", Score: 0.9}}
	matrix := [][]float64{{1, 0.9}, {0.9, 1}}
	snippets := []scan.Snippet{mkSnippet("a.go"), mkSnippet("b.go")}

	got, ignored := applyPairIgnores(pairs, matrix, snippets, nil)

	if ignored != 0 {
		t.Errorf("ignored count: got %d, want 0", ignored)
	}
	if len(got) != 1 {
		t.Errorf("pairs len: got %d, want 1", len(got))
	}
	if matrix[0][1] != 0.9 || matrix[1][0] != 0.9 {
		t.Errorf("matrix should be untouched, got %v", matrix)
	}
}

func TestApplyPairIgnores_GivenMatchingRule_WhenInvoked_ThenDropsPairAndZeroesMatrix(t *testing.T) {
	m, err := config.CompileIgnorePairs([]config.IgnorePair{{A: "a.go", B: "b.go"}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	pairs := []report.Pair{
		{NameA: "a.go", NameB: "b.go", Score: 0.9},
		{NameA: "a.go", NameB: "c.go", Score: 0.8}, // unrelated, must survive
	}
	matrix := [][]float64{
		{1.0, 0.9, 0.8},
		{0.9, 1.0, 0.4},
		{0.8, 0.4, 1.0},
	}
	snippets := []scan.Snippet{mkSnippet("a.go"), mkSnippet("b.go"), mkSnippet("c.go")}

	got, ignored := applyPairIgnores(pairs, matrix, snippets, m)

	if ignored != 1 {
		t.Errorf("ignored count: got %d, want 1", ignored)
	}
	if len(got) != 1 {
		t.Fatalf("pairs len: got %d, want 1", len(got))
	}
	if got[0].NameA != "a.go" || got[0].NameB != "c.go" {
		t.Errorf("surviving pair: got (%s,%s), want (a.go,c.go)", got[0].NameA, got[0].NameB)
	}

	// The matrix entry between a and b must be zeroed (both directions).
	// DBSCAN computes distance as 1.0-matrix; zero → distance 1.0 → not
	// neighbours, so they cannot be clustered together.
	if matrix[0][1] != 0 || matrix[1][0] != 0 {
		t.Errorf("matrix[a][b] expected zeroed, got %v / %v", matrix[0][1], matrix[1][0])
	}
	// Unrelated entries must be untouched.
	if matrix[0][2] != 0.8 || matrix[2][0] != 0.8 {
		t.Errorf("matrix[a][c] should be 0.8, got %v / %v", matrix[0][2], matrix[2][0])
	}
}

func TestApplyPairIgnores_GivenLineRangedSnippetNames_WhenMatchingByPath_ThenDropsThemRegardless(t *testing.T) {
	// Real snippet names include line ranges. The user copies the path
	// out of the report and writes "auth/handler.go parseRequest"; the
	// matcher must reach it through the splitter formatting.
	m, err := config.CompileIgnorePairs([]config.IgnorePair{{
		A: "auth/handler.go parseRequest",
		B: "api/middleware.go parseRequest",
	}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	pairs := []report.Pair{
		{NameA: "auth/handler.go:10-40 parseRequest", NameB: "api/middleware.go:1-25 parseRequest", Score: 0.85},
	}
	matrix := [][]float64{{1.0, 0.85}, {0.85, 1.0}}
	snippets := []scan.Snippet{
		mkSnippet("auth/handler.go:10-40 parseRequest"),
		mkSnippet("api/middleware.go:1-25 parseRequest"),
	}

	got, ignored := applyPairIgnores(pairs, matrix, snippets, m)

	if ignored != 1 || len(got) != 0 {
		t.Errorf("expected the line-ranged pair to be ignored; got %d pairs, ignored=%d", len(got), ignored)
	}
}

func TestApplyPairIgnores_GivenSnippetNotInIndex_WhenMatchingPair_ThenStillFiltersWithoutPanic(t *testing.T) {
	// Defensive: if a pair somehow references a name absent from the
	// snippets slice (shouldn't happen in practice, but pipelines mutate),
	// the helper should still drop the pair without dereferencing a
	// missing index.
	m, err := config.CompileIgnorePairs([]config.IgnorePair{{A: "a.go", B: "b.go"}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	pairs := []report.Pair{{NameA: "a.go", NameB: "b.go", Score: 0.9}}
	matrix := [][]float64{{1, 0.9}, {0.9, 1}}
	snippets := []scan.Snippet{mkSnippet("a.go")} // missing b.go

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic: %v", r)
		}
	}()

	got, ignored := applyPairIgnores(pairs, matrix, snippets, m)
	if ignored != 1 || len(got) != 0 {
		t.Errorf("expected the pair to be dropped, got %d ignored, %d kept", ignored, len(got))
	}
}
