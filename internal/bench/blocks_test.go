// blocks_test.go is the executable contract for sub-function
// block-level partial-clone detection — the feature planned in
// docs/comparative-algorithms-review.md §5.3. The fixtures under
// testdata/bench/blocks are the ground-truth corpus: positives embed a
// shared block inside structurally unrelated host functions (invisible
// to union-normalized function-level Jaccard by construction — see
// TestBlockClones_FixturesAreInvisibleAtFunctionLevel, which runs
// today); negatives share only language boilerplate and must never
// become block findings.
package bench

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/similarity"
)

// BlockMatch is the intended result shape for one detected sub-function
// block clone: 1-based source line ranges of the shared block on both
// sides, plus the block's containment score
// (intersection / min(|A|, |B|) over the block's own fingerprints,
// per review §5.3).
//
// NOTE: this type and the detectBlocks seam below are declared in the
// test package for now. The real implementation will live in a new
// internal/blocks package; when it lands, replace this declaration with
// the internal/blocks types, assign its entry point to detectBlocks,
// and delete the t.Skip in TestBlockClones_GroundTruth — no other test
// change should be needed.
type BlockMatch struct {
	AStartLine, AEndLine int // line range of the block in snippet A's file
	BStartLine, BEndLine int // line range of the block in snippet B's file
	Containment          float64
}

// detectBlocks is the seam the future implementation plugs into. It
// must return only matches that survive exact-token verification and
// the min-block-lines floor on BOTH sides — coalesced boilerplate runs
// (err-check chains, logging blocks) that fail verification must be
// filtered inside the implementation, not by callers.
var detectBlocks func(a, b scan.Snippet, minBlockLines int) []BlockMatch

const (
	// blockMinLines mirrors the planned --min-block-lines default band
	// (review §5.3 proposes ~8-10; the contract pins the permissive end).
	blockMinLines = 8
	// blockContainmentMin is the containment floor a reported positive
	// block must reach.
	blockContainmentMin = 0.8
)

type lineRange struct{ start, end int }

type blockCase struct {
	name     string
	positive bool
	// Expected shared-block line ranges for positives, duplicated from
	// the comment header of each fixture (a.* / b.*). A reported match
	// must overlap these on both sides.
	a, b lineRange
}

// blockCases is the ground-truth table. Ranges mirror the fixture
// headers; if a fixture is edited, update both places.
var blockCases = []blockCase{
	{name: "verbatim-go", positive: true, a: lineRange{13, 28}, b: lineRange{13, 28}},
	{name: "verbatim-python", positive: true, a: lineRange{9, 20}, b: lineRange{9, 20}},
	{name: "renamed-go", positive: true, a: lineRange{13, 27}, b: lineRange{13, 27}},
	{name: "containment-go", positive: true, a: lineRange{8, 19}, b: lineRange{24, 35}},
	{name: "gapped-go", positive: true, a: lineRange{14, 30}, b: lineRange{14, 30}},
	{name: "errcheck-chain-go"},
	{name: "logging-block-js"},
	{name: "import-adjacent-python"},
}

func blockCaseDir(c blockCase) string {
	kind := "negative"
	if c.positive {
		kind = "positive"
	}
	return filepath.Join("..", "..", "testdata", "bench", "blocks", kind, c.name)
}

func overlaps(gotStart, gotEnd int, want lineRange) bool {
	return gotStart <= want.end && gotEnd >= want.start
}

// TestBlockClones_GroundTruth is the acceptance contract for review
// §5.3. Positives must yield at least one verified block match that
// overlaps the expected ranges on both sides with containment >= 0.8
// and length >= min-block-lines; negatives must yield zero matches.
// Skipped until the implementation is wired into detectBlocks.
func TestBlockClones_GroundTruth(t *testing.T) {
	if detectBlocks == nil {
		t.Skip("block-level detection not implemented — contract for review §5.3; assign internal/blocks' entry point to detectBlocks and remove this skip")
	}
	for _, c := range blockCases {
		t.Run(c.name, func(t *testing.T) {
			a, b := caseSnippets(t, blockCaseDir(c))
			var matches []BlockMatch
			for _, sa := range a {
				for _, sb := range b {
					matches = append(matches, detectBlocks(sa, sb, blockMinLines)...)
				}
			}
			if !c.positive {
				if len(matches) != 0 {
					t.Fatalf("negative case produced %d block match(es), want 0; first: %+v", len(matches), matches[0])
				}
				return
			}
			for _, m := range matches {
				if m.Containment < blockContainmentMin {
					continue
				}
				if m.AEndLine-m.AStartLine+1 < blockMinLines || m.BEndLine-m.BStartLine+1 < blockMinLines {
					continue
				}
				if overlaps(m.AStartLine, m.AEndLine, c.a) && overlaps(m.BStartLine, m.BEndLine, c.b) {
					t.Logf("found block a:%d-%d b:%d-%d containment=%.2f",
						m.AStartLine, m.AEndLine, m.BStartLine, m.BEndLine, m.Containment)
					return
				}
			}
			t.Fatalf("no verified block match overlapping a:%d-%d b:%d-%d (containment >= %.2f, >= %d lines); got %d match(es): %+v",
				c.a.start, c.a.end, c.b.start, c.b.end, blockContainmentMin, blockMinLines, len(matches), matches)
		})
	}
}

// TestBlockClones_FixturesAreInvisibleAtFunctionLevel runs TODAY (no
// skip): it documents the recall gap block-level detection must close,
// and protects the fixtures from drifting into function-level-visible
// territory. Every positive's best whole-function cross-file pair —
// including sub-chunks such as goroutine closures emitted by splitGo —
// must score BELOW the default report threshold; otherwise the case
// would already be a function-level finding and would no longer
// exercise block detection. Negatives are logged for visibility only.
func TestBlockClones_FixturesAreInvisibleAtFunctionLevel(t *testing.T) {
	type loadedBlockCase struct {
		blockCase
		a, b []scan.Snippet
	}
	var all []loadedBlockCase
	var streams [][]string
	for _, c := range blockCases {
		a, b := caseSnippets(t, blockCaseDir(c))
		all = append(all, loadedBlockCase{c, a, b})
		for _, s := range append(append([]scan.Snippet{}, a...), b...) {
			streams = append(streams, s.Tokens)
		}
	}
	corpus := similarity.NewCorpus(streams)
	vec := func(s scan.Snippet) similarity.NormalizedVector {
		return similarity.Normalize(corpus.Vectorize(s.Tokens))
	}
	// Snippet.Name embeds the relative fixture path; keep only the
	// basename portion for readable logs.
	shortName := func(s scan.Snippet) string {
		name := s.Name
		if i := strings.LastIndex(name, string(filepath.Separator)); i >= 0 {
			name = name[i+1:]
		}
		return name
	}

	for _, c := range all {
		var best scored
		var bestPair string
		for _, sa := range c.a {
			va := vec(sa)
			for _, sb := range c.b {
				if s := pairScore(sa, sb, va, vec(sb)); s.combined > best.combined {
					best = s
					bestPair = shortName(sa) + " <-> " + shortName(sb)
				}
			}
		}
		t.Logf("%-30s best pair struct=%.2f sem=%.2f combined=%.2f (%s)",
			c.name, best.structural, best.semantic, best.combined, bestPair)
		if c.positive && best.combined >= defaultThreshold {
			t.Errorf("%s: best whole-function pair scores %.2f >= %.2f — the shared block is visible at function level, so the fixture no longer exercises block detection; make the host functions larger or more distinct",
				c.name, best.combined, defaultThreshold)
		}
		if c.name == "containment-go" {
			// The §3.6 containment case gets an explicit assertion: the
			// small function is fully verbatim-contained in the big
			// host, and union-normalized Jaccard still can't see it.
			var small, host scan.Snippet
			var found bool
			for _, sa := range c.a {
				if strings.Contains(sa.Name, "clampWindow") {
					small = sa
					found = true
				}
			}
			if !found {
				t.Fatalf("containment-go: clampWindow snippet not found in a.go")
			}
			found = false
			for _, sb := range c.b {
				if strings.Contains(sb.Name, "renderInventoryPage") {
					host = sb
					found = true
				}
			}
			if !found {
				t.Fatalf("containment-go: renderInventoryPage snippet not found in b.go")
			}
			s := pairScore(small, host, vec(small), vec(host))
			if s.combined >= defaultThreshold {
				t.Errorf("containment-go: clampWindow <-> renderInventoryPage scores %.2f >= %.2f despite being asserted invisible — grow the host function",
					s.combined, defaultThreshold)
			}
		}
	}
}
