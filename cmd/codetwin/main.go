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
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ccsrvs/codetwin/internal/baseline"
	"github.com/ccsrvs/codetwin/internal/blocks"
	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/cluster"
	"github.com/ccsrvs/codetwin/internal/config"
	"github.com/ccsrvs/codetwin/internal/deadcode"
	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/git"
	"github.com/ccsrvs/codetwin/internal/pathutil"
	"github.com/ccsrvs/codetwin/internal/refactor"
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

// buildVersion is stamped by the release workflow via
// `-ldflags "-X main.buildVersion=<tag>"`; local builds report "dev".
var buildVersion = "dev"

func main() {
	// Subcommand dispatch happens before flag parsing: everything else
	// in the CLI is flag-driven, so a bare first argument can only be a
	// scan path — a directory named like a subcommand needs the ./name
	// form.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "agent-install":
			runUpdateNotifier()
			runAgentInstallCLI(os.Args[2:])
			return
		case "update":
			runUpdateCLI(os.Args[2:])
			return
		case updateCheckCmdName:
			runBackgroundCheck()
			return
		}
	}

	threshold := flag.Float64("threshold", 0.50, "minimum similarity score to report (0.0–1.0)")
	plain := flag.Bool("plain", false, "plain text output (no ANSI colors, suitable for CI)")
	jsonOut := flag.Bool("json", false, "output results as JSON")
	verbose := flag.Bool("verbose", false, "show all pairs including weak similarities")
	minLines := flag.Int("min-lines", 3, "skip chunks with fewer than N non-blank lines")
	eps := flag.Float64("eps", 0.35, "DBSCAN epsilon: max distance for two snippets to be neighbours (linking requires pair score ≥ 1−eps; the default keeps clusters in the 'strong clone' band)")
	minPts := flag.Int("min-pts", 2, "DBSCAN minPts: minimum cluster size")
	preview := flag.Bool("preview", false, "show a short code excerpt for each finding")
	previewLines := flag.Int("preview-lines", 10, "max lines per preview; 0 = show whole snippet")
	sortMode := flag.String("sort", "score", "result ordering: score | score-asc | size | size-asc | name | age | age-asc (age modes require --blame)")
	limit := flag.Int("limit", 0, "show only the top N pairs and N clusters (0 = no limit)")
	minConfLines := flag.Int("min-confidence-lines", similarity.DefaultMinConfidenceLines, "dampen pair scores when min(LinesA, LinesB) < N (0 = off); ramps from 0.5× at 0 lines to 1.0× at N")
	minBlockLines := flag.Int("min-block-lines", blocks.DefaultMinBlockLines, "report sub-function partial clones (shared blocks inside below-threshold pairs) spanning at least N non-blank lines on both sides; 0 disables block detection")
	granularityFlag := flag.String("granularity", string(scan.GranularityFunction), "chunking unit: function (per-definition chunks, the default) | file (each source file is one whole-file snippet)")
	noProgress := flag.Bool("no-progress", false, "suppress progress output on stderr")
	noCache := flag.Bool("no-cache", false, "do not read or write .codetwin-cache.bin")
	rebuildCache := flag.Bool("rebuild-cache", false, "ignore any existing cache and rebuild it from scratch")
	debug := flag.Bool("debug", false, "print phase checkpoints with elapsed time to stderr")
	crossLangOnly := flag.Bool("cross-lang-only", false, "only report pairs whose two snippets are in different languages")
	crossRepoOnly := flag.Bool("cross-repo-only", false, "only report findings whose endpoints are in different repos; requires two or more directory roots (each root is a repo)")
	includeTests := flag.Bool("include-tests", false, "include test↔test pairs and test-only clusters in the report; by default they are suppressed and replaced by a one-line summary")
	flat := flag.Bool("flat", false, "list every pair individually; by default pairs whose endpoints share a cluster are collapsed into the cluster")
	since := flag.String("since", "", "PR-delta mode: keep only pairs where at least one snippet overlaps lines changed since <ref> (any committish; e.g. main, HEAD~5, abc123)")
	blame := flag.Bool("blame", false, "annotate each finding with git provenance (when introduced, by whom, last touched). Requires git on PATH and a git repository.")
	updateBaselinePath := flag.String("update-baseline", "", "after the scan, write a clone-watchlist snapshot of the visible clusters to <file> and exit 0 (the normal report still prints). Compare later runs against it with --baseline.")
	baselinePath := flag.String("baseline", "", "compare this scan's clusters against the snapshot in <file>: drift events print to stderr (one line each) and any drift exits 1 — a CI gate. Create the snapshot with --update-baseline; both runs must use the same threshold/eps/min-pts/granularity/include-tests.")
	suggest := flag.String("suggest", "", "print a unified diff that adds a starter helper extracted from the matching pair or partial-clone block (look up the 8-char ID in --json output). Pairs: Go, Python, Java, JS/TS, Rust, Elixir; blocks: Go and Python. Other languages print a 'note' explaining why.")
	suggestAll := flag.Bool("suggest-all", false, "with --json: populate `suggested_patch` on every visible pair and partial clone. Off by default — synthesis adds work proportional to finding count.")
	deadCode := flag.Bool("dead-code", false, "report definitions nothing in the scan references (name-based reachability; conservative). Adds a DEAD CODE section / dead_symbols in JSON. Requires --granularity function.")
	skill := flag.Bool("skill", false, "print the codetwin skill guide and exit")
	guide := flag.Bool("guide", false, "print the report interpretation guide and exit")
	showVersion := flag.Bool("version", false, "print the codetwin version and exit")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Println(buildVersion)
		return
	}
	if *skill {
		fmt.Print(skillBody)
		return
	}
	if *guide {
		fmt.Print(guideBody)
		return
	}

	// One stderr line when a newer release is cached; at most one
	// detached background check per day. Never blocks the scan.
	runUpdateNotifier()

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
			preview, previewLines, sortMode, limit, minConfLines, includeTests,
			minBlockLines, granularityFlag)
	}

	// Validate after config defaults are applied so a bad value fails
	// identically whether it came from --granularity or .codetwin.json.
	granularity, err := scan.ParseGranularity(*granularityFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Dead-code analysis needs per-definition chunks: whole-file
	// granularity has no symbols to check reachability for.
	if *deadCode && granularity == scan.GranularityFile {
		fmt.Fprintln(os.Stderr, "error: --dead-code requires --granularity function (whole-file chunks carry no definitions)")
		os.Exit(1)
	}

	// Clone watchlist (bet #5): validate the flag combination and load
	// the comparison snapshot BEFORE any file processing, so schema or
	// scan-params mismatches fail fast. baselineParams must be built
	// after config defaults so .codetwin.json overrides are captured.
	if *updateBaselinePath != "" && *baselinePath != "" {
		fmt.Fprintln(os.Stderr, "error: --baseline and --update-baseline are mutually exclusive (snapshot with --update-baseline, gate with --baseline)")
		os.Exit(1)
	}
	baselineParams := baseline.Params{
		Threshold: *threshold, Eps: *eps, MinPts: *minPts,
		Granularity: string(granularity), IncludeTests: *includeTests,
	}
	var baseSnap *baseline.Snapshot
	if *baselinePath != "" {
		snap := loadBaselineForCompare(*baselinePath, baselineParams)
		baseSnap = &snap
	}

	// cfgRules is a nil-safe view of cfg for rule compilation: with no
	// config file every rule list is empty and compileIfAny yields nil
	// matchers whose absence downstream code already handles.
	var cfgRules config.Config
	if cfg != nil {
		cfgRules = *cfg
	}
	ignoreMatcher, err := compileIfAny(cfgRules.IgnorePaths, config.CompileIgnorePaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error in ignore_paths: %v\n", err)
		os.Exit(1)
	}
	stripPatterns, err := compileIfAny(cfgRules.IgnorePatterns, config.CompileIgnorePatterns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning in ignore_patterns: %v\n", err)
		// Continue with whatever patterns compiled successfully.
	}
	pairIgnoreMatcher, err := compileIfAny(cfgRules.IgnorePairs, config.CompileIgnorePairs)
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

	files, repos, err := collectFiles(paths, ignoreMatcher)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error collecting files: %v\n", err)
		os.Exit(1)
	}
	debugf("collectFiles: %d files (%d directory roots)", len(files), len(repos.dirs))

	// Cross-repo mode (roadmap bet #6) switches on automatically when
	// two or more directory roots are given: each root is a repo.
	// --cross-repo-only without that is a contradiction — no snippet
	// would carry a repo label and every finding would be filtered out.
	if *crossRepoOnly && !repos.MultiRepo() {
		fmt.Fprintln(os.Stderr, "error: --cross-repo-only requires at least two directory roots (e.g. codetwin ../svc-a ../svc-b)")
		os.Exit(1)
	}

	if len(files) < 2 {
		fmt.Fprintln(os.Stderr, "error: need at least 2 source files to compare")
		os.Exit(1)
	}

	// Resolve git up-front when --since or --blame are on so we fail
	// fast (before any file processing) when git is missing or we're
	// outside a repo. Both failure modes are explicit opt-in errors:
	// the user asked for a git-dependent feature, so silent degradation
	// would hide the real problem.
	var gitRepo *git.Repo
	var sinceDiff git.DiffMap
	if *since != "" || *blame {
		gitRepo, err = git.Open(".")
		if err != nil {
			label, verb := requestedGitFlags(*since, *blame)
			switch {
			case errors.Is(err, git.ErrGitNotInstalled):
				fmt.Fprintf(os.Stderr, "error: %s %s the git binary on PATH\n", label, verb)
			case errors.Is(err, git.ErrNotARepo):
				fmt.Fprintf(os.Stderr, "error: %s %s running inside a git repository\n", label, verb)
			default:
				fmt.Fprintf(os.Stderr, "error: git: %v\n", err)
			}
			os.Exit(1)
		}
		// Multi-root scans must not mix git repositories: --since and
		// --blame resolve exactly one repo, so roots spread across
		// different working trees would produce silently wrong output.
		// Fail fast instead (documented limitation).
		if repos.MultiRepo() {
			label, _ := requestedGitFlags(*since, *blame)
			if err := repos.ensureSingleGitRepo(label); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		}
	}
	if *since != "" {
		sinceDiff, err = gitRepo.ChangedSince(*since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: --since %q: %v\n", *since, err)
			os.Exit(1)
		}
		debugf("--since %s: %d files changed", *since, len(sinceDiff))
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
		granularity,
		func() { done.Add(1) },
	)
	if progStop != nil {
		close(progStop)
		progWg.Wait()
	}
	debugf("scan.ProcessFiles: %d snippets from %d files (%d warnings)",
		len(snippets), len(files), len(fileWarnings))
	// Cross-repo namespacing: with two or more directory roots, each
	// snippet gets its root's repo label and a "label:" name prefix.
	// Runs before the sort so every downstream surface (pair IDs,
	// clusters, previews) sees one consistent set of names. Single-root
	// and file-argument invocations skip this entirely.
	if repos.MultiRepo() {
		namespaceSnippets(snippets, repos)
		debugf("multi-repo: %d roots, labels %v", len(repos.dirs), repos.labels)
	}
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

	// Dead-code analysis runs over the full snippet set, independent of
	// the similarity matrix, threshold, and --since filtering: liveness
	// is a whole-corpus property, so a --since scan still reports
	// against every scanned file.
	var deadSymbols []report.DeadSymbol
	if *deadCode {
		deadFindings, deadWarnings := deadcode.Analyze(snippets)
		for _, w := range deadWarnings {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
		deadSymbols = toReportDeadSymbols(deadFindings, *limit)
		debugf("--dead-code: %d findings (%d shown)", len(deadFindings), len(deadSymbols))
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
	matrix, pairs, blockCands := similarity.BuildMatrix(
		snippets, vectors, *minConfLines, *threshold,
		func(d, _ int64) { matrixDone.Store(d) },
	)
	if matrixProgStop != nil {
		close(matrixProgStop)
		matrixProgWg.Wait()
	}
	debugf("similarity.BuildMatrix: %d pairs above materialization floor (%.2f), %d block candidates in gray band",
		len(pairs), similarity.MaterializationFloor(*threshold), len(blockCands))

	// Tag each pair endpoint with its snippet's test-file classification
	// so report.Prepare can segregate test↔test findings by default.
	// Metadata only — no score or matrix changes.
	markTestPairs(pairs, snippets)

	if pairIgnoreMatcher != nil {
		var ignored int
		pairs, ignored = applyPairIgnores(pairs, matrix, snippets, pairIgnoreMatcher)
		debugf("ignore_pairs: dropped %d pairs", ignored)
	}

	// Block-level partial clones (review §5.3): a second detection
	// channel over the gray-band candidates — pairs too diluted to
	// render at function level that may still hide a copied block.
	var partialClones []report.BlockClone
	if *minBlockLines > 0 && len(blockCands) > 0 {
		partialClones = detectBlockClones(blockCands, snippets, *minBlockLines, pairIgnoreMatcher)
		debugf("blocks: %d partial clones from %d candidates", len(partialClones), len(blockCands))
	}

	if *since != "" {
		var dropped int
		pairs, dropped = filterPairsBySince(pairs, snippets, gitRepo.Root, sinceDiff)
		debugf("--since: dropped %d pairs not overlapping diff", dropped)
		var blocksDropped int
		partialClones, blocksDropped = filterBlocksBySince(partialClones, gitRepo.Root, sinceDiff)
		debugf("--since: dropped %d partial clones not overlapping diff", blocksDropped)
	}

	if *blame {
		provs := computeProvenance(snippets, gitRepo)
		attachProvenance(pairs, provs)
		debugf("--blame: provenance attached to %d snippets", len(provs))
	}

	distFn := func(i, j int) float64 { return 1.0 - matrix[i][j] }
	clusterResult := cluster.DBSCAN(n, *eps, *minPts, distFn)
	debugf("DBSCAN: %d clusters", clusterResult.NumClusters)
	groups := cluster.Groups(clusterResult)

	snippetNames := make([]string, len(snippets))
	for i, s := range snippets {
		snippetNames[i] = s.Name
	}
	// Per-member repo labels feed cluster grouping and the cross-repo
	// tag. nil on single-root scans so Cluster.MemberRepos stays nil and
	// rendering / JSON are untouched.
	var snippetRepos []string
	if repos.MultiRepo() {
		snippetRepos = make([]string, len(snippets))
		for i, s := range snippets {
			snippetRepos[i] = s.Repo
		}
	}
	clusters := buildReportClusters(groups, matrix, snippetNames, snippetRepos, *threshold)
	markTestOnlyClusters(clusters, snippets)
	debugf("clusters built: %d (from %d DBSCAN groups)", len(clusters), len(groups))

	if *since != "" {
		before := len(clusters)
		clusters = filterClustersBySince(clusters, snippets, gitRepo.Root, sinceDiff)
		debugf("--since: dropped %d clusters with no member in the diff", before-len(clusters))
	}

	opts := report.Options{
		Plain:         *plain,
		Threshold:     *threshold,
		Verbose:       *verbose,
		Sort:          report.SortMode(*sortMode),
		Limit:         *limit,
		CrossLangOnly: *crossLangOnly,
		CrossRepoOnly: *crossRepoOnly,
		IncludeTests:  *includeTests,
		Flat:          *flat,
		DeadCode:      deadSymbols,
	}

	// Sort + threshold filter + limit ONCE here in main.go, then build
	// previews scoped to just the snippets that will actually render.
	// On a big repo this avoids an O(shown²) MatchRange storm over
	// thousands of snippets when --limit means we'll only show a handful.
	visiblePairs, visibleClusters, suppressed := report.Prepare(pairs, clusters, opts)
	visibleBlocks, suppressedBlocks := report.PrepareBlocks(partialClones, opts)
	suppressed.TestTestBlocks = suppressedBlocks
	// Render re-runs Prepare on the already-filtered slices, which counts
	// zero suppressions — carry the real counts through Options.
	opts.Suppressed = suppressed
	opts.PartialClones = visibleBlocks
	debugf("prepared: %d visible pairs, %d visible clusters, %d partial clones (%d test↔test pairs, %d test-only clusters, %d test↔test blocks suppressed)",
		len(visiblePairs), len(visibleClusters), len(visibleBlocks),
		suppressed.TestTestPairs, suppressed.TestOnlyClusters, suppressed.TestTestBlocks)

	// Clone watchlist: snapshot exactly what the report shows — the
	// prepared, post-suppression clusters — so test segregation and
	// --include-tests compose naturally with baselines. Drift is
	// computed here (before output) so --json can embed it; the stderr
	// lines and exit code land in finishBaseline after the report.
	var curSnap baseline.Snapshot
	var driftEvents []baseline.Event
	if *updateBaselinePath != "" || *baselinePath != "" {
		curSnap = buildBaselineSnapshot(visibleClusters, snippets, paths, baselineParams)
		if baseSnap != nil {
			driftEvents = baseline.Diff(*baseSnap, curSnap)
			debugf("--baseline: %d drift events against %d baseline clusters", len(driftEvents), len(baseSnap.Clusters))
		}
	}

	// --suggest <pair-id> short-circuits the rest of the report. We
	// look up across all materialized pairs (not just visiblePairs) so
	// the user can target a sub-threshold pair without having to
	// re-tune --threshold. Materialization reaches down to
	// similarity.MaterializationFloor(threshold) — a 0.20 band below
	// the threshold (never below 0.30). Partial-clone block IDs are
	// searched too (all detected blocks, not just visible ones); pairs
	// win the 1-in-4-billion ID collision.
	if *suggest != "" {
		if showProgress {
			fmt.Fprint(os.Stderr, "\r\033[K")
		}
		if err := emitSuggestion(*suggest, pairs, partialClones, snippets); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		debugf("done (suggest)")
		return
	}

	if *preview {
		if showProgress {
			fmt.Fprint(os.Stderr, "\r\033[Kbuilding previews...")
		}
		opts.Previews = buildPreviews(visiblePairs, visibleClusters, snippets, *previewLines)
		addBlockPreviews(opts.Previews, visibleBlocks, snippets, *previewLines)
		debugf("previews built: %d", len(opts.Previews))
	}

	if showProgress {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}

	if *jsonOut {
		var suggestions, blockSuggestions map[string]jsonPatch
		if *suggestAll {
			suggestions = buildSuggestionMap(visiblePairs, snippets)
			blockSuggestions = buildBlockSuggestionMap(visibleBlocks, snippets)
			debugf("--suggest-all: built %d pair + %d block suggestions",
				len(suggestions), len(blockSuggestions))
		}
		printJSON(visiblePairs, visibleClusters, visibleBlocks, opts.Previews, suggestions, blockSuggestions, suppressed, driftEvents, deadSymbols)
		debugf("done (json)")
		finishBaseline(*updateBaselinePath, *baselinePath, curSnap, driftEvents)
		return
	}

	// Render gets already-prepared inputs. Its internal Prepare call is
	// idempotent on sorted+filtered+limited data.
	report.Render(os.Stdout, visiblePairs, visibleClusters, opts)
	debugf("done (rendered)")
	finishBaseline(*updateBaselinePath, *baselinePath, curSnap, driftEvents)
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
	nameIdx := snippetIndex(snippets)

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

// ── Cluster building ──────────────────────────────────────────────────────────

// clusterStats returns the average and minimum internal pair score over
// all distinct member pairs, read from the similarity matrix. Groups
// with fewer than two members (which DBSCAN won't produce, but guard
// anyway) yield (0, 0).
func clusterStats(members []int, matrix [][]float64) (avg, min float64) {
	if len(members) < 2 {
		return 0, 0
	}
	var sum float64
	var nPairs int
	min = 1.0
	for k := 0; k < len(members); k++ {
		for l := k + 1; l < len(members); l++ {
			s := matrix[members[k]][members[l]]
			sum += s
			if s < min {
				min = s
			}
			nPairs++
		}
	}
	return sum / float64(nPairs), min
}

// buildReportClusters converts DBSCAN member groups into report.Clusters
// carrying both the average internal pair score (Score) and the minimum
// (MinScore, "cohesion"), computed from the in-memory similarity matrix.
//
// DBSCAN links transitively: with eps 0.35 any chain of pairs scoring
// ≥ 0.65 merges into one cluster even when its endpoints barely resemble
// each other. The report frames each cluster as one refactoring task, so
// low-cohesion chains are actively misleading. When a cluster's minimum
// internal score falls below the report threshold, its members are
// re-linked single-linkage at pair score ≥ threshold and each connected
// component becomes its own cluster; components of size 1 have no
// threshold-strength partner and drop out as noise. Split clusters get
// their avg/min recomputed over just their own members.
//
// IDs are assigned deterministically by first member name, so cluster
// numbering is stable across runs regardless of map iteration order.
// repos, when non-nil, is parallel to names and carries each snippet's
// repo label from a multi-root scan; each cluster then gets a
// MemberRepos slice parallel to its Members. nil (single-root) leaves
// MemberRepos nil so repo-aware rendering and JSON stay switched off.
func buildReportClusters(
	groups map[int][]int,
	matrix [][]float64,
	names []string,
	repos []string,
	threshold float64,
) []report.Cluster {
	memberLists := make([][]int, 0, len(groups))
	for _, members := range groups {
		_, min := clusterStats(members, matrix)
		if min >= threshold {
			memberLists = append(memberLists, members)
			continue
		}
		link := func(a, b int) bool { return matrix[a][b] >= threshold }
		for _, comp := range cluster.Components(members, link) {
			if len(comp) < 2 {
				continue // singleton at the stricter bound → noise
			}
			memberLists = append(memberLists, comp)
		}
	}

	clusters := make([]report.Cluster, 0, len(memberLists))
	for _, members := range memberLists {
		avg, min := clusterStats(members, matrix)
		memberNames := make([]string, len(members))
		var memberRepos []string
		if repos != nil {
			memberRepos = make([]string, len(members))
		}
		for k, idx := range members {
			memberNames[k] = names[idx]
			if memberRepos != nil {
				memberRepos[k] = repos[idx]
			}
		}
		clusters = append(clusters, report.Cluster{
			Members: memberNames, MemberRepos: memberRepos,
			Score: avg, MinScore: min,
		})
	}
	// Deterministic renumbering: order by first member name. Clusters are
	// disjoint, so first members are unique and the order is total.
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Members[0] < clusters[j].Members[0]
	})
	for i := range clusters {
		clusters[i].ID = i
	}
	return clusters
}

// ── Refactor suggestions (--suggest / --suggest-all) ─────────────────────────

// emitSuggestion looks up an 8-char ID across pairs and partial-clone
// blocks (pairs take precedence on the 1-in-4-billion collision), runs
// the matching refactor pipeline (align → synthesize → patch), and
// writes the resulting unified diff to stdout. When synthesis is
// rejected, prints a single "note: <reason>" line on stderr and exits
// 1 — that matches the failure semantics of `--since` on a
// non-existent ref and gives CI pipelines a clean way to detect "no
// patch produced."
func emitSuggestion(id string, pairs []report.Pair, blocks []report.BlockClone, snippets []scan.Snippet) error {
	if pair, ok := findPairByID(id, pairs); ok {
		return emitPairSuggestion(pair, snippets)
	}
	if bc, ok := findBlockByID(id, blocks); ok {
		return emitBlockSuggestion(bc, snippets)
	}
	return fmt.Errorf("no pair or partial clone matches id %q (lower --threshold or check the id from --json output)", id)
}

func emitPairSuggestion(pair report.Pair, snippets []scan.Snippet) error {
	a, ok := findSnippet(pair.NameA, snippets)
	if !ok {
		return fmt.Errorf("snippet not found: %s", pair.NameA)
	}
	b, ok := findSnippet(pair.NameB, snippets)
	if !ok {
		return fmt.Errorf("snippet not found: %s", pair.NameB)
	}
	al := refactor.Align(a, b)
	s := refactor.Synthesize(a, b, pair.ID, al)
	if s.Note != "" {
		fmt.Fprintf(os.Stderr, "note: %s\n", s.Note)
		os.Exit(1)
	}
	diff, err := refactor.BuildPatch(a.Path, s)
	if err != nil {
		return fmt.Errorf("build patch: %w", err)
	}
	fmt.Print(diff)
	return nil
}

// emitBlockSuggestion is emitSuggestion's block-mode path: slice both
// hosts' chunk code to the matched block spans, run the block-mode
// pipeline (Align → SynthesizeBlock), and emit a diff that inserts the
// helper right after side A's enclosing function. Rejections (notably
// languages without a block-mode emitter) follow the pair-level
// contract: "note: <reason>" on stderr, exit 1.
func emitBlockSuggestion(bc report.BlockClone, snippets []scan.Snippet) error {
	s, hostA, err := synthesizeBlockSuggestion(bc, snippets)
	if err != nil {
		return err
	}
	if s.Note != "" {
		fmt.Fprintf(os.Stderr, "note: %s\n", s.Note)
		os.Exit(1)
	}
	diff, err := refactor.BuildPatchInsertAfter(hostA.Path, hostA.EndLine, s)
	if err != nil {
		return fmt.Errorf("build patch: %w", err)
	}
	fmt.Print(diff)
	return nil
}

// buildSuggestionMap synthesizes a Suggestion for every visible pair
// and packages it as a jsonPatch. Used by --suggest-all to populate
// `suggested_patch` on each pair in the JSON output. Pairs whose
// snippets can't be resolved are skipped silently — they wouldn't have
// rendered anyway.
func buildSuggestionMap(pairs []report.Pair, snippets []scan.Snippet) map[string]jsonPatch {
	out := make(map[string]jsonPatch, len(pairs))
	byName := snippetsByName(snippets)
	for _, p := range pairs {
		a, okA := byName[p.NameA]
		b, okB := byName[p.NameB]
		if !okA || !okB {
			continue
		}
		al := refactor.Align(a, b)
		sug := refactor.Synthesize(a, b, p.ID, al)
		out[p.ID] = suggestionPatch(sug, func() (string, error) {
			return refactor.BuildPatch(a.Path, sug)
		})
	}
	return out
}

// suggestionPatch packages a Suggestion as a jsonPatch, attaching the
// unified diff when synthesis produced a helper (a diff-build error
// replaces the Note). Shared by the pair and block --suggest-all
// builders.
func suggestionPatch(sug refactor.Suggestion, buildDiff func() (string, error)) jsonPatch {
	patch := jsonPatch{
		HelperName: sug.HelperName,
		Confidence: sug.Confidence,
		Note:       sug.Note,
	}
	if sug.HelperSrc != "" {
		diff, err := buildDiff()
		if err != nil {
			patch.Note = "error: " + err.Error()
		} else {
			patch.UnifiedDiff = diff
		}
	}
	return patch
}

// findByKey returns the first item whose key equals want — the shared
// linear-lookup shape behind findPairByID, findBlockByID, and
// findSnippet.
func findByKey[T any](items []T, want string, key func(T) string) (T, bool) {
	for _, it := range items {
		if key(it) == want {
			return it, true
		}
	}
	var zero T
	return zero, false
}

func findPairByID(id string, pairs []report.Pair) (report.Pair, bool) {
	return findByKey(pairs, id, func(p report.Pair) string { return p.ID })
}

func findSnippet(name string, snippets []scan.Snippet) (scan.Snippet, bool) {
	return findByKey(snippets, name, func(s scan.Snippet) string { return s.Name })
}

// ── JSON output ───────────────────────────────────────────────────────────────

type jsonOutput struct {
	Pairs    []jsonPair             `json:"pairs"`
	Clusters []jsonCluster          `json:"clusters"`
	Previews map[string]jsonPreview `json:"previews,omitempty"`

	// PartialClones lists sub-function block-level findings (review
	// §5.3). Omitted when block detection found nothing or was
	// disabled with --min-block-lines 0.
	PartialClones []jsonBlockClone `json:"partial_clones,omitempty"`

	// Suppressed summarizes findings dropped by the default test-code
	// segregation. Omitted entirely when nothing was suppressed — in
	// particular with --include-tests, so that flag preserves the exact
	// pre-segregation JSON schema for CI consumers.
	Suppressed *jsonSuppressed `json:"suppressed,omitempty"`

	// DeadSymbols lists --dead-code findings: definitions nothing in
	// the scan references. Omitted when the flag is off or every
	// definition proved alive.
	DeadSymbols []jsonDeadSymbol `json:"dead_symbols,omitempty"`

	// Drift lists clone-watchlist events from --baseline mode. Omitted
	// when there is no drift (and in runs without --baseline), so the
	// schema stays byte-stable for consumers that don't use baselines.
	Drift []jsonDriftEvent `json:"drift,omitempty"`
}

// jsonDriftEvent mirrors baseline.Event in the JSON schema. Cluster is
// an index into the current run's cluster list — except for
// cluster-dissolved, where it indexes the baseline's list (the cluster
// no longer exists in this run); Detail always carries member keys.
type jsonDriftEvent struct {
	Kind    string `json:"kind"`
	Cluster int    `json:"cluster"`
	Detail  string `json:"detail"`
}

// jsonSuppressed mirrors report.Suppressed in the JSON schema.
type jsonSuppressed struct {
	TestTestPairs    int `json:"test_test_pairs,omitempty"`
	TestOnlyClusters int `json:"test_only_clusters,omitempty"`
	TestTestBlocks   int `json:"test_test_blocks,omitempty"`
}

type jsonPair struct {
	ID         string  `json:"id,omitempty"`
	FileA      string  `json:"file_a"`
	FileB      string  `json:"file_b"`
	Score      float64 `json:"score"`
	Structural float64 `json:"structural"`
	Semantic   float64 `json:"semantic"`
	// Lexical is present only on pairs where the lexical sub-score was
	// computed (combined score above the near-clone band). A pointer so
	// a measured 0.0 — fully disjoint vocabulary, the strongest
	// structural-twin evidence — still serializes instead of being
	// dropped by omitempty.
	Lexical        *float64        `json:"lexical,omitempty"`
	Label          string          `json:"label"`
	LangA          string          `json:"lang_a,omitempty"`
	LangB          string          `json:"lang_b,omitempty"`
	RepoA          string          `json:"repo_a,omitempty"`
	RepoB          string          `json:"repo_b,omitempty"`
	ProvenanceA    *jsonProvenance `json:"provenance_a,omitempty"`
	ProvenanceB    *jsonProvenance `json:"provenance_b,omitempty"`
	SuggestedPatch *jsonPatch      `json:"suggested_patch,omitempty"`
}

// jsonPatch is the shape of `suggested_patch` on each pair when
// --suggest-all is set. UnifiedDiff is empty when synthesis was
// rejected; in that case Note explains why.
type jsonPatch struct {
	UnifiedDiff string  `json:"unified_diff,omitempty"`
	HelperName  string  `json:"helper_name,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	Note        string  `json:"note,omitempty"`
}

type jsonProvenance struct {
	FirstCommit string `json:"first_commit"`
	FirstAuthor string `json:"first_author"`
	FirstDate   string `json:"first_date"`
	LastCommit  string `json:"last_commit,omitempty"`
	LastAuthor  string `json:"last_author,omitempty"`
	LastDate    string `json:"last_date,omitempty"`
}

func toJSONProvenance(p *report.Provenance) *jsonProvenance {
	if p == nil {
		return nil
	}
	out := &jsonProvenance{
		FirstCommit: p.FirstCommit,
		FirstAuthor: p.FirstAuthor,
		FirstDate:   p.FirstTime.UTC().Format("2006-01-02"),
	}
	if p.LastCommit != "" && p.LastCommit != p.FirstCommit {
		out.LastCommit = p.LastCommit
		out.LastAuthor = p.LastAuthor
		out.LastDate = p.LastTime.UTC().Format("2006-01-02")
	}
	return out
}

type jsonCluster struct {
	ID      int      `json:"id"`
	Members []string `json:"members"`
	Score   float64  `json:"score"`
	// MinScore is the cluster's cohesion: the minimum internal pair
	// score over all distinct member pairs. A MinScore far below Score
	// flags a transitively chained family.
	MinScore float64 `json:"min_score"`
	// MemberRepos (parallel to Members) and CrossRepo appear only on
	// multi-root scans; single-root JSON is schema-identical to the
	// pre-multi-repo output.
	MemberRepos []string `json:"member_repos,omitempty"`
	CrossRepo   bool     `json:"cross_repo,omitempty"`
}

type jsonPreview struct {
	StartLine int    `json:"start_line"`
	Text      string `json:"text"`
}

// printJSON emits the prepared (already sorted, threshold-filtered, limited)
// pairs and clusters as JSON. Sort and limit are applied upstream via
// report.Prepare so JSON consumers see the same set of findings as the
// terminal renderer. When suggestions is non-nil, each pair's
// suggested_patch field is populated by ID lookup. Non-zero suppressed
// counts add a top-level `suppressed` summary object.
// jsonDeadSymbol mirrors report.DeadSymbol for --json consumers. Name
// uses the same "path:start-end symbol" format as pair/cluster names.
type jsonDeadSymbol struct {
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Kind     string `json:"kind"`
	Lang     string `json:"lang"`
	Exported bool   `json:"exported"`
	Verdict  string `json:"verdict"`
	TestRefs int    `json:"test_refs,omitempty"`
}

// toReportDeadSymbols converts analysis findings to the display type,
// applying --limit the same way the pair and cluster sections do.
func toReportDeadSymbols(findings []deadcode.Finding, limit int) []report.DeadSymbol {
	if limit > 0 && len(findings) > limit {
		findings = findings[:limit]
	}
	out := make([]report.DeadSymbol, 0, len(findings))
	for _, f := range findings {
		out = append(out, report.DeadSymbol{
			Name:     f.Name,
			Symbol:   f.Symbol,
			Kind:     string(f.Kind),
			Lang:     string(f.Lang),
			Exported: f.Exported,
			Verdict:  string(f.Verdict),
			TestRefs: f.TestRefs,
		})
	}
	return out
}

func printJSON(pairs []report.Pair, clusters []report.Cluster, blockClones []report.BlockClone, previews map[string]report.Preview, suggestions, blockSuggestions map[string]jsonPatch, suppressed report.Suppressed, drift []baseline.Event, deadSymbols []report.DeadSymbol) {
	out := jsonOutput{PartialClones: toJSONBlockClones(blockClones, blockSuggestions)}
	for _, d := range deadSymbols {
		out.DeadSymbols = append(out.DeadSymbols, jsonDeadSymbol{
			Name: d.Name, Symbol: d.Symbol, Kind: d.Kind, Lang: d.Lang,
			Exported: d.Exported, Verdict: d.Verdict, TestRefs: d.TestRefs,
		})
	}
	for _, e := range drift {
		out.Drift = append(out.Drift, jsonDriftEvent{Kind: string(e.Kind), Cluster: e.Cluster, Detail: e.Detail})
	}
	if suppressed.TestTestPairs > 0 || suppressed.TestOnlyClusters > 0 || suppressed.TestTestBlocks > 0 {
		out.Suppressed = &jsonSuppressed{
			TestTestPairs:    suppressed.TestTestPairs,
			TestOnlyClusters: suppressed.TestOnlyClusters,
			TestTestBlocks:   suppressed.TestTestBlocks,
		}
	}
	if len(previews) > 0 {
		out.Previews = make(map[string]jsonPreview, len(previews))
		for k, v := range previews {
			out.Previews[k] = jsonPreview{StartLine: v.StartLine, Text: v.Text}
		}
	}
	for _, p := range pairs {
		jp := jsonPair{
			ID:    p.ID,
			FileA: p.NameA, FileB: p.NameB,
			Score: p.Score, Structural: p.Structural, Semantic: p.Semantic,
			Label: report.JSONLabel(p),
			LangA: p.LangA, LangB: p.LangB,
			RepoA: p.RepoA, RepoB: p.RepoB,
			ProvenanceA: toJSONProvenance(p.ProvenanceA),
			ProvenanceB: toJSONProvenance(p.ProvenanceB),
		}
		if p.LexicalComputed {
			lex := p.Lexical
			jp.Lexical = &lex
		}
		if suggestions != nil {
			if patch, ok := suggestions[p.ID]; ok {
				jp.SuggestedPatch = &patch
			}
		}
		out.Pairs = append(out.Pairs, jp)
	}
	for _, c := range clusters {
		out.Clusters = append(out.Clusters, jsonCluster{
			ID: c.ID, Members: c.Members, Score: c.Score, MinScore: c.MinScore,
			MemberRepos: c.MemberRepos, CrossRepo: c.CrossRepo(),
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "error: writing JSON output: %v\n", err)
		os.Exit(1)
	}
}

// ── File collection ───────────────────────────────────────────────────────────

// collectFiles walks the given paths and returns the supported source
// files plus a repoMap recording which directory root each file came
// from. The repoMap only takes effect downstream when two or more
// directory roots were given (repoMap.MultiRepo) — that's the automatic
// cross-repo mode; direct file arguments never carry a repo label.
func collectFiles(paths []string, ignore *config.IgnoreMatcher) ([]string, *repoMap, error) {
	deduped, err := pathutil.Dedupe(paths)
	if err != nil {
		return nil, nil, err
	}
	repos := newRepoMap()
	var files []string
	for _, p := range deduped {
		info, err := os.Stat(p)
		if err != nil {
			return nil, nil, err
		}
		if info.IsDir() {
			label := repos.addRoot(p)
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
					repos.addFile(path, p, label)
				}
				return nil
			})
			if err != nil {
				return nil, nil, err
			}
		} else if supportedExts[filepath.Ext(p)] && !ignore.Match(p, false) {
			files = append(files, p)
		}
	}
	return files, repos, nil
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
	minConfLines *int, includeTests *bool, minBlockLines *int,
	granularity *string,
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
	if d.IncludeTests != nil && !explicit["include-tests"] {
		*includeTests = *d.IncludeTests
	}
	if d.MinBlockLines != nil && !explicit["min-block-lines"] {
		*minBlockLines = *d.MinBlockLines
	}
	if d.Granularity != nil && !explicit["granularity"] {
		*granularity = *d.Granularity
	}
}

// compileIfAny is the one compile-config-rules path (ignore_paths,
// ignore_patterns, ignore_pairs — formerly three near-identical
// wrappers): an empty rule list yields the zero (nil) result with no
// error, so callers can plumb the result through without nil checks;
// otherwise compile runs on the rules. For ignore_patterns the compile
// error arrives alongside the patterns that DID compile, so the caller
// can warn and continue rather than fail the run.
func compileIfAny[R, T any](rules []R, compile func([]R) (T, error)) (T, error) {
	if len(rules) == 0 {
		var zero T
		return zero, nil
	}
	return compile(rules)
}

// requestedGitFlags returns a human-readable label and matching verb
// ("requires" / "require") for whichever git-dependent flags were set,
// so error messages stay grammatical for both the one-flag and two-flag
// cases.
func requestedGitFlags(since string, blame bool) (label, verb string) {
	switch {
	case since != "" && blame:
		return "--since and --blame", "require"
	case since != "":
		return "--since", "requires"
	case blame:
		return "--blame", "requires"
	}
	return "git-dependent flags", "require"
}

// computeProvenance runs git blame for each unique snippet and returns
// a name → Provenance map. Untracked files and other recoverable blame
// errors are silently skipped; the snippet just won't have provenance
// attached. Catastrophic git errors print a one-line warning.
func computeProvenance(snippets []scan.Snippet, repo *git.Repo) map[string]*report.Provenance {
	out := make(map[string]*report.Provenance, len(snippets))
	for _, s := range snippets {
		if _, seen := out[s.Name]; seen {
			continue
		}
		br, err := repo.Blame(s.Path, s.StartLine, s.EndLine)
		if err != nil {
			if !errors.Is(err, git.ErrFileNotTracked) {
				fmt.Fprintf(os.Stderr, "warning: blame %s: %v\n", s.Name, err)
			}
			continue
		}
		out[s.Name] = &report.Provenance{
			FirstCommit: br.FirstCommit,
			FirstAuthor: br.FirstAuthor,
			FirstTime:   br.FirstTime,
			LastCommit:  br.LastCommit,
			LastAuthor:  br.LastAuthor,
			LastTime:    br.LastTime,
		}
	}
	return out
}

// attachProvenance copies entries from a snippet-name keyed map onto
// each pair's two endpoints. Pairs whose endpoints have no provenance
// are left as-is (nil pointers).
func attachProvenance(pairs []report.Pair, provs map[string]*report.Provenance) {
	for i := range pairs {
		if p, ok := provs[pairs[i].NameA]; ok {
			pairs[i].ProvenanceA = p
		}
		if p, ok := provs[pairs[i].NameB]; ok {
			pairs[i].ProvenanceB = p
		}
	}
}

// snippetMap builds a name-keyed lookup map over snippets, with
// value(i, s) choosing what each name maps to — the scaffolding every
// routine that resolves pair/cluster endpoints back to snippets by
// name previously rebuilt inline. The three shapes in use follow.
func snippetMap[V any](snippets []scan.Snippet, value func(i int, s scan.Snippet) V) map[string]V {
	m := make(map[string]V, len(snippets))
	for i, s := range snippets {
		m[s.Name] = value(i, s)
	}
	return m
}

// snippetIndex maps each snippet's name to its position in snippets
// (buildPreviews, the --since filters, applyPairIgnores).
func snippetIndex(snippets []scan.Snippet) map[string]int {
	return snippetMap(snippets, func(i int, _ scan.Snippet) int { return i })
}

// snippetsByName maps each snippet's name to the snippet itself, for
// the suggestion and preview builders that need the full value.
func snippetsByName(snippets []scan.Snippet) map[string]scan.Snippet {
	return snippetMap(snippets, func(_ int, s scan.Snippet) scan.Snippet { return s })
}

// snippetTestFlags maps each snippet's name to its test-file
// classification, shared by markTestPairs and markTestOnlyClusters.
func snippetTestFlags(snippets []scan.Snippet) map[string]bool {
	return snippetMap(snippets, func(_ int, s scan.Snippet) bool { return s.IsTest })
}

// markTestPairs sets each pair's IsTestA/IsTestB from the endpoint
// snippets' test-file classification (scan.IsTestFile on the scanned
// path). Presentation metadata only: report.Prepare uses the flags to
// suppress test↔test pairs by default; scores are untouched.
func markTestPairs(pairs []report.Pair, snippets []scan.Snippet) {
	isTest := snippetTestFlags(snippets)
	for i := range pairs {
		pairs[i].IsTestA = isTest[pairs[i].NameA]
		pairs[i].IsTestB = isTest[pairs[i].NameB]
	}
}

// markTestOnlyClusters sets Cluster.TestOnly on clusters whose every
// member is a test snippet. Runs after buildReportClusters so the flag
// reflects the final member lists (low-cohesion splitting may have
// regrouped members). Same presentation-only contract as markTestPairs.
func markTestOnlyClusters(clusters []report.Cluster, snippets []scan.Snippet) {
	isTest := snippetTestFlags(snippets)
	for i := range clusters {
		allTest := len(clusters[i].Members) > 0
		for _, m := range clusters[i].Members {
			if !isTest[m] {
				allTest = false
				break
			}
		}
		clusters[i].TestOnly = allTest
	}
}

// keepTouching is the shared --since filter loop: it keeps the items
// for which touches reports diff overlap and returns the survivors plus
// the dropped count. Backs filterPairsBySince, filterClustersBySince,
// and filterBlocksBySince.
func keepTouching[T any](items []T, touches func(T) bool) ([]T, int) {
	kept := make([]T, 0, len(items))
	dropped := 0
	for _, it := range items {
		if touches(it) {
			kept = append(kept, it)
			continue
		}
		dropped++
	}
	return kept, dropped
}

// filterPairsBySince keeps only pairs where at least one snippet's source
// range overlaps a line range in the supplied DiffMap. Snippets whose
// path resolves outside repoRoot can never overlap — as can pairs whose
// endpoints can't be resolved by name — and are treated as non-touching.
func filterPairsBySince(
	pairs []report.Pair,
	snippets []scan.Snippet,
	repoRoot string,
	diff git.DiffMap,
) ([]report.Pair, int) {
	idx := snippetIndex(snippets)
	return keepTouching(pairs, func(p report.Pair) bool {
		ai, okA := idx[p.NameA]
		bi, okB := idx[p.NameB]
		if !okA || !okB {
			return false
		}
		a, b := snippets[ai], snippets[bi]
		return diff.Touches(repoRoot, a.Path, a.StartLine, a.EndLine) ||
			diff.Touches(repoRoot, b.Path, b.StartLine, b.EndLine)
	})
}

// filterClustersBySince keeps only clusters where at least one member
// snippet's source range overlaps the diff. Members are looked up by
// name; unknown names are treated as non-touching.
func filterClustersBySince(
	clusters []report.Cluster,
	snippets []scan.Snippet,
	repoRoot string,
	diff git.DiffMap,
) []report.Cluster {
	idx := snippetIndex(snippets)
	kept, _ := keepTouching(clusters, func(c report.Cluster) bool {
		for _, m := range c.Members {
			si, ok := idx[m]
			if !ok {
				continue
			}
			s := snippets[si]
			if diff.Touches(repoRoot, s.Path, s.StartLine, s.EndLine) {
				return true
			}
		}
		return false
	})
	return kept
}

// applyPairIgnores drops pairs that match the user's ignore_pairs and zeros
// the corresponding matrix entries so DBSCAN sees the two snippets as
// maximally distant and won't co-cluster them. Returns the surviving pairs
// (a fresh slice — input is not mutated beyond the matrix) and the count of
// ignored pairs. A nil matcher or empty pair list short-circuits.
//
// Endpoints are matched against the UN-prefixed snippet name (the repo
// label of a multi-root scan is stripped first), so ignore_pairs
// entries stay portable between single-root and multi-root invocations.
// Note the path part is the root-relative path in multi-root mode.
func applyPairIgnores(
	pairs []report.Pair,
	matrix [][]float64,
	snippets []scan.Snippet,
	matcher *config.PairIgnoreMatcher,
) ([]report.Pair, int) {
	if matcher == nil || len(pairs) == 0 {
		return pairs, 0
	}
	nameIdx := snippetIndex(snippets)
	kept := make([]report.Pair, 0, len(pairs))
	ignored := 0
	for _, p := range pairs {
		if matcher.Match(stripRepoPrefix(p.NameA, p.RepoA), stripRepoPrefix(p.NameB, p.RepoB)) {
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
  codetwin agent-install <agent> [--scope project|user]   (see agent-install --list)
  codetwin update [--check]      self-update from the latest GitHub release
                                 (a background check runs at most once a day and
                                 prints a one-line notice; opt out with
                                 CODETWIN_NO_UPDATE_CHECK=1)

  Paths can be files or directories (scanned recursively).
  Supported: .go .js .ts .jsx .tsx .py .java .rs .ex .exs

  Two or more DIRECTORY roots switch on cross-repo mode: each root is a
  "repo" (labelled by its base name; duplicates become name~2, name~3 …
  by input order), snippet names gain a "repo:" prefix with paths shown
  relative to their root, clusters spanning >=2 repos are tagged
  cross-repo with members grouped per repo, and JSON gains
  repo_a/repo_b/member_repos/cross_repo fields. Single-root and
  file-argument runs are unchanged.

FLAGS:
  --threshold float    minimum score to report (default 0.50)
  --plain              no ANSI colors, suitable for pipes and CI
  --json               output as JSON
  --verbose            show all pairs including weak similarities
  --min-lines int      skip chunks with fewer than N non-blank lines (default 3)
  --eps float          DBSCAN epsilon distance (default 0.35; links pairs ≥ 65%%,
                       the 'strong clone' band)
  --min-pts int        DBSCAN min cluster size (default 2)
  --preview            show a short code excerpt for each finding
  --preview-lines int  max lines per preview; 0 = show whole snippet (default 10)
  --sort string        result ordering: score | score-asc | size | size-asc | name | age | age-asc
                       (default score; age modes require --blame)
  --limit int          show only the top N pairs and N clusters (0 = no limit)
  --flat               list every pair individually; by default pairs inside a
                       cluster render once as the cluster (families first)
  --min-confidence-lines int  dampen pair scores when min(LinesA, LinesB) < N
                       (default 10; 0 = off)
  --min-block-lines int  report sub-function PARTIAL CLONES: shared blocks of at
                       least N non-blank lines (both sides) hiding inside pairs
                       below the report threshold (default 8; 0 = off)
  --granularity string chunking unit: function | file (default function).
                       file mode skips the splitter — each source file is one
                       whole-file snippet, for module-level consolidation and
                       languages without a splitter
  --dead-code          report definitions nothing in the scan references, in a
                       DEAD CODE section (JSON: dead_symbols). Verdicts: dead |
                       test-only | unused-in-scan. Conservative name-based
                       reachability; requires --granularity function
  --no-progress        suppress the live progress indicator on stderr
  --no-cache           skip reading and writing .codetwin-cache.bin
  --rebuild-cache      ignore any existing cache and rebuild it from scratch
  --debug              print phase checkpoints with elapsed time to stderr
  --cross-lang-only    report only pairs whose two snippets are in different languages
                       (e.g. duplicate logic across Go service + TS dashboard)
  --cross-repo-only    report only findings whose endpoints are in different repos
                       (requires >=2 directory roots; composes with --cross-lang-only)
  --include-tests      include test↔test pairs and test-only clusters; by default
                       they are suppressed and replaced by a one-line summary
                       (test↔production pairs and mixed clusters always render)
  --since string       PR-delta mode: keep only findings where ≥1 endpoint overlaps
                       lines changed since <ref> (e.g. main, HEAD~5, abc123).
                       Requires git on PATH and a git repository.
  --blame              annotate each finding with git provenance (when introduced,
                       by whom, last touched). Pairs --sort=age for "newest clones first".
                       Requires git on PATH and a git repository.
  --update-baseline string  clone watchlist: after the scan, write a snapshot of the
                       visible clusters to <file> and exit 0 (the report still prints)
  --baseline string    compare this scan against the snapshot in <file>: drift events
                       print to stderr ('drift: <kind> cluster <n>: <detail>'), any
                       drift exits 1 (CI gate). Both runs must use the same threshold/
                       eps/min-pts/granularity/include-tests. Mutually exclusive with
                       --update-baseline.
  --suggest string     print a unified diff that adds a starter helper extracted from
                       the pair or partial-clone block with the given 8-char ID (look
                       it up in --json output). Pairs: all six languages; blocks: Go
                       and Python. Rejections print a 'note' on stderr and exit 1.
  --suggest-all        with --json: populate suggested_patch on every visible pair and
                       partial clone (off by default — synthesis cost scales with
                       finding count)
  --skill              print the full skill guide and exit
  --guide              print the report interpretation guide and exit
  --version            print the codetwin version and exit

EXAMPLES:
  codetwin ./src
  codetwin --threshold 0.6 ./pkg
  codetwin --plain ./src > report.txt
  codetwin --json ./src | jq '.pairs[] | select(.score > 0.8)'
  codetwin ./utils/a.go ./utils/b.go
  codetwin ../svc-a ../svc-b ../svc-c              # cross-repo scan
  codetwin --cross-repo-only ../svc-a ../svc-b     # only repo-spanning findings

SCORING:
  > 95%%  Exact clone       — extract shared utility, delete one
  > 85%%  Near clone        — virtually identical; treat as a clone unless intentional
  > 65%%  Strong clone      — parameterize differing parts
  > 45%%  Refactor target   — evaluate shared abstraction
  < 45%%  Weak similarity   — probably coincidental

  The Exact clone label additionally requires both snippets to span at
  least 10 non-blank lines; shorter pairs render as Near clones even at
  a perfect score (the score itself is unchanged).

  Pairs above 85%% whose raw identifier/string vocabulary barely
  overlaps (lexical < 20%%) render as Structural twins instead: same
  shape, different content — likely parallel boilerplate (table tests,
  per-field validators) rather than copy-paste. Labels only; the
  numeric score is never altered.

  Run 'codetwin --guide' for a full explanation of the score, the
  structural/semantic split, and how clusters differ from pairs.

`)
}
