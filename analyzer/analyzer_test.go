package analyzer_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/analyzer"
	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/cluster"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/similarity"
)

func TestRunMatchesLegacyPipeline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := []string{
		writeSource(t, dir, "a.go", `package sample
func Total(values []int) int {
	total := 0
	for _, value := range values { total += value }
	return total
}`),
		writeSource(t, dir, "b.go", `package sample
func Sum(numbers []int) int {
	result := 0
	for _, number := range numbers { result += number }
	return result
}`),
		writeSource(t, dir, "c.go", `package sample
func Greeting(name string) string { return "hello " + name }
`),
	}

	request := analyzer.Request{
		Files: files, MinLines: 1, Threshold: 0.5, Epsilon: 0.35,
		MinPoints: 2, MinConfidenceLines: similarity.DefaultMinConfidenceLines,
		Granularity: analyzer.GranularityFunction, NoCache: true,
	}
	got, err := (analyzer.Analyzer{}).Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantSnippets, warnings := scan.ProcessFiles(
		files, request.MinLines, nil, cache.New(), "", scan.GranularityFunction, nil,
	)
	if len(warnings) != 0 {
		t.Fatalf("legacy scan warnings: %v", warnings)
	}
	sort.Slice(wantSnippets, func(i, j int) bool { return wantSnippets[i].Name < wantSnippets[j].Name })
	streams := make([][]string, len(wantSnippets))
	for i := range wantSnippets {
		streams[i] = wantSnippets[i].Tokens
	}
	corpus := similarity.NewCorpus(streams)
	vectors := make([]similarity.NormalizedVector, len(wantSnippets))
	for i := range wantSnippets {
		vectors[i] = similarity.Normalize(corpus.Vectorize(wantSnippets[i].Tokens))
	}
	graph, wantPairs, _ := similarity.BuildGraph(
		wantSnippets, vectors, request.MinConfidenceLines, request.Threshold, nil,
	)
	for i := range wantPairs {
		wantPairs[i].IsTestA = wantSnippets[iForName(wantSnippets, wantPairs[i].NameA)].IsTest
		wantPairs[i].IsTestB = wantSnippets[iForName(wantSnippets, wantPairs[i].NameB)].IsTest
	}
	wantClusterResult := cluster.DBSCAN(len(wantSnippets), request.Epsilon, request.MinPoints,
		func(i, j int) float64 { return 1 - graph.Score(i, j) })

	if !reflect.DeepEqual(got.Snippets, wantSnippets) {
		t.Fatal("Analyzer snippets differ from the characterized pipeline")
	}
	if !reflect.DeepEqual(got.Pairs, wantPairs) {
		t.Fatalf("Analyzer pairs differ from the characterized pipeline\ngot:  %#v\nwant: %#v", got.Pairs, wantPairs)
	}
	if got.Stats.Snippets != len(wantSnippets) || got.Stats.TotalPairs != 3 {
		t.Fatalf("Stats = %#v", got.Stats)
	}
	if len(cluster.Groups(wantClusterResult)) != len(got.Clusters) {
		t.Fatalf("cluster count = %d, want %d", len(got.Clusters), len(cluster.Groups(wantClusterResult)))
	}
	_ = report.Pair{} // keep the comparison anchored to the report contract
}

func TestRunReturnsPreCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (analyzer.Analyzer{}).Run(ctx, analyzer.Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
}

func TestRunCancelsDuringScan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var files []string
	for i := 0; i < 20; i++ {
		files = append(files, writeSource(t, dir, string(rune('a'+i))+".go", `package sample
func Work() int { return 1 }
`))
	}
	ctx, cancel := context.WithCancel(context.Background())
	completed := 0
	_, err := (analyzer.Analyzer{}).Run(ctx, analyzer.Request{
		Files: files, MinLines: 1, Threshold: 0.5, Epsilon: 0.35,
		MinPoints: 2, MinConfidenceLines: 1, Granularity: analyzer.GranularityFunction,
		NoCache: true,
		OnProgress: func(p analyzer.Progress) {
			if p.Stage == analyzer.StageScan {
				completed = p.Completed
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	if completed == 0 || completed >= len(files) {
		t.Fatalf("scan completed %d/%d files before cancellation", completed, len(files))
	}
}

func TestRunRejectsInvalidRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		req  analyzer.Request
	}{
		{name: "no files", req: analyzer.Request{Granularity: analyzer.GranularityFunction}},
		{name: "bad granularity", req: analyzer.Request{Files: []string{"x"}, Granularity: "line"}},
		{name: "bad strip pattern", req: analyzer.Request{Files: []string{"x"}, Granularity: analyzer.GranularityFunction, StripPatterns: []string{"["}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (analyzer.Analyzer{}).Run(context.Background(), tt.req); err == nil {
				t.Fatal("Run error = nil")
			}
		})
	}
}

func TestRunRejectsInsufficientInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	one := writeSource(t, dir, "one.go", "package sample\nfunc One() int { return 1 }\n")
	_, err := (analyzer.Analyzer{}).Run(context.Background(), analyzer.Request{
		Files: []string{one}, MinLines: 1, Granularity: analyzer.GranularityFunction, NoCache: true,
	})
	if err == nil {
		t.Fatal("single-snippet error = nil")
	}
}

func TestRunUsesStringPatternsAndWarmCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := []string{
		writeSource(t, dir, "a.go", "package sample\n// generated omit\nfunc Same() int { return 1 }\n"),
		writeSource(t, dir, "b.go", "package sample\n// generated omit\nfunc SameAgain() int { return 1 }\n"),
	}
	req := analyzer.Request{
		Files: files, MinLines: 1, Threshold: .3, Epsilon: .7, MinPoints: 2,
		MinConfidenceLines: 1, Granularity: analyzer.GranularityFunction,
		StripPatterns: []string{`(?m)^// generated.*$`}, CacheDir: dir,
	}
	if _, err := (analyzer.Analyzer{}).Run(context.Background(), req); err != nil {
		t.Fatalf("cold Run: %v", err)
	}
	warm, err := (analyzer.Analyzer{}).Run(context.Background(), req)
	if err != nil {
		t.Fatalf("warm Run: %v", err)
	}
	if len(warm.Warnings) != 0 {
		t.Fatalf("warm cache warnings = %v", warm.Warnings)
	}
}

func TestRunHonorsCancellationAfterTransform(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := []string{
		writeSource(t, dir, "a.go", "package sample\nfunc A() int { return 1 }\n"),
		writeSource(t, dir, "b.go", "package sample\nfunc B() int { return 2 }\n"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := (analyzer.Analyzer{}).Run(ctx, analyzer.Request{
		Files: files, MinLines: 1, Granularity: analyzer.GranularityFunction,
		DeadCode: true, NoCache: true,
		Transform: func([]analyzer.Snippet) { cancel() },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
}

func writeSource(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func iForName(snippets []scan.Snippet, name string) int {
	for i := range snippets {
		if snippets[i].Name == name {
			return i
		}
	}
	return -1
}

// A caller passing only compiled strip patterns (no PatternIdentity)
// must not be served tokens cached by a run without those patterns.
func TestRunCompiledPatternsDoNotReuseUnpatternedCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := "package sample\nfunc Same() int {\n\tlog.Debug(\"x\")\n\tlog.Debug(\"y\")\n\treturn 1\n}\n"
	files := []string{
		writeSource(t, dir, "a.go", body),
		writeSource(t, dir, "b.go", strings.ReplaceAll(body, "Same", "Other")),
	}
	base := analyzer.Request{
		Files: files, MinLines: 1, Threshold: .1, Epsilon: .7, MinPoints: 2,
		MinConfidenceLines: 1, Granularity: analyzer.GranularityFunction, CacheDir: dir,
	}
	if _, err := (analyzer.Analyzer{}).Run(context.Background(), base); err != nil {
		t.Fatalf("unpatterned Run: %v", err)
	}

	patterned := base
	patterned.CompiledStripPatterns = []*regexp.Regexp{regexp.MustCompile(`log\.Debug\([^)]*\)`)}
	warm, err := (analyzer.Analyzer{}).Run(context.Background(), patterned)
	if err != nil {
		t.Fatalf("patterned Run: %v", err)
	}
	patterned.NoCache = true
	cold, err := (analyzer.Analyzer{}).Run(context.Background(), patterned)
	if err != nil {
		t.Fatalf("uncached patterned Run: %v", err)
	}
	for i := range cold.Snippets {
		if !reflect.DeepEqual(warm.Snippets[i].Tokens, cold.Snippets[i].Tokens) {
			t.Fatalf("snippet %s: cached tokens %v, uncached %v", cold.Snippets[i].Name, warm.Snippets[i].Tokens, cold.Snippets[i].Tokens)
		}
	}
}
