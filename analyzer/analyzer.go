// Package analyzer provides CodeTwin's reusable, cancellable analysis API.
// It owns the compute pipeline; command-line parsing and rendering remain
// adapters around this package.
package analyzer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ccsrvs/codetwin/internal/blocks"
	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/cluster"
	"github.com/ccsrvs/codetwin/internal/deadcode"
	"github.com/ccsrvs/codetwin/internal/paircache"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/similarity"
)

type Granularity = scan.Granularity

const (
	GranularityFunction = scan.GranularityFunction
	GranularityFile     = scan.GranularityFile
)

type Snippet = scan.Snippet
type Pair = report.Pair
type Cluster = report.Cluster
type BlockClone = report.BlockClone
type DeadSymbol = report.DeadSymbol

type Stage string

const (
	StageScan    Stage = "scan"
	StageScore   Stage = "score"
	StageBlocks  Stage = "blocks"
	StageCluster Stage = "cluster"
)

type Progress struct {
	Stage     Stage
	Completed int
	Total     int
}

// Request describes one analysis without binding callers to CLI flags or
// process-global state. Files are explicit so IDEs, daemons, and agents can
// supply their own discovery policy.
type Request struct {
	Files              []string
	MinLines           int
	Threshold          float64
	Epsilon            float64
	MinPoints          int
	MinConfidenceLines int
	MinBlockLines      int
	Granularity        Granularity
	IncludeWeakPairs   bool
	DeadCode           bool
	DeadCodeLimit      int

	StripPatterns []string
	// CompiledStripPatterns lets adapters reuse patterns they already parsed.
	// It is combined with StripPatterns; PatternIdentity may preserve the
	// adapter's original cache identity when only compiled patterns are passed.
	CompiledStripPatterns []*regexp.Regexp
	PatternIdentity       []string
	IgnorePair            func(nameA, nameB string) bool
	Transform             func([]Snippet)

	NoCache      bool
	RebuildCache bool
	CacheDir     string
	OnProgress   func(Progress)
}

type Stats struct {
	Files          int
	Snippets       int
	TotalPairs     int64
	CandidatePairs int64
	CacheHits      int64
	Recomputed     int64
	IgnoredPairs   int
}

type Result struct {
	Snippets      []Snippet
	Pairs         []Pair
	Clusters      []Cluster
	PartialClones []BlockClone
	DeadSymbols   []DeadSymbol
	Warnings      []string
	Stats         Stats
}

// Analyzer is stateless and safe for concurrent use. Persistent cache state
// is loaded per Run from Request.CacheDir.
type Analyzer struct{}

func (Analyzer) Run(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("analyzer: nil context")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(req.Files) == 0 {
		return Result{}, errors.New("analyzer: no files to analyze")
	}
	if _, err := scan.ParseGranularity(string(req.Granularity)); err != nil {
		return Result{}, fmt.Errorf("analyzer: %w", err)
	}

	patterns := append([]*regexp.Regexp(nil), req.CompiledStripPatterns...)
	for _, pattern := range req.StripPatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return Result{}, fmt.Errorf("analyzer: invalid strip pattern %q: %w", pattern, err)
		}
		patterns = append(patterns, re)
	}

	cacheDir := req.CacheDir
	if cacheDir == "" {
		cacheDir = "."
	}
	storage := cache.NewGobStorage(cacheDir)
	cacheState := cache.New()
	var warnings []string
	if !req.NoCache && !req.RebuildCache {
		loaded, err := storage.Load()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("cache load failed: %v", err))
		} else {
			cacheState = loaded
		}
	}

	reportProgress := serializedProgress(req.OnProgress)
	var filesDone atomic.Int64
	patternIdentity := req.PatternIdentity
	if patternIdentity == nil {
		patternIdentity = req.StripPatterns
	}
	scanMinLines := req.MinLines
	if req.DeadCode {
		scanMinLines = 1
	}
	snippets, scanWarnings, err := scan.ProcessFilesContext(
		ctx, req.Files, scanMinLines, patterns, cacheState,
		cache.PatternsHash(patternIdentity), req.Granularity,
		func() {
			done := filesDone.Add(1)
			reportProgress(Progress{Stage: StageScan, Completed: int(done), Total: len(req.Files)})
		},
	)
	if err != nil {
		return Result{}, err
	}
	warnings = append(warnings, scanWarnings...)
	if req.Transform != nil {
		req.Transform(snippets)
	}
	sort.Slice(snippets, func(i, j int) bool { return snippets[i].Name < snippets[j].Name })
	if len(snippets) < 2 && !req.DeadCode {
		return Result{}, errors.New("analyzer: not enough parseable snippets to compare")
	}

	var deadSymbols []report.DeadSymbol
	if req.DeadCode {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		findings, deadWarnings := deadcode.Analyze(snippets, req.Files...)
		warnings = append(warnings, deadWarnings...)
		deadSymbols = makeDeadSymbols(findings, req.DeadCodeLimit)
		kept := snippets[:0]
		for _, snippet := range snippets {
			if snippet.NonBlankLn >= req.MinLines {
				kept = append(kept, snippet)
			}
		}
		snippets = kept
	}

	streams := make([][]string, len(snippets))
	for i := range snippets {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		streams[i] = snippets[i].Tokens
	}
	corpus := similarity.NewCorpus(streams)
	vectors := make([]similarity.NormalizedVector, len(snippets))
	for i := range snippets {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		vectors[i] = similarity.Normalize(corpus.Vectorize(snippets[i].Tokens))
	}

	totalPairs := int64(len(snippets)) * int64(len(snippets)-1) / 2
	stats := Stats{Files: len(req.Files), Snippets: len(snippets), TotalPairs: totalPairs}
	var scoreStore paircache.Store
	if !req.NoCache {
		scoreStore = cacheState
	}
	graph, pairs, blockCandidates, err := similarity.BuildGraphContext(
		ctx, snippets, vectors, req.MinConfidenceLines, req.Threshold,
		func(done, total int64) {
			reportProgress(Progress{Stage: StageScore, Completed: int(done), Total: int(total)})
		},
		similarity.MatrixOptions{
			IncludeWeakPairs: req.IncludeWeakPairs,
			ScoreCache:       scoreStore,
			OnCandidates: func(selected, _ int64) {
				stats.CandidatePairs = selected
			},
			OnScoreCache: func(hits, misses int64) {
				stats.CacheHits, stats.Recomputed = hits, misses
			},
		},
	)
	if err != nil {
		return Result{}, err
	}

	if !req.NoCache {
		if err := storage.Save(cacheState); err != nil {
			warnings = append(warnings, fmt.Sprintf("cache save failed: %v", err))
		}
	}
	markTestPairs(pairs, snippets)
	if req.IgnorePair != nil {
		pairs, stats.IgnoredPairs = applyPairIgnores(pairs, graph, snippets, req.IgnorePair)
	}

	partialClones, err := detectBlockClones(
		ctx, blockCandidates, snippets, req.MinBlockLines, req.IgnorePair, reportProgress,
	)
	if err != nil {
		return Result{}, err
	}

	clusterResult, err := cluster.DBSCANContext(
		ctx, len(snippets), req.Epsilon, req.MinPoints,
		func(i, j int) float64 { return 1 - graph.Score(i, j) },
	)
	if err != nil {
		return Result{}, err
	}
	reportProgress(Progress{Stage: StageCluster, Completed: len(snippets), Total: len(snippets)})
	clusters, err := buildReportClusters(ctx, cluster.Groups(clusterResult), graph, snippets, req.Threshold)
	if err != nil {
		return Result{}, err
	}
	markTestOnlyClusters(clusters, snippets)

	return Result{
		Snippets: snippets, Pairs: pairs, Clusters: clusters,
		PartialClones: partialClones, DeadSymbols: deadSymbols,
		Warnings: warnings, Stats: stats,
	}, nil
}

func serializedProgress(callback func(Progress)) func(Progress) {
	if callback == nil {
		return func(Progress) {}
	}
	var mu sync.Mutex
	return func(progress Progress) {
		mu.Lock()
		defer mu.Unlock()
		callback(progress)
	}
}

func markTestPairs(pairs []report.Pair, snippets []scan.Snippet) {
	tests := make(map[string]bool, len(snippets))
	for _, snippet := range snippets {
		tests[snippet.Name] = snippet.IsTest
	}
	for i := range pairs {
		pairs[i].IsTestA = tests[pairs[i].NameA]
		pairs[i].IsTestB = tests[pairs[i].NameB]
	}
}

func applyPairIgnores(
	pairs []report.Pair,
	graph similarity.MutableGraph,
	snippets []scan.Snippet,
	ignore func(string, string) bool,
) ([]report.Pair, int) {
	indices := make(map[string]int, len(snippets))
	for i, snippet := range snippets {
		indices[snippet.Name] = i
	}
	kept := make([]report.Pair, 0, len(pairs))
	ignored := 0
	for _, pair := range pairs {
		a, b := stripRepoPrefix(pair.NameA, pair.RepoA), stripRepoPrefix(pair.NameB, pair.RepoB)
		if !ignore(a, b) {
			kept = append(kept, pair)
			continue
		}
		ignored++
		i, okI := indices[pair.NameA]
		j, okJ := indices[pair.NameB]
		if okI && okJ {
			graph.SetScore(i, j, 0)
		}
	}
	return kept, ignored
}

var chunkNameRE = regexp.MustCompile(`^(.*):(\d+)-(\d+)(?: (.+))?$`)

func detectBlockClones(
	ctx context.Context,
	candidates [][2]int,
	snippets []scan.Snippet,
	minLines int,
	ignore func(string, string) bool,
	progress func(Progress),
) ([]report.BlockClone, error) {
	if minLines <= 0 || len(candidates) == 0 {
		return nil, nil
	}
	var out []report.BlockClone
	for i, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a, b := snippets[candidate[0]], snippets[candidate[1]]
		if ignore != nil && ignore(stripRepoPrefix(a.Name, a.Repo), stripRepoPrefix(b.Name, b.Repo)) {
			continue
		}
		fileA, symbolA := splitChunkName(a.Name)
		fileB, symbolB := splitChunkName(b.Name)
		for _, match := range blocks.Detect(a, b, minLines) {
			clone := report.BlockClone{
				FileA: fileA, SymbolA: symbolA, PathA: a.Path, ChunkA: a.Name,
				AStartLine: match.AStartLine, AEndLine: match.AEndLine,
				FileB: fileB, SymbolB: symbolB, PathB: b.Path, ChunkB: b.Name,
				BStartLine: match.BStartLine, BEndLine: match.BEndLine,
				Containment: match.Containment, LinesA: match.ALines, LinesB: match.BLines,
				IsTestA: a.IsTest, IsTestB: b.IsTest, RepoA: a.Repo, RepoB: b.Repo,
			}
			clone.ID = report.PairID(clone.RangeNameA(), clone.RangeNameB())
			out = append(out, clone)
		}
		progress(Progress{Stage: StageBlocks, Completed: i + 1, Total: len(candidates)})
	}
	return dedupeBlockClones(out), nil
}

func splitChunkName(name string) (string, string) {
	match := chunkNameRE.FindStringSubmatch(name)
	if match == nil {
		return name, ""
	}
	return match[1], match[4]
}

func dedupeBlockClones(clones []report.BlockClone) []report.BlockClone {
	sort.SliceStable(clones, func(i, j int) bool { return report.BlockLess(clones[i], clones[j]) })
	kept := clones[:0:0]
	for _, clone := range clones {
		duplicate := false
		for _, existing := range kept {
			if existing.FileA == clone.FileA && existing.FileB == clone.FileB &&
				overlaps(existing.AStartLine, existing.AEndLine, clone.AStartLine, clone.AEndLine) &&
				overlaps(existing.BStartLine, existing.BEndLine, clone.BStartLine, clone.BEndLine) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			kept = append(kept, clone)
		}
	}
	return kept
}

func overlaps(aStart, aEnd, bStart, bEnd int) bool {
	return aStart <= bEnd && aEnd >= bStart
}

func buildReportClusters(
	ctx context.Context,
	groups map[int][]int,
	graph similarity.Graph,
	snippets []scan.Snippet,
	threshold float64,
) ([]report.Cluster, error) {
	memberLists := make([][]int, 0, len(groups))
	for _, members := range groups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, min := clusterStats(members, graph)
		if min >= threshold {
			memberLists = append(memberLists, members)
			continue
		}
		for _, component := range cluster.Components(members, func(a, b int) bool {
			return graph.Score(a, b) >= threshold
		}) {
			if len(component) >= 2 {
				memberLists = append(memberLists, component)
			}
		}
	}
	clusters := make([]report.Cluster, 0, len(memberLists))
	for _, members := range memberLists {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		avg, min := clusterStats(members, graph)
		names := make([]string, len(members))
		var repos []string
		multiRepo := false
		for _, snippet := range snippets {
			if snippet.Repo != "" {
				multiRepo = true
				break
			}
		}
		if multiRepo {
			repos = make([]string, len(members))
		}
		for i, index := range members {
			names[i] = snippets[index].Name
			if repos != nil {
				repos[i] = snippets[index].Repo
			}
		}
		clusters = append(clusters, report.Cluster{Members: names, MemberRepos: repos, Score: avg, MinScore: min})
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].Members[0] < clusters[j].Members[0] })
	for i := range clusters {
		clusters[i].ID = i
	}
	return clusters, nil
}

func clusterStats(members []int, graph similarity.Graph) (float64, float64) {
	if len(members) < 2 {
		return 0, 0
	}
	min, sum, count := 1.0, 0.0, 0
	for i := 0; i < len(members); i++ {
		for j := i + 1; j < len(members); j++ {
			score := graph.Score(members[i], members[j])
			sum += score
			if score < min {
				min = score
			}
			count++
		}
	}
	return sum / float64(count), min
}

func markTestOnlyClusters(clusters []report.Cluster, snippets []scan.Snippet) {
	tests := make(map[string]bool, len(snippets))
	for _, snippet := range snippets {
		tests[snippet.Name] = snippet.IsTest
	}
	for i := range clusters {
		allTests := len(clusters[i].Members) > 0
		for _, member := range clusters[i].Members {
			if !tests[member] {
				allTests = false
				break
			}
		}
		clusters[i].TestOnly = allTests
	}
}

func stripRepoPrefix(name, repo string) string {
	if repo == "" {
		return name
	}
	return strings.TrimPrefix(name, repo+":")
}

func makeDeadSymbols(findings []deadcode.Finding, limit int) []report.DeadSymbol {
	if limit > 0 && len(findings) > limit {
		findings = findings[:limit]
	}
	out := make([]report.DeadSymbol, len(findings))
	for i, finding := range findings {
		out[i] = report.DeadSymbol{
			Name: finding.Name, Symbol: finding.Symbol,
			Kind: string(finding.Kind), Lang: string(finding.Lang),
			Exported: finding.Exported, Verdict: string(finding.Verdict),
			TestRefs: finding.TestRefs,
		}
	}
	return out
}
