package report

import (
	"reflect"
	"strings"
	"testing"
)

// Cross-repo scanning (roadmap bet #6): repo-aware cluster metadata,
// the CrossRepoOnly filter, and the per-repo cluster rendering.

func TestClusterRepoSpan_GivenNoMemberRepos_ThenNilSpanAndNotCrossRepo(t *testing.T) {
	c := Cluster{Members: []string{"a.go", "b.go"}}
	if got := c.RepoSpan(); got != nil {
		t.Errorf("RepoSpan on single-root cluster: got %v, want nil", got)
	}
	if c.CrossRepo() {
		t.Error("CrossRepo on single-root cluster: got true, want false")
	}
}

func TestClusterRepoSpan_GivenTwoRepos_ThenSpanInFirstAppearanceOrder(t *testing.T) {
	c := Cluster{
		Members:     []string{"svc-b:x.go", "svc-a:y.go", "svc-b:z.go"},
		MemberRepos: []string{"svc-b", "svc-a", "svc-b"},
	}
	want := []string{"svc-b", "svc-a"}
	if got := c.RepoSpan(); !reflect.DeepEqual(got, want) {
		t.Errorf("RepoSpan: got %v, want %v", got, want)
	}
	if !c.CrossRepo() {
		t.Error("CrossRepo: got false, want true")
	}
}

func TestClusterRepoSpan_GivenSingleRepoAndEmpties_ThenNotCrossRepo(t *testing.T) {
	c := Cluster{
		Members:     []string{"svc-a:x.go", "y.go", "svc-a:z.go"},
		MemberRepos: []string{"svc-a", "", "svc-a"},
	}
	if got, want := c.RepoSpan(), []string{"svc-a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("RepoSpan: got %v, want %v", got, want)
	}
	if c.CrossRepo() {
		t.Error("CrossRepo with one repo + empty labels: got true, want false")
	}
}

func TestPrepare_CrossRepoOnly_KeepsOnlyPairsSpanningRepos(t *testing.T) {
	pairs := []Pair{
		{NameA: "svc-a:x.go", NameB: "svc-b:y.go", Score: 0.9, RepoA: "svc-a", RepoB: "svc-b"},
		{NameA: "svc-a:x.go", NameB: "svc-a:z.go", Score: 0.9, RepoA: "svc-a", RepoB: "svc-a"},
		{NameA: "x.go", NameB: "svc-b:y.go", Score: 0.9, RepoA: "", RepoB: "svc-b"},
	}
	got, _, _ := Prepare(pairs, nil, Options{Threshold: 0.5, CrossRepoOnly: true})
	if len(got) != 1 || got[0].NameA != "svc-a:x.go" || got[0].NameB != "svc-b:y.go" {
		t.Errorf("CrossRepoOnly pairs: got %+v, want only the svc-a↔svc-b pair", got)
	}
}

func TestPrepare_CrossRepoOnly_ComposesWithCrossLangOnly(t *testing.T) {
	pairs := []Pair{
		// cross-repo AND cross-lang → survives both filters
		{NameA: "a", NameB: "b", Score: 0.9, RepoA: "svc-a", RepoB: "svc-b", LangA: "Go", LangB: "Python"},
		// cross-repo but same-lang → dropped by CrossLangOnly
		{NameA: "c", NameB: "d", Score: 0.9, RepoA: "svc-a", RepoB: "svc-b", LangA: "Go", LangB: "Go"},
		// cross-lang but same-repo → dropped by CrossRepoOnly
		{NameA: "e", NameB: "f", Score: 0.9, RepoA: "svc-a", RepoB: "svc-a", LangA: "Go", LangB: "Python"},
	}
	got, _, _ := Prepare(pairs, nil, Options{Threshold: 0.5, CrossRepoOnly: true, CrossLangOnly: true})
	if len(got) != 1 || got[0].NameA != "a" {
		t.Errorf("composed filters: got %+v, want only pair a↔b", got)
	}
}

func TestPrepare_CrossRepoOnly_KeepsOnlyClustersSpanningRepos(t *testing.T) {
	clusters := []Cluster{
		{ID: 0, Members: []string{"svc-a:x", "svc-b:y"}, MemberRepos: []string{"svc-a", "svc-b"}, Score: 0.9},
		{ID: 1, Members: []string{"svc-c:m", "svc-c:n"}, MemberRepos: []string{"svc-c", "svc-c"}, Score: 0.9},
		{ID: 2, Members: []string{"p", "q"}, Score: 0.9}, // no repo info at all
	}
	_, got, _ := Prepare(nil, clusters, Options{Threshold: 0.5, CrossRepoOnly: true})
	if len(got) != 1 || got[0].ID != 0 {
		t.Errorf("CrossRepoOnly clusters: got %+v, want only cluster 0", got)
	}
}

func TestPrepareBlocks_CrossRepoOnly_KeepsOnlySpanningBlocks(t *testing.T) {
	blocks := []BlockClone{
		{FileA: "svc-a:x.go", FileB: "svc-b:y.go", RepoA: "svc-a", RepoB: "svc-b", Containment: 0.9},
		{FileA: "svc-a:x.go", FileB: "svc-a:z.go", RepoA: "svc-a", RepoB: "svc-a", Containment: 0.9},
		{FileA: "x.go", FileB: "y.go", Containment: 0.9},
	}
	got, suppressed := PrepareBlocks(blocks, Options{CrossRepoOnly: true})
	if suppressed != 0 {
		t.Errorf("suppressed: got %d, want 0", suppressed)
	}
	if len(got) != 1 || got[0].RepoB != "svc-b" {
		t.Errorf("CrossRepoOnly blocks: got %+v, want only the svc-a↔svc-b block", got)
	}
}

func TestRender_CrossRepoCluster_TagsHeaderAndGroupsMembersPerRepo(t *testing.T) {
	clusters := []Cluster{{
		ID:          0,
		Members:     []string{"svc-a:pricing.go:7-26 ApplyDiscount", "svc-a:billing.go:1-20 Bill", "svc-b:billing.go:7-26 ApplyDiscount"},
		MemberRepos: []string{"svc-a", "svc-a", "svc-b"},
		Score:       0.97,
		MinScore:    0.95,
	}}
	var buf strings.Builder
	Render(&buf, nil, clusters, Options{Plain: true, Threshold: 0.5})
	out := buf.String()

	if !strings.Contains(out, "cross-repo") {
		t.Errorf("cross-repo tag missing from header:\n%s", out)
	}
	for _, want := range []string{
		"svc-a — 2 snippets",
		"svc-b — 1 snippet",
		"· svc-a:pricing.go:7-26 ApplyDiscount",
		"· svc-b:billing.go:7-26 ApplyDiscount",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("per-repo grouping missing %q:\n%s", want, out)
		}
	}
	// Repo group lines must come before their members.
	if strings.Index(out, "svc-b — 1 snippet") > strings.Index(out, "svc-b:billing.go") {
		t.Errorf("repo group line should precede its members:\n%s", out)
	}
}

func TestRender_SingleRepoCluster_IsByteIdenticalToPreMultiRepoOutput(t *testing.T) {
	// The compatibility contract: a cluster without MemberRepos (any
	// single-root scan) must render exactly as before the cross-repo
	// feature — no tag, no grouping lines.
	mk := func(withRepos bool) string {
		c := Cluster{
			ID:       0,
			Members:  []string{"a.go:1-10 Foo", "b.go:1-10 Foo"},
			Score:    0.97,
			MinScore: 0.95,
		}
		if withRepos {
			// Same repo on every member: still not cross-repo, so the
			// members render flat; only names differ from the bare case.
			c.MemberRepos = []string{"svc-a", "svc-a"}
		}
		var buf strings.Builder
		Render(&buf, nil, []Cluster{c}, Options{Plain: true, Threshold: 0.5})
		return buf.String()
	}
	bare := mk(false)
	if strings.Contains(bare, "cross-repo") {
		t.Errorf("single-root render must not contain the cross-repo tag:\n%s", bare)
	}
	sameRepo := mk(true)
	if sameRepo != bare {
		t.Errorf("single-repo cluster with MemberRepos should render identically to one without:\nwith repos:\n%s\nbare:\n%s", sameRepo, bare)
	}
}
