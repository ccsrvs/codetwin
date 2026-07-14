package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ccsrvs/codetwin/internal/scan"
)

// Cross-repo scanning unit tests: repo label assignment, snippet name
// namespacing, and the repo-prefix stripper used by ignore_pairs.

func TestRepoMap_AddRoot_LabelsAreBaseNames(t *testing.T) {
	rm := newRepoMap()
	if got := rm.addRoot("../svc-a"); got != "svc-a" {
		t.Errorf("label for ../svc-a: got %q, want svc-a", got)
	}
	if got := rm.addRoot("/org/repos/svc-b"); got != "svc-b" {
		t.Errorf("label for /org/repos/svc-b: got %q, want svc-b", got)
	}
	if !rm.MultiRepo() {
		t.Error("MultiRepo with two roots: got false, want true")
	}
}

func TestRepoMap_AddRoot_DuplicateBaseNamesDisambiguateByInputOrder(t *testing.T) {
	rm := newRepoMap()
	got := []string{
		rm.addRoot("/teams/payments/api"),
		rm.addRoot("/teams/identity/api"),
		rm.addRoot("/teams/search/api"),
	}
	want := []string{"api", "api~2", "api~3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("duplicate labels: got %v, want %v", got, want)
	}
}

func TestRepoMap_AddRoot_DotRootLabelsAsDirectoryName(t *testing.T) {
	rm := newRepoMap()
	label := rm.addRoot(".")
	cwdBase := filepath.Base(mustAbs(t, "."))
	if label != cwdBase {
		t.Errorf("label for '.': got %q, want the CWD base name %q", label, cwdBase)
	}
}

func TestRepoMap_SingleRootIsNotMultiRepo(t *testing.T) {
	rm := newRepoMap()
	rm.addRoot("./src")
	if rm.MultiRepo() {
		t.Error("MultiRepo with one root: got true, want false")
	}
}

func TestNamespaceSnippets_RewritesNameToLabelAndRootRelativePath(t *testing.T) {
	rm := newRepoMap()
	label := rm.addRoot("testdata/multirepo/svc-a")
	scanned := filepath.Join("testdata", "multirepo", "svc-a", "pricing.go")
	rm.addFile(scanned, "testdata/multirepo/svc-a", label)

	snippets := []scan.Snippet{{
		Name: scanned + ":7-26 ApplyDiscount",
		Path: mustAbs(t, scanned),
	}}
	namespaceSnippets(snippets, rm)

	if snippets[0].Repo != "svc-a" {
		t.Errorf("Repo: got %q, want svc-a", snippets[0].Repo)
	}
	if want := "svc-a:pricing.go:7-26 ApplyDiscount"; snippets[0].Name != want {
		t.Errorf("Name: got %q, want %q", snippets[0].Name, want)
	}
}

func TestNamespaceSnippets_WholeFileChunkNameGetsPrefixToo(t *testing.T) {
	rm := newRepoMap()
	label := rm.addRoot("svc-b")
	rm.addFile(filepath.Join("svc-b", "util", "x.go"), "svc-b", label)

	snippets := []scan.Snippet{{
		Name: filepath.Join("svc-b", "util", "x.go"), // whole-file fallback chunk
		Path: mustAbs(t, filepath.Join("svc-b", "util", "x.go")),
	}}
	namespaceSnippets(snippets, rm)
	if want := "svc-b:util/x.go"; snippets[0].Name != want {
		t.Errorf("Name: got %q, want %q", snippets[0].Name, want)
	}
}

func TestNamespaceSnippets_UnknownPathIsLeftAlone(t *testing.T) {
	// Direct file arguments in a mixed invocation are never registered
	// in the repoMap — their snippets keep bare names and empty Repo.
	rm := newRepoMap()
	rm.addRoot("svc-a")
	rm.addRoot("svc-b")
	snippets := []scan.Snippet{{Name: "lone.go:1-5 F", Path: mustAbs(t, "lone.go")}}
	namespaceSnippets(snippets, rm)
	if snippets[0].Name != "lone.go:1-5 F" || snippets[0].Repo != "" {
		t.Errorf("direct file arg snippet mutated: %+v", snippets[0])
	}
}

func TestNamespaceSnippets_CachedNameWithForeignSpellingFallsBackToPlainPrefix(t *testing.T) {
	// A cache entry written under a different path spelling can't be
	// rewritten root-relative; it must still gain the label prefix.
	rm := newRepoMap()
	label := rm.addRoot("svc-a")
	scanned := filepath.Join("svc-a", "pricing.go")
	rm.addFile(scanned, "svc-a", label)

	snippets := []scan.Snippet{{
		Name: "/somewhere/else/pricing.go:7-26 ApplyDiscount",
		Path: mustAbs(t, scanned),
	}}
	namespaceSnippets(snippets, rm)
	if want := "svc-a:/somewhere/else/pricing.go:7-26 ApplyDiscount"; snippets[0].Name != want {
		t.Errorf("fallback Name: got %q, want %q", snippets[0].Name, want)
	}
}

func TestStripRepoPrefix(t *testing.T) {
	cases := []struct {
		name, repo, want string
	}{
		{"svc-a:pricing.go:7-26 ApplyDiscount", "svc-a", "pricing.go:7-26 ApplyDiscount"},
		{"pricing.go:7-26 ApplyDiscount", "", "pricing.go:7-26 ApplyDiscount"},
		// Only the exact "repo:" prefix is stripped.
		{"svc-a2:pricing.go", "svc-a", "svc-a2:pricing.go"},
	}
	for _, c := range cases {
		if got := stripRepoPrefix(c.name, c.repo); got != c.want {
			t.Errorf("stripRepoPrefix(%q, %q) = %q, want %q", c.name, c.repo, got, c.want)
		}
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("abs %q: %v", p, err)
	}
	return abs
}
