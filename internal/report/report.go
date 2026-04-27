// Package report renders analysis results to a terminal with ANSI colors
// and structured tabular output. Supports plain text mode for CI pipelines.
package report

import (
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
}

// Cluster is a group of snippets identified as a refactoring family.
type Cluster struct {
	ID      int
	Members []string
}

// Preview is a code excerpt to display under a pair or cluster member.
// StartLine is the 1-based line number in the original source where Text
// begins, so rendered line numbers match the underlying file.
type Preview struct {
	StartLine int
	Text      string
}

// Options controls rendering behaviour.
type Options struct {
	Plain     bool    // disable ANSI color codes (for CI / file output)
	Threshold float64 // only show pairs >= this score
	Verbose   bool    // show all pairs, not just actionable ones

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

// Render writes the full report to w.
func Render(w io.Writer, pairs []Pair, clusters []Cluster, opts Options) {
	// Sort pairs by descending score
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Score > pairs[j].Score
	})

	printHeader(w, opts)

	if len(pairs) == 0 {
		fmt.Fprintf(w, "\n%s  No similarities found above threshold %.0f%%%s\n\n",
			color(green, opts), opts.Threshold*100, color(reset, opts))
		return
	}

	printPairs(w, pairs, opts)
	printClusters(w, clusters, opts)
	printSummary(w, pairs, clusters, opts)
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
		if p.Score < opts.Threshold && !opts.Verbose {
			continue
		}
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
	exact := 0
	strong := 0
	candidates := 0
	for _, p := range pairs {
		switch {
		case p.Score > 0.85:
			exact++
		case p.Score > 0.65:
			strong++
		case p.Score > 0.45:
			candidates++
		}
	}

	fmt.Fprintf(w, "%s%s SUMMARY%s\n",
		color(bold, opts), color(white, opts), color(reset, opts))
	fmt.Fprintf(w, "%s%s%s\n",
		color(grey, opts), strings.Repeat("─", 60), color(reset, opts))
	fmt.Fprintf(w, "  %sExact clones%s      %s%d%s\n",
		color(grey, opts), color(reset, opts), color(red, opts), exact, color(reset, opts))
	fmt.Fprintf(w, "  %sStrong clones%s     %s%d%s\n",
		color(grey, opts), color(reset, opts), color(orange, opts), strong, color(reset, opts))
	fmt.Fprintf(w, "  %sRefactor targets%s  %s%d%s\n",
		color(grey, opts), color(reset, opts), color(yellow, opts), candidates, color(reset, opts))
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
