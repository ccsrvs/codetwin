package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestBlame_GivenSingleCommitFile_ReturnsThatCommitForBothFirstAndLast(t *testing.T) {
	requireGit(t)
	dir := initRepoWithCommit(t)
	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	br, err := repo.Blame(filepath.Join(dir, "foo.go"), 1, 3)
	if err != nil {
		t.Fatalf("Blame: %v", err)
	}
	if br.FirstCommit == "" {
		t.Fatal("FirstCommit empty")
	}
	if br.FirstCommit != br.LastCommit {
		t.Errorf("FirstCommit (%q) != LastCommit (%q) for single-commit file", br.FirstCommit, br.LastCommit)
	}
	if br.FirstAuthor != "codetwin-test" {
		t.Errorf("FirstAuthor = %q, want codetwin-test", br.FirstAuthor)
	}
	if br.FirstTime.IsZero() {
		t.Errorf("FirstTime is zero")
	}
}

func TestBlame_GivenMultiCommitRange_FirstIsOldestLastIsNewest(t *testing.T) {
	requireGit(t)
	dir := initRepoWithCommit(t) // foo.go: a\nb\nc\n committed by initial

	// Second commit: rewrite line 2 only. Force the author/committer
	// date a day after the initial commit so the two records have
	// distinct author-time values (back-to-back commits in tests
	// otherwise share a second-resolution timestamp).
	path := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(path, []byte("a\nB-new\nc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	add := exec.Command("git", "-C", dir, "add", "foo.go")
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v (%s)", err, out)
	}
	commit := exec.Command("git", "-C", dir, "commit", "-q", "-m", "second")
	commit.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2030-01-02T00:00:00+0000",
		"GIT_COMMITTER_DATE=2030-01-02T00:00:00+0000",
	)
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v (%s)", err, out)
	}

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	br, err := repo.Blame(path, 1, 3)
	if err != nil {
		t.Fatalf("Blame: %v", err)
	}
	if br.FirstCommit == br.LastCommit {
		t.Errorf("expected different first and last commits; got both = %q", br.FirstCommit)
	}
	if !br.FirstTime.Before(br.LastTime) {
		t.Errorf("FirstTime (%v) should be strictly before LastTime (%v)", br.FirstTime, br.LastTime)
	}
}

func TestBlame_GivenUntrackedFile_ReturnsErrFileNotTracked(t *testing.T) {
	requireGit(t)
	dir := initRepoWithCommit(t)
	untracked := filepath.Join(dir, "untracked.go")
	if err := os.WriteFile(untracked, []byte("nothing\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = repo.Blame(untracked, 1, 1)
	if !errors.Is(err, ErrFileNotTracked) {
		t.Errorf("Blame(untracked) error = %v, want ErrFileNotTracked", err)
	}
}

func TestBlame_GivenNonexistentFile_ReturnsError(t *testing.T) {
	requireGit(t)
	dir := initRepoWithCommit(t)
	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = repo.Blame(filepath.Join(dir, "does-not-exist.go"), 1, 1)
	if err == nil {
		t.Errorf("expected error for nonexistent file, got nil")
	}
}

func TestBlame_TimeIsParsedAsUnix(t *testing.T) {
	requireGit(t)
	dir := initRepoWithCommit(t)
	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	br, err := repo.Blame(filepath.Join(dir, "foo.go"), 1, 1)
	if err != nil {
		t.Fatalf("Blame: %v", err)
	}
	// initial commit happened during test setup → within last 5 minutes
	if time.Since(br.FirstTime) > 5*time.Minute {
		t.Errorf("FirstTime %v looks suspiciously old (test only just ran)", br.FirstTime)
	}
}
