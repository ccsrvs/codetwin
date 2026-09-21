package analyzer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/ccsrvs/codetwin/internal/deadcode"
	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/similarity"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

func TestRunSupportsTransformIgnoreDeadCodeAndCacheWarnings(t *testing.T) {
	dir := t.TempDir()
	a := writeAnalyzerFile(t, dir, "a.go", `package sample
func unusedAlpha() int { return 1 }
func Similar(values []int) int { total := 0; for _, value := range values { total += value }; return total }
`)
	b := writeAnalyzerFile(t, dir, "b.go", `package sample
func unusedBeta() int { return 2 }
func SimilarAgain(values []int) int { total := 0; for _, value := range values { total += value }; return total }
`)
	badCacheDir := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(badCacheDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ignored := 0
	result, err := (Analyzer{}).Run(context.Background(), Request{
		Files: []string{a, b}, MinLines: 1, Threshold: 0.3, Epsilon: 0.7,
		MinPoints: 2, MinConfidenceLines: 1, Granularity: GranularityFunction,
		DeadCode: true, DeadCodeLimit: 1, CacheDir: badCacheDir,
		CompiledStripPatterns: []*regexp.Regexp{regexp.MustCompile(`(?m)^// generated.*$`)},
		PatternIdentity:       []string{`(?m)^// generated.*$`},
		Transform: func(snippets []Snippet) {
			for i := range snippets {
				snippets[i].Repo = "repo"
				snippets[i].Name = "repo:" + snippets[i].Name
			}
		},
		IgnorePair: func(a, b string) bool {
			ignored++
			return true
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ignored == 0 || result.Stats.IgnoredPairs == 0 || len(result.Pairs) != 0 {
		t.Fatalf("ignore result: calls=%d stats=%#v pairs=%d", ignored, result.Stats, len(result.Pairs))
	}
	if len(result.DeadSymbols) != 1 {
		t.Fatalf("dead symbols = %d, want limit 1", len(result.DeadSymbols))
	}
	if len(result.Warnings) < 2 {
		t.Fatalf("warnings = %v, want cache load and save warnings", result.Warnings)
	}
}

func TestRunCancelsDuringScoring(t *testing.T) {
	dir := t.TempDir()
	files := make([]string, 300)
	for i := range files {
		files[i] = writeAnalyzerFile(t, dir, filepath.Base(filepath.Join(dir, formatIndex(i)+".go")),
			"package sample\nfunc Work"+formatIndex(i)+"(v int) int { return v + "+formatIndex(i)+" }\n")
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := (Analyzer{}).Run(ctx, Request{
		Files: files, MinLines: 1, Threshold: 0.3, Epsilon: 0.7,
		MinPoints: 2, MinConfidenceLines: 1, Granularity: GranularityFunction, NoCache: true,
		OnProgress: func(progress Progress) {
			if progress.Stage == StageScore {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
}

func TestPipelineHelpers(t *testing.T) {
	snippets := []scan.Snippet{
		{Name: "repo:a.go:1-5 A", Repo: "repo", IsTest: true},
		{Name: "repo:b.go:1-5 B", Repo: "repo", IsTest: true},
		{Name: "repo:c.go:1-5 C", Repo: "repo"},
	}
	graph := similarity.NewSparseGraph(3)
	graph.SetScore(0, 1, 0.9)
	pairs := []report.Pair{{NameA: snippets[0].Name, NameB: snippets[1].Name, RepoA: "repo", RepoB: "repo"}}
	kept, ignored := applyPairIgnores(pairs, graph, snippets, func(a, b string) bool {
		return a == "a.go:1-5 A" && b == "b.go:1-5 B"
	})
	if len(kept) != 0 || ignored != 1 || graph.Score(0, 1) != 0 {
		t.Fatalf("applyPairIgnores = %v, %d, score %.2f", kept, ignored, graph.Score(0, 1))
	}
	if got, n := applyPairIgnores(nil, graph, snippets, func(string, string) bool { return false }); len(got) != 0 || n != 0 {
		t.Fatalf("empty ignores = %v, %d", got, n)
	}
	kept, ignored = applyPairIgnores(pairs, graph, snippets, func(string, string) bool { return false })
	if len(kept) != 1 || ignored != 0 {
		t.Fatalf("kept ignores = %v, %d", kept, ignored)
	}
	if got := stripRepoPrefix("plain.go:1-2 A", ""); got != "plain.go:1-2 A" {
		t.Fatalf("stripRepoPrefix plain = %q", got)
	}

	graph.SetScore(0, 1, 0.8)
	graph.SetScore(1, 2, 0.2)
	graph.SetScore(0, 2, 0.1)
	clusters, err := buildReportClusters(context.Background(), map[int][]int{0: {0, 1, 2}}, graph, snippets, 0.7)
	if err != nil || len(clusters) != 1 || len(clusters[0].Members) != 2 || len(clusters[0].MemberRepos) != 2 {
		t.Fatalf("buildReportClusters = %#v, %v", clusters, err)
	}
	markTestOnlyClusters(clusters, snippets)
	if !clusters[0].TestOnly {
		t.Fatal("two-test cluster was not marked test-only")
	}
	if avg, min := clusterStats([]int{0}, graph); avg != 0 || min != 0 {
		t.Fatalf("singleton stats = %.2f, %.2f", avg, min)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := buildReportClusters(canceled, map[int][]int{0: {0, 1}}, graph, snippets, 0.7); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled clusters error = %v", err)
	}
	ordered, err := buildReportClusters(context.Background(), map[int][]int{0: {1, 2}, 1: {0, 1}}, graph, snippets, 0)
	if err != nil || len(ordered) != 2 || ordered[0].Members[0] != snippets[0].Name {
		t.Fatalf("ordered clusters = %#v, %v", ordered, err)
	}
}

func TestBlockHelpersAndCancellation(t *testing.T) {
	shared := `
	if cfg == nil { return errNil }
	total := 0
	for _, item := range cfg.Items {
		if item.Count < 0 { return errNegative }
		total += item.Count
	}
	if total > cfg.Max { return errOverflow }
	return nil
`
	a := analyzerSnippet(t, "repo:a.go:1-20 HostA", "func HostA(cfg *Config) error {\n"+shared+"\n}")
	b := analyzerSnippet(t, "repo:b.go:4-24 HostB", "func HostB(cfg *Config) error {\nlog.Start()\n"+shared+"\n}")
	a.Repo, b.Repo = "repo", "repo"
	clones, err := detectBlockClones(context.Background(), [][2]int{{0, 1}}, []scan.Snippet{a, b}, 5, nil, func(Progress) {})
	if err != nil || len(clones) == 0 {
		t.Fatalf("detectBlockClones = %#v, %v", clones, err)
	}
	if clones[0].FileA != "repo:a.go" || clones[0].SymbolA != "HostA" {
		t.Fatalf("block identity = %#v", clones[0])
	}
	if file, symbol := splitChunkName("plain.go"); file != "plain.go" || symbol != "" {
		t.Fatalf("split plain = %q, %q", file, symbol)
	}
	if got := dedupeBlockClones([]report.BlockClone{
		{FileA: "a", FileB: "b", AStartLine: 1, AEndLine: 10, BStartLine: 1, BEndLine: 10, Containment: 1},
		{FileA: "a", FileB: "b", AStartLine: 2, AEndLine: 9, BStartLine: 2, BEndLine: 9, Containment: .9},
	}); len(got) != 1 {
		t.Fatalf("dedupe len = %d", len(got))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := detectBlockClones(ctx, [][2]int{{0, 1}}, []scan.Snippet{a, b}, 5, nil, func(Progress) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled blocks error = %v", err)
	}
	if got, err := detectBlockClones(context.Background(), nil, nil, 0, nil, func(Progress) {}); err != nil || got != nil {
		t.Fatalf("disabled blocks = %v, %v", got, err)
	}
	if got, err := detectBlockClones(context.Background(), [][2]int{{0, 1}}, []scan.Snippet{a, b}, 5,
		func(string, string) bool { return true }, func(Progress) {}); err != nil || len(got) != 0 {
		t.Fatalf("ignored blocks = %v, %v", got, err)
	}
}

func TestMakeDeadSymbols(t *testing.T) {
	findings := []deadcode.Finding{
		{Name: "a.go:1-2 unused", Symbol: "unused", Kind: splitter.KindFunction, Lang: tokenizer.Go, Exported: false, Verdict: deadcode.VerdictDead, TestRefs: 2},
		{Name: "b.go:1-2 Exported", Symbol: "Exported", Kind: splitter.KindFunction, Lang: tokenizer.Go, Exported: true, Verdict: deadcode.VerdictUnusedInScan},
	}
	got := makeDeadSymbols(findings, 1)
	if len(got) != 1 || got[0].Verdict != "dead" || got[0].Lang != "go" || got[0].TestRefs != 2 {
		t.Fatalf("makeDeadSymbols = %#v", got)
	}
}

func analyzerSnippet(t *testing.T, name, code string) scan.Snippet {
	t.Helper()
	tokens, lines := tokenizer.TokenizeWithLines(code, tokenizer.Go)
	return scan.Snippet{
		Name: name, Path: name, Code: code, StartLine: 1, EndLine: 100,
		Lang: tokenizer.Go, Tokens: tokens, Lines: lines,
		Fps: fingerprint.GeneratePositional(tokens, fingerprint.DefaultK, fingerprint.DefaultW),
	}
}

func writeAnalyzerFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func formatIndex(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return string(digits[i])
	}
	return formatIndex(i/10) + string(digits[i%10])
}
