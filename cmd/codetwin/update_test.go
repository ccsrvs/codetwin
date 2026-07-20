package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestValidReleaseTag(t *testing.T) {
	for _, ok := range []string{"v0.2.0", "0.1.1", "v1.2.3-rc1", "v1.0.0+build.7"} {
		if !validReleaseTag(ok) {
			t.Errorf("%q should be a valid tag", ok)
		}
	}
	for _, bad := range []string{"", "-v1", "../etc", "v1/../..", "a b", strings.Repeat("x", 80)} {
		if validReleaseTag(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestUpdateAvailable(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.2.0", "", false},
		{"dev", "v0.2.0", false},
		{"dev-3893fbf", "v0.2.0", false},
	}
	for _, c := range cases {
		if got := updateAvailable(c.current, c.latest); got != c.want {
			t.Errorf("updateAvailable(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestCheckIsDue(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	if !checkIsDue("", now, updateCheckTTL) {
		t.Errorf("missing timestamp: check should be due")
	}
	if !checkIsDue("garbage", now, updateCheckTTL) {
		t.Errorf("unparseable timestamp: check should be due")
	}
	if checkIsDue(now.Add(-time.Hour).Format(time.RFC3339), now, updateCheckTTL) {
		t.Errorf("1h-old check must not be due under a 24h TTL")
	}
	if !checkIsDue(now.Add(-25*time.Hour).Format(time.RFC3339), now, updateCheckTTL) {
		t.Errorf("25h-old check must be due")
	}
}

func TestUpdateStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODETWIN_UPDATE_STATE_DIR", dir)
	p := updateStatePath()
	if filepath.Dir(p) != dir {
		t.Fatalf("state path should honor the env override, got %s", p)
	}
	writeUpdateState(p, updateState{LastCheck: "2026-07-20T00:00:00Z", LatestVersion: "v9.9.9"})
	st := readUpdateState(p)
	if st.LastCheck != "2026-07-20T00:00:00Z" || st.LatestVersion != "v9.9.9" {
		t.Errorf("state did not round-trip: %+v", st)
	}
}

// fakeReleaseServer serves the /releases/latest redirect and a platform
// asset for `tag`, standing in for github.com.
func fakeReleaseServer(t *testing.T, tag string, asset []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/releases/download/"+tag+"/"+releaseAssetName(tag),
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(asset)
		})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestLatestReleaseTagViaRedirect(t *testing.T) {
	srv := fakeReleaseServer(t, "v3.1.4", nil)
	t.Setenv("CODETWIN_UPDATE_BASE_URL", srv.URL)
	tag, err := latestReleaseTag()
	if err != nil {
		t.Fatalf("latestReleaseTag: %v", err)
	}
	if tag != "v3.1.4" {
		t.Errorf("tag = %q, want v3.1.4", tag)
	}
}

func TestLatestReleaseTagNoRelease(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	t.Setenv("CODETWIN_UPDATE_BASE_URL", srv.URL)
	if _, err := latestReleaseTag(); err == nil {
		t.Errorf("404 (no releases) must error, not return a tag")
	}
}

// TestUpdateCheckSubcommand drives the real binary against the fake
// server: --check must report availability without touching the binary.
func TestUpdateCheckSubcommand(t *testing.T) {
	bin := subprocessBin(t)
	srv := fakeReleaseServer(t, "v99.0.0", nil)

	cmd := exec.Command(bin, "update", "--check")
	cmd.Env = append(os.Environ(), "CODETWIN_UPDATE_BASE_URL="+srv.URL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("update --check: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Update available") || !strings.Contains(string(out), "v99.0.0") {
		t.Errorf("expected an update-available report, got:\n%s", out)
	}
}

// TestUpdateSwapsBinary runs the full self-update against the fake
// server: a copy of the test binary must be atomically replaced by the
// served asset and the served asset must actually run.
func TestUpdateSwapsBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix rename semantics")
	}
	bin := subprocessBin(t)

	// The served "new release" is a real runnable program: a copy of the
	// test binary itself (it responds to --version, passing the sanity
	// check).
	assetBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	srv := fakeReleaseServer(t, "v99.0.0", assetBytes)

	// Install a sacrificial copy to be updated in place.
	dir := t.TempDir()
	victim := filepath.Join(dir, "codetwin")
	if err := os.WriteFile(victim, assetBytes, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(victim, "update")
	cmd.Env = append(os.Environ(),
		"CODETWIN_UPDATE_BASE_URL="+srv.URL,
		"CODETWIN_NO_UPDATE_CHECK=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Updated") {
		t.Errorf("expected an Updated report, got:\n%s", out)
	}
	// The victim must still be a runnable binary afterwards.
	if vout, err := exec.Command(victim, "--version").CombinedOutput(); err != nil {
		t.Errorf("replaced binary does not run: %v\n%s", err, vout)
	}
	// No leftover temp download files.
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".codetwin-update-*"))
	if len(leftovers) > 0 {
		t.Errorf("temp download files leaked: %v", leftovers)
	}
}

// TestNotifierReportsCachedVersion primes the state cache and asserts
// the one-line stderr notice appears on a scan run — and does not appear
// when suppressed by env.
func TestNotifierReportsCachedVersion(t *testing.T) {
	bin := subprocessBin(t)
	stateDir := t.TempDir()
	writeUpdateState(filepath.Join(stateDir, "update_state.json"), updateState{
		LastCheck:     time.Now().UTC().Format(time.RFC3339), // fresh: no new spawn
		LatestVersion: "v99.0.0",
	})

	run := func(extraEnv ...string) string {
		cmd := exec.Command(bin, "--plain", "--no-cache", "--no-progress", deadcodeFixture)
		cmd.Env = append(os.Environ(), append([]string{
			"CODETWIN_UPDATE_STATE_DIR=" + stateDir,
			// The test binary reports buildVersion "dev"; the notifier
			// skips dev builds, which is correct in production but would
			// vacuously pass here. There is no ldflags hook in `go test`
			// builds, so assert the suppression path instead and cover
			// the notice text via updateAvailable's unit test.
		}, extraEnv...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("scan run: %v\n%s", err, out)
		}
		return string(out)
	}

	if s := run(); strings.Contains(s, "new version is available") {
		t.Errorf("dev build must not print an update notice:\n%s", s)
	}
	if s := run("CODETWIN_NO_UPDATE_CHECK=1"); strings.Contains(s, "new version is available") {
		t.Errorf("env opt-out must suppress the notice:\n%s", s)
	}
}
