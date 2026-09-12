package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestParseUnifiedDiff_GivenSingleFileEdit_ReturnsAddedLineRange(t *testing.T) {
	out := []byte(`diff --git a/foo.go b/foo.go
index abc..def 100644
--- a/foo.go
+++ b/foo.go
@@ -10,3 +10,5 @@
-old1
-old2
-old3
+new1
+new2
+new3
+new4
+new5
`)
	m := parseUnifiedDiff(out)
	got := m["foo.go"]
	want := []LineRange{{Start: 10, End: 14}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("foo.go ranges = %+v, want %+v", got, want)
	}
}

func TestParseUnifiedDiff_GivenMultipleHunks_AccumulatesRanges(t *testing.T) {
	out := []byte(`diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1 +1 @@
-a
+b
@@ -10,0 +11,3 @@
+c
+d
+e
`)
	m := parseUnifiedDiff(out)
	got := m["foo.go"]
	want := []LineRange{{Start: 1, End: 1}, {Start: 11, End: 13}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("foo.go ranges = %+v, want %+v", got, want)
	}
}

func TestParseUnifiedDiff_GivenPureDeletionHunk_DropsHunk(t *testing.T) {
	// `+5,0` means zero new lines — pure deletion. Nothing in the
	// new file overlaps that "range" so we skip it entirely.
	out := []byte(`diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -5,3 +5,0 @@
-x
-y
-z
`)
	m := parseUnifiedDiff(out)
	if len(m) != 0 {
		t.Errorf("expected empty diff map for pure-deletion hunk, got %+v", m)
	}
}

func TestParseUnifiedDiff_GivenDeletedFile_OmitsFromMap(t *testing.T) {
	out := []byte(`diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1,5 +0,0 @@
-line1
-line2
-line3
-line4
-line5
`)
	m := parseUnifiedDiff(out)
	if _, ok := m["gone.go"]; ok {
		t.Errorf("deleted file should not appear in diff map, got %+v", m)
	}
}

func TestParseUnifiedDiff_GivenNewFile_RecordsFullRange(t *testing.T) {
	out := []byte(`diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package new
+
+func New() {}
`)
	m := parseUnifiedDiff(out)
	got := m["new.go"]
	want := []LineRange{{Start: 1, End: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("new.go ranges = %+v, want %+v", got, want)
	}
}

func TestParseUnifiedDiff_GivenMultipleFiles_KeysByPath(t *testing.T) {
	out := []byte(`diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1 +1 @@
-a
+A
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -2 +2 @@
-b
+B
`)
	m := parseUnifiedDiff(out)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"a.go", "b.go"}) {
		t.Errorf("keys = %v, want [a.go b.go]", keys)
	}
}

func TestDiffMap_TouchesIntersectsAnyRange(t *testing.T) {
	root := "/tmp/repo"
	m := DiffMap{
		"pkg/foo.go": {{Start: 10, End: 20}, {Start: 50, End: 60}},
	}
	cases := []struct {
		name       string
		path       string
		start, end int
		want       bool
	}{
		{"overlap left edge", "/tmp/repo/pkg/foo.go", 5, 10, true},
		{"overlap right edge", "/tmp/repo/pkg/foo.go", 20, 25, true},
		{"contained", "/tmp/repo/pkg/foo.go", 12, 18, true},
		{"contains range", "/tmp/repo/pkg/foo.go", 1, 100, true},
		{"between two ranges", "/tmp/repo/pkg/foo.go", 30, 40, false},
		{"unknown file", "/tmp/repo/other.go", 12, 18, false},
		{"file outside repo", "/different/repo/pkg/foo.go", 12, 18, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := m.Touches(root, c.path, c.start, c.end); got != c.want {
				t.Errorf("Touches(%s, %d, %d) = %v, want %v", c.path, c.start, c.end, got, c.want)
			}
		})
	}
}

func TestParseHunkHeader_GivenMalformedInputs_ReturnsFalse(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"missing plus marker", "@@ -1,2 1,3 @@"},
		{"non-numeric start", "@@ -1,2 +abc,3 @@"},
		{"non-numeric count", "@@ -1,2 +5,xyz @@"},
		{"new-side count zero (deletion)", "@@ -5,3 +5,0 @@"},
		{"missing trailing space", "@@ -1,2 +5,3@@"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := parseHunkHeader(c.in); ok {
				t.Errorf("expected (_, false) for malformed input %q", c.in)
			}
		})
	}
}

func TestParseHunkHeader_GivenSingleLineForm_DefaultsCountToOne(t *testing.T) {
	// `@@ -10 +20 @@` (no comma) implies count=1 on each side.
	lr, ok := parseHunkHeader("@@ -10 +20 @@")
	if !ok {
		t.Fatalf("expected ok for `@@ -10 +20 @@`")
	}
	if lr.Start != 20 || lr.End != 20 {
		t.Errorf("got %+v, want {20, 20}", lr)
	}
}

func TestStripDiffPrefix_LeavesUnusualPrefixesIntact(t *testing.T) {
	cases := []struct{ in, want string }{
		{"b/foo.go", "foo.go"},
		{"b/nested/a.go", "nested/a.go"},
		{"foo.go", "foo.go"},           // no prefix at all
		{"src/foo.go", "src/foo.go"},   // arbitrary --dst-prefix=src/
		{"workdir/x/y", "workdir/x/y"}, // arbitrary --dst-prefix=workdir/
	}
	for _, c := range cases {
		if got := stripDiffPrefix(c.in); got != c.want {
			t.Errorf("stripDiffPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTouches_GivenPathInsideRepoButOutsideDiff_ReturnsFalse(t *testing.T) {
	// Path with a relative-to-root resolution that doesn't start with
	// ".." but the file isn't in the diff at all.
	m := DiffMap{"only.go": {{Start: 1, End: 5}}}
	if m.Touches("/repo", "/repo/other.go", 1, 100) {
		t.Errorf("expected false for file not in diff")
	}
}

func TestChangedSince_GivenUnknownRef_ReturnsError(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, out)
	}
	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = repo.ChangedSince("refs/heads/this-ref-does-not-exist-xyz123")
	if err == nil {
		t.Errorf("expected error for unknown ref, got nil")
	}
}

func TestChangedSince_GivenEditedFile_ReportsChangedLines(t *testing.T) {
	requireGit(t)
	dir := initRepoWithCommit(t)

	// Edit the file: append two lines so the diff shows lines 4-5.
	path := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	m, err := repo.ChangedSince("HEAD")
	if err != nil {
		t.Fatalf("ChangedSince: %v", err)
	}
	got, ok := m["foo.go"]
	if !ok {
		t.Fatalf("expected foo.go in diff map, got %+v", m)
	}
	want := []LineRange{{Start: 4, End: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("foo.go ranges = %+v, want %+v", got, want)
	}
}

// initRepoWithCommit creates a fresh repo with one committed file
// (foo.go containing 3 lines) and returns the repo path.
func initRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "-C", dir, "init", "-q"},
		{"git", "-C", dir, "config", "user.email", "test@codetwin.local"},
		{"git", "-C", dir, "config", "user.name", "codetwin-test"},
		{"git", "-C", dir, "config", "commit.gpgsign", "false"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v (%s)", c, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "foo.go"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	add := exec.Command("git", "-C", dir, "add", "foo.go")
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v (%s)", err, out)
	}
	commit := exec.Command("git", "-C", dir, "commit", "-q", "-m", "initial")
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v (%s)", err, out)
	}
	return dir
}

func TestParseUnifiedDiff_QuotedPaths(t *testing.T) {
	cases := []struct{ header, path string }{
		{`"b/caf\303\251.go"`, "café.go"},
		{`"b/tab\tname.go"`, "tab\tname.go"},
		{`"b/quote\"name.go"`, "quote\"name.go"},
		{`"b/back\\slash.go"`, "back\\slash.go"},
		{"b/space name.go", "space name.go"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			out := []byte("--- a/old.go\n+++ " + tc.header + "\n@@ -1 +1 @@\n-old\n+new\n")
			want := DiffMap{tc.path: {{Start: 1, End: 1}}}
			if got := parseUnifiedDiff(out); !reflect.DeepEqual(got, want) {
				t.Errorf("diff map = %#v, want %#v", got, want)
			}
		})
	}
}

func TestChangedSince_UnicodeFilename(t *testing.T) {
	requireGit(t)
	dir := initRepoWithCommit(t)
	name := "café.go"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"config", "core.quotePath", "true"},
		{"add", "--", name},
		{"commit", "-q", "-m", "add unicode filename"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := repo.ChangedSince("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !m.Touches(repo.Root, path, 4, 4) {
		t.Errorf("changed line in %q was missed: %#v", name, m)
	}
	if m.Touches(repo.Root, path, 1, 3) {
		t.Error("unchanged lines should not be marked as touched")
	}
}

func TestParseUnifiedDiff_ContentResemblingFileHeader(t *testing.T) {
	out := []byte("diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-old\n+++ b/not-a-file.go\n@@ -3 +3 @@\n-old\n+new\n")
	want := DiffMap{"foo.go": {{Start: 1, End: 1}, {Start: 3, End: 3}}}
	if got := parseUnifiedDiff(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("diff map = %#v, want %#v", got, want)
	}
}

func TestChangedSince_IgnoresDisplayPrefixConfig(t *testing.T) {
	requireGit(t)
	dir := initRepoWithCommit(t)
	for _, setting := range []string{"diff.noprefix", "diff.mnemonicprefix"} {
		t.Run(setting, func(t *testing.T) {
			if out, err := exec.Command("git", "-C", dir, "config", setting, "true").CombinedOutput(); err != nil {
				t.Fatalf("config: %v (%s)", err, out)
			}
			t.Cleanup(func() { _ = exec.Command("git", "-C", dir, "config", "--unset", setting).Run() })
			path := filepath.Join(dir, "foo.go")
			if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			repo, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			m, err := repo.ChangedSince("HEAD")
			if err != nil {
				t.Fatal(err)
			}
			if !m.Touches(repo.Root, path, 4, 4) {
				t.Fatalf("changed line missed with %s: %#v", setting, m)
			}
		})
	}
}

func TestParseUnifiedDiff_LongSourceLineDoesNotHideLaterHunks(t *testing.T) {
	out := []byte("diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-old\n+" + strings.Repeat("x", 16*1024*1024) + "\n@@ -3 +3 @@\n-old\n+new\n")
	want := DiffMap{"foo.go": {{Start: 1, End: 1}, {Start: 3, End: 3}}}
	if got := parseUnifiedDiff(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("diff map = %#v, want %#v", got, want)
	}
}

func TestDiffMap_TouchesDotPrefixedDescendant(t *testing.T) {
	root := t.TempDir()
	name := "..generated.go"
	m := DiffMap{name: {{Start: 1, End: 5}}}
	if !m.Touches(root, filepath.Join(root, name), 2, 3) {
		t.Fatal("a filename beginning with two dots is still inside the repository")
	}
	if m.Touches(root, filepath.Join(root, "..", name), 2, 3) {
		t.Fatal("a parent-directory traversal must remain outside the repository")
	}
}
