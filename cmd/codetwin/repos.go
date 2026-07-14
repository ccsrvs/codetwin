package main

// Cross-repo / org-level scanning (roadmap bet #6): when the CLI is
// given two or more DIRECTORY roots, each root is treated as a "repo".
// Snippets gain a repo label (the root's base name) and their display
// names are prefixed with it ("svc-a:handler.go:10-30 Parse", with the
// file path shown relative to its root), pairs/clusters/partial clones
// carry per-endpoint repo info, and the report groups cluster members
// per repo. Single-root and file-argument invocations are completely
// unchanged — no labels, no prefixes, byte-identical output.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ccsrvs/codetwin/internal/git"
	"github.com/ccsrvs/codetwin/internal/scan"
)

// fileRepo is one collected file's repo assignment: the repo label, the
// file's path relative to its root (used as the display path), and the
// path string it was scanned under (used to rewrite the snippet name).
type fileRepo struct {
	label   string
	rel     string
	scanned string
}

// repoMap records which directory root (and therefore which repo label)
// every collected file belongs to. Built by collectFiles; consulted only
// when MultiRepo() — i.e. at least two directory roots were given.
type repoMap struct {
	dirs   []string            // directory roots as given, deduped input order
	labels []string            // parallel to dirs: assigned repo label
	byFile map[string]fileRepo // absolute file path → repo assignment
}

func newRepoMap() *repoMap {
	return &repoMap{byFile: make(map[string]fileRepo)}
}

// MultiRepo reports whether the invocation had two or more directory
// roots — the trigger for all repo-aware behaviour.
func (rm *repoMap) MultiRepo() bool { return len(rm.dirs) >= 2 }

// addRoot registers a directory root and returns its repo label: the
// base name of the root's absolute path. Duplicate base names are
// disambiguated deterministically by input order with a "~N" suffix —
// two roots both named "api" become "api" and "api~2".
func (rm *repoMap) addRoot(dir string) string {
	base := repoBaseName(dir)
	label := base
	for n := 2; rm.labelTaken(label); n++ {
		label = fmt.Sprintf("%s~%d", base, n)
	}
	rm.dirs = append(rm.dirs, dir)
	rm.labels = append(rm.labels, label)
	return label
}

// labelTaken reports whether a repo label is already assigned.
func (rm *repoMap) labelTaken(label string) bool {
	for _, l := range rm.labels {
		if l == label {
			return true
		}
	}
	return false
}

// addFile records one collected file's repo assignment. root is the
// directory root the walk started from (as given on the CLI), scanned
// is the path the file will be processed under (WalkDir's path).
func (rm *repoMap) addFile(scanned, root, label string) {
	abs, err := filepath.Abs(scanned)
	if err != nil {
		return // unresolvable path → no repo assignment; name stays as-is
	}
	rel, err := filepath.Rel(root, scanned)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = scanned
	}
	rm.byFile[filepath.Clean(abs)] = fileRepo{
		label:   label,
		rel:     filepath.ToSlash(rel),
		scanned: scanned,
	}
}

// repoBaseName derives the label base for a directory root: the base
// name of its absolute path, so "." labels as the current directory's
// name rather than ".".
func repoBaseName(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = filepath.Clean(dir)
	}
	return filepath.Base(abs)
}

// namespaceSnippets rewrites snippet identities for a multi-root scan:
// each snippet collected under a directory root gets Repo = the root's
// label and a display name of "label:relpath:start-end Symbol" (the
// root-relative path replaces the as-scanned path). Snippets that came
// from direct file arguments in a mixed invocation keep their name and
// an empty Repo. Must run before snippets are sorted / compared so
// every downstream surface (pairs, clusters, previews, --suggest IDs)
// sees one consistent set of names.
func namespaceSnippets(snippets []scan.Snippet, rm *repoMap) {
	for i := range snippets {
		fr, ok := rm.byFile[snippets[i].Path]
		if !ok {
			continue
		}
		snippets[i].Repo = fr.label
		if rest, found := strings.CutPrefix(snippets[i].Name, fr.scanned); found {
			snippets[i].Name = fr.label + ":" + fr.rel + rest
		} else {
			// The name doesn't start with the scanned path (e.g. a cache
			// entry written under a different path spelling). Fall back
			// to a plain prefix — still unambiguous, just not
			// root-relative.
			snippets[i].Name = fr.label + ":" + snippets[i].Name
		}
	}
}

// stripRepoPrefix removes the "repo:" display prefix from a namespaced
// snippet name. ignore_pairs endpoints are matched against this
// un-prefixed (root-relative) form so one config works for both
// single-root and multi-root invocations.
func stripRepoPrefix(name, repo string) string {
	if repo == "" {
		return name
	}
	return strings.TrimPrefix(name, repo+":")
}

// ensureSingleGitRepo verifies that every directory root of a
// multi-root scan lives inside the same git working tree. --since and
// --blame resolve one repository (from the CWD), so roots spread across
// different git repos would silently produce wrong output — failing
// fast with a clear message beats that. Roots inside one repo (e.g.
// `codetwin ./internal ./cmd`) pass.
func (rm *repoMap) ensureSingleGitRepo(flagLabel string) error {
	seen := make(map[string]bool)
	var roots []string
	for i, d := range rm.dirs {
		r, err := git.Open(d)
		if err != nil {
			return fmt.Errorf("%s with multiple roots: root %q (%s): %w",
				flagLabel, d, rm.labels[i], err)
		}
		if !seen[r.Root] {
			seen[r.Root] = true
			roots = append(roots, r.Root)
		}
	}
	if len(roots) > 1 {
		return fmt.Errorf("%s is not supported when roots live in different git repositories (found %d: %s); run codetwin once per repository",
			flagLabel, len(roots), strings.Join(roots, ", "))
	}
	return nil
}
