package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// updateBaseURL is the public GitHub repo the self-update talks to. The
// repo is public, so both the version probe and the asset download are
// plain unauthenticated HTTPS — no GitHub CLI, no API token, and no API
// rate limits (the /releases/latest redirect is a regular web endpoint).
// Overridable via CODETWIN_UPDATE_BASE_URL so tests can stand in a local
// server.
func updateBaseURL() string {
	if v := os.Getenv("CODETWIN_UPDATE_BASE_URL"); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return "https://github.com/ccsrvs/codetwin"
}

// updateNetTimeout bounds each network round-trip of the self-update.
const updateNetTimeout = 60 * time.Second

// releaseTagRe is a strict allowlist for release tags ("v0.2.0",
// "v1.2.3-rc1"). The tag comes off the network and ends up in a download
// URL and next to an exec of the downloaded file, so it is validated at
// both the source and the sink — defense in depth.
var releaseTagRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]{0,63}$`)

func validReleaseTag(tag string) bool { return releaseTagRe.MatchString(tag) }

// latestReleaseTag resolves the newest release tag from the 302 redirect
// GitHub serves at /releases/latest: the Location header's last path
// element is the tag. One HEAD-sized round-trip, no JSON, no API quota.
func latestReleaseTag() (string, error) {
	client := &http.Client{
		Timeout: updateNetTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(updateBaseURL() + "/releases/latest")
	if err != nil {
		return "", fmt.Errorf("could not reach %s: %w", updateBaseURL(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("no release redirect at %s/releases/latest (HTTP %d — no published releases?)",
			updateBaseURL(), resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	tag := path.Base(loc)
	if loc == "" || tag == "." || tag == "/" {
		return "", fmt.Errorf("release redirect carried no usable Location header")
	}
	if !validReleaseTag(tag) {
		return "", fmt.Errorf("unexpected release tag %q from redirect", tag)
	}
	return tag, nil
}

// releaseAssetName is the published asset filename for this platform.
func releaseAssetName(tag string) string {
	name := fmt.Sprintf("codetwin-%s-%s-%s", tag, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// runUpdateCLI is the `codetwin update` entry point, dispatched from
// main before the scan flags parse. Exits the process on error.
func runUpdateCLI(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "only report whether an update is available; do not download")
	force := fs.Bool("force", false, "reinstall even if already on the latest version")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `
codetwin update — self-update from the latest GitHub release

USAGE:
  codetwin update [--check] [--force]

Resolves the latest release over plain HTTPS (no GitHub CLI or token
needed), and atomically replaces this binary when a newer one exists.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if err := runUpdate(*check, *force); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runUpdate(check, force bool) error {
	latest, err := latestReleaseTag()
	if err != nil {
		return err
	}
	current := buildVersion
	available := current != latest

	if check {
		if available {
			fmt.Printf("Update available: %s → %s\nRun `codetwin update` to install it.\n", current, latest)
		} else {
			fmt.Printf("Up to date (%s)\n", current)
		}
		return nil
	}
	if !available && !force {
		fmt.Printf("Already on the latest version (%s)\n", current)
		return nil
	}

	// Resolve the running binary, following symlinks so the real file is
	// replaced, not a link to it.
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	tmpName, err := downloadReleaseBinary(latest, exe)
	if err != nil {
		return err
	}
	defer os.Remove(tmpName) // no-op once renamed into place

	if err := replaceBinary(tmpName, exe); err != nil {
		return fmt.Errorf("could not replace %s (permission denied?): %w", exe, err)
	}
	fmt.Printf("Updated %s\n  %s → %s\n", exe, current, latest)
	return nil
}

// downloadReleaseBinary fetches the platform asset for `tag` into a temp
// file next to exe — same directory, so the final rename stays on one
// filesystem and is atomic — then verifies the download actually runs
// before it is allowed anywhere near the live binary.
func downloadReleaseBinary(tag, exe string) (string, error) {
	if !validReleaseTag(tag) {
		return "", fmt.Errorf("refusing to download unexpected release tag %q", tag)
	}
	asset := releaseAssetName(tag)
	url := updateBaseURL() + "/releases/download/" + tag + "/" + asset

	tmp, err := os.CreateTemp(filepath.Dir(exe), ".codetwin-update-*")
	if err != nil {
		return "", fmt.Errorf("cannot write next to %s (permission denied?): %w", exe, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }

	client := &http.Client{Timeout: updateNetTimeout}
	resp, err := client.Get(url)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		cleanup()
		return "", fmt.Errorf("download of %s failed: HTTP %d (no prebuilt binary for %s/%s in %s?)",
			asset, resp.StatusCode, runtime.GOOS, runtime.GOARCH, tag)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		cleanup()
		return "", fmt.Errorf("download interrupted: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	// A corrupt or wrong-platform download must never brick the install.
	if out, err := exec.Command(tmpName, "--version").CombinedOutput(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("downloaded binary failed to run (%w): %s", err, strings.TrimSpace(string(out)))
	}
	return tmpName, nil
}

// replaceBinary moves newPath over dest. On Unix, renaming over the
// running binary is safe — the live process keeps its inode. Windows
// refuses to overwrite a running .exe, so move it aside first and
// restore on failure.
func replaceBinary(newPath, dest string) error {
	if runtime.GOOS == "windows" {
		old := dest + ".old"
		os.Remove(old)
		if err := os.Rename(dest, old); err != nil {
			return err
		}
		if err := os.Rename(newPath, dest); err != nil {
			_ = os.Rename(old, dest) // best-effort restore
			return err
		}
		os.Remove(old) // may fail while the old exe still runs; harmless
		return nil
	}
	return os.Rename(newPath, dest)
}
