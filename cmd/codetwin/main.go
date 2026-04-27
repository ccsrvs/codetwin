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
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ccsrvs/codetwin/internal/cluster"
	"github.com/ccsrvs/codetwin/internal/config"
	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/similarity"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

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
	flag.Usage = usage
	flag.Parse()

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
			preview, previewLines, sortMode, limit)
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

	paths := flag.Args()
	if len(paths) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	files, err := collectFiles(paths, ignoreMatcher)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error collecting files: %v\n", err)
		os.Exit(1)
	}

	if len(files) < 2 {
		fmt.Fprintln(os.Stderr, "error: need at least 2 source files to compare")
		os.Exit(1)
	}

	type snippet struct {
		name       string
		lang       tokenizer.Language
		code       string
		startLine  int
		nonBlankLn int // non-blank line count of the chunk (used for size sorts)
		tokens     []string
		lines      []int // parallel to tokens; 1-based source line of each token, relative to chunk start
		fps        fingerprint.PositionalSet
	}

	snippets := make([]snippet, 0, len(files))
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not read %s: %v\n", path, err)
			continue
		}
		code := string(data)
		lang := tokenizer.Detect(path, code)
		for _, ch := range splitter.Split(path, code, lang) {
			if countLines(ch.Code) < *minLines {
				continue
			}
			tokens, lines := tokenizer.TokenizeWithLines(ch.Code, lang, tokenizer.WithStripPatterns(stripPatterns))
			if len(tokens) == 0 {
				continue
			}
			snippets = append(snippets, snippet{
				name:       chunkName(ch),
				lang:       lang,
				code:       ch.Code,
				startLine:  ch.StartLine,
				nonBlankLn: countLines(ch.Code),
				tokens:     tokens,
				lines:      lines,
				fps:        fingerprint.GeneratePositional(tokens, fingerprint.DefaultK, fingerprint.DefaultW),
			})
		}
	}

	if len(snippets) < 2 {
		fmt.Fprintln(os.Stderr, "error: not enough parseable snippets to compare")
		os.Exit(1)
	}

	tokenStreams := make([][]string, len(snippets))
	for i, s := range snippets {
		tokenStreams[i] = s.tokens
	}
	corpus := similarity.NewCorpus(tokenStreams)

	vectors := make([]similarity.Vector, len(snippets))
	for i, s := range snippets {
		vectors[i] = corpus.Vectorize(s.tokens)
	}

	n := len(snippets)
	matrix := make([][]float64, n)
	for i := range matrix {
		matrix[i] = make([]float64, n)
		matrix[i][i] = 1.0
	}

	var pairs []report.Pair
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			structural := fingerprint.Jaccard(snippets[i].fps.Set, snippets[j].fps.Set)
			semantic := similarity.Cosine(vectors[i], vectors[j])
			combined := similarity.Combined(structural, semantic, 0.5)
			matrix[i][j] = combined
			matrix[j][i] = combined

			pairs = append(pairs, report.Pair{
				NameA:      snippets[i].name,
				NameB:      snippets[j].name,
				Structural: structural,
				Semantic:   semantic,
				Score:      combined,
				LinesA:     snippets[i].nonBlankLn,
				LinesB:     snippets[j].nonBlankLn,
			})
		}
	}

	distFn := func(i, j int) float64 { return 1.0 - matrix[i][j] }
	clusterResult := cluster.DBSCAN(n, *eps, *minPts, distFn)
	groups := cluster.Groups(clusterResult)

	clusters := make([]report.Cluster, 0, len(groups))
	for id, members := range groups {
		names := make([]string, len(members))
		for k, idx := range members {
			names[k] = snippets[idx].name
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

	var previews map[string]report.Preview
	if *preview {
		nameIdx := make(map[string]int, len(snippets))
		for i, s := range snippets {
			nameIdx[s.name] = i
		}

		// Determine which snippets will appear in the rendered report.
		shown := make(map[int]bool)
		for _, p := range pairs {
			if p.Score >= *threshold {
				shown[nameIdx[p.NameA]] = true
				shown[nameIdx[p.NameB]] = true
			}
		}
		for _, c := range clusters {
			for _, m := range c.Members {
				shown[nameIdx[m]] = true
			}
		}

		// For each shown snippet, find the bounding token range covered by
		// fingerprints shared with any OTHER shown snippet. Falls back to
		// (-1, -1) when no positional match exists, in which case we render
		// the chunk's leading lines like before.
		type rng struct{ first, last int }
		ranges := make(map[int]rng, len(shown))
		for i := range shown {
			for j := range shown {
				if i == j {
					continue
				}
				f, l := fingerprint.MatchRange(snippets[i].fps, snippets[j].fps)
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

		previews = make(map[string]report.Preview, len(shown))
		for i := range shown {
			s := snippets[i]
			if r, ok := ranges[i]; ok {
				previews[s.name] = buildMatchPreview(s.code, s.lines, s.startLine, r.first, r.last, s.fps.K, *previewLines)
			} else {
				// No structural overlap — fall back to a leading excerpt.
				previews[s.name] = report.Preview{
					StartLine: s.startLine,
					Text:      extractPreview(s.code, *previewLines),
				}
			}
		}
	}

	opts := report.Options{
		Plain:     *plain,
		Threshold: *threshold,
		Verbose:   *verbose,
		Sort:      report.SortMode(*sortMode),
		Limit:     *limit,
		Previews:  previews,
	}

	if *jsonOut {
		visiblePairs, visibleClusters := report.Prepare(pairs, clusters, opts)
		printJSON(visiblePairs, visibleClusters, previews)
		return
	}

	report.Render(os.Stdout, pairs, clusters, opts)
}

// chunkName produces a unique, human-readable identifier for a chunk. The
// format is "path:start-end Symbol" when the symbol is known, "path:start-end"
// when it isn't, and just "path" for whole-file fallback chunks (those have
// no symbol and start at line 1).
func chunkName(ch splitter.Chunk) string {
	if ch.Symbol == "" && ch.StartLine == 1 {
		return ch.Path
	}
	if ch.Symbol != "" {
		return fmt.Sprintf("%s:%d-%d %s", ch.Path, ch.StartLine, ch.EndLine, ch.Symbol)
	}
	return fmt.Sprintf("%s:%d-%d", ch.Path, ch.StartLine, ch.EndLine)
}

// extractPreview returns the first n lines of code as a single newline-joined
// string. When n <= 0 the entire code is returned (unlimited mode). Line
// numbers are preserved by the caller via the chunk's StartLine, so this
// function does not skip leading blanks.
func extractPreview(code string, n int) string {
	lines := strings.Split(code, "\n")
	if n <= 0 || n > len(lines) {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:n], "\n")
}

// buildMatchPreview returns a Preview focused on the line range covered by
// [firstTok, lastTok], extending the last token by k-1 to cover the full
// k-gram. Behavior by maxLines:
//
//	maxLines == 0:          show the whole chunk (unlimited)
//	chunk lines <= maxLines: show the whole chunk (it fits)
//	otherwise:              focus on the match range, taking up to maxLines
//	                        lines starting at the first matching line
func buildMatchPreview(code string, tokenLines []int, chunkStartLine, firstTok, lastTok, k, maxLines int) report.Preview {
	chunkLines := strings.Split(code, "\n")
	if maxLines <= 0 || len(chunkLines) <= maxLines {
		return report.Preview{
			StartLine: chunkStartLine,
			Text:      strings.Join(chunkLines, "\n"),
		}
	}

	if firstTok < 0 || firstTok >= len(tokenLines) {
		return report.Preview{
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

	return report.Preview{
		StartLine: chunkStartLine + chunkFirstLine - 1,
		Text:      strings.Join(selected, "\n"),
	}
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
			Label: jsonLabel(p.Score),
		})
	}
	for _, c := range clusters {
		out.Clusters = append(out.Clusters, jsonCluster{ID: c.ID, Members: c.Members, Score: c.Score})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}

func jsonLabel(score float64) string {
	switch {
	case score > 0.85:
		return "exact_clone"
	case score > 0.65:
		return "strong_clone"
	case score > 0.45:
		return "refactor_candidate"
	default:
		return "weak_similarity"
	}
}

// ── File collection ───────────────────────────────────────────────────────────

func collectFiles(paths []string, ignore *config.IgnoreMatcher) ([]string, error) {
	deduped, err := dedupePaths(paths)
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

// dedupePaths removes duplicate inputs and inputs that are contained within
// another input on the list. Given ./src and ./src/utils, only ./src is
// returned because walking it will already cover ./src/utils. Identical
// paths (after canonicalization) are also collapsed.
//
// Order from the input slice is preserved for the survivors so users see
// their first-mentioned form in error messages and progress.
func dedupePaths(paths []string) ([]string, error) {
	type entry struct {
		original string
		abs      string
	}
	var entries []entry
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		abs = filepath.Clean(abs)
		if seen[abs] {
			continue
		}
		seen[abs] = true
		entries = append(entries, entry{original: p, abs: abs})
	}

	out := make([]string, 0, len(entries))
	for _, ai := range entries {
		contained := false
		for _, aj := range entries {
			if aj.abs == ai.abs {
				continue
			}
			if pathContains(aj.abs, ai.abs) {
				contained = true
				break
			}
		}
		if !contained {
			out = append(out, ai.original)
		}
	}
	return out, nil
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

// pathContains reports whether `child` lives inside the directory tree
// rooted at `parent`. Both paths must be absolute and clean. The check is
// purely lexical (no filesystem access): parent must be a strict prefix of
// child with the next character being a separator, so /foo does not match
// /foobar.
func pathContains(parent, child string) bool {
	if !strings.HasPrefix(child, parent) {
		return false
	}
	rest := child[len(parent):]
	return len(rest) > 0 && rest[0] == filepath.Separator
}

func countLines(code string) int {
	n := 0
	for _, line := range strings.Split(code, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
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

EXAMPLES:
  codetwin ./src
  codetwin --threshold 0.6 ./pkg
  codetwin --plain ./src > report.txt
  codetwin --json ./src | jq '.pairs[] | select(.score > 0.8)'
  codetwin ./utils/a.go ./utils/b.go

SCORING:
  > 85%%  Exact clone       — extract shared utility, delete one
  > 65%%  Strong clone      — parameterize differing parts
  > 45%%  Refactor target   — evaluate shared abstraction
  < 45%%  Weak similarity   — probably coincidental

`)
}