// Package refactor turns a clone pair into a refactor *suggestion*: a
// language-agnostic alignment of the two snippets' source lines, plus a
// per-language synthesizer that emits a starter helper and a unified
// diff. The alignment layer (this file) is the language-agnostic
// scaffolding; synthesis (synth.go) and diff emission (patch.go) are
// per-language.
//
// The v1 synthesizer for Go produces a *starter* helper — a literal
// copy of snippet A's body, annotated with a comment block listing the
// divergences in B. Codetwin doesn't try to fully parameterize the
// extraction; it gives the human (or the Claude skill) a structured
// starting point. This is deliberate: full parameterization without a
// language AST is unsafe (parameter typing, scope capture, control
// flow). The starter-helper approach makes the boundary explicit.
//
// Why line-level instead of token-level: the tokenizer normalizes
// literals (`0.07` → `NUM`, `"foo"` → `STR`) so a token-level diff
// would find a numeric-literal-only swap "identical." We diff the raw
// source lines directly. Snippets are short (function-level) so the
// quadratic LCS is fine.
package refactor

import (
	"strings"

	"github.com/ccsrvs/codetwin/internal/scan"
)

// LineSpan is a half-open line range (1-based, end exclusive) common
// to A and B.
type LineSpan struct {
	AStart, AEnd int
	BStart, BEnd int
}

// Hole is a half-open line range where A and B diverge. AText/BText
// recover the source bytes from each side's Snippet.Code so callers
// can show readers exactly what differs without reading source files
// again. Either side may be a zero-width range when the divergence is
// one-sided (an inserted block of lines on only one side); the
// corresponding Text is "" in that case.
type Hole struct {
	AStart, AEnd int    // 1-based, end exclusive in A.Code lines
	BStart, BEnd int
	AText, BText string // joined source lines, no trailing newline
}

// Alignment is a complete partition of A's and B's source lines into
// common spans (matching lines in both) and holes (diverging lines).
// The common spans, in order, form the LCS over lines; holes are the
// gaps between them.
type Alignment struct {
	Common []LineSpan
	Holes  []Hole
}

// CommonLines reports how many A-side lines the alignment classifies
// as common across A and B. Used by the synthesizer's confidence score
// and the rejection rule for "no meaningful overlap".
func (a Alignment) CommonLines() int {
	n := 0
	for _, s := range a.Common {
		if s.AEnd > s.AStart {
			n += s.AEnd - s.AStart
		}
	}
	return n
}

// Align partitions a.Code and b.Code into common runs and holes using
// a standard longest-common-subsequence DP at the line level. Snippets
// are function-level and typically under 50 lines, so the O(N*M) DP is
// trivially fast.
//
// Lines are compared after trimming trailing whitespace (so reformat
// noise doesn't break alignment) but otherwise verbatim. Differences
// in indentation are preserved as divergences — they usually signal a
// genuine structural difference worth surfacing.
func Align(a, b scan.Snippet) Alignment {
	aLines := splitNoTrailingEmpty(a.Code)
	bLines := splitNoTrailingEmpty(b.Code)
	aKeys := normalizeLines(aLines)
	bKeys := normalizeLines(bLines)

	if len(aLines) == 0 || len(bLines) == 0 {
		return Alignment{
			Holes: []Hole{singleHole(aLines, bLines, 0, len(aLines), 0, len(bLines))},
		}
	}

	// LCS DP. dp[i][j] = LCS length of aKeys[i:] and bKeys[j:].
	n, m := len(aKeys), len(bKeys)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if aKeys[i] == bKeys[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	type match struct{ i, j int }
	var matches []match
	i, j := 0, 0
	for i < n && j < m {
		if aKeys[i] == bKeys[j] {
			matches = append(matches, match{i, j})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}

	// Group matches into maximal common spans (consecutive in both
	// A-index and B-index) and emit holes for the gaps.
	var common []LineSpan
	var holes []Hole

	prevAEnd, prevBEnd := 0, 0
	k := 0
	for k < len(matches) {
		runStart := k
		for k+1 < len(matches) &&
			matches[k+1].i == matches[k].i+1 &&
			matches[k+1].j == matches[k].j+1 {
			k++
		}
		aStart := matches[runStart].i
		aEnd := matches[k].i + 1
		bStart := matches[runStart].j
		bEnd := matches[k].j + 1

		if aStart > prevAEnd || bStart > prevBEnd {
			holes = append(holes, singleHole(aLines, bLines, prevAEnd, aStart, prevBEnd, bStart))
		}
		common = append(common, LineSpan{AStart: aStart, AEnd: aEnd, BStart: bStart, BEnd: bEnd})
		prevAEnd, prevBEnd = aEnd, bEnd
		k++
	}
	if prevAEnd < n || prevBEnd < m {
		holes = append(holes, singleHole(aLines, bLines, prevAEnd, n, prevBEnd, m))
	}

	// Convert 0-based half-open indices into 1-based half-open line
	// ranges before returning, so callers can address Code lines by the
	// same 1-based convention used everywhere else in codetwin.
	for i := range common {
		common[i].AStart++
		common[i].BStart++
		common[i].AEnd++
		common[i].BEnd++
	}
	for i := range holes {
		if holes[i].AEnd > holes[i].AStart {
			holes[i].AStart++
			holes[i].AEnd++
		}
		if holes[i].BEnd > holes[i].BStart {
			holes[i].BStart++
			holes[i].BEnd++
		}
	}
	return Alignment{Common: common, Holes: holes}
}

// splitNoTrailingEmpty splits code into lines, dropping a single
// trailing empty line that the splitter often appends (a chunk's Code
// may end with `\n`, which strings.Split otherwise turns into an extra
// empty element).
func splitNoTrailingEmpty(code string) []string {
	lines := strings.Split(code, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// normalizeLines produces the comparison key for each source line:
// trimmed-right (so reformat-induced trailing whitespace doesn't break
// alignment). Indentation is preserved.
func normalizeLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, " \t\r")
	}
	return out
}

// singleHole builds a Hole from 0-based half-open ranges in aLines and
// bLines. The resulting Hole has 0-based indices; Align bumps to
// 1-based before returning.
func singleHole(aLines, bLines []string, aStart, aEnd, bStart, bEnd int) Hole {
	h := Hole{AStart: aStart, AEnd: aEnd, BStart: bStart, BEnd: bEnd}
	if aEnd > aStart {
		h.AText = strings.Join(aLines[aStart:aEnd], "\n")
	}
	if bEnd > bStart {
		h.BText = strings.Join(bLines[bStart:bEnd], "\n")
	}
	return h
}
