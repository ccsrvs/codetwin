package git

import (
	"os"
	"path/filepath"
	"testing"
)

// symlinkToRepo creates a symlink pointing at the repo dir and returns
// the path to foo.go as seen through the symlink. Skips the test when
// the platform can't create symlinks (e.g. Windows without privilege).
func symlinkToRepo(t *testing.T, repoDir string) string {
	t.Helper()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(repoDir, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	return filepath.Join(link, "foo.go")
}

func TestBlame_GivenSymlinkedPathIntoRepo_ResolvesInsideRepo(t *testing.T) {
	requireGit(t)
	dir := initRepoWithCommit(t)
	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	br, err := repo.Blame(symlinkToRepo(t, dir), 1, 3)
	if err != nil {
		t.Fatalf("Blame via symlinked path: %v", err)
	}
	if br.FirstCommit == "" {
		t.Error("FirstCommit empty for symlinked path")
	}
}

func TestTouches_GivenSymlinkedPathIntoRepo_MatchesDiffEntry(t *testing.T) {
	requireGit(t)
	dir := initRepoWithCommit(t)
	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Modify foo.go in the working tree so it appears in the diff.
	if err := os.WriteFile(filepath.Join(dir, "foo.go"), []byte("a\nCHANGED\nc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	m, err := repo.ChangedSince("HEAD")
	if err != nil {
		t.Fatalf("ChangedSince: %v", err)
	}

	if !m.Touches(repo.Root, symlinkToRepo(t, dir), 1, 3) {
		t.Error("Touches = false for a symlinked path to a changed file; --since would silently drop this pair")
	}
}
