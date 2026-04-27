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
	"strings"

	"github.com/ccsrvs/codetwin/internal/cluster"
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
	previewLines := flag.Int("preview-lines", 10, "number of lines to show in each preview")
	flag.Usage = usage
	flag.Parse()

	paths := flag.Args()
	if len(paths) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	files, err := collectFiles(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error collecting files: %v\n", err)
		os.Exit(1)
	}

	if len(files) < 2 {
		fmt.Fprintln(os.Stderr, "error: need at least 2 source files to compare")
		os.Exit(1)
	}

	type snippet struct {
		name      string
		lang      tokenizer.Language
		code      string
		startLine int
		tokens    []string
		fps       fingerprint.Set
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
			tokens := tokenizer.Tokenize(ch.Code, lang)
			if len(tokens) == 0 {
				continue
			}
			snippets = append(snippets, snippet{
				name:      chunkName(ch),
				lang:      lang,
				code:      ch.Code,
				startLine: ch.StartLine,
				tokens:    tokens,
				fps:       fingerprint.Generate(tokens, fingerprint.DefaultK, fingerprint.DefaultW),
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
			structural := fingerprint.Jaccard(snippets[i].fps, snippets[j].fps)
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
		clusters = append(clusters, report.Cluster{ID: id, Members: names})
	}

	var previews map[string]report.Preview
	if *preview {
		previews = make(map[string]report.Preview, len(snippets))
		for _, s := range snippets {
			previews[s.name] = report.Preview{
				StartLine: s.startLine,
				Text:      extractPreview(s.code, *previewLines),
			}
		}
	}

	opts := report.Options{
		Plain:     *plain,
		Threshold: *threshold,
		Verbose:   *verbose,
		Previews:  previews,
	}

	if *jsonOut {
		printJSON(pairs, clusters, previews, *threshold)
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
// string. Line numbers are preserved by the caller via the chunk's StartLine,
// so this function does not skip leading blanks.
func extractPreview(code string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(code, "\n")
	if n > len(lines) {
		n = len(lines)
	}
	return strings.Join(lines[:n], "\n")
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
}

type jsonPreview struct {
	StartLine int    `json:"start_line"`
	Text      string `json:"text"`
}

func printJSON(pairs []report.Pair, clusters []report.Cluster, previews map[string]report.Preview, threshold float64) {
	out := jsonOutput{}
	if len(previews) > 0 {
		out.Previews = make(map[string]jsonPreview, len(previews))
		for k, v := range previews {
			out.Previews[k] = jsonPreview{StartLine: v.StartLine, Text: v.Text}
		}
	}
	for _, p := range pairs {
		if p.Score < threshold {
			continue
		}
		out.Pairs = append(out.Pairs, jsonPair{
			FileA: p.NameA, FileB: p.NameB,
			Score: p.Score, Structural: p.Structural, Semantic: p.Semantic,
			Label: jsonLabel(p.Score),
		})
	}
	for _, c := range clusters {
		out.Clusters = append(out.Clusters, jsonCluster{ID: c.ID, Members: c.Members})
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

func collectFiles(paths []string) ([]string, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				if !d.IsDir() && supportedExts[filepath.Ext(path)] {
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else if supportedExts[filepath.Ext(p)] {
			files = append(files, p)
		}
	}
	return files, nil
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
  --preview-lines int  number of lines per preview (default 10)

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