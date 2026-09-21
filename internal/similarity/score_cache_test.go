package similarity

import (
	"testing"

	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

func TestSnippetScoreDigestTracksScoringInputs(t *testing.T) {
	base := newScoreKeyFixture()
	baseKey := snippetScoreDigest(base.a, base.va)

	tests := []struct {
		name string
		edit func(*scoreKeyFixture)
	}{
		{"path", func(f *scoreKeyFixture) { f.a.Path = "changed.go" }},
		{"start line", func(f *scoreKeyFixture) { f.a.StartLine++ }},
		{"end line", func(f *scoreKeyFixture) { f.a.EndLine++ }},
		{"line confidence", func(f *scoreKeyFixture) { f.a.NonBlankLn++ }},
		{"language", func(f *scoreKeyFixture) { f.a.Lang = tokenizer.Python }},
		{"kind", func(f *scoreKeyFixture) { f.a.Kind = splitter.KindClass }},
		{"fingerprint k", func(f *scoreKeyFixture) { f.a.Fps.K++ }},
		{"fingerprint set", func(f *scoreKeyFixture) { f.a.Fps.Set = fingerprint.Set{99: {}} }},
		{"semantic vector", func(f *scoreKeyFixture) { f.va = Normalize(Vector{"shared": 2, "a": 1}) }},
		{"lexical terms", func(f *scoreKeyFixture) { f.a.LexTerms = []string{"changed", "terms", "now"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := newScoreKeyFixture()
			tt.edit(&changed)
			if got := snippetScoreDigest(changed.a, changed.va); got == baseKey {
				t.Fatalf("%s change did not invalidate document key", tt.name)
			}
		})
	}
	if pairScoreCacheContext(20) == pairScoreCacheContext(21) {
		t.Fatal("min confidence change did not invalidate pair-score context")
	}
	fallback := newScoreKeyFixture()
	fallback.a.CacheKey = ""
	first := snippetScoreDigest(fallback.a, fallback.va)
	fallback.va = Normalize(Vector{"different-term": 1, "a": 1})
	if second := snippetScoreDigest(fallback.a, fallback.va); second == first {
		t.Fatal("fallback document key ignored semantic term identities")
	}
}

type scoreKeyFixture struct {
	a  scan.Snippet
	va NormalizedVector
}

func newScoreKeyFixture() scoreKeyFixture {
	a := makeSnippet("a.go", "a.go", []string{"a"})
	a.StartLine, a.EndLine, a.Lang = 1, 10, tokenizer.Go
	a.LexTerms = []string{"alpha", "shared", "term"}
	return scoreKeyFixture{
		a:  a,
		va: Normalize(Vector{"shared": 1, "a": 1}),
	}
}
