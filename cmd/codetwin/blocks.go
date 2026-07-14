package main

// Block-level partial-clone orchestration (review §5.3): run
// blocks.Detect over the gray-band candidate index pairs BuildMatrix
// collected, and package verified matches as report.BlockClones for
// the PARTIAL CLONES section / `partial_clones` JSON array.

import (
	"regexp"
	"sort"

	"github.com/ccsrvs/codetwin/internal/blocks"
	"github.com/ccsrvs/codetwin/internal/config"
	"github.com/ccsrvs/codetwin/internal/git"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
)

// chunkNameRe inverts splitter.Chunk.Name(): "path:start-end Symbol",
// "path:start-end", or bare "path" (whole-file chunks, which don't
// match and fall back to (name, no symbol)). The greedy path group
// binds the LAST ":<digits>-<digits>" occurrence, so paths containing
// colons still parse.
var chunkNameRe = regexp.MustCompile(`^(.*):(\d+)-(\d+)(?: (.+))?$`)

// splitChunkName extracts the file path and symbol from a snippet name.
func splitChunkName(name string) (file, symbol string) {
	m := chunkNameRe.FindStringSubmatch(name)
	if m == nil {
		return name, ""
	}
	return m[1], m[4]
}

// detectBlockClones runs block detection over the candidate snippet
// index pairs and returns deduplicated, deterministically ordered
// findings. Pairs matching the user's ignore_pairs are skipped — a
// suppressed pair must not resurface as a block match.
func detectBlockClones(
	cands [][2]int,
	snippets []scan.Snippet,
	minBlockLines int,
	ignore *config.PairIgnoreMatcher,
) []report.BlockClone {
	var out []report.BlockClone
	for _, c := range cands {
		a, b := snippets[c[0]], snippets[c[1]]
		if ignore != nil && ignore.Match(a.Name, b.Name) {
			continue
		}
		fileA, symA := splitChunkName(a.Name)
		fileB, symB := splitChunkName(b.Name)
		for _, m := range blocks.Detect(a, b, minBlockLines) {
			bc := report.BlockClone{
				FileA: fileA, SymbolA: symA, PathA: a.Path,
				AStartLine: m.AStartLine, AEndLine: m.AEndLine,
				FileB: fileB, SymbolB: symB, PathB: b.Path,
				BStartLine: m.BStartLine, BEndLine: m.BEndLine,
				Containment: m.Containment,
				LinesA:      m.ALines, LinesB: m.BLines,
				IsTestA: a.IsTest, IsTestB: b.IsTest,
			}
			bc.ID = report.PairID(bc.RangeNameA(), bc.RangeNameB())
			out = append(out, bc)
		}
	}
	return dedupeBlockClones(out)
}

// dedupeBlockClones collapses findings that describe the same
// underlying block through different chunk pairs (e.g. an outer
// function and a closure it contains both pairing against the same
// counterpart): within one file pair, of two findings whose line
// ranges overlap on BOTH sides only the better one (higher
// containment, then bigger, then stable order) survives.
func dedupeBlockClones(bcs []report.BlockClone) []report.BlockClone {
	sorted := append([]report.BlockClone(nil), bcs...)
	sortBlockClones(sorted)
	kept := sorted[:0:0]
	for _, b := range sorted {
		dup := false
		for _, k := range kept {
			if k.FileA == b.FileA && k.FileB == b.FileB &&
				rangesOverlap(k.AStartLine, k.AEndLine, b.AStartLine, b.AEndLine) &&
				rangesOverlap(k.BStartLine, k.BEndLine, b.BStartLine, b.BEndLine) {
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, b)
		}
	}
	return kept
}

// sortBlockClones orders findings best-first: containment descending,
// then min-side block size descending, then range names — the same
// ordering report.PrepareBlocks uses, applied here so dedup keeps the
// strongest representative of each overlap group.
func sortBlockClones(bcs []report.BlockClone) {
	less := func(a, b report.BlockClone) bool {
		if a.Containment != b.Containment {
			return a.Containment > b.Containment
		}
		am, bm := a.LinesA, b.LinesA
		if a.LinesB < am {
			am = a.LinesB
		}
		if b.LinesB < bm {
			bm = b.LinesB
		}
		if am != bm {
			return am > bm
		}
		if ra, rb := a.RangeNameA(), b.RangeNameA(); ra != rb {
			return ra < rb
		}
		return a.RangeNameB() < b.RangeNameB()
	}
	sort.SliceStable(bcs, func(i, j int) bool { return less(bcs[i], bcs[j]) })
}

func rangesOverlap(aStart, aEnd, bStart, bEnd int) bool {
	return aStart <= bEnd && aEnd >= bStart
}

// filterBlocksBySince keeps only block clones whose block line range
// (either side) overlaps lines changed since the --since ref — the
// same DiffMap.Touches gate pairs go through, but over the block's
// real line ranges rather than the enclosing chunks'.
func filterBlocksBySince(bcs []report.BlockClone, repoRoot string, diff git.DiffMap) ([]report.BlockClone, int) {
	kept := make([]report.BlockClone, 0, len(bcs))
	dropped := 0
	for _, b := range bcs {
		if diff.Touches(repoRoot, b.PathA, b.AStartLine, b.AEndLine) ||
			diff.Touches(repoRoot, b.PathB, b.BStartLine, b.BEndLine) {
			kept = append(kept, b)
			continue
		}
		dropped++
	}
	return kept, dropped
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
}

func toJSONBlockClones(bcs []report.BlockClone) []jsonBlockClone {
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
		}
	}
	return out
}
