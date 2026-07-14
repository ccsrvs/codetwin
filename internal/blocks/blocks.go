// Package blocks implements sub-function block-level partial-clone
// detection (algorithms review §5.3): finding a shared block of code
// embedded inside two otherwise-unrelated host functions — the case
// union-normalized function-level Jaccard dilutes quadratically as the
// hosts grow.
//
// The algorithm is seed–extend–chain over the snippets' normalized
// token streams, anchored on shared winnowing fingerprints:
//
//  1. Seed: every fingerprint hash selected by BOTH snippets pins a
//     k-gram position on each side. Winnowing guarantees any shared
//     token run of ≥ k+w−1 tokens selects at least one common
//     fingerprint, so seeds are a complete candidate generator for
//     blocks longer than ~2 source lines — no new indexing needed.
//  2. Extend: each seed pair is grown left and right while the
//     normalized tokens are exactly equal, producing a maximal matched
//     segment. This is the exact-token verification: a hash match that
//     isn't a real token match dies here, and segment boundaries land
//     precisely where the two streams diverge.
//  3. Chain: segments that follow each other on BOTH sides with a gap
//     of at most gapBudget tokens coalesce into a chain — bridging
//     small divergences (an edited line inside an otherwise-copied
//     block) without letting scattered boilerplate stanzas link up
//     across the whole function.
//  4. Verify: a chain (or contiguous subchain) is promoted to a Match
//     only when its containment — matched tokens over the smaller
//     side's total span tokens — reaches MinContainment, and the
//     spanned block has at least minBlockLines non-blank lines on BOTH
//     sides. Boilerplate runs (err-check chains, logging blocks) fail
//     one bar or the other: either their matched span is mostly
//     divergent filler (low containment) or the truly-identical run is
//     too short (line floor).
//
// All functions are pure; detection runs per candidate pair with no
// shared state, so callers may parallelize freely.
package blocks

import (
	"sort"
	"strings"

	"github.com/ccsrvs/codetwin/internal/scan"
)

const (
	// DefaultMinBlockLines is the default --min-block-lines value: a
	// block must span at least this many non-blank lines on BOTH sides
	// to be reported. Mirrors the review §5.3 band (~8–10) at the
	// permissive end pinned by the bench contract.
	DefaultMinBlockLines = 8

	// MinContainment is the verification floor: the fraction of the
	// smaller side's span tokens that are exactly matched. Verbatim and
	// renamed blocks sit at ~1.0 and a block with one edited line at
	// ~0.9+ (the min-side denominator forgives insertion-only edits
	// entirely), while coalesced boilerplate — err-check chains
	// interleaved with divergent initializers — tops out around 0.83 on
	// the bench negatives. 0.85 splits the populations with margin on
	// both sides; the bench contract's floor is 0.8.
	MinContainment = 0.85

	// gapBudget is the maximum unmatched-token gap (per side) bridged
	// when chaining matched segments. ~20 tokens ≈ 2 source lines at
	// this tokenizer's density: wide enough to bridge a single edited
	// line inside a copied block, narrow enough that boilerplate
	// stanzas separated by multi-line divergent logic stay unchained.
	gapBudget = 20

	// maxSeedPositions caps how many positions a single fingerprint
	// hash may occupy on a side before it is skipped as a seed. A hash
	// recurring many times is ubiquitous boilerplate; seeding from it
	// adds quadratic seed pairs and no signal (real blocks are anchored
	// by their rarer neighbors anyway).
	maxSeedPositions = 4
)

// Match is one detected sub-function block clone: 1-based source line
// ranges of the shared block on both sides, plus the block's
// containment score (matched tokens / min-side span tokens) and each
// side's non-blank line count.
type Match struct {
	AStartLine, AEndLine int // line range of the block in snippet A's file
	BStartLine, BEndLine int // line range of the block in snippet B's file
	Containment          float64
	ALines, BLines       int // non-blank lines of the block on each side
}

// Detect finds verified block clones between two snippets. It returns
// only matches that survive exact-token verification (containment ≥
// MinContainment) and the minBlockLines non-blank-line floor on BOTH
// sides; overlapping and nested candidates are deduplicated down to
// the maximal non-overlapping set. minBlockLines <= 0 disables
// detection entirely. The result is deterministic for given inputs.
func Detect(a, b scan.Snippet, minBlockLines int) []Match {
	if minBlockLines <= 0 {
		return nil
	}
	k := a.Fps.K
	if k <= 0 || k != b.Fps.K {
		return nil
	}
	segs := matchedSegments(a.Tokens, b.Tokens, a.Fps.Positions, b.Fps.Positions, k)
	if len(segs) == 0 {
		return nil
	}
	sa, sb := newSide(a), newSide(b)
	var cands []candidate
	for _, chain := range chainSegments(segs, gapBudget) {
		if c, ok := bestSubchain(sa, sb, chain, minBlockLines); ok {
			cands = append(cands, c)
		}
	}
	cands = dedupeOverlapping(cands)
	out := make([]Match, len(cands))
	for i, c := range cands {
		out[i] = c.Match
	}
	return out
}

// segment is a maximal run of exactly-equal normalized tokens:
// a.Tokens[aStart : aStart+length] == b.Tokens[bStart : bStart+length].
type segment struct {
	aStart, bStart, length int
}

func (s segment) aEnd() int { return s.aStart + s.length - 1 }
func (s segment) bEnd() int { return s.bStart + s.length - 1 }

// matchedSegments seeds on shared fingerprint positions and extends
// each seed to its maximal exactly-equal segment. Segments are deduped
// (many seeds inside one shared block extend to the same maximal
// segment) and returned sorted by (aStart, bStart), which restores
// determinism after the map iteration.
func matchedSegments(ta, tb []string, posA, posB map[uint32][]int, k int) []segment {
	seen := make(map[[2]int]struct{})
	var segs []segment
	for h, pas := range posA {
		pbs, ok := posB[h]
		if !ok {
			continue
		}
		if len(pas) > maxSeedPositions || len(pbs) > maxSeedPositions {
			continue
		}
		for _, pa := range pas {
			for _, pb := range pbs {
				if pa < 0 || pb < 0 || pa+k > len(ta) || pb+k > len(tb) {
					continue
				}
				if !equalTokens(ta[pa:pa+k], tb[pb:pb+k]) {
					continue // hash collision, not a real match
				}
				s := extendSeed(ta, tb, pa, pb, k)
				key := [2]int{s.aStart, s.bStart}
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				segs = append(segs, s)
			}
		}
	}
	sort.Slice(segs, func(i, j int) bool {
		if segs[i].aStart != segs[j].aStart {
			return segs[i].aStart < segs[j].aStart
		}
		return segs[i].bStart < segs[j].bStart
	})
	return segs
}

func equalTokens(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// extendSeed grows the equal k-gram at (pa, pb) left and right while
// tokens stay exactly equal, yielding the maximal segment through that
// seed. Extension is deterministic, so any two seeds inside the same
// shared run produce the identical maximal segment.
func extendSeed(ta, tb []string, pa, pb, k int) segment {
	s, t := pa, pb
	for s > 0 && t > 0 && ta[s-1] == tb[t-1] {
		s--
		t--
	}
	e, f := pa+k-1, pb+k-1
	for e+1 < len(ta) && f+1 < len(tb) && ta[e+1] == tb[f+1] {
		e++
		f++
	}
	return segment{aStart: s, bStart: t, length: e - s + 1}
}

// chainable reports whether y may directly follow x in a chain: y
// starts strictly after x ends on BOTH sides, with each side's
// unmatched gap within budget. Requiring progression on both sides
// keeps chains order-consistent (no crossing alignments).
func chainable(x, y segment, gap int) bool {
	aGap := y.aStart - x.aEnd() - 1
	bGap := y.bStart - x.bEnd() - 1
	return aGap >= 0 && bGap >= 0 && aGap <= gap && bGap <= gap
}

// chainSegments partitions the segments into chains. It repeatedly
// extracts the chain with the most matched tokens (dynamic program
// over the sorted segment list), removes its segments, and repeats —
// so alternative alignments of the same region (shifted matches inside
// repetitive code) end up in separate, lower-value chains that the
// overlap dedup later discards.
func chainSegments(segs []segment, gap int) [][]segment {
	var chains [][]segment
	remaining := segs
	for len(remaining) > 0 {
		var chain []segment
		chain, remaining = extractBestChain(remaining, gap)
		chains = append(chains, chain)
	}
	return chains
}

// extractBestChain finds the maximum-matched-tokens chain among segs
// (which must be sorted by aStart) and returns it along with the
// segments not used by it. Ties resolve to the earliest segment, so
// extraction is deterministic.
func extractBestChain(segs []segment, gap int) (chain, rest []segment) {
	n := len(segs)
	score := make([]int, n)
	prev := make([]int, n)
	best := 0
	for i := 0; i < n; i++ {
		score[i] = segs[i].length
		prev[i] = -1
		for j := 0; j < i; j++ {
			if !chainable(segs[j], segs[i], gap) {
				continue
			}
			if s := score[j] + segs[i].length; s > score[i] {
				score[i] = s
				prev[i] = j
			}
		}
		if score[i] > score[best] {
			best = i
		}
	}
	inChain := make([]bool, n)
	for i := best; i >= 0; i = prev[i] {
		inChain[i] = true
	}
	for i := 0; i < n; i++ {
		if inChain[i] {
			chain = append(chain, segs[i])
		} else {
			rest = append(rest, segs[i])
		}
	}
	return chain, rest
}

// side pairs a snippet with a prefix-sum of its non-blank lines so
// per-subchain line floors are O(1) instead of re-splitting Code.
type side struct {
	snip scan.Snippet
	// nbPrefix[i] = number of non-blank lines among chunk-relative
	// lines 1..i (nbPrefix[0] = 0).
	nbPrefix []int
}

func newSide(s scan.Snippet) side {
	lines := strings.Split(s.Code, "\n")
	prefix := make([]int, len(lines)+1)
	for i, ln := range lines {
		prefix[i+1] = prefix[i]
		if strings.TrimSpace(ln) != "" {
			prefix[i+1]++
		}
	}
	return side{snip: s, nbPrefix: prefix}
}

// span converts an inclusive token range to (absolute start line,
// absolute end line, non-blank line count).
func (s side) span(firstTok, lastTok int) (startLine, endLine, nonBlank int) {
	if len(s.snip.Lines) == 0 {
		return 0, 0, 0
	}
	if lastTok >= len(s.snip.Lines) {
		lastTok = len(s.snip.Lines) - 1
	}
	if firstTok < 0 {
		firstTok = 0
	}
	rs, re := s.snip.Lines[firstTok], s.snip.Lines[lastTok]
	if re < rs {
		re = rs
	}
	hi, lo := re, rs-1
	if hi >= len(s.nbPrefix) {
		hi = len(s.nbPrefix) - 1
	}
	if lo < 0 {
		lo = 0
	}
	if lo > hi {
		lo = hi
	}
	return s.snip.StartLine + rs - 1, s.snip.StartLine + re - 1, s.nbPrefix[hi] - s.nbPrefix[lo]
}

// candidate is a verified match plus its matched-token mass, kept for
// overlap-dedup priority.
type candidate struct {
	Match
	matched int
}

// bestSubchain scans every contiguous subchain of a chain and returns
// the best one that passes verification: containment ≥ MinContainment
// and ≥ minBlockLines non-blank lines on both sides. "Best" is the
// most matched tokens (ties: higher containment, then smaller span,
// then earliest start), so a chain that sprawled into nearby
// coincidental segments is trimmed back to the region that actually
// verifies rather than rejected outright.
func bestSubchain(sa, sb side, chain []segment, minBlockLines int) (candidate, bool) {
	n := len(chain)
	prefix := make([]int, n+1)
	for i, s := range chain {
		prefix[i+1] = prefix[i] + s.length
	}
	var best candidate
	found := false
	for i := 0; i < n; i++ {
		for j := i; j < n; j++ {
			matched := prefix[j+1] - prefix[i]
			aSpan := chain[j].aEnd() - chain[i].aStart + 1
			bSpan := chain[j].bEnd() - chain[i].bStart + 1
			minSpan := aSpan
			if bSpan < minSpan {
				minSpan = bSpan
			}
			if minSpan <= 0 {
				continue
			}
			cont := float64(matched) / float64(minSpan)
			if cont < MinContainment {
				continue
			}
			aStartLn, aEndLn, aNB := sa.span(chain[i].aStart, chain[j].aEnd())
			bStartLn, bEndLn, bNB := sb.span(chain[i].bStart, chain[j].bEnd())
			if aNB < minBlockLines || bNB < minBlockLines {
				continue
			}
			c := candidate{
				Match: Match{
					AStartLine: aStartLn, AEndLine: aEndLn,
					BStartLine: bStartLn, BEndLine: bEndLn,
					Containment: cont,
					ALines:      aNB, BLines: bNB,
				},
				matched: matched,
			}
			if !found || betterCandidate(c, best) {
				best = c
				found = true
			}
		}
	}
	return best, found
}

// betterCandidate orders candidates for both subchain selection and
// overlap dedup: more matched tokens, then higher containment, then
// smaller total span (tighter block), then earliest position.
func betterCandidate(a, b candidate) bool {
	if a.matched != b.matched {
		return a.matched > b.matched
	}
	if a.Containment != b.Containment {
		return a.Containment > b.Containment
	}
	aSpan := (a.AEndLine - a.AStartLine) + (a.BEndLine - a.BStartLine)
	bSpan := (b.AEndLine - b.AStartLine) + (b.BEndLine - b.BStartLine)
	if aSpan != bSpan {
		return aSpan < bSpan
	}
	if a.AStartLine != b.AStartLine {
		return a.AStartLine < b.AStartLine
	}
	return a.BStartLine < b.BStartLine
}

// dedupeOverlapping keeps the maximal non-overlapping matches: sorted
// best-first, a candidate is dropped when its line ranges overlap an
// already-kept candidate on BOTH sides (the same underlying block seen
// through a shifted alignment). Overlap on one side only is kept — a
// block genuinely duplicated at two spots on the other side is two
// findings. Output is re-sorted by position for stable presentation.
func dedupeOverlapping(cands []candidate) []candidate {
	sort.Slice(cands, func(i, j int) bool { return betterCandidate(cands[i], cands[j]) })
	kept := cands[:0:0]
	for _, c := range cands {
		clash := false
		for _, k := range kept {
			if linesOverlap(c.AStartLine, c.AEndLine, k.AStartLine, k.AEndLine) &&
				linesOverlap(c.BStartLine, c.BEndLine, k.BStartLine, k.BEndLine) {
				clash = true
				break
			}
		}
		if !clash {
			kept = append(kept, c)
		}
	}
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].AStartLine != kept[j].AStartLine {
			return kept[i].AStartLine < kept[j].AStartLine
		}
		return kept[i].BStartLine < kept[j].BStartLine
	})
	return kept
}

func linesOverlap(aStart, aEnd, bStart, bEnd int) bool {
	return aStart <= bEnd && aEnd >= bStart
}
