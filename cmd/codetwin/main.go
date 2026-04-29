// codetwin — multi-language code similarity detector
//
// Usage:
//
//	codetwin [flags] <path> [<path>...]
//
// Examples:
//
//	codetwin ./src                                 # scan a directory recursively
//	codetwin ./src/a.go ./src/b.go                 # compare specific files
//	codetwin --threshold 0.6 ./pkg                 # only show pairs >= 60%
//	codetwin --plain ./src > report.txt            # CI-friendly plain text output
//	codetwin --json ./src > report.json            # machine-readable JSON output
//	codetwin --preview ./src                       # show code snippet previews
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/cluster"
	"github.com/ccsrvs/codetwin/internal/config"
	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/pathutil"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/similarity"
)

// skillBody is the full skill guide printed by --skill. It mirrors the
// repo-root codetwin-SKILL.md but lives next to the binary so the loader-
// visible markdown can stay short and let agents fetch detail on demand.
//
//go:embed skill.md
var skillBody string

// guideBody is the interpretation guide printed by --guide. Distinct from
// --skill: this is for humans reading the report (what scores mean, how
// clusters differ from pairs), not agents running the tool.
//
//go:embed guide.md
var guideBody string

var supportedExts = map[string]bool{
	".go": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".py": true, ".java": true, ".rs": true, ".ex": true, ".exs": true,
}

func main() {
	threshold := flag.Float64("threshold", 0.30, "minimum similarity score to report (0.0–1.0)")
	plain := flag.Bool("plain", false, "plain text output (no ANSI colors, suitable for CI)")
	jsonOut := flag.Bool("json", false, "output results as JSON")
	verbose := flag.Bool("verbose", false, "show all pairs including weak similarities")
	minLines := flag.Int("min-lines", 3, "skip files with fewer than N non-blank lines")
	eps := flag.Float64("eps", 0.45, "DBSCAN epsilon: max distance for two snippets to be neighbours")
	minPts := flag.Int("min-pts", 2, "DBSCAN minPts: minimum cluster size")
	preview := flag.Bool("preview", false, "show a short code excerpt for each finding")
	previewLines := flag.Int("preview-lines", 10, "max lines per preview; 0 = show whole snippet")
	sortMode := flag.String("sort", "score", "result ordering: score | score-asc | size | size-asc | name")
	limit := flag.Int("limit", 0, "show only the top N pairs and N clusters (0 = no limit)")
	minConfLines := flag.Int("min-confidence-lines", 0, "dampen pair scores when min(LinesA, LinesB) < N (0 = off); ramps from 0.5× at 0 lines to 1.0× at N")
	noProgress := flag.Bool("no-progress", false, "suppress progress output on stderr")
	noCache := flag.Bool("no-cache", false, "do not read or write .codetwin-cache.bin")
	rebuildCache := flag.Bool("rebuild-cache", false, "ignore any existing cache and rebuild it from scratch")
	debug := flag.Bool("debug", false, "print phase checkpoints with elapsed time to stderr")
	skill := flag.Bool("skill", false, "print the codetwin skill guide and exit")
	guide := flag.Bool("guide", false, "print the report interpretation guide and exit")
	flag.Usage = usage
	flag.Parse()

	if *skill {
		fmt.Print(skillBody)
		return
	}
	if *guide {
		fmt.Print(guideBody)
		return
	}

	isTTY := stderrIsTTY()
	startTime := time.Now()
	debugf := func(format string, args ...any) {
		if !*debug {
			return
		}
		elapsed := time.Since(startTime).Round(time.Millisecond)
		// Only clear the active \r-overwrite progress line when stderr
		// is a real terminal — otherwise the escape characters show up
		// as garbage in pipes / log captures.
		if isTTY {
			fmt.Fprint(os.Stderr, "\r\033[K")
		}
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintf(os.Stderr, "[debug %v] %s\n", elapsed, msg)
	}

	// Optional .codetwin.json in CWD: provides flag default overrides plus
	// ignore_paths / ignore_patterns. Missing file is fine. Apply defaults
	// only to flags the user did NOT pass on the CLI so explicit args win.
	cfg, err := config.Load(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}
	if cfg != nil && cfg.Defaults != nil {
		applied := flagsExplicitlySet()
		applyConfigDefaults(cfg.Defaults, applied,
			threshold, plain, jsonOut, verbose, minLines, eps, minPts,
			preview, previewLines, sortMode, limit, minConfLines)
	}

	ignoreMatcher, err := compileIgnoreMatcher(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error in ignore_paths: %v\n", err)
		os.Exit(1)
	}
	stripPatterns, err := compileStripPatterns(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning in ignore_patterns: %v\n", err)
		// Continue with whatever patterns compiled successfully.
	}
	pairIgnoreMatcher, err := compilePairIgnoreMatcher(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error in ignore_pairs: %v\n", err)
		os.Exit(1)
	}

	paths := flag.Args()
	if len(paths) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	debugf("starting; loaded config=%v patternsHash=%q", cfg != nil, "")

	files, err := collectFiles(paths, ignoreMatcher)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error collecting files: %v\n", err)
		os.Exit(1)
	}
	debugf("collectFiles: %d files", len(files))

	if len(files) < 2 {
		fmt.Fprintln(os.Stderr, "error: need at least 2 source files to compare")
		os.Exit(1)
	}

	showProgress := !*noProgress && isTTY

	// Cache stores per-file tokenize+fingerprint output keyed by content
	// hash + ignore_patterns hash + tokenizer version. Hits skip the
	// expensive splitter+tokenizer+fingerprint work on unchanged files.
	var cacheState *cache.Cache
	if *noCache || *rebuildCache {
		cacheState = cache.New()
	} else {
		cacheState, err = cache.Load(".")
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: cache load failed: %v\n", err)
			cacheState = cache.New()
		}
	}
	patternsHash := cache.PatternsHash(stripPatternStrings(cfg))
	debugf("cache loaded: %d entries", len(cacheState.Entries))

	var done atomic.Int64
	totalFiles := int64(len(files))
	var progStop chan struct{}
	var progWg sync.WaitGroup
	if showProgress && totalFiles > 0 {
		progStop = make(chan struct{})
		progWg.Add(1)
		go reportProgress(&done, totalFiles, progStop, &progWg, "processing files")
	}
	snippets, fileWarnings := scan.ProcessFiles(
		files, *minLines, stripPatterns, cacheState, patternsHash,
		func() { done.Add(1) },
	)
	if progStop != nil {
		close(progStop)
		progWg.Wait()
	}
	debugf("scan.ProcessFiles: %d snippets from %d files (%d warnings)",
		len(snippets), len(files), len(fileWarnings))
	// Workers complete in nondeterministic order; sort by name so snippet
	// indices (and therefore pair construction order, cluster IDs, and any
	// equal-score tie ordering) are stable across runs.
	sort.Slice(snippets, func(i, j int) bool {
		return snippets[i].Name < snippets[j].Name
	})
	for _, w := range fileWarnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}

	if len(snippets) < 2 {
		fmt.Fprintln(os.Stderr, "error: not enough parseable snippets to compare")
		os.Exit(1)
	}

	// Static placeholder so the user has a visible indicator during the
	// silent gap between phase 1 ("processing files") and phase 2
	// ("comparing snippets"). Covers cache save + corpus build +
	// vectorize + matrix alloc + hash-index build. The matrix progress
	// bar's first \r-prefixed tick overwrites this line cleanly.
	if showProgress {
		fmt.Fprint(os.Stderr, "\rindexing snippets...")
	}

	if !*noCache {
		if err := cacheState.Save("."); err != nil {
			if showProgress {
				fmt.Fprint(os.Stderr, "\r\033[K")
			}
			fmt.Fprintf(os.Stderr, "warning: cache save failed: %v\n", err)
			if showProgress {
				fmt.Fprint(os.Stderr, "\rindexing snippets...")
			}
		}
		debugf("cache saved")
	}

	tokenStreams := make([][]string, len(snippets))
	for i, s := range snippets {
		tokenStreams[i] = s.Tokens
	}
	corpus := similarity.NewCorpus(tokenStreams)
	debugf("corpus built")

	// NormalizedVector precomputes each vector's L2 norm so the inner-loop
	// cosine is just one dot-product map-walk plus a divide.
	vectors := make([]similarity.NormalizedVector, len(snippets))
	for i, s := range snippets {
		vectors[i] = similarity.Normalize(corpus.Vectorize(s.Tokens))
	}
	debugf("vectorized %d snippets", len(vectors))

	n := len(snippets)
	matrixBytes := int64(n) * int64(n) * 8
	debugf("allocating matrix: %d × %d (%d MB)", n, n, matrixBytes/(1024*1024))

	totalPairs := int64(n) * int64(n-1) / 2
	debugf("comparing %d × %d = %d pairs", n, n, totalPairs)

	var matrixDone atomic.Int64
	var matrixProgStop chan struct{}
	var matrixProgWg sync.WaitGroup
	if showProgress && totalPairs > 0 {
		matrixProgStop = make(chan struct{})
		matrixProgWg.Add(1)
		go reportProgress(&matrixDone, totalPairs, matrixProgStop, &matrixProgWg, "comparing snippets")
	}
	matrix, pairs := similarity.BuildMatrix(
		snippets, vectors, *minConfLines,
		func(d, _ int64) { matrixDone.Store(d) },
	)
	if matrixProgStop != nil {
		close(matrixProgStop)
		matrixProgWg.Wait()
	}
	debugf("similarity.BuildMatrix: %d pairs above noise floor", len(pairs))

	if pairIgnoreMatcher != nil {
		var ignored int
		pairs, ignored = applyPairIgnores(pairs, matrix, snippets, pairIgnoreMatcher)
		debugf("ignore_pairs: dropped %d pairs", ignored)
	}

	distFn := func(i, j int) float64 { return 1.0 - matrix[i][j] }
	clusterResult := cluster.DBSCAN(n, *eps, *minPts, distFn)
	debugf("DBSCAN: %d clusters", clusterResult.NumClusters)
	groups := cluster.Groups(clusterResult)

	clusters := make([]report.Cluster, 0, len(groups))
	for id, members := range groups {
		names := make([]string, len(members))
		for k, idx := range members {
			names[k] = snippets[idx].Name
		}
		// Average internal pair score: mean of matrix[a][b] for every distinct
		// member pair. Single-member clusters (which DBSCAN won't produce, but
		// guard anyway) get a score of 0.
		var sum float64
		var nPairs int
		for k := 0; k < len(members); k++ {
			for l := k + 1; l < len(members); l++ {
				sum += matrix[members[k]][members[l]]
				nPairs++
			}
		}
		avg := 0.0
		if nPairs > 0 {
			avg = sum / float64(nPairs)
		}
		clusters = append(clusters, report.Cluster{ID: id, Members: names, Score: avg})
	}
	debugf("clusters built: %d", len(clusters))

	opts := report.Options{
		Plain:     *plain,
		Threshold: *threshold,
		Verbose:   *verbose,
		Sort:      report.SortMode(*sortMode),
		Limit:     *limit,
	}

	// Sort + threshold filter + limit ONCE here in main.go, then build
	// previews scoped to just the snippets that will actually render.
	// On a big repo this avoids an O(shown²) MatchRange storm over
	// thousands of snippets when --limit means we'll only show a handful.
	visiblePairs, visibleClusters := report.Prepare(pairs, clusters, opts)
	debugf("prepared: %d visible pairs, %d visible clusters",
		len(visiblePairs), len(visibleClusters))

	if *preview {
		if showProgress {
			fmt.Fprint(os.Stderr, "\r\033[Kbuilding previews...")
		}
		opts.Previews = buildPreviews(visiblePairs, visibleClusters, snippets, *previewLines)
		debugf("previews built: %d", len(opts.Previews))
	}

	if showProgress {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}

	if *jsonOut {
		printJSON(visiblePairs, visibleClusters, opts.Previews)
		debugf("done (json)")
		return
	}

	// Render gets already-prepared inputs. Its internal Prepare call is
	// idempotent on sorted+filtered+limited data.
	report.Render(os.Stdout, visiblePairs, visibleClusters, opts)
	debugf("done (rendered)")
}

// buildPreviews computes a Preview map for every snippet that appears in
// the supplied (already-prepared) visible pairs and clusters. Match
// ranges are derived only over the visible set, so cost is O(visible²)
// rather than O(allShown²). For each snippet, the bounding token range
// covered by fingerprints shared with any OTHER visible snippet becomes
// the preview region (subject to --preview-lines), with a leading-excerpt
// fallback when there's no structural overlap.
func buildPreviews(
	visiblePairs []report.Pair,
	visibleClusters []report.Cluster,
	snippets []scan.Snippet,
	previewLines int,
) map[string]report.Preview {
	nameIdx := make(map[string]int, len(snippets))
	for i, s := range snippets {
		nameIdx[s.Name] = i
	}

	visible := make(map[int]struct{})
	for _, p := range visiblePairs {
		if i, ok := nameIdx[p.NameA]; ok {
			visible[i] = struct{}{}
		}
		if i, ok := nameIdx[p.NameB]; ok {
			visible[i] = struct{}{}
		}
	}
	for _, c := range visibleClusters {
		for _, m := range c.Members {
			if i, ok := nameIdx[m]; ok {
				visible[i] = struct{}{}
			}
		}
	}

	type rng struct{ first, last int }
	ranges := make(map[int]rng, len(visible))
	for i := range visible {
		for j := range visible {
			if i == j {
				continue
			}
			f, l := fingerprint.MatchRange(snippets[i].Fps, snippets[j].Fps)
			if f < 0 {
				continue
			}
			r, ok := ranges[i]
			if !ok {
				ranges[i] = rng{first: f, last: l}
				continue
			}
			if f < r.first {
				r.first = f
			}
			if l > r.last {
				r.last = l
			}
			ranges[i] = r
		}
	}

	previews := make(map[string]report.Preview, len(visible))
	for i := range visible {
		s := snippets[i]
		if r, ok := ranges[i]; ok {
			previews[s.Name] = report.BuildMatchPreview(s.Code, s.Lines, s.StartLine, r.first, r.last, s.Fps.K, previewLines)
		} else {
			previews[s.Name] = report.Preview{
				StartLine: s.StartLine,
				Text:      report.ExtractPreview(s.Code, previewLines),
			}
		}
	}
	return previews
}

// ── JSON output ───────────────────────────────────────────────────────────────

type jsonOutput struct {
	Pairs    []jsonPair             `json:"pairs"`
	Clusters []jsonCluster          `json:"clusters"`
	Previews map[string]jsonPreview `json:"previews,omitempty"`
}

type jsonPair struct {
	FileA      string  `json:"file_a"`
	FileB      string  `json:"file_b"`
	Score      float64 `json:"score"`
	Structural float64 `json:"structural"`
	Semantic   float64 `json:"semantic"`
	Label      string  `json:"label"`
}

type jsonCluster struct {
	ID      int      `json:"id"`
	Members []string `json:"members"`
	Score   float64  `json:"score"`
}

type jsonPreview struct {
	StartLine int    `json:"start_line"`
	Text      string `json:"text"`
}

// printJSON emits the prepared (already sorted, threshold-filtered, limited)
// pairs and clusters as JSON. Sort and limit are applied upstream via
// report.Prepare so JSON consumers see the same set of findings as the
// terminal renderer.
func printJSON(pairs []report.Pair, clusters []report.Cluster, previews map[string]report.Preview) {
	out := jsonOutput{}
	if len(previews) > 0 {
		out.Previews = make(map[string]jsonPreview, len(previews))
		for k, v := range previews {
			out.Previews[k] = jsonPreview{StartLine: v.StartLine, Text: v.Text}
		}
	}
	for _, p := range pairs {
		out.Pairs = append(out.Pairs, jsonPair{
			FileA: p.NameA, FileB: p.NameB,
			Score: p.Score, Structural: p.Structural, Semantic: p.Semantic,
			Label: report.JSONLabel(p.Score),
		})
	}
	for _, c := range clusters {
		out.Clusters = append(out.Clusters, jsonCluster{ID: c.ID, Members: c.Members, Score: c.Score})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}

// ── File collection ───────────────────────────────────────────────────────────

func collectFiles(paths []string, ignore *config.IgnoreMatcher) ([]string, error) {
	deduped, err := pathutil.Dedupe(paths)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, p := range deduped {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				// Skip dotfile dirs (.git, .idea, etc.) but never the walk
				// root itself — passing "." as a path would otherwise be
				// rejected before any file got visited.
				if d.IsDir() && path != p && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				if ignore.Match(path, d.IsDir()) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if !d.IsDir() && supportedExts[filepath.Ext(path)] {
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else if supportedExts[filepath.Ext(p)] && !ignore.Match(p, false) {
			files = append(files, p)
		}
	}
	return files, nil
}

// stripPatternStrings extracts the raw ignore_patterns strings (not the
// compiled regexes) from cfg, for cache key derivation. Hashing the raw
// strings means a config edit that changes a pattern invalidates only the
// affected cache entries.
func stripPatternStrings(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	return cfg.IgnorePatterns
}

// reportProgress prints a one-line progress indicator to stderr until stop
// is closed. The line is cleared before returning so it doesn't bleed into
// the report output. label appears at the start of every tick (e.g.
// "processing files" or "comparing snippets").
func reportProgress(done *atomic.Int64, total int64, stop <-chan struct{}, wg *sync.WaitGroup, label string) {
	defer wg.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			fmt.Fprint(os.Stderr, "\r\033[K")
			return
		case <-ticker.C:
			d := done.Load()
			pct := float64(d) / float64(total) * 100
			fmt.Fprintf(os.Stderr, "\r%s: %d/%d (%.1f%%)", label, d, total, pct)
		}
	}
}

// stderrIsTTY reports whether stderr is connected to a terminal. Used to
// auto-suppress the progress indicator when the caller is piping output
// into a file or running in CI.
func stderrIsTTY() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// flagsExplicitlySet returns the set of flag names that were passed on the
// command line. Used to decide which flag defaults a config file may
// override (CLI args always win).
func flagsExplicitlySet() map[string]bool {
	set := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

// applyConfigDefaults overrides each flag's value with the config default,
// but only when the user did NOT explicitly pass the flag on the CLI.
// Pointers in d distinguish "not specified in config" from "set to zero".
func applyConfigDefaults(
	d *config.Defaults,
	explicit map[string]bool,
	threshold *float64, plain *bool, jsonOut *bool, verbose *bool,
	minLines *int, eps *float64, minPts *int,
	preview *bool, previewLines *int, sortMode *string, limit *int,
	minConfLines *int,
) {
	if d.Threshold != nil && !explicit["threshold"] {
		*threshold = *d.Threshold
	}
	if d.Plain != nil && !explicit["plain"] {
		*plain = *d.Plain
	}
	if d.JSON != nil && !explicit["json"] {
		*jsonOut = *d.JSON
	}
	if d.Verbose != nil && !explicit["verbose"] {
		*verbose = *d.Verbose
	}
	if d.MinLines != nil && !explicit["min-lines"] {
		*minLines = *d.MinLines
	}
	if d.Eps != nil && !explicit["eps"] {
		*eps = *d.Eps
	}
	if d.MinPts != nil && !explicit["min-pts"] {
		*minPts = *d.MinPts
	}
	if d.Preview != nil && !explicit["preview"] {
		*preview = *d.Preview
	}
	if d.PreviewLines != nil && !explicit["preview-lines"] {
		*previewLines = *d.PreviewLines
	}
	if d.Sort != nil && !explicit["sort"] {
		*sortMode = *d.Sort
	}
	if d.Limit != nil && !explicit["limit"] {
		*limit = *d.Limit
	}
	if d.MinConfidenceLines != nil && !explicit["min-confidence-lines"] {
		*minConfLines = *d.MinConfidenceLines
	}
}

// compileIgnoreMatcher returns a (possibly nil-safe) matcher built from
// cfg.IgnorePaths. A nil cfg yields a nil matcher whose Match method is a
// no-op.
func compileIgnoreMatcher(cfg *config.Config) (*config.IgnoreMatcher, error) {
	if cfg == nil || len(cfg.IgnorePaths) == 0 {
		return nil, nil
	}
	return config.CompileIgnorePaths(cfg.IgnorePaths)
}

// compileStripPatterns compiles cfg.IgnorePatterns to regexes. Errors are
// returned alongside the patterns that DID compile so the caller can warn
// and continue rather than fail the run.
func compileStripPatterns(cfg *config.Config) ([]*regexp.Regexp, error) {
	if cfg == nil || len(cfg.IgnorePatterns) == 0 {
		return nil, nil
	}
	return config.CompileIgnorePatterns(cfg.IgnorePatterns)
}

// compilePairIgnoreMatcher returns a matcher built from cfg.IgnorePairs.
// nil-safe: a nil cfg or empty list yields a nil matcher whose Match is a
// no-op, so callers can plumb the result through without nil checks.
func compilePairIgnoreMatcher(cfg *config.Config) (*config.PairIgnoreMatcher, error) {
	if cfg == nil || len(cfg.IgnorePairs) == 0 {
		return nil, nil
	}
	return config.CompileIgnorePairs(cfg.IgnorePairs)
}

// applyPairIgnores drops pairs that match the user's ignore_pairs and zeros
// the corresponding matrix entries so DBSCAN sees the two snippets as
// maximally distant and won't co-cluster them. Returns the surviving pairs
// (a fresh slice — input is not mutated beyond the matrix) and the count of
// ignored pairs. A nil matcher or empty pair list short-circuits.
func applyPairIgnores(
	pairs []report.Pair,
	matrix [][]float64,
	snippets []scan.Snippet,
	matcher *config.PairIgnoreMatcher,
) ([]report.Pair, int) {
	if matcher == nil || len(pairs) == 0 {
		return pairs, 0
	}
	nameIdx := make(map[string]int, len(snippets))
	for i, s := range snippets {
		nameIdx[s.Name] = i
	}
	kept := make([]report.Pair, 0, len(pairs))
	ignored := 0
	for _, p := range pairs {
		if matcher.Match(p.NameA, p.NameB) {
			ignored++
			i, okA := nameIdx[p.NameA]
			j, okB := nameIdx[p.NameB]
			if okA && okB {
				matrix[i][j] = 0
				matrix[j][i] = 0
			}
			continue
		}
		kept = append(kept, p)
	}
	return kept, ignored
}

func usage() {
	fmt.Fprintf(os.Stderr, `
codetwin — multi-language code similarity detector

USAGE:
  codetwin [flags] <path> [<path>...]

  Paths can be files or directories (scanned recursively).
  Supported: .go .js .ts .jsx .tsx .py .java .rs .ex .exs

FLAGS:
  --threshold float    minimum score to report (default 0.30)
  --plain              no ANSI colors, suitable for pipes and CI
  --json               output as JSON
  --verbose            show all pairs including weak similarities
  --min-lines int      skip files with fewer than N non-blank lines (default 3)
  --eps float          DBSCAN epsilon distance (default 0.45)
  --min-pts int        DBSCAN min cluster size (default 2)
  --preview            show a short code excerpt for each finding
  --preview-lines int  max lines per preview; 0 = show whole snippet (default 10)
  --sort string        result ordering: score | score-asc | size | size-asc | name (default score)
  --limit int          show only the top N pairs and N clusters (0 = no limit)
  --min-confidence-lines int  dampen pair scores when min(LinesA, LinesB) < N (0 = off)
  --no-progress        suppress the live progress indicator on stderr
  --no-cache           skip reading and writing .codetwin-cache.bin
  --rebuild-cache      ignore any existing cache and rebuild it from scratch
  --debug              print phase checkpoints with elapsed time to stderr
  --skill              print the full skill guide and exit
  --guide              print the report interpretation guide and exit

EXAMPLES:
  codetwin ./src
  codetwin --threshold 0.6 ./pkg
  codetwin --plain ./src > report.txt
  codetwin --json ./src | jq '.pairs[] | select(.score > 0.8)'
  codetwin ./utils/a.go ./utils/b.go

SCORING:
  > 95%%  Exact clone       — extract shared utility, delete one
  > 85%%  Near clone        — virtually identical; treat as a clone unless intentional
  > 65%%  Strong clone      — parameterize differing parts
  > 45%%  Refactor target   — evaluate shared abstraction
  < 45%%  Weak similarity   — probably coincidental

  Run 'codetwin --guide' for a full explanation of the score, the
  structural/semantic split, and how clusters differ from pairs.

`)
}
