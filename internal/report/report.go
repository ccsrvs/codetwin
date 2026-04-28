// Package report renders analysis results to a terminal with ANSI colors
// and structured tabular output. Supports plain text mode for CI pipelines.
package report

import (
	"cmp"
	"container/heap"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Pair represents a similarity finding between two snippets.
type Pair struct {
	NameA      string
	NameB      string
	Structural float64 // Jaccard
	Semantic   float64 // Cosine
	Score      float64 // Combined
	LinesA     int     // non-blank line count of snippet A's chunk
	LinesB     int     // non-blank line count of snippet B's chunk
}

// Cluster is a group of snippets identified as a refactoring family.
type Cluster struct {
	ID      int
	Members []string
	Score   float64 // average internal pair score across the cluster's members
}

// Preview is a code excerpt to display under a pair or cluster member.
// StartLine is the 1-based line number in the original source where Text
// begins, so rendered line numbers match the underlying file.
type Preview struct {
	StartLine int
	Text      string
}

// ExtractPreview returns the first n lines of code as a single newline-joined
// string. When n <= 0 the entire code is returned (unlimited mode). Line
// numbers are preserved by the caller via the chunk's StartLine, so this
// function does not skip leading blanks.
func ExtractPreview(code string, n int) string {
	lines := strings.Split(code, "\n")
	if n <= 0 || n > len(lines) {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:n], "\n")
}

// BuildMatchPreview returns a Preview focused on the line range covered by
// [firstTok, lastTok], extending the last token by k-1 to cover the full
// k-gram. Behavior by maxLines:
//
//	maxLines == 0:          show the whole chunk (unlimited)
//	chunk lines <= maxLines: show the whole chunk (it fits)
//	otherwise:              focus on the match range, taking up to maxLines
//	                        lines starting at the first matching line
func BuildMatchPreview(code string, tokenLines []int, chunkStartLine, firstTok, lastTok, k, maxLines int) Preview {
	chunkLines := strings.Split(code, "\n")
	if maxLines <= 0 || len(chunkLines) <= maxLines {
		return Preview{
			StartLine: chunkStartLine,
			Text:      strings.Join(chunkLines, "\n"),
		}
	}

	if firstTok < 0 || firstTok >= len(tokenLines) {
		return Preview{
			StartLine: chunkStartLine,
			Text:      strings.Join(chunkLines[:maxLines], "\n"),
		}
	}
	endTok := lastTok + k - 1
	if endTok >= len(tokenLines) {
		endTok = len(tokenLines) - 1
	}
	if endTok < firstTok {
		endTok = firstTok
	}

	chunkFirstLine := tokenLines[firstTok]
	chunkLastLine := tokenLines[endTok]
	if chunkLastLine < chunkFirstLine {
		chunkLastLine = chunkFirstLine
	}
	if chunkFirstLine > len(chunkLines) {
		chunkFirstLine = len(chunkLines)
	}
	if chunkLastLine > len(chunkLines) {
		chunkLastLine = len(chunkLines)
	}

	selected := chunkLines[chunkFirstLine-1 : chunkLastLine]
	if len(selected) > maxLines {
		selected = selected[:maxLines]
	}

	return Preview{
		StartLine: chunkStartLine + chunkFirstLine - 1,
		Text:      strings.Join(selected, "\n"),
	}
}

// SortMode controls the ordering of pairs and clusters in the rendered
// report. The same mode applies to both sections, with each section using
// the natural interpretation: pair size = max(LinesA, LinesB), cluster size
// = number of members.
type SortMode string

const (
	SortScore    SortMode = "score"     // descending by score (default)
	SortScoreAsc SortMode = "score-asc" // ascending by score
	SortSize     SortMode = "size"      // descending by size
	SortSizeAsc  SortMode = "size-asc"  // ascending by size
	SortName     SortMode = "name"      // alphabetical by NameA / first member
)

// Options controls rendering behaviour.
type Options struct {
	Plain     bool     // disable ANSI color codes (for CI / file output)
	Threshold float64  // hide pairs below this score (unless Verbose)
	Verbose   bool     // include pairs below threshold
	Sort      SortMode // ordering for pairs and clusters; "" = SortScore
	Limit     int      // cap pairs and clusters at N items each (0 = no limit)

	// Previews, when non-nil, maps a snippet name to a code excerpt with its
	// originating start line. Entries with empty Text are skipped.
	Previews map[string]Preview
}

// ANSI color codes
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	red    = "\033[31m"
	orange = "\033[33m"
	yellow = "\033[93m"
	green  = "\033[32m"
	cyan   = "\033[36m"
	grey   = "\033[90m"
	purple = "\033[35m"
	white  = "\033[97m"
)

// Prepare applies the report pipeline to raw pairs+clusters: filter by
// Options.Threshold (unless Verbose), then sort by Options.Sort, then cap
// each section to Options.Limit.
//
// Order matters for performance on big repos. Filtering before sorting
// drops millions of below-threshold pairs before they pay the n-log-n
// sort cost. When Options.Limit is small, a top-K heap walk replaces the
// full sort entirely — turning 20s of sorting on 11M pairs into a single
// O(n log k) pass.
//
// Both Render and JSON consumers call Prepare so the two output formats
// always reflect the same set of findings.
func Prepare(pairs []Pair, clusters []Cluster, opts Options) ([]Pair, []Cluster) {
	visiblePairs := pairs
	if !opts.Verbose {
		visiblePairs = make([]Pair, 0, len(pairs))
		for _, p := range pairs {
			if p.Score >= opts.Threshold {
				visiblePairs = append(visiblePairs, p)
			}
		}
	}

	visiblePairs = sortAndLimit(visiblePairs, pairLessFunc(opts.Sort), opts.Limit)
	visibleClusters := sortAndLimit(clusters, clusterLessFunc(opts.Sort), opts.Limit)
	return visiblePairs, visibleClusters
}

// sortAndLimit returns up to `limit` items in the order defined by `less`
// (where `less(a, b)` is true when a should appear before b). When limit
// is 0 (unlimited) or larger than the input, it falls back to a stable
// full sort. Otherwise it uses a top-K min-heap, which is O(n log k)
// instead of O(n log n) — a big win for "show me the top 5 of 11 million
// pairs."
func sortAndLimit[T any](items []T, less func(a, b T) bool, limit int) []T {
	if limit <= 0 || limit >= len(items) {
		sort.SliceStable(items, func(i, j int) bool { return less(items[i], items[j]) })
		return items
	}
	// Min-heap of capacity `limit` keyed by `less`, so the root is the
	// worst entry currently in the heap. For each subsequent item, if it
	// beats the root we evict the root.
	h := &topKHeap[T]{items: make([]T, 0, limit+1), less: less}
	for i := range items {
		if len(h.items) < limit {
			heap.Push(h, items[i])
		} else if less(items[i], h.items[0]) {
			h.items[0] = items[i]
			heap.Fix(h, 0)
		}
	}
	// Drain the heap into a slice, then reverse so the output is best-first.
	out := make([]T, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		out[i] = heap.Pop(h).(T)
	}
	return out
}

// topKHeap is a min-heap of items ordered by `less`. The root is the
// worst item currently retained, so a new item only displaces the root
// when `less(new, root)` — i.e. new is "better" than the worst.
type topKHeap[T any] struct {
	items []T
	less  func(a, b T) bool
}

func (h *topKHeap[T]) Len() int { return len(h.items) }
func (h *topKHeap[T]) Less(i, j int) bool {
	// Min-heap on the *output* ordering: the heap's Less returns true
	// when items[i] is "worse" than items[j]. Since the caller's
	// `less(a, b)` returns true when a should appear before b in the
	// output (a is "better"), the heap's Less is the inverse.
	return h.less(h.items[j], h.items[i])
}
func (h *topKHeap[T]) Swap(i, j int) { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *topKHeap[T]) Push(x any)    { h.items = append(h.items, x.(T)) }
func (h *topKHeap[T]) Pop() any {
	old := h.items
	n := len(old)
	x := old[n-1]
	h.items = old[:n-1]
	return x
}

// Render writes the full report to w. Calls Prepare internally; callers that
// have already prepared their data (e.g. shared between text and JSON
// output) can call printPairs/printClusters/printSummary directly, but the
// usual flow is to let Render do everything.
func Render(w io.Writer, pairs []Pair, clusters []Cluster, opts Options) {
	visiblePairs, visibleClusters := Prepare(pairs, clusters, opts)

	printHeader(w, opts)

	if len(visiblePairs) == 0 {
		fmt.Fprintf(w, "\n%s  No similarities found above threshold %.0f%%%s\n\n",
			color(green, opts), opts.Threshold*100, color(reset, opts))
		return
	}

	printPairs(w, visiblePairs, opts)
	printClusters(w, visibleClusters, opts)
	printSummary(w, visiblePairs, visibleClusters, opts)
}

// lessChain composes int-returning comparators into a less function. The
// first non-zero comparator decides the ordering; ties fall through to
// the next. Returns false on a full tie so sort.SliceStable preserves
// the input order.
func lessChain[T any](cmps ...func(a, b T) int) func(a, b T) bool {
	return func(a, b T) bool {
		for _, c := range cmps {
			if r := c(a, b); r != 0 {
				return r < 0
			}
		}
		return false
	}
}

// Pair comparators.
func cmpPairScoreAsc(a, b Pair) int  { return cmp.Compare(a.Score, b.Score) }
func cmpPairScoreDesc(a, b Pair) int { return cmp.Compare(b.Score, a.Score) }
func cmpPairSizeDesc(a, b Pair) int  { return cmp.Compare(pairSize(b), pairSize(a)) }
func cmpPairSizeAsc(a, b Pair) int   { return cmp.Compare(pairSize(a), pairSize(b)) }
func cmpPairNameA(a, b Pair) int     { return cmp.Compare(a.NameA, b.NameA) }
func cmpPairNameB(a, b Pair) int     { return cmp.Compare(a.NameB, b.NameB) }

// Cluster comparators.
func cmpClusterScoreAsc(a, b Cluster) int  { return cmp.Compare(a.Score, b.Score) }
func cmpClusterScoreDesc(a, b Cluster) int { return cmp.Compare(b.Score, a.Score) }
func cmpClusterSizeDesc(a, b Cluster) int  { return cmp.Compare(len(b.Members), len(a.Members)) }
func cmpClusterSizeAsc(a, b Cluster) int   { return cmp.Compare(len(a.Members), len(b.Members)) }
func cmpClusterID(a, b Cluster) int        { return cmp.Compare(a.ID, b.ID) }
func cmpClusterFirstMember(a, b Cluster) int {
	return cmp.Compare(firstMember(a), firstMember(b))
}

// pairLessFunc returns the value-based less function for the given sort
// mode: less(a, b) is true when a should appear before b in the output.
// Used by both the full sort path and the top-K heap path so the two
// share a single source of truth for ordering. Tied pairs rely on
// sort.SliceStable to preserve input order.
func pairLessFunc(mode SortMode) func(a, b Pair) bool {
	switch mode {
	case SortScoreAsc:
		return lessChain(cmpPairScoreAsc)
	case SortSize:
		return lessChain(cmpPairSizeDesc)
	case SortSizeAsc:
		return lessChain(cmpPairSizeAsc)
	case SortName:
		return lessChain(cmpPairNameA, cmpPairNameB)
	default: // SortScore or empty
		return lessChain(cmpPairScoreDesc)
	}
}

// clusterLessFunc returns the value-based less function for clusters.
// Score and Size sorts use ID as a stable tiebreaker so runs with
// identical input produce identical output.
func clusterLessFunc(mode SortMode) func(a, b Cluster) bool {
	switch mode {
	case SortScoreAsc:
		return lessChain(cmpClusterScoreAsc, cmpClusterID)
	case SortSize:
		return lessChain(cmpClusterSizeDesc, cmpClusterID)
	case SortSizeAsc:
		return lessChain(cmpClusterSizeAsc, cmpClusterID)
	case SortName:
		return lessChain(cmpClusterFirstMember)
	default: // SortScore or empty
		return lessChain(cmpClusterScoreDesc, cmpClusterID)
	}
}

func pairSize(p Pair) int {
	if p.LinesA > p.LinesB {
		return p.LinesA
	}
	return p.LinesB
}

func firstMember(c Cluster) string {
	if len(c.Members) == 0 {
		return ""
	}
	return c.Members[0]
}

func printHeader(w io.Writer, opts Options) {
	fmt.Fprintf(w, "\n%s%s codetwin · code similarity report %s\n",
		color(bold, opts), color(purple, opts), color(reset, opts))
	fmt.Fprintf(w, "%s%s%s\n\n",
		color(grey, opts),
		strings.Repeat("─", 60),
		color(reset, opts))
}

func printPairs(w io.Writer, pairs []Pair, opts Options) {
	fmt.Fprintf(w, "%s%s SIMILARITY PAIRS%s\n\n",
		color(bold, opts), color(white, opts), color(reset, opts))

	for _, p := range pairs {
		label, clr := classify(p.Score)

		fmt.Fprintf(w, "  %s%s%s  %s%3.0f%%%s\n",
			color(clr, opts), color(bold, opts), label,
			color(clr, opts), p.Score*100, color(reset, opts))

		fmt.Fprintf(w, "  %s  %s%s%s\n",
			color(grey, opts),
			color(cyan, opts), p.NameA,
			color(reset, opts))
		printPreview(w, p.NameA, opts)

		fmt.Fprintf(w, "  %s  %s%s%s\n",
			color(grey, opts),
			color(cyan, opts), p.NameB,
			color(reset, opts))
		printPreview(w, p.NameB, opts)

		fmt.Fprintf(w, "  %sstructural: %3.0f%%  semantic: %3.0f%%%s\n\n",
			color(grey, opts),
			p.Structural*100, p.Semantic*100,
			color(reset, opts))
	}
}

// printPreview emits a line-numbered code excerpt under the snippet name when
// opts.Previews has an entry for the given name. No-op otherwise. Line
// numbers are rendered as preview.StartLine + offset so they match the
// underlying source file rather than restarting at 1.
func printPreview(w io.Writer, name string, opts Options) {
	if opts.Previews == nil {
		return
	}
	pv, ok := opts.Previews[name]
	if !ok || pv.Text == "" {
		return
	}
	start := pv.StartLine
	if start < 1 {
		start = 1
	}
	lines := strings.Split(strings.TrimRight(pv.Text, "\n"), "\n")
	for i, line := range lines {
		fmt.Fprintf(w, "      %s%4d │%s %s\n",
			color(grey, opts), start+i, color(reset, opts), line)
	}
}

func printClusters(w io.Writer, clusters []Cluster, opts Options) {
	if len(clusters) == 0 {
		return
	}

	fmt.Fprintf(w, "%s%s REFACTORING CLUSTERS%s\n\n",
		color(bold, opts), color(white, opts), color(reset, opts))

	for _, c := range clusters {
		fmt.Fprintf(w, "  %sCluster %d%s — %d snippets\n",
			color(green, opts), c.ID+1, color(reset, opts), len(c.Members))
		for _, m := range c.Members {
			fmt.Fprintf(w, "    %s·%s %s\n", color(grey, opts), color(reset, opts), m)
			printPreview(w, m, opts)
		}
		fmt.Fprintln(w)
	}
}

func printSummary(w io.Writer, pairs []Pair, clusters []Cluster, opts Options) {
	// pairs is already filtered+limited by Render, so each bucket count is a
	// straightforward classification of what the reader sees.
	exact, near, strong, candidates, weak := 0, 0, 0, 0, 0
	for _, p := range pairs {
		switch {
		case p.Score > 0.95:
			exact++
		case p.Score > 0.85:
			near++
		case p.Score > 0.65:
			strong++
		case p.Score > 0.45:
			candidates++
		default:
			weak++
		}
	}

	fmt.Fprintf(w, "%s%s SUMMARY%s\n",
		color(bold, opts), color(white, opts), color(reset, opts))
	fmt.Fprintf(w, "%s%s%s\n",
		color(grey, opts), strings.Repeat("─", 60), color(reset, opts))
	fmt.Fprintf(w, "  %sPairs shown%s       %s%d%s\n",
		color(grey, opts), color(reset, opts), color(cyan, opts), len(pairs), color(reset, opts))
	fmt.Fprintf(w, "  %sExact clones%s      %s%d%s\n",
		color(grey, opts), color(reset, opts), color(red, opts), exact, color(reset, opts))
	fmt.Fprintf(w, "  %sNear clones%s       %s%d%s\n",
		color(grey, opts), color(reset, opts), color(red, opts), near, color(reset, opts))
	fmt.Fprintf(w, "  %sStrong clones%s     %s%d%s\n",
		color(grey, opts), color(reset, opts), color(orange, opts), strong, color(reset, opts))
	fmt.Fprintf(w, "  %sRefactor targets%s  %s%d%s\n",
		color(grey, opts), color(reset, opts), color(yellow, opts), candidates, color(reset, opts))
	if weak > 0 {
		fmt.Fprintf(w, "  %sWeak similarities%s %s%d%s\n",
			color(grey, opts), color(reset, opts), color(grey, opts), weak, color(reset, opts))
	}
	fmt.Fprintf(w, "  %sClusters found%s    %s%d%s\n\n",
		color(grey, opts), color(reset, opts), color(green, opts), len(clusters), color(reset, opts))
}

// tier groups the per-band facts (boundary, terminal label, color, JSON
// label) so all surfaces draw from the same source — adding or moving a
// band is a single-line edit.
type tier struct {
	above float64
	label string
	color string
	json  string
}

var tiers = []tier{
	{0.95, "[EXACT CLONE     ]", red, "exact_clone"},
	{0.85, "[NEAR CLONE      ]", red, "near_clone"},
	{0.65, "[STRONG CLONE    ]", orange, "strong_clone"},
	{0.45, "[REFACTOR TARGET ]", yellow, "refactor_candidate"},
	{-1, "[WEAK SIMILARITY ]", grey, "weak_similarity"},
}

func tierFor(score float64) tier {
	for _, t := range tiers {
		if score > t.above {
			return t
		}
	}
	return tiers[len(tiers)-1]
}

func classify(score float64) (string, string) {
	t := tierFor(score)
	return t.label, t.color
}

// JSONLabel returns the snake-case classification name used in JSON output.
func JSONLabel(score float64) string { return tierFor(score).json }

func color(code string, opts Options) string {
	if opts.Plain {
		return ""
	}
	return code
}
