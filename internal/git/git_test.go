package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// withGitBin temporarily swaps the package-level git binary path so a
// test can simulate "git not installed" without mutating PATH.
func withGitBin(t *testing.T, bin string) {
	t.Helper()
	prev := gitBin
	gitBin = bin
	t.Cleanup(func() { gitBin = prev })
}

// requireGit skips the test when no real git is available on PATH.
// Integration tests that shell out to git can't run on systems without
// it, but they shouldn't fail the suite either.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed on PATH; skipping integration test")
	}
}

func TestOpen_GivenMissingGitBinary_ReturnsErrGitNotInstalled(t *testing.T) {
	withGitBin(t, "/nonexistent/path/to/git-binary-that-does-not-exist-12345")
	dir := t.TempDir()
	_, err := Open(dir)
	if !errors.Is(err, ErrGitNotInstalled) {
		t.Fatalf("Open() error = %v, want ErrGitNotInstalled", err)
	}
}

func TestOpen_GivenDirOutsideAnyRepo_ReturnsErrNotARepo(t *testing.T) {
	requireGit(t)
	// t.TempDir() returns a fresh isolated path under the system temp
	// dir. On macOS and Linux that's not inside any git working tree,
	// so `git rev-parse --show-toplevel` will exit non-zero.
	dir := t.TempDir()
	_, err := Open(dir)
	if !errors.Is(err, ErrNotARepo) {
		t.Fatalf("Open() error = %v, want ErrNotARepo", err)
	}
}

func TestOpen_GivenInitializedRepo_FindsRoot(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, out)
	}

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(%q) error = %v, want nil", dir, err)
	}
	// Resolve symlinks on both sides — macOS temp dirs go through
	// /var → /private/var, and `git rev-parse --show-toplevel`
	// always emits the realpath form.
	wantReal, _ := filepath.EvalSymlinks(dir)
	gotReal, _ := filepath.EvalSymlinks(repo.Root)
	if wantReal != gotReal {
		t.Errorf("Repo.Root = %q, want %q (after symlink resolution)", repo.Root, dir)
	}
}

func TestOpen_GivenSubdirOfRepo_FindsRoot(t *testing.T) {
	requireGit(t)
	repoDir := t.TempDir()
	if out, err := exec.Command("git", "-C", repoDir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, out)
	}
	sub := filepath.Join(repoDir, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}

	repo, err := Open(sub)
	if err != nil {
		t.Fatalf("Open(%q) error = %v, want nil", sub, err)
	}
	wantReal, _ := filepath.EvalSymlinks(repoDir)
	gotReal, _ := filepath.EvalSymlinks(repo.Root)
	if wantReal != gotReal {
		t.Errorf("Repo.Root = %q, want %q (after symlink resolution)", repo.Root, repoDir)
	}
}
