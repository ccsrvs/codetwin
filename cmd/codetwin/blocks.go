package main

// CLI adapters for partial-clone filtering, previews, suggestions, and JSON.

import (
	"fmt"

	"github.com/ccsrvs/codetwin/internal/git"
	"github.com/ccsrvs/codetwin/internal/refactor"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
)

// filterBlocksBySince keeps only block clones whose block line range
// (either side) overlaps lines changed since the --since ref — the
// same DiffMap.Touches gate pairs go through, but over the block's
// real line ranges rather than the enclosing chunks'.
func filterBlocksBySince(bcs []report.BlockClone, repoRoot string, diff git.DiffMap) ([]report.BlockClone, int) {
	return keepTouching(bcs, func(b report.BlockClone) bool {
		return diff.Touches(repoRoot, b.PathA, b.AStartLine, b.AEndLine) ||
			diff.Touches(repoRoot, b.PathB, b.BStartLine, b.BEndLine)
	})
}

// addBlockPreviews inserts a Preview for each side of every visible
// partial clone into the previews map, keyed by the side's
// "file:start-end" range name. Range-qualified keys keep block
// previews from colliding with the whole-chunk preview of the same
// snippet (chunk previews are keyed by the snippet's full name).
// Unlike pair previews there is no MatchRange work: block matches
// already carry exact line ranges, so each preview is just the block
// slice of the host chunk's code, truncated to previewLines.
func addBlockPreviews(
	previews map[string]report.Preview,
	blocks []report.BlockClone,
	snippets []scan.Snippet,
	previewLines int,
) {
	if len(blocks) == 0 {
		return
	}
	byName := snippetsByName(snippets)
	for _, b := range blocks {
		if s, ok := byName[b.ChunkA]; ok {
			previews[b.RangeNameA()] = report.BuildBlockPreview(
				s.Code, s.StartLine, b.AStartLine, b.AEndLine, previewLines)
		}
		if s, ok := byName[b.ChunkB]; ok {
			previews[b.RangeNameB()] = report.BuildBlockPreview(
				s.Code, s.StartLine, b.BStartLine, b.BEndLine, previewLines)
		}
	}
}

// blockPseudoSnippets resolves a block clone back to its two host
// snippets and slices each side's chunk code down to the block's line
// range, yielding the pseudo-snippets the refactor pipeline consumes.
// The second return is the host snippet of side A (whose EndLine is
// where the emitted helper gets inserted after).
func blockPseudoSnippets(bc report.BlockClone, snippets []scan.Snippet) (pa, pb, hostA scan.Snippet, err error) {
	a, ok := findSnippet(bc.ChunkA, snippets)
	if !ok {
		return pa, pb, hostA, fmt.Errorf("snippet not found: %s", bc.ChunkA)
	}
	b, ok := findSnippet(bc.ChunkB, snippets)
	if !ok {
		return pa, pb, hostA, fmt.Errorf("snippet not found: %s", bc.ChunkB)
	}
	pa = refactor.SliceBlock(a, bc.AStartLine, bc.AEndLine, bc.RangeNameA())
	pb = refactor.SliceBlock(b, bc.BStartLine, bc.BEndLine, bc.RangeNameB())
	return pa, pb, a, nil
}

// synthesizeBlockSuggestion runs the block-mode refactor pipeline
// (slice → align → synthesize) for one partial clone.
func synthesizeBlockSuggestion(bc report.BlockClone, snippets []scan.Snippet) (refactor.Suggestion, scan.Snippet, error) {
	pa, pb, hostA, err := blockPseudoSnippets(bc, snippets)
	if err != nil {
		return refactor.Suggestion{}, hostA, err
	}
	al := refactor.Align(pa, pb)
	return refactor.SynthesizeBlock(pa, pb, bc.ID, al), hostA, nil
}

// buildBlockSuggestionMap synthesizes a block-mode Suggestion for
// every visible partial clone, packaged as jsonPatch entries keyed by
// block ID. Used by --suggest-all to populate `suggested_patch` on the
// partial_clones array. Blocks whose host snippets can't be resolved
// are skipped silently, mirroring buildSuggestionMap.
func buildBlockSuggestionMap(blocks []report.BlockClone, snippets []scan.Snippet) map[string]jsonPatch {
	out := make(map[string]jsonPatch, len(blocks))
	for _, bc := range blocks {
		sug, hostA, err := synthesizeBlockSuggestion(bc, snippets)
		if err != nil {
			continue
		}
		out[bc.ID] = suggestionPatch(sug, func() (string, error) {
			return refactor.BuildPatchInsertAfter(hostA.Path, hostA.EndLine, sug)
		})
	}
	return out
}

func findBlockByID(id string, blocks []report.BlockClone) (report.BlockClone, bool) {
	return findByKey(blocks, id, func(b report.BlockClone) string { return b.ID })
}

// jsonBlockClone is the `partial_clones` array element schema.
type jsonBlockClone struct {
	ID          string  `json:"id"`
	FileA       string  `json:"file_a"`
	StartLineA  int     `json:"start_line_a"`
	EndLineA    int     `json:"end_line_a"`
	SymbolA     string  `json:"symbol_a,omitempty"`
	FileB       string  `json:"file_b"`
	StartLineB  int     `json:"start_line_b"`
	EndLineB    int     `json:"end_line_b"`
	SymbolB     string  `json:"symbol_b,omitempty"`
	Containment float64 `json:"containment"`
	LinesA      int     `json:"lines_a"`
	LinesB      int     `json:"lines_b"`

	// SuggestedPatch is populated by --suggest-all (block-mode
	// synthesis); nil otherwise. Same shape as the pair-level field.
	SuggestedPatch *jsonPatch `json:"suggested_patch,omitempty"`

	// RepoA / RepoB appear only on multi-root scans (omitempty keeps
	// the single-root schema untouched).
	RepoA string `json:"repo_a,omitempty"`
	RepoB string `json:"repo_b,omitempty"`
}

// toJSONBlockClones converts the visible blocks to their JSON schema,
// attaching --suggest-all block suggestions by ID when present.
func toJSONBlockClones(bcs []report.BlockClone, suggestions map[string]jsonPatch) []jsonBlockClone {
	if len(bcs) == 0 {
		return nil
	}
	out := make([]jsonBlockClone, len(bcs))
	for i, b := range bcs {
		out[i] = jsonBlockClone{
			ID:    b.ID,
			FileA: b.FileA, StartLineA: b.AStartLine, EndLineA: b.AEndLine, SymbolA: b.SymbolA,
			FileB: b.FileB, StartLineB: b.BStartLine, EndLineB: b.BEndLine, SymbolB: b.SymbolB,
			Containment: b.Containment,
			LinesA:      b.LinesA, LinesB: b.LinesB,
			RepoA: b.RepoA, RepoB: b.RepoB,
		}
		if suggestions != nil {
			if patch, ok := suggestions[b.ID]; ok {
				p := patch
				out[i].SuggestedPatch = &p
			}
		}
	}
	return out
}
