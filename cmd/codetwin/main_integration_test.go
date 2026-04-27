// Package main_test contains end-to-end integration tests that exercise
// the full pipeline: tokenize → fingerprint → similarity → cluster → report.
// These tests work directly against the internal packages without spawning
// a subprocess, so they run with `go test ./...` and need no build step.
package main_test

import (
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/cluster"
	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/similarity"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// testSnippet bundles a named code fragment with its derived artefacts.
type testSnippet struct {
	name   string
	lang   tokenizer.Language
	code   string
	tokens []string
	fps    fingerprint.Set
	vector similarity.Vector
}

// sampleSnippets are the canonical cross-language sum-loop family used
// throughout the integration tests.
var sampleCode = []struct {
	name string
	lang tokenizer.Language
	code string
}{
	{
		name: "JS: sumArray",
		lang: tokenizer.JavaScript,
		code: `function sumArray(arr) {
  let total = 0;
  for (let i = 0; i < arr.length; i++) {
    total += arr[i];
  }
  return total;
}`,
	},
	{
		name: "JS: addNumbers",
		lang: tokenizer.JavaScript,
		code: `function addNumbers(nums) {
  let result = 0;
  for (let i = 0; i < nums.length; i++) {
    result += nums[i];
  }
  return result;
}`,
	},
	{
		name: "Python: sum_list",
		lang: tokenizer.Python,
		code: `def sum_list(items):
    total = 0
    for i in range(len(items)):
        total += items[i]
    return total`,
	},
	{
		name: "Go: SumSlice",
		lang: tokenizer.Go,
		code: `func SumSlice(nums []int) int {
  total := 0
  for i := 0; i < len(nums); i++ {
    total += nums[i]
  }
  return total
}`,
	},
	{
		name: "Elixir: sum_list",
		lang: tokenizer.Elixir,
		code: `defmodule MathUtils do
  def sum_list(items) do
    Enum.reduce(items, 0, fn x, acc -> acc + x end)
  end
end`,
	},
	{
		name: "Elixir: add_all",
		lang: tokenizer.Elixir,
		code: `defmodule NumberUtils do
  def add_all(numbers) do
    Enum.reduce(numbers, 0, fn n, total -> total + n end)
  end
end`,
	},
	// Deliberately unrelated snippet — should not cluster with the sum family
	{
		name: "JS: fetchUser",
		lang: tokenizer.JavaScript,
		code: `async function fetchUser(id) {
  const response = await fetch('/api/users/' + id);
  const data = await response.json();
  return data;
}`,
	},
}

// buildPipeline runs the full analysis pipeline and returns pairs and clusters.
func buildPipeline(t *testing.T) ([]report.Pair, []report.Cluster, []testSnippet) {
	t.Helper()

	snippets := make([]testSnippet, len(sampleCode))
	tokenStreams := make([][]string, len(sampleCode))

	for i, s := range sampleCode {
		tokens := tokenizer.Tokenize(s.code, s.lang)
		fps := fingerprint.Generate(tokens, fingerprint.DefaultK, fingerprint.DefaultW)
		snippets[i] = testSnippet{name: s.name, lang: s.lang, code: s.code, tokens: tokens, fps: fps}
		tokenStreams[i] = tokens
	}

	corpus := similarity.NewCorpus(tokenStreams)
	for i := range snippets {
		snippets[i].vector = corpus.Vectorize(snippets[i].tokens)
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
			semantic := similarity.Cosine(snippets[i].vector, snippets[j].vector)
			combined := similarity.Combined(structural, semantic, 0.5)
			matrix[i][j] = combined
			matrix[j][i] = combined
			pairs = append(pairs, report.Pair{
				NameA: snippets[i].name, NameB: snippets[j].name,
				Structural: structural, Semantic: semantic, Score: combined,
			})
		}
	}

	distFn := func(i, j int) float64 { return 1.0 - matrix[i][j] }
	clusterResult := cluster.DBSCAN(n, 0.45, 2, distFn)
	groups := cluster.Groups(clusterResult)

	clusters := make([]report.Cluster, 0, len(groups))
	for id, members := range groups {
		names := make([]string, len(members))
		for k, idx := range members {
			names[k] = snippets[idx].name
		}
		clusters = append(clusters, report.Cluster{ID: id, Members: names})
	}

	return pairs, clusters, snippets
}

// ── Integration tests ─────────────────────────────────────────────────────────

func TestPipeline_TwoJSSumFunctionsHaveHighSimilarity(t *testing.T) {
	pairs, _, _ := buildPipeline(t)

	for _, p := range pairs {
		if (p.NameA == "JS: sumArray" && p.NameB == "JS: addNumbers") ||
			(p.NameA == "JS: addNumbers" && p.NameB == "JS: sumArray") {
			if p.Score < 0.80 {
				t.Errorf("JS sumArray ↔ addNumbers score = %.2f; want >= 0.80", p.Score)
			}
			return
		}
	}
	t.Error("Pair JS: sumArray ↔ JS: addNumbers not found in results")
}

func TestPipeline_CrossLanguageSumFunctionsHaveMeaningfulSimilarity(t *testing.T) {
	pairs, _, _ := buildPipeline(t)

	for _, p := range pairs {
		isJSGo := (strings.Contains(p.NameA, "sumArray") && strings.Contains(p.NameB, "SumSlice")) ||
			(strings.Contains(p.NameA, "SumSlice") && strings.Contains(p.NameB, "sumArray"))
		if isJSGo {
			if p.Score < 0.35 {
				t.Errorf("Cross-language JS↔Go sum score = %.2f; want >= 0.35", p.Score)
			}
			return
		}
	}
	t.Error("Cross-language JS sumArray ↔ Go SumSlice pair not found")
}

func TestPipeline_UnrelatedSnippetHasLowSimilarityToSumFamily(t *testing.T) {
	pairs, _, _ := buildPipeline(t)

	for _, p := range pairs {
		involvesFetch := strings.Contains(p.NameA, "fetchUser") || strings.Contains(p.NameB, "fetchUser")
		involvesSumJS := strings.Contains(p.NameA, "sumArray") || strings.Contains(p.NameB, "sumArray")
		if involvesFetch && involvesSumJS {
			if p.Score > 0.60 {
				t.Errorf("fetchUser ↔ sumArray score = %.2f; want < 0.60 (unrelated code)", p.Score)
			}
			return
		}
	}
}

func TestPipeline_SumFamilyFormsAtLeastOneCluster(t *testing.T) {
	_, clusters, _ := buildPipeline(t)

	if len(clusters) == 0 {
		t.Fatal("Expected at least one cluster for the sum-loop family, got none")
	}

	// At least one cluster should contain 2+ sum-family members
	sumNames := map[string]bool{
		"JS: sumArray": true, "JS: addNumbers": true,
		"Python: sum_list": true, "Go: SumSlice": true,
		"Elixir: sum_list": true, "Elixir: add_all": true,
	}

	for _, c := range clusters {
		sumCount := 0
		for _, m := range c.Members {
			if sumNames[m] {
				sumCount++
			}
		}
		if sumCount >= 2 {
			return // pass
		}
	}
	t.Errorf("No cluster contains 2+ sum-family members. Clusters: %+v", clusters)
}

func TestPipeline_ReportRendersWithoutPanicking(t *testing.T) {
	pairs, clusters, _ := buildPipeline(t)

	var buf strings.Builder
	opts := report.Options{Plain: true, Threshold: 0.30, Verbose: false}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("report.Render panicked: %v", r)
		}
	}()

	report.Render(&buf, pairs, clusters, opts)

	if buf.Len() == 0 {
		t.Error("report.Render produced empty output")
	}
}

func TestPipeline_AllPairScoresAreInValidRange(t *testing.T) {
	pairs, _, _ := buildPipeline(t)

	for _, p := range pairs {
		if p.Score < 0 || p.Score > 1.0+1e-9 {
			t.Errorf("Pair %s ↔ %s has out-of-range score: %.4f", p.NameA, p.NameB, p.Score)
		}
		if p.Structural < 0 || p.Structural > 1.0+1e-9 {
			t.Errorf("Pair %s ↔ %s has out-of-range structural score: %.4f", p.NameA, p.NameB, p.Structural)
		}
		if p.Semantic < 0 || p.Semantic > 1.0+1e-9 {
			t.Errorf("Pair %s ↔ %s has out-of-range semantic score: %.4f", p.NameA, p.NameB, p.Semantic)
		}
	}
}

func TestPipeline_TwoElixirSumFunctionsHaveHighSimilarity(t *testing.T) {
	pairs, _, _ := buildPipeline(t)

	for _, p := range pairs {
		isElixirPair := (p.NameA == "Elixir: sum_list" && p.NameB == "Elixir: add_all") ||
			(p.NameA == "Elixir: add_all" && p.NameB == "Elixir: sum_list")
		if isElixirPair {
			if p.Score < 0.75 {
				t.Errorf("Elixir sum_list ↔ add_all score = %.2f; want >= 0.75", p.Score)
			}
			return
		}
	}
	t.Error("Pair Elixir: sum_list ↔ Elixir: add_all not found in results")
}

func TestPipeline_ElixirSnippetsHaveMeaningfulSimilarityToOtherSumFamily(t *testing.T) {
	pairs, _, _ := buildPipeline(t)

	for _, p := range pairs {
		isElixirVsJS := (strings.Contains(p.NameA, "Elixir") && strings.Contains(p.NameB, "sumArray")) ||
			(strings.Contains(p.NameB, "Elixir") && strings.Contains(p.NameA, "sumArray"))
		if isElixirVsJS {
			// Elixir uses a functional style so cross-language score will be lower
			// than intra-language; we just want a non-trivial signal
			if p.Score < 0.20 {
				t.Errorf("Elixir ↔ JS sum score = %.2f; want >= 0.20 (some signal expected)", p.Score)
			}
			return
		}
	}
	t.Error("Elixir ↔ JS sumArray pair not found in results")
}

func TestPipeline_ElixirTokenizerProducesNonEmptyTokens(t *testing.T) {
	_, _, snippets := buildPipeline(t)

	for _, s := range snippets {
		if s.lang == tokenizer.Elixir && len(s.tokens) == 0 {
			t.Errorf("Elixir snippet %q produced zero tokens — tokenizer may be broken", s.name)
		}
	}
}

func TestPipeline_ElixirSnippetsDoNotClusterWithFetchUser(t *testing.T) {
	pairs, _, _ := buildPipeline(t)

	for _, p := range pairs {
		elixirVsFetch := (strings.Contains(p.NameA, "Elixir") && strings.Contains(p.NameB, "fetchUser")) ||
			(strings.Contains(p.NameB, "Elixir") && strings.Contains(p.NameA, "fetchUser"))
		if elixirVsFetch && p.Score > 0.55 {
			t.Errorf("Elixir sum ↔ fetchUser score = %.2f; want < 0.55 (should be unrelated)", p.Score)
		}
	}
}
