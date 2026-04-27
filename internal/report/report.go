// Package report renders analysis results to a terminal with ANSI colors
// and structured tabular output. Supports plain text mode for CI pipelines.
package report

import (
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

	visiblePairs = sortAndLimitPairs(visiblePairs, opts.Sort, opts.Limit)
	visibleClusters := sortAndLimitClusters(clusters, opts.Sort, opts.Limit)
	return visiblePairs, visibleClusters
}

// sortAndLimitPairs returns up to `limit` pairs in the order specified by
// `mode`. When limit is 0 (unlimited) or larger than the input, it falls
// back to a full sort. Otherwise it uses a top-K heap, which is O(n log k)
// instead of the full O(n log n) — a big win for "show me the top 5 of
// 11 million pairs."
func sortAndLimitPairs(pairs []Pair, mode SortMode, limit int) []Pair {
	if limit <= 0 || limit >= len(pairs) {
		sortPairs(pairs, mode)
		return pairs
	}
	less := pairLessFunc(mode)
	// Build a min-heap of capacity `limit` keyed by `less`, so the root
	// is the worst entry currently in the heap. For each subsequent pair,
	// if it beats the root we evict the root.
	h := &pairHeap{items: make([]Pair, 0, limit+1), less: less}
	for i := range pairs {
		if len(h.items) < limit {
			heap.Push(h, pairs[i])
		} else if less(pairs[i], h.items[0]) {
			h.items[0] = pairs[i]
			heap.Fix(h, 0)
		}
	}
	// Drain the heap into a slice, then reverse so the output is best-first.
	out := make([]Pair, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		out[i] = heap.Pop(h).(Pair)
	}
	return out
}

// sortAndLimitClusters mirrors sortAndLimitPairs for clusters. Cluster
// counts are usually small (hundreds at most), so the speedup matters
// less here, but using the same code path keeps semantics consistent.
func sortAndLimitClusters(clusters []Cluster, mode SortMode, limit int) []Cluster {
	if limit <= 0 || limit >= len(clusters) {
		sortClusters(clusters, mode)
		return clusters
	}
	less := clusterLessFunc(mode)
	h := &clusterHeap{items: make([]Cluster, 0, limit+1), less: less}
	for i := range clusters {
		if len(h.items) < limit {
			heap.Push(h, clusters[i])
		} else if less(clusters[i], h.items[0]) {
			h.items[0] = clusters[i]
			heap.Fix(h, 0)
		}
	}
	out := make([]Cluster, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		out[i] = heap.Pop(h).(Cluster)
	}
	return out
}

// pairHeap is a min-heap of pairs ordered by `less`. The root is the
// worst pair currently retained, so a new pair only displaces the root
// when `less(new, root)` — i.e. new is "better" than the worst.
type pairHeap struct {
	items []Pair
	less  func(a, b Pair) bool
}

func (h *pairHeap) Len() int { return len(h.items) }
func (h *pairHeap) Less(i, j int) bool {
	// We want a min-heap on the *output* ordering, so the heap's Less
	// returns true when items[i] is "worse" than items[j]. Since the
	// caller's `less(a, b)` returns true when a should appear before b
	// in the output (a is "better"), the heap's Less is the inverse.
	return h.less(h.items[j], h.items[i])
}
func (h *pairHeap) Swap(i, j int) { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *pairHeap) Push(x any)    { h.items = append(h.items, x.(Pair)) }
func (h *pairHeap) Pop() any {
	old := h.items
	n := len(old)
	x := old[n-1]
	h.items = old[:n-1]
	return x
}

// clusterHeap mirrors pairHeap for clusters.
type clusterHeap struct {
	items []Cluster
	less  func(a, b Cluster) bool
}

func (h *clusterHeap) Len() int            { return len(h.items) }
func (h *clusterHeap) Less(i, j int) bool  { return h.less(h.items[j], h.items[i]) }
func (h *clusterHeap) Swap(i, j int)       { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *clusterHeap) Push(x any)          { h.items = append(h.items, x.(Cluster)) }
func (h *clusterHeap) Pop() any {
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

// pairLessFunc returns the value-based less function for the given sort
// mode: less(a, b) is true when a should appear before b in the output.
// Used by both the full sort path and the top-K heap path so the two
// share a single source of truth for ordering.
func pairLessFunc(mode SortMode) func(a, b Pair) bool {
	switch mode {
	case SortScoreAsc:
		return func(a, b Pair) bool { return a.Score < b.Score }
	case SortSize:
		return func(a, b Pair) bool { return pairSize(a) > pairSize(b) }
	case SortSizeAsc:
		return func(a, b Pair) bool { return pairSize(a) < pairSize(b) }
	case SortName:
		return func(a, b Pair) bool {
			if a.NameA != b.NameA {
				return a.NameA < b.NameA
			}
			return a.NameB < b.NameB
		}
	default: // SortScore or empty
		return func(a, b Pair) bool { return a.Score > b.Score }
	}
}

// clusterLessFunc returns the value-based less function for clusters.
// Score sorts use ID as a stable tiebreaker so runs with identical input
// produce identical output.
func clusterLessFunc(mode SortMode) func(a, b Cluster) bool {
	switch mode {
	case SortScoreAsc:
		return func(a, b Cluster) bool {
			if a.Score != b.Score {
				return a.Score < b.Score
			}
			return a.ID < b.ID
		}
	case SortSize:
		return func(a, b Cluster) bool {
			if len(a.Members) != len(b.Members) {
				return len(a.Members) > len(b.Members)
			}
			return a.ID < b.ID
		}
	case SortSizeAsc:
		return func(a, b Cluster) bool {
			if len(a.Members) != len(b.Members) {
				return len(a.Members) < len(b.Members)
			}
			return a.ID < b.ID
		}
	case SortName:
		return func(a, b Cluster) bool {
			return firstMember(a) < firstMember(b)
		}
	default: // SortScore or empty
		return func(a, b Cluster) bool {
			if a.Score != b.Score {
				return a.Score > b.Score
			}
			return a.ID < b.ID
		}
	}
}

// sortPairs orders pairs in place per mode. Stable so that ties preserve
// the caller's input order — important for deterministic output across
// runs.
func sortPairs(pairs []Pair, mode SortMode) {
	less := pairLessFunc(mode)
	sort.SliceStable(pairs, func(i, j int) bool { return less(pairs[i], pairs[j]) })
}

// sortClusters orders clusters in place per mode.
func sortClusters(clusters []Cluster, mode SortMode) {
	less := clusterLessFunc(mode)
	sort.SliceStable(clusters, func(i, j int) bool { return less(clusters[i], clusters[j]) })
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
	exact := 0
	strong := 0
	candidates := 0
	weak := 0
	for _, p := range pairs {
		switch {
		case p.Score > 0.85:
			exact++
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

func classify(score float64) (string, string) {
	switch {
	case score > 0.85:
		return "[EXACT CLONE     ]", red
	case score > 0.65:
		return "[STRONG CLONE    ]", orange
	case score > 0.45:
		return "[REFACTOR TARGET ]", yellow
	default:
		return "[WEAK SIMILARITY ]", grey
	}
}

func color(code string, opts Options) string {
	if opts.Plain {
		return ""
	}
	return code
}
