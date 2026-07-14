package main

// Subprocess CLI tests for cross-repo / org-level scanning (roadmap
// bet #6): repo namespacing on multi-root invocations, per-repo cluster
// grouping and the cross-repo tag, --cross-repo-only, the single-root
// compatibility contract, duplicate-label disambiguation, and the
// interactions called out in the roadmap plan (test segregation,
// ignore_pairs, --suggest, cache, --since/--blame guard).

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	svcA = "../../testdata/multirepo/svc-a"
	svcB = "../../testdata/multirepo/svc-b"
	svcC = "../../testdata/multirepo/svc-c"
)

type multirepoJSONDoc struct {
	Pairs []struct {
		ID    string `json:"id"`
		FileA string `json:"file_a"`
		FileB string `json:"file_b"`
		RepoA string `json:"repo_a"`
		RepoB string `json:"repo_b"`
		Label string `json:"label"`
	} `json:"pairs"`
	Clusters []struct {
		ID          int      `json:"id"`
		Members     []string `json:"members"`
		MemberRepos []string `json:"member_repos"`
		CrossRepo   bool     `json:"cross_repo"`
	} `json:"clusters"`
}

func runMultirepoJSON(t *testing.T, bin string, args ...string) multirepoJSONDoc {
	t.Helper()
	full := append([]string{"--json", "--no-cache", "--no-progress"}, args...)
	out, err := exec.Command(bin, full...).Output()
	if err != nil {
		t.Fatalf("run %v: %v\nstdout:\n%s", full, err, out)
	}
	var doc multirepoJSONDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, out)
	}
	return doc
}

// TestMultirepo_JSONClusterSpansTwoRepos is the roadmap's acceptance
// check: `codetwin svc-a svc-b svc-c --json | jq '.clusters[0].members'`
// returns members from at least two repos, with per-member repo info.
func TestMultirepo_JSONClusterSpansTwoRepos(t *testing.T) {
	bin := subprocessBin(t)
	doc := runMultirepoJSON(t, bin, svcA, svcB, svcC)

	if len(doc.Clusters) == 0 {
		t.Fatal("expected at least one cluster")
	}
	first := doc.Clusters[0]
	repos := make(map[string]bool)
	for _, r := range first.MemberRepos {
		repos[r] = true
	}
	if len(repos) < 2 {
		t.Errorf("clusters[0] should span >= 2 repos, got member_repos %v", first.MemberRepos)
	}
	if !first.CrossRepo {
		t.Errorf("clusters[0].cross_repo = false, want true (members %v)", first.Members)
	}
	var sawA, sawB bool
	for _, m := range first.Members {
		sawA = sawA || strings.HasPrefix(m, "svc-a:")
		sawB = sawB || strings.HasPrefix(m, "svc-b:")
	}
	if !sawA || !sawB {
		t.Errorf("clusters[0].members should carry svc-a: and svc-b: prefixes, got %v", first.Members)
	}
	if len(first.MemberRepos) != len(first.Members) {
		t.Errorf("member_repos (%d) must parallel members (%d)", len(first.MemberRepos), len(first.Members))
	}

	// The cross-repo pair carries both repo labels; the svc-c internal
	// pair carries the same label on both ends.
	var sawCross, sawSame bool
	for _, p := range doc.Pairs {
		if p.RepoA == "svc-a" && p.RepoB == "svc-b" || p.RepoA == "svc-b" && p.RepoB == "svc-a" {
			sawCross = true
		}
		if p.RepoA == "svc-c" && p.RepoB == "svc-c" {
			sawSame = true
		}
	}
	if !sawCross {
		t.Errorf("no pair with repo_a/repo_b spanning svc-a and svc-b: %+v", doc.Pairs)
	}
	if !sawSame {
		t.Errorf("no svc-c internal pair with repo_a == repo_b == svc-c: %+v", doc.Pairs)
	}
}

// TestMultirepo_TerminalGroupsMembersPerRepoAndTagsCrossRepo: the
// terminal report must visually separate the repos under a cross-repo
// cluster and tag its header.
func TestMultirepo_TerminalGroupsMembersPerRepoAndTagsCrossRepo(t *testing.T) {
	bin := subprocessBin(t)
	out, err := exec.Command(bin, "--plain", "--no-cache", "--no-progress",
		svcA, svcB, svcC).Output()
	if err != nil {
		t.Fatalf("terminal run: %v\nstdout:\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{
		"· cross-repo",
		"svc-a — 1 snippet",
		"svc-b — 1 snippet",
		"· svc-a:pricing.go:7-26 ApplyDiscount",
		"· svc-b:billing.go:7-26 ApplyDiscount",
		// The svc-c same-repo cluster renders flat (prefixes only).
		"· svc-c:metrics.go:9-20 AccumulateLatency",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("terminal output missing %q:\n%s", want, text)
		}
	}
	// Exactly one cluster is cross-repo.
	if got := strings.Count(text, "cross-repo"); got != 1 {
		t.Errorf("cross-repo tag count = %d, want 1:\n%s", got, text)
	}
}

// TestMultirepo_CrossRepoOnlyKeepsOnlySpanningFindings: the svc-c
// internal clone pair/cluster must disappear; the svc-a↔svc-b family
// stays.
func TestMultirepo_CrossRepoOnlyKeepsOnlySpanningFindings(t *testing.T) {
	bin := subprocessBin(t)
	doc := runMultirepoJSON(t, bin, "--cross-repo-only", svcA, svcB, svcC)

	if len(doc.Clusters) != 1 || !doc.Clusters[0].CrossRepo {
		t.Fatalf("expected exactly the cross-repo cluster, got %+v", doc.Clusters)
	}
	for _, p := range doc.Pairs {
		if p.RepoA == "" || p.RepoB == "" || p.RepoA == p.RepoB {
			t.Errorf("--cross-repo-only leaked a non-spanning pair: %+v", p)
		}
	}
	if len(doc.Pairs) == 0 {
		t.Error("--cross-repo-only should keep the svc-a↔svc-b pair")
	}
}

// TestMultirepo_CrossRepoOnlySingleRootErrors: with fewer than two
// directory roots the flag is a contradiction — fail fast.
func TestMultirepo_CrossRepoOnlySingleRootErrors(t *testing.T) {
	bin := subprocessBin(t)
	cmd := exec.Command(bin, "--cross-repo-only", "--no-cache", "--no-progress", svcC)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for --cross-repo-only with one root")
	}
	if !strings.Contains(stderr.String(), "requires at least two directory roots") {
		t.Errorf("stderr missing the two-roots message:\n%s", stderr.String())
	}
}

// TestMultirepo_SingleRootOutputHasNoRepoArtifacts pins the
// compatibility contract: single-root invocations carry no repo fields
// in JSON and no repo prefixes in names — the pre-cross-repo schema,
// untouched.
func TestMultirepo_SingleRootOutputHasNoRepoArtifacts(t *testing.T) {
	bin := subprocessBin(t)
	out, err := exec.Command(bin, "--json", "--no-cache", "--no-progress", svcC).Output()
	if err != nil {
		t.Fatalf("single-root run: %v\nstdout:\n%s", err, out)
	}
	text := string(out)
	for _, banned := range []string{`"repo_a"`, `"repo_b"`, `"member_repos"`, `"cross_repo"`} {
		if strings.Contains(text, banned) {
			t.Errorf("single-root JSON must not contain %s:\n%s", banned, text)
		}
	}
	var doc multirepoJSONDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	for _, p := range doc.Pairs {
		if !strings.HasPrefix(p.FileA, svcC) || !strings.HasPrefix(p.FileB, svcC) {
			t.Errorf("single-root names must be the as-scanned paths, got %q / %q", p.FileA, p.FileB)
		}
	}

	term, err := exec.Command(bin, "--plain", "--no-cache", "--no-progress", svcC).Output()
	if err != nil {
		t.Fatalf("single-root terminal run: %v", err)
	}
	if strings.Contains(string(term), "cross-repo") {
		t.Errorf("single-root terminal output must not mention cross-repo:\n%s", term)
	}
}

// TestMultirepo_FileArgumentsGetNoRepoPrefix: invocations made of file
// arguments (no directory roots) are unchanged too.
func TestMultirepo_FileArgumentsGetNoRepoPrefix(t *testing.T) {
	bin := subprocessBin(t)
	doc := runMultirepoJSON(t, bin,
		svcA+"/pricing.go", svcB+"/billing.go")
	if len(doc.Pairs) == 0 {
		t.Fatal("expected the ApplyDiscount pair from the two file args")
	}
	for _, p := range doc.Pairs {
		if p.RepoA != "" || p.RepoB != "" {
			t.Errorf("file-argument pair must carry no repo labels: %+v", p)
		}
		if !strings.HasPrefix(p.FileA, svcA) {
			t.Errorf("file-argument names must be as-scanned paths, got %q", p.FileA)
		}
	}
}

// TestMultirepo_DuplicateRootBaseNamesDisambiguate: two roots that are
// both named "api" become labels "api" and "api~2" by input order.
func TestMultirepo_DuplicateRootBaseNamesDisambiguate(t *testing.T) {
	bin := subprocessBin(t)
	tmp := t.TempDir()
	first := filepath.Join(tmp, "payments", "api")
	second := filepath.Join(tmp, "identity", "api")
	copyFixtureFile(t, svcA+"/pricing.go", filepath.Join(first, "pricing.go"))
	copyFixtureFile(t, svcB+"/billing.go", filepath.Join(second, "billing.go"))

	doc := runMultirepoJSON(t, bin, first, second)
	if len(doc.Pairs) == 0 {
		t.Fatal("expected the ApplyDiscount pair across the two api roots")
	}
	p := doc.Pairs[0]
	labels := map[string]bool{p.RepoA: true, p.RepoB: true}
	if !labels["api"] || !labels["api~2"] {
		t.Errorf("duplicate roots should label api / api~2, got %q / %q", p.RepoA, p.RepoB)
	}
	if !strings.HasPrefix(p.FileA, "api:") && !strings.HasPrefix(p.FileA, "api~2:") {
		t.Errorf("names should carry the disambiguated labels, got %q", p.FileA)
	}
}

// TestMultirepo_TestSegregationStillClassifiesPerRepo: the copy-pasted
// _test.go pair between svc-a and svc-b is cross-repo but still
// test↔test — suppressed by default, restored by --include-tests.
func TestMultirepo_TestSegregationStillClassifiesPerRepo(t *testing.T) {
	bin := subprocessBin(t)
	out, err := exec.Command(bin, "--plain", "--no-cache", "--no-progress",
		svcA, svcB, svcC).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(string(out), "test↔test pair suppressed") &&
		!strings.Contains(string(out), "test-only cluster suppressed") {
		t.Errorf("expected the cross-repo test↔test finding to be suppressed:\n%s", out)
	}

	restored := runMultirepoJSON(t, bin, "--include-tests", svcA, svcB, svcC)
	var sawTestPair bool
	for _, p := range restored.Pairs {
		if strings.Contains(p.FileA, "pricing_test.go") && strings.Contains(p.FileB, "billing_test.go") ||
			strings.Contains(p.FileA, "billing_test.go") && strings.Contains(p.FileB, "pricing_test.go") {
			sawTestPair = true
			if p.RepoA == p.RepoB {
				t.Errorf("test↔test pair should span repos, got %q/%q", p.RepoA, p.RepoB)
			}
		}
	}
	if !sawTestPair {
		t.Error("--include-tests should restore the cross-repo test↔test pair")
	}
}

// TestMultirepo_IgnorePairsMatchUnprefixedNames: ignore_pairs endpoints
// are written without the repo label (the documented contract) and must
// still suppress the namespaced pair.
func TestMultirepo_IgnorePairsMatchUnprefixedNames(t *testing.T) {
	bin := subprocessBin(t)
	tmp := t.TempDir()
	cfg := `{"ignore_pairs": [{"a": "pricing.go ApplyDiscount", "b": "billing.go ApplyDiscount"}]}`
	if err := os.WriteFile(filepath.Join(tmp, ".codetwin.json"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	absA, absB, absC := mustAbs(t, svcA), mustAbs(t, svcB), mustAbs(t, svcC)
	cmd := exec.Command(bin, "--json", "--no-cache", "--no-progress", absA, absB, absC)
	cmd.Dir = tmp // .codetwin.json is loaded from the CWD
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run with ignore_pairs: %v\nstdout:\n%s", err, out)
	}
	var doc multirepoJSONDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, out)
	}
	for _, p := range doc.Pairs {
		if strings.Contains(p.FileA, "ApplyDiscount") && strings.Contains(p.FileB, "ApplyDiscount") &&
			!strings.Contains(p.FileA, "_test") {
			t.Errorf("ignore_pairs with un-prefixed endpoints failed to suppress %q ↔ %q", p.FileA, p.FileB)
		}
	}
	for _, c := range doc.Clusters {
		if c.CrossRepo {
			t.Errorf("ignored pair must not resurface as a cross-repo cluster: %+v", c)
		}
	}
}

// TestMultirepo_SuggestByIDWorksAcrossRoots: pair IDs from a multi-root
// --json run resolve with --suggest in the same multi-root invocation.
func TestMultirepo_SuggestByIDWorksAcrossRoots(t *testing.T) {
	bin := subprocessBin(t)
	doc := runMultirepoJSON(t, bin, svcA, svcB, svcC)
	var id string
	for _, p := range doc.Pairs {
		if p.RepoA != p.RepoB && strings.Contains(p.FileA, "ApplyDiscount") {
			id = p.ID
			break
		}
	}
	if id == "" {
		t.Fatal("no cross-repo ApplyDiscount pair found to suggest")
	}
	out, err := exec.Command(bin, "--no-cache", "--no-progress",
		"--suggest", id, svcA, svcB, svcC).Output()
	if err != nil {
		t.Fatalf("--suggest %s: %v\nstdout:\n%s", id, err, out)
	}
	if !strings.Contains(string(out), "@@") || !strings.Contains(string(out), "ApplyDiscount") {
		t.Errorf("--suggest output missing diff hunk / helper:\n%s", out)
	}
}

// TestMultirepo_CacheWorksAcrossRoots: cache keys are absolute-path
// based, so a warm second run must produce identical output.
func TestMultirepo_CacheWorksAcrossRoots(t *testing.T) {
	bin := subprocessBin(t)
	tmp := t.TempDir()
	absA, absB, absC := mustAbs(t, svcA), mustAbs(t, svcB), mustAbs(t, svcC)

	run := func() string {
		cmd := exec.Command(bin, "--json", "--no-progress", absA, absB, absC)
		cmd.Dir = tmp // cache file lands in the CWD
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("cached run: %v\nstdout:\n%s", err, out)
		}
		return string(out)
	}
	cold := run()
	if _, err := os.Stat(filepath.Join(tmp, ".codetwin-cache.bin")); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	warm := run()
	if cold != warm {
		t.Errorf("cache-warm output differs from cold output:\ncold:\n%s\nwarm:\n%s", cold, warm)
	}
}

// TestMultirepo_SinceAcrossDifferentGitReposErrors: --since (and
// --blame) refuse multi-root scans whose roots live in different git
// repositories — fail-fast beats silently wrong output.
func TestMultirepo_SinceAcrossDifferentGitReposErrors(t *testing.T) {
	bin := subprocessBin(t)
	tmp := t.TempDir()
	repo1 := filepath.Join(tmp, "repo1")
	repo2 := filepath.Join(tmp, "repo2")
	copyFixtureFile(t, svcA+"/pricing.go", filepath.Join(repo1, "pricing.go"))
	copyFixtureFile(t, svcB+"/billing.go", filepath.Join(repo2, "billing.go"))
	gitInit(t, repo1)
	gitInit(t, repo2)

	cmd := exec.Command(bin, "--no-cache", "--no-progress", "--blame", repo1, repo2)
	cmd.Dir = repo1 // CWD inside a repo so the git.Open(".") gate passes
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("expected non-zero exit for --blame across two git repos")
	}
	if !strings.Contains(stderr.String(), "different git repositories") {
		t.Errorf("stderr missing the different-repos message:\n%s", stderr.String())
	}
}

// TestMultirepo_SinceWithRootsInSameGitRepoStillWorks: two roots inside
// ONE git repository (the ./internal ./cmd shape) must keep working.
func TestMultirepo_SinceWithRootsInSameGitRepoStillWorks(t *testing.T) {
	bin := subprocessBin(t)
	tmp := t.TempDir()
	dirA := filepath.Join(tmp, "svc-a")
	dirB := filepath.Join(tmp, "svc-b")
	copyFixtureFile(t, svcA+"/pricing.go", filepath.Join(dirA, "pricing.go"))
	copyFixtureFile(t, svcB+"/billing.go", filepath.Join(dirB, "billing.go"))
	gitInit(t, tmp)

	cmd := exec.Command(bin, "--no-cache", "--no-progress", "--plain", "--blame", dirA, dirB)
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--blame with same-repo roots should succeed: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "cross-repo") {
		t.Errorf("expected the cross-repo cluster to render:\n%s", out)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func copyFixtureFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", dst, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "add", "-A"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}
}
