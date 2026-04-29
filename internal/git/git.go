// Package git wraps the small set of git invocations codetwin needs:
// repository discovery, working-tree diff against a ref, and per-line
// blame metadata. The package is built around two named errors —
// ErrGitNotInstalled and ErrNotARepo — so callers can degrade gracefully
// when git is missing or the user is running codetwin outside a repo.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrGitNotInstalled is returned when the git binary cannot be located
// on PATH. Callers should treat this as "git-dependent features are
// unavailable" rather than a fatal error.
var ErrGitNotInstalled = errors.New("git binary not found on PATH")

// ErrNotARepo is returned when the supplied directory is not inside a
// git working tree. Callers should treat this as "git-dependent
// features are unavailable for this scan" rather than a fatal error.
var ErrNotARepo = errors.New("not inside a git repository")

// gitBin is the name (or path) of the git executable to run. It's a
// package-level variable so tests can point it at a nonexistent binary
// to exercise the ErrGitNotInstalled path without mutating PATH.
var gitBin = "git"

// Repo represents a discovered git repository. Root is the absolute
// path to the working tree root, suitable for use as the -C argument to
// subsequent git commands.
type Repo struct {
	Root string
}

// Open locates the git repository that contains dir. Returns
// ErrGitNotInstalled if the git binary is missing, or ErrNotARepo if
// dir is not inside a working tree. Both cases are recoverable —
// callers should fall back to a git-less code path rather than aborting.
func Open(dir string) (*Repo, error) {
	if _, err := exec.LookPath(gitBin); err != nil {
		return nil, ErrGitNotInstalled
	}
	cmd := exec.Command(gitBin, "-C", dir, "rev-parse", "--show-toplevel")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// `git rev-parse --show-toplevel` exits non-zero when not in a
		// repo. We collapse every failure mode to ErrNotARepo because
		// that's the only actionable outcome for callers — there's no
		// meaningful recovery from a corrupted repo either.
		return nil, fmt.Errorf("%w: %s", ErrNotARepo, strings.TrimSpace(stderr.String()))
	}
	root := strings.TrimSpace(stdout.String())
	if root == "" {
		return nil, ErrNotARepo
	}
	return &Repo{Root: root}, nil
}

// run executes a git subcommand inside the repo and returns its stdout
// on success. Stderr is folded into the returned error so callers don't
// have to thread two streams through their handlers.
func (r *Repo) run(args ...string) ([]byte, error) {
	full := append([]string{"-C", r.Root}, args...)
	cmd := exec.Command(gitBin, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
