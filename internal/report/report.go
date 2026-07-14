// Package report renders analysis results to a terminal with ANSI colors
// and structured tabular output. Supports plain text mode for CI pipelines.
package report

import (
	"cmp"
	"container/heap"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PairID returns a stable, order-invariant 8-char hex digest for a pair
// of snippet names. Sorting the inputs before hashing means
// PairID(a,b) == PairID(b,a). The 8-char prefix is enough namespace to
// disambiguate every pair on a real corpus (32 bits of entropy) while
// staying short enough to type at the CLI.
func PairID(nameA, nameB string) string {
	lo, hi := nameA, nameB
	if hi < lo {
		lo, hi = hi, lo
	}
	sum := sha1.Sum([]byte(lo + "|" + hi))
	return hex.EncodeToString(sum[:4])
}

// Pair represents a similarity finding between two snippets.
type Pair struct {
	// ID is a stable, order-invariant 8-char hex digest derived from
	// (sorted) NameA + NameB. Lets `--suggest <id>` address one pair
	// across runs without rerunning the whole pipeline. Empty only on
	// pairs constructed outside BuildMatrix (e.g. test fixtures).
	ID         string
	NameA      string
	NameB      string
	Structural float64 // Jaccard
	Semantic   float64 // Cosine
	Score      float64 // Combined
	LinesA     int     // non-blank line count of snippet A's chunk
	LinesB     int     // non-blank line count of snippet B's chunk
	LangA      string  // detected language of snippet A (e.g. "Go", "Python"); empty when unknown
	LangB      string  // detected language of snippet B; empty when unknown
	IsTestA    bool    // snippet A's file path follows its language's test convention
	IsTestB    bool    // snippet B's file path follows its language's test convention
	RepoA      string  // repo label of snippet A in a multi-root scan; empty otherwise
	RepoB      string  // repo label of snippet B in a multi-root scan; empty otherwise

	// Lexical is the Jaccard similarity of the two snippets' raw-code
	// vocabulary (identifier + string-literal words; see
	// tokenizer.LexicalTerms). It NEVER feeds the numeric Score — it
	// only modulates the top label bands: a pair above
	// StructuralTwinMinScore whose Lexical falls below
	// StructuralTwinMaxLexical renders as a structural twin instead of
	// an exact/near clone. LexicalComputed distinguishes a measured 0
	// from "not computed" (BuildMatrix computes it lazily, only for
	// pairs in the bands that read it); when false, Lexical is
	// meaningless and the label logic ignores it.
	Lexical         float64
	LexicalComputed bool

	// ProvenanceA / ProvenanceB carry git blame metadata for each
	// endpoint, populated when --blame is on. Nil when blame wasn't
	// computed for that snippet (no git, untracked file, blame off).
	ProvenanceA *Provenance
	ProvenanceB *Provenance
}

// Provenance is the per-snippet git blame summary: when (and by whom)
// the snippet's lines were first introduced and most recently touched.
type Provenance struct {
	FirstCommit string
	FirstAuthor string
	FirstTime   time.Time
	LastCommit  string
	LastAuthor  string
	LastTime    time.Time
}

// Cluster is a group of snippets identified as a refactoring family.
type Cluster struct {
	ID      int
	Members []string
	Score   float64 // average internal pair score across the cluster's members

	// MinScore is the cluster's cohesion: the minimum internal pair
	// score over all distinct member pairs. DBSCAN links transitively,
	// so a chained family can contain endpoints that barely resemble
	// each other — a low MinScore relative to Score is the tell. Zero
	// when never computed (e.g. clusters built outside main.go).
	MinScore float64

	// TestOnly marks clusters whose every member is a test snippet.
	// Such clusters are suppressed by default (Options.IncludeTests
	// restores them); mixed test/production clusters always render.
	TestOnly bool

	// MemberRepos carries the repo label of each member, parallel to
	// Members, in a multi-root scan. Nil on single-root scans, so all
	// repo-aware rendering and JSON fields switch off and the output
	// stays byte-identical to the pre-multi-repo format.
	MemberRepos []string
}

// RepoSpan returns the distinct non-empty repo labels among the
// cluster's members, in first-appearance order. Empty when MemberRepos
// was never populated (single-root scan) or doesn't parallel Members.
// A span of two or more repos marks the cluster as cross-repo — the
// "promote to a shared library" candidates a multi-root scan exists to
// surface.
func (c Cluster) RepoSpan() []string {
	if len(c.MemberRepos) != len(c.Members) {
		return nil
	}
	var span []string
	seen := make(map[string]bool, len(c.MemberRepos))
	for _, r := range c.MemberRepos {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		span = append(span, r)
	}
	return span
}

// CrossRepo reports whether the cluster's members span at least two
// distinct repos.
func (c Cluster) CrossRepo() bool { return len(c.RepoSpan()) >= 2 }

// Suppressed counts findings dropped by the default test-code
// segregation: test↔test pairs, clusters whose members are all test
// snippets, and test↔test partial clones. Counted after threshold
// filtering, so the numbers describe findings that would otherwise
// have rendered.
type Suppressed struct {
	TestTestPairs    int
	TestOnlyClusters int
	TestTestBlocks   int
}

// BlockClone is one sub-function partial-clone finding (review §5.3):
// a shared block of code detected inside two functions whose overall
// pair score sat below the report threshold. Unlike a Pair it carries
// real line ranges — the block, not the enclosing chunk — and its
// quality bar is Containment (the fraction of the smaller side's block
// tokens exactly matched on the other side), not the combined score,
// so Options.Threshold never filters it.
type BlockClone struct {
	// ID is a stable, order-invariant 8-char digest of the two range
	// names ("file:start-end"), following the Pair ID convention.
	ID string

	FileA                string // display path of side A's file (as scanned)
	SymbolA              string // enclosing chunk's symbol, may be empty
	AStartLine, AEndLine int    // 1-based line range of the block in file A

	FileB                string
	SymbolB              string
	BStartLine, BEndLine int

	Containment    float64
	LinesA, LinesB int // non-blank lines of the block on each side

	IsTestA, IsTestB bool

	// RepoA / RepoB are the endpoints' repo labels in a multi-root
	// scan; empty otherwise.
	RepoA, RepoB string

	// PathA / PathB are the absolute paths of the two files, carried
	// for --since diff filtering. Never rendered.
	PathA, PathB string

	// ChunkA / ChunkB are the enclosing snippets' names (the
	// splitter's "path:start-end Symbol" form), carried so downstream
	// consumers (--suggest, --preview) can resolve each side back to
	// its host snippet and slice the block's code out of it. Never
	// rendered or serialized.
	ChunkA, ChunkB string
}

// RangeNameA returns side A's "file:start-end" range name, the unit
// the BlockClone ID is derived from.
func (b BlockClone) RangeNameA() string {
	return fmt.Sprintf("%s:%d-%d", b.FileA, b.AStartLine, b.AEndLine)
}

// RangeNameB is RangeNameA for side B.
func (b BlockClone) RangeNameB() string {
	return fmt.Sprintf("%s:%d-%d", b.FileB, b.BStartLine, b.BEndLine)
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

// BuildBlockPreview returns a Preview of a block clone's exact line
// range, sliced out of the enclosing chunk's code. Unlike
// BuildMatchPreview there is no whole-chunk fallback: block matches
// know their precise bounds, so the preview is always the block
// itself, truncated to maxLines when positive (maxLines <= 0 shows
// the whole block). blockStart/blockEnd are 1-based absolute source
// lines (the BlockClone convention); chunkStartLine anchors them into
// code.
func BuildBlockPreview(code string, chunkStartLine, blockStart, blockEnd, maxLines int) Preview {
	chunkLines := strings.Split(strings.TrimSuffix(code, "\n"), "\n")
	first := blockStart - chunkStartLine
	last := blockEnd - chunkStartLine
	if first < 0 {
		first = 0
	}
	if last > len(chunkLines)-1 {
		last = len(chunkLines) - 1
	}
	if last < first {
		last = first
	}
	selected := chunkLines[first : last+1]
	if maxLines > 0 && len(selected) > maxLines {
		selected = selected[:maxLines]
	}
	return Preview{
		StartLine: chunkStartLine + first,
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
	SortAge      SortMode = "age"       // newest pair first (max introduction date desc)
	SortAgeAsc   SortMode = "age-asc"   // oldest pair first (max introduction date asc)
)

// Options controls rendering behaviour.
type Options struct {
	Plain         bool     // disable ANSI color codes (for CI / file output)
	Threshold     float64  // hide pairs below this score (unless Verbose)
	Verbose       bool     // include pairs below threshold
	Sort          SortMode // ordering for pairs and clusters; "" = SortScore
	Limit         int      // cap pairs and clusters at N items each (0 = no limit)
	CrossLangOnly bool     // keep only pairs whose two snippets have different, known languages

	// CrossRepoOnly keeps only findings whose endpoints live in
	// different repos of a multi-root scan: pairs and partial clones
	// with two distinct non-empty repo labels, clusters spanning at
	// least two repos. Composes with CrossLangOnly (both filters
	// apply). Meaningless on single-root scans, where no snippet has a
	// repo label — everything would be filtered out.
	CrossRepoOnly bool

	// IncludeTests keeps test↔test pairs and test-only clusters in the
	// report. By default (false) they are suppressed and replaced by a
	// one-line summary — test scaffolding is forced into a common shape
	// by the API under test, so test↔test token-clones are rarely
	// actionable. test↔production pairs and mixed clusters always render.
	IncludeTests bool

	// Suppressed carries counts from an upstream Prepare call. Callers
	// that Prepare before Render (to build previews, say) should copy
	// the returned counts here so Render's summary can report them —
	// Render's own internal Prepare sees already-filtered data and
	// would otherwise count zero.
	Suppressed Suppressed

	// Flat lists every pair individually, the pre-cluster-first
	// behaviour. By default the terminal report is cluster-first: a
	// clone family of n members implies n·(n-1)/2 pairs, so families
	// render once as clusters and their internal pairs are collapsed
	// out of the pairs section. JSON output is always flat.
	Flat bool

	// Previews, when non-nil, maps a snippet name to a code excerpt with its
	// originating start line. Entries with empty Text are skipped.
	Previews map[string]Preview

	// PartialClones are the block-level findings to render in the
	// PARTIAL CLONES section, already prepared via PrepareBlocks
	// (test-suppressed, sorted, limited). Options.Threshold does not
	// apply to them — containment is their quality bar.
	PartialClones []BlockClone
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

// isCrossLang reports whether a pair of language labels is a confirmed
// cross-language match. Pairs involving an unclassified snippet — Lang
// "" or the tokenizer's Unknown sentinel string — are NOT cross-language:
// we can't confirm the languages differ, and two unclassifiable files are
// more likely the SAME language (BuildMatrix blends Unknown↔Unknown as
// same-language for the same reason). The string literal mirrors
// tokenizer.Unknown; report stays free of internal imports.
func isCrossLang(langA, langB string) bool {
	const unknown = "unknown" // == string(tokenizer.Unknown)
	if langA == "" || langB == "" || langA == unknown || langB == unknown {
		return false
	}
	return langA != langB
}

// Prepare applies the report pipeline to raw pairs+clusters: filter by
// Options.Threshold (unless Verbose), suppress test↔test pairs and
// test-only clusters (unless Options.IncludeTests), then sort by
// Options.Sort, then cap each section to Options.Limit.
//
// Order matters for performance on big repos. Filtering before sorting
// drops millions of below-threshold pairs before they pay the n-log-n
// sort cost. When Options.Limit is small, a top-K heap walk replaces the
// full sort entirely — turning 20s of sorting on 11M pairs into a single
// O(n log k) pass.
//
// Test suppression runs after the threshold/cross-lang checks so the
// returned Suppressed counts describe only findings that would have
// rendered, and before the limit so --limit applies to what remains.
//
// Both Render and JSON consumers call Prepare so the two output formats
// always reflect the same set of findings.
func Prepare(pairs []Pair, clusters []Cluster, opts Options) ([]Pair, []Cluster, Suppressed) {
	var sup Suppressed
	visiblePairs := pairs
	if !opts.Verbose || opts.CrossLangOnly || opts.CrossRepoOnly || !opts.IncludeTests {
		visiblePairs = make([]Pair, 0, len(pairs))
		for _, p := range pairs {
			if !opts.Verbose && p.Score < opts.Threshold {
				continue
			}
			if opts.CrossLangOnly && !isCrossLang(p.LangA, p.LangB) {
				continue
			}
			if opts.CrossRepoOnly && (p.RepoA == "" || p.RepoB == "" || p.RepoA == p.RepoB) {
				continue
			}
			if !opts.IncludeTests && p.IsTestA && p.IsTestB {
				sup.TestTestPairs++
				continue
			}
			visiblePairs = append(visiblePairs, p)
		}
	}

	visibleClusters := clusters
	if !opts.IncludeTests || opts.CrossRepoOnly {
		visibleClusters = make([]Cluster, 0, len(clusters))
		for _, c := range clusters {
			// Cross-repo check runs before the test check (mirroring the
			// pair loop) so the suppressed counts describe only findings
			// that would otherwise have rendered.
			if opts.CrossRepoOnly && !c.CrossRepo() {
				continue
			}
			if !opts.IncludeTests && c.TestOnly {
				sup.TestOnlyClusters++
				continue
			}
			visibleClusters = append(visibleClusters, c)
		}
	}

	visiblePairs = sortAndLimit(visiblePairs, pairLessFunc(opts.Sort), opts.Limit)
	visibleClusters = sortAndLimit(visibleClusters, clusterLessFunc(opts.Sort), opts.Limit)
	return visiblePairs, visibleClusters, sup
}

// PrepareBlocks applies the report pipeline to block-clone findings:
// keep only cross-repo blocks when Options.CrossRepoOnly is set,
// suppress test↔test blocks (unless Options.IncludeTests), order by
// containment (descending; ties by size then range names so output is
// deterministic), then cap at Options.Limit. Options.Threshold is
// deliberately NOT applied — a block finding's quality bar is its
// containment, which the detector already enforced. Returns the
// visible blocks and the count suppressed by test segregation.
func PrepareBlocks(blocks []BlockClone, opts Options) ([]BlockClone, int) {
	suppressed := 0
	visible := make([]BlockClone, 0, len(blocks))
	for _, b := range blocks {
		if opts.CrossRepoOnly && (b.RepoA == "" || b.RepoB == "" || b.RepoA == b.RepoB) {
			continue
		}
		if !opts.IncludeTests && b.IsTestA && b.IsTestB {
			suppressed++
			continue
		}
		visible = append(visible, b)
	}
	visible = sortAndLimit(visible, BlockLess, opts.Limit)
	return visible, suppressed
}

// BlockLess orders block clones best-first: higher containment, then
// bigger block (min-side non-blank lines), then range names. Exported
// so the CLI's pre-dedup ordering (sortBlockClones) and PrepareBlocks
// share one definition of "best".
func BlockLess(a, b BlockClone) bool {
	if a.Containment != b.Containment {
		return a.Containment > b.Containment
	}
	am, bm := minInt(a.LinesA, a.LinesB), minInt(b.LinesA, b.LinesB)
	if am != bm {
		return am > bm
	}
	if ra, rb := a.RangeNameA(), b.RangeNameA(); ra != rb {
		return ra < rb
	}
	return a.RangeNameB() < b.RangeNameB()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
//
// The default layout is cluster-first: clone families render as clusters
// at the top, and pairs whose two endpoints belong to the same cluster
// are collapsed out of the pairs section — a family of n members implies
// n·(n-1)/2 pairs, and listing them individually buries everything else.
// Options.Flat restores the flat everything-is-a-pair listing.
func Render(w io.Writer, pairs []Pair, clusters []Cluster, opts Options) {
	visiblePairs, visibleClusters, sup := Prepare(pairs, clusters, opts)
	// Callers that prepared upstream pass their counts via opts; our own
	// Prepare call re-counts anything still present, so the sum is right
	// whether the input was raw or already prepared.
	sup.TestTestPairs += opts.Suppressed.TestTestPairs
	sup.TestOnlyClusters += opts.Suppressed.TestOnlyClusters
	sup.TestTestBlocks += opts.Suppressed.TestTestBlocks

	printHeader(w, opts)

	if len(visiblePairs) == 0 && len(visibleClusters) == 0 && len(opts.PartialClones) == 0 {
		fmt.Fprintf(w, "\n%s  No similarities found above threshold %.0f%%%s\n",
			color(green, opts), opts.Threshold*100, color(reset, opts))
		printSuppressed(w, sup, opts)
		fmt.Fprintln(w)
		return
	}

	if opts.Flat {
		printPairs(w, visiblePairs, opts)
		printClusters(w, visibleClusters, opts)
		printPartialClones(w, opts.PartialClones, opts)
		printSummary(w, visiblePairs, visiblePairs, visibleClusters, 0, 0, sup, opts)
		return
	}

	shownPairs, collapsed, relations := SplitPairsByCluster(visiblePairs, visibleClusters)
	printClusters(w, visibleClusters, opts)
	printRelations(w, relations, opts)
	if len(shownPairs) > 0 {
		printPairs(w, shownPairs, opts)
	}
	printPartialClones(w, opts.PartialClones, opts)
	crossCollapsed := 0
	for _, r := range relations {
		crossCollapsed += r.Count
	}
	printSummary(w, shownPairs, visiblePairs, visibleClusters, collapsed, crossCollapsed, sup, opts)
}

// ClusterRelation aggregates the pairs whose endpoints sit in two
// different clusters. A pair of families with n and m members can
// produce up to n·m such pairs; one relation line represents them all.
type ClusterRelation struct {
	A, B  int // Cluster.ID of the two families, A < B
	Count int // number of pairs between them
	Max   float64
}

// SplitPairsByCluster partitions pairs into three groups: pairs fully
// represented by one cluster (both endpoints in the same cluster —
// dropped, counted in collapsed), pairs bridging two clusters
// (aggregated per cluster pair into relations, sorted by Max
// descending), and pairs with at least one unclustered endpoint
// (returned in outside — the only pairs still worth listing one by
// one, since a clustered endpoint is already visible in its family).
func SplitPairsByCluster(pairs []Pair, clusters []Cluster) (outside []Pair, collapsed int, relations []ClusterRelation) {
	memberOf := make(map[string]int) // name → index into clusters
	for i, c := range clusters {
		for _, m := range c.Members {
			memberOf[m] = i
		}
	}
	type key struct{ a, b int }
	rel := make(map[key]*ClusterRelation)
	outside = make([]Pair, 0, len(pairs))
	for _, p := range pairs {
		ia, okA := memberOf[p.NameA]
		ib, okB := memberOf[p.NameB]
		switch {
		case okA && okB && ia == ib:
			collapsed++
		case okA && okB:
			if ia > ib {
				ia, ib = ib, ia
			}
			k := key{ia, ib}
			r, ok := rel[k]
			if !ok {
				// Clusters arrive sorted by score, not ID, so order the
				// displayed IDs explicitly.
				a, b := clusters[ia].ID, clusters[ib].ID
				if a > b {
					a, b = b, a
				}
				r = &ClusterRelation{A: a, B: b}
				rel[k] = r
			}
			r.Count++
			if p.Score > r.Max {
				r.Max = p.Score
			}
		default:
			outside = append(outside, p)
		}
	}
	relations = make([]ClusterRelation, 0, len(rel))
	for _, r := range rel {
		relations = append(relations, *r)
	}
	sort.Slice(relations, func(i, j int) bool {
		if relations[i].Max != relations[j].Max {
			return relations[i].Max > relations[j].Max
		}
		if relations[i].A != relations[j].A {
			return relations[i].A < relations[j].A
		}
		return relations[i].B < relations[j].B
	})
	return outside, collapsed, relations
}

// printSectionTitle emits the bold-white banner line every findings
// section shares (SIMILARITY PAIRS, RELATED CLUSTERS, PARTIAL CLONES,
// REFACTORING CLUSTERS).
func printSectionTitle(w io.Writer, title string, opts Options) {
	fmt.Fprintf(w, "%s%s %s%s\n\n",
		color(bold, opts), color(white, opts), title, color(reset, opts))
}

// printRelations renders one line per pair of related clusters.
func printRelations(w io.Writer, relations []ClusterRelation, opts Options) {
	if len(relations) == 0 {
		return
	}
	printSectionTitle(w, "RELATED CLUSTERS", opts)
	for _, r := range relations {
		_, clr := classify(r.Max)
		fmt.Fprintf(w, "  %sCluster %d ↔ Cluster %d%s — %d pairs, %sup to %3.0f%%%s\n",
			color(green, opts), r.A+1, r.B+1, color(reset, opts), r.Count,
			color(clr, opts), r.Max*100, color(reset, opts))
	}
	fmt.Fprintln(w)
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

// cmpPairAgeDesc orders pairs by their introduction date — the newer
// of the two endpoints' FirstTime — descending. A pair is "introduced"
// when its second endpoint lands; the older endpoint is the original.
// Pairs with no provenance sort to the end (treated as zero-time).
func cmpPairAgeDesc(a, b Pair) int { return cmp.Compare(pairIntro(b).Unix(), pairIntro(a).Unix()) }
func cmpPairAgeAsc(a, b Pair) int  { return cmp.Compare(pairIntro(a).Unix(), pairIntro(b).Unix()) }

func pairIntro(p Pair) time.Time {
	var t time.Time
	if p.ProvenanceA != nil && p.ProvenanceA.FirstTime.After(t) {
		t = p.ProvenanceA.FirstTime
	}
	if p.ProvenanceB != nil && p.ProvenanceB.FirstTime.After(t) {
		t = p.ProvenanceB.FirstTime
	}
	return t
}

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
	case SortAge:
		return lessChain(cmpPairAgeDesc, cmpPairScoreDesc)
	case SortAgeAsc:
		return lessChain(cmpPairAgeAsc, cmpPairScoreDesc)
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
	printSectionTitle(w, "SIMILARITY PAIRS", opts)

	for _, p := range pairs {
		label, clr := classifyPair(p)

		fmt.Fprintf(w, "  %s%s%s  %s%3.0f%%%s\n",
			color(clr, opts), color(bold, opts), label,
			color(clr, opts), p.Score*100, color(reset, opts))

		fmt.Fprintf(w, "  %s  %s%s%s\n",
			color(grey, opts),
			color(cyan, opts), p.NameA,
			color(reset, opts))
		printProvenance(w, p.ProvenanceA, opts)
		printPreview(w, p.NameA, opts)

		fmt.Fprintf(w, "  %s  %s%s%s\n",
			color(grey, opts),
			color(cyan, opts), p.NameB,
			color(reset, opts))
		printProvenance(w, p.ProvenanceB, opts)
		printPreview(w, p.NameB, opts)

		// The lexical sub-score renders only when it was computed
		// (pairs above the near-clone band), so lower-band pair lines
		// are byte-identical to the pre-lexical format.
		lexical := ""
		if p.LexicalComputed {
			lexical = fmt.Sprintf("  lexical: %3.0f%%", p.Lexical*100)
		}
		fmt.Fprintf(w, "  %sstructural: %3.0f%%  semantic: %3.0f%%%s%s\n\n",
			color(grey, opts),
			p.Structural*100, p.Semantic*100, lexical,
			color(reset, opts))
	}
}

// printProvenance emits a one-line "origin" summary under a snippet's
// name: when it was first introduced, by whom, and (when distinct) when
// it was last touched. No-op when prov is nil so callers can hand it
// the raw Pair field unconditionally.
func printProvenance(w io.Writer, prov *Provenance, opts Options) {
	if prov == nil || prov.FirstCommit == "" {
		return
	}
	first := prov.FirstTime.Format("2006-01-02")
	short := shortSHA(prov.FirstCommit)
	if prov.LastCommit != "" && prov.LastCommit != prov.FirstCommit {
		fmt.Fprintf(w, "      %sintroduced %s by %s (%s); last touched %s by %s (%s)%s\n",
			color(grey, opts),
			first, prov.FirstAuthor, short,
			prov.LastTime.Format("2006-01-02"), prov.LastAuthor, shortSHA(prov.LastCommit),
			color(reset, opts))
		return
	}
	fmt.Fprintf(w, "      %sintroduced %s by %s (%s)%s\n",
		color(grey, opts),
		first, prov.FirstAuthor, short, color(reset, opts))
}

func shortSHA(s string) string {
	if len(s) <= 7 {
		return s
	}
	return s[:7]
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

// printPartialClones renders the block-level findings section. Each
// finding shows the containment score, the block's line ranges in both
// files, and (when known) the enclosing function of each side. With
// --preview on, each side also gets a line-numbered excerpt of its
// matched block range — Options.Previews is keyed by the side's
// "file:start-end" range name (range-qualified so a block preview
// never collides with the whole-chunk preview of the same snippet).
func printPartialClones(w io.Writer, blocks []BlockClone, opts Options) {
	if len(blocks) == 0 {
		return
	}
	printSectionTitle(w, "PARTIAL CLONES", opts)
	for _, b := range blocks {
		fmt.Fprintf(w, "  %s%s[PARTIAL CLONE   ]%s  %s%3.0f%% contained%s · %d lines\n",
			color(orange, opts), color(bold, opts), color(reset, opts),
			color(orange, opts), b.Containment*100, color(reset, opts),
			minInt(b.LinesA, b.LinesB))
		printBlockSide(w, b.RangeNameA(), b.SymbolA, opts)
		printPreview(w, b.RangeNameA(), opts)
		printBlockSide(w, b.RangeNameB(), b.SymbolB, opts)
		printPreview(w, b.RangeNameB(), opts)
		fmt.Fprintln(w)
	}
}

// printBlockSide renders one side of a partial clone as
// "file:start-end ⊂ EnclosingFunc" (the ⊂ suffix only when the
// enclosing chunk has a symbol).
func printBlockSide(w io.Writer, rangeName, symbol string, opts Options) {
	container := ""
	if symbol != "" {
		container = fmt.Sprintf(" %s⊂ %s%s", color(grey, opts), symbol, color(reset, opts))
	}
	fmt.Fprintf(w, "  %s  %s%s%s%s\n",
		color(grey, opts), color(cyan, opts), rangeName, color(reset, opts), container)
}

func printClusters(w io.Writer, clusters []Cluster, opts Options) {
	if len(clusters) == 0 {
		return
	}

	printSectionTitle(w, "REFACTORING CLUSTERS", opts)

	for _, c := range clusters {
		// A cluster spanning two or more repos of a multi-root scan is a
		// "promote to a shared library" candidate — tag it in the header
		// and group its members per repo below.
		crossRepo := c.CrossRepo()
		tag := ""
		if crossRepo {
			tag = fmt.Sprintf(" · %scross-repo%s", color(purple, opts), color(reset, opts))
		}
		if c.Score > 0 {
			_, clr := classify(c.Score)
			// Cohesion (the weakest internal pair) renders alongside the
			// average when it was computed — the gap between the two is
			// how you spot a transitively chained family.
			cohesion := ""
			if c.MinScore > 0 {
				_, minClr := classify(c.MinScore)
				cohesion = fmt.Sprintf(" · %scohesion %3.0f%%%s",
					color(minClr, opts), c.MinScore*100, color(reset, opts))
			}
			fmt.Fprintf(w, "  %sCluster %d%s — %d snippets · %savg similarity %3.0f%%%s%s%s\n",
				color(green, opts), c.ID+1, color(reset, opts), len(c.Members),
				color(clr, opts), c.Score*100, color(reset, opts), cohesion, tag)
		} else {
			fmt.Fprintf(w, "  %sCluster %d%s — %d snippets%s\n",
				color(green, opts), c.ID+1, color(reset, opts), len(c.Members), tag)
		}
		if crossRepo {
			printClusterMembersByRepo(w, c, opts)
		} else {
			for _, m := range c.Members {
				fmt.Fprintf(w, "    %s·%s %s\n", color(grey, opts), color(reset, opts), m)
				printPreview(w, m, opts)
			}
		}
		fmt.Fprintln(w)
	}
}

// printClusterMembersByRepo renders a cross-repo cluster's members
// grouped under one "repo — N snippets" line per repo, in
// first-appearance order, so "this family spans svc-a and svc-b" reads
// at a glance. Only called for clusters whose RepoSpan has two or more
// repos; members with an empty repo label (direct file arguments in a
// mixed invocation) group under the last position with an empty label.
func printClusterMembersByRepo(w io.Writer, c Cluster, opts Options) {
	order := make([]string, 0, 4)
	byRepo := make(map[string][]string, 4)
	for i, m := range c.Members {
		r := c.MemberRepos[i]
		if _, ok := byRepo[r]; !ok {
			order = append(order, r)
		}
		byRepo[r] = append(byRepo[r], m)
	}
	for _, r := range order {
		members := byRepo[r]
		label := r
		if label == "" {
			label = "(no repo)"
		}
		fmt.Fprintf(w, "    %s%s%s %s— %d %s%s\n",
			color(purple, opts), label, color(reset, opts),
			color(grey, opts), len(members), plural(len(members), "snippet", "snippets"),
			color(reset, opts))
		for _, m := range members {
			fmt.Fprintf(w, "      %s·%s %s\n", color(grey, opts), color(reset, opts), m)
			printPreview(w, m, opts)
		}
	}
}

// printSummary reports the scan outcome. shown is what the pairs
// section listed; allVisible is every pair above the threshold,
// including those collapsed into clusters — the tier buckets classify
// allVisible so "Exact clones" describes the scan, not just the
// standalone leftovers (a repo whose exact clones all live inside
// clusters would otherwise report "Exact clones 0").
func printSummary(w io.Writer, shown, allVisible []Pair, clusters []Cluster, collapsed, crossCollapsed int, sup Suppressed, opts Options) {
	exact, near, twins, strong, candidates, weak := 0, 0, 0, 0, 0, 0
	for _, p := range allVisible {
		// Bucket by the same gated classification the pair labels use,
		// so "Exact clones N" never counts a pair that rendered as a
		// near clone under the short-snippet evidence gate or as a
		// structural twin under the lexical gate.
		switch tierForPair(p).json {
		case "exact_clone":
			exact++
		case "near_clone":
			near++
		case "structural_twin":
			twins++
		case "strong_clone":
			strong++
		case "refactor_candidate":
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
		color(grey, opts), color(reset, opts), color(cyan, opts), len(shown), color(reset, opts))
	if collapsed > 0 {
		fmt.Fprintf(w, "  %sIn-cluster pairs%s  %s%d%s %s(inside the clusters above; --flat lists them)%s\n",
			color(grey, opts), color(reset, opts), color(cyan, opts), collapsed, color(reset, opts),
			color(grey, opts), color(reset, opts))
	}
	if crossCollapsed > 0 {
		fmt.Fprintf(w, "  %sCross-cluster%s     %s%d%s %s(aggregated under RELATED CLUSTERS; --flat lists them)%s\n",
			color(grey, opts), color(reset, opts), color(cyan, opts), crossCollapsed, color(reset, opts),
			color(grey, opts), color(reset, opts))
	}
	fmt.Fprintf(w, "  %sExact clones%s      %s%d%s\n",
		color(grey, opts), color(reset, opts), color(red, opts), exact, color(reset, opts))
	fmt.Fprintf(w, "  %sNear clones%s       %s%d%s\n",
		color(grey, opts), color(reset, opts), color(red, opts), near, color(reset, opts))
	if twins > 0 {
		fmt.Fprintf(w, "  %sStructural twins%s  %s%d%s %s(same shape, different content)%s\n",
			color(grey, opts), color(reset, opts), color(purple, opts), twins, color(reset, opts),
			color(grey, opts), color(reset, opts))
	}
	fmt.Fprintf(w, "  %sStrong clones%s     %s%d%s\n",
		color(grey, opts), color(reset, opts), color(orange, opts), strong, color(reset, opts))
	fmt.Fprintf(w, "  %sRefactor targets%s  %s%d%s\n",
		color(grey, opts), color(reset, opts), color(yellow, opts), candidates, color(reset, opts))
	if weak > 0 {
		fmt.Fprintf(w, "  %sWeak similarities%s %s%d%s\n",
			color(grey, opts), color(reset, opts), color(grey, opts), weak, color(reset, opts))
	}
	fmt.Fprintf(w, "  %sClusters found%s    %s%d%s\n",
		color(grey, opts), color(reset, opts), color(green, opts), len(clusters), color(reset, opts))
	if len(opts.PartialClones) > 0 {
		fmt.Fprintf(w, "  %sPartial clones%s    %s%d%s %s(sub-function blocks; see PARTIAL CLONES)%s\n",
			color(grey, opts), color(reset, opts), color(orange, opts), len(opts.PartialClones), color(reset, opts),
			color(grey, opts), color(reset, opts))
	}
	printSuppressed(w, sup, opts)
	fmt.Fprintln(w)
}

// printSuppressed emits one summary line per suppressed-finding class
// (test↔test pairs, test-only clusters). No-op when nothing was
// suppressed, so reports without test code look exactly as before.
func printSuppressed(w io.Writer, sup Suppressed, opts Options) {
	if sup.TestTestPairs > 0 {
		fmt.Fprintf(w, "  %s%s test↔test %s suppressed (--include-tests to show)%s\n",
			color(grey, opts), groupDigits(sup.TestTestPairs),
			plural(sup.TestTestPairs, "pair", "pairs"), color(reset, opts))
	}
	if sup.TestOnlyClusters > 0 {
		fmt.Fprintf(w, "  %s%s test-only %s suppressed (--include-tests to show)%s\n",
			color(grey, opts), groupDigits(sup.TestOnlyClusters),
			plural(sup.TestOnlyClusters, "cluster", "clusters"), color(reset, opts))
	}
	if sup.TestTestBlocks > 0 {
		fmt.Fprintf(w, "  %s%s test↔test partial %s suppressed (--include-tests to show)%s\n",
			color(grey, opts), groupDigits(sup.TestTestBlocks),
			plural(sup.TestTestBlocks, "clone", "clones"), color(reset, opts))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// groupDigits renders n with comma thousands separators ("1819" → "1,819").
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var sb strings.Builder
	if neg {
		sb.WriteByte('-')
	}
	pre := len(s) % 3
	if pre == 0 {
		pre = 3
	}
	sb.WriteString(s[:pre])
	for i := pre; i < len(s); i += 3 {
		sb.WriteByte(',')
		sb.WriteString(s[i : i+3])
	}
	return sb.String()
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

// structuralTwinTier is a band MODIFIER, not a score band: pairs in the
// exact/near bands (score > StructuralTwinMinScore) whose lexical
// overlap falls below StructuralTwinMaxLexical render with this tier
// instead. Same shape, different content — likely parallel boilerplate
// (table tests, per-field validators, generated handlers) rather than
// copy-paste. It never appears via tierFor (a plain score lookup);
// only tierForPair can select it.
var structuralTwinTier = tier{-1, "[STRUCTURAL TWIN ]", purple, "structural_twin"}

// StructuralTwinMinScore is the combined-score floor above which the
// lexical sub-score is consulted at all: only the exact/near bands
// (> 0.85) claim "this was copied", so only they need the content
// check. Pairs at or below this score are never modified.
const StructuralTwinMinScore = 0.85

// StructuralTwinMaxLexical is the lexical floor for the top bands: a
// pair above StructuralTwinMinScore whose lexical Jaccard is below
// this renders as STRUCTURAL TWIN. Tuned against internal/bench: the
// twins fixtures (disjoint vocabulary by construction) measure
// 0.00–0.07, while the renamed positives — systematic renames that
// keep the vocabulary a real rename keeps (helper calls, field names,
// string literals) — measure 0.29–0.60. 0.20 splits the two
// populations with margin on both sides; rename-invariance
// (go-renamed, python-renamed scoring 1.0 AND labeling exact) is
// pinned by TestBench_GroundTruth.
const StructuralTwinMaxLexical = 0.20

// ExactCloneMinLines is the evidence floor for the top report band: a
// pair whose smaller snippet has fewer than this many non-blank lines
// never renders as an exact clone, regardless of score. Short snippets
// can hit a perfect score by sharing nothing but API-forced shape
// (test scaffolding, tiny wrappers), so a "100% exact clone" label on a
// 4-line pair overstates the evidence. Such pairs demote one band and
// render as near clones — only the label moves; the numeric score is
// untouched.
const ExactCloneMinLines = 10

func tierFor(score float64) tier {
	for _, t := range tiers {
		if score > t.above {
			return t
		}
	}
	return tiers[len(tiers)-1]
}

// tierForPair classifies a pair by score, then applies the two
// evidence gates that can move a top-band label. Precedence: the
// content check (structural twin) runs BEFORE the length gate, because
// it makes the stronger, more specific claim — a content-divergent
// pair isn't a slightly-less-certain copy-paste finding, it's a
// different kind of finding entirely (parallel boilerplate), whereas
// the length gate merely tempers the confidence of a copy-paste claim.
// A short, content-divergent pair is therefore a STRUCTURAL TWIN, not
// a "near clone (short)".
//
// The length gate: the exact-clone band additionally requires
// min(lines) >= ExactCloneMinLines; shorter pairs demote one band.
// Pairs with unknown line counts (0) fail the gate too — no evidence,
// no top band. Neither gate ever changes the numeric score.
func tierForPair(p Pair) tier {
	t := tierFor(p.Score)
	if p.Score > StructuralTwinMinScore && p.LexicalComputed &&
		p.Lexical < StructuralTwinMaxLexical {
		return structuralTwinTier
	}
	if t.json == tiers[0].json && minPairLines(p) < ExactCloneMinLines {
		return tiers[1]
	}
	return t
}

func minPairLines(p Pair) int {
	if p.LinesA < p.LinesB {
		return p.LinesA
	}
	return p.LinesB
}

// classify is the score-only band lookup, used where no per-pair line
// evidence exists (cluster scores, cluster relations). Pair labels go
// through classifyPair so the exact-clone gate applies.
func classify(score float64) (string, string) {
	t := tierFor(score)
	return t.label, t.color
}

func classifyPair(p Pair) (string, string) {
	t := tierForPair(p)
	return t.label, t.color
}

// JSONLabel returns the snake-case classification name used in JSON
// output for a pair. It applies the same exact-clone evidence gate as
// the terminal label, so the two surfaces always agree.
func JSONLabel(p Pair) string { return tierForPair(p).json }

func color(code string, opts Options) string {
	if opts.Plain {
		return ""
	}
	return code
}
