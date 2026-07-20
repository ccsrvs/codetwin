package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// updateCheckCmdName is the hidden subcommand the foreground spawns
// detached to run the daily network check, so no user-facing invocation
// ever waits on the network.
const updateCheckCmdName = "__update-check"

// updateCheckTTL throttles the background check to once per day.
const updateCheckTTL = 24 * time.Hour

// noUpdateCheckEnv disables the notifier entirely when truthy. Also set
// on the detached child so a check can never recurse into another.
const noUpdateCheckEnv = "CODETWIN_NO_UPDATE_CHECK"

// updateState is the persisted throttle + last-seen-release cache.
type updateState struct {
	LastCheck     string `json:"last_check"`     // RFC3339 UTC of the last background check
	LatestVersion string `json:"latest_version"` // newest release tag seen by a check
}

// updateStatePath is <user-config-dir>/codetwin/update_state.json,
// overridable via CODETWIN_UPDATE_STATE_DIR (tests), falling back to
// the temp dir when no config dir resolves.
func updateStatePath() string {
	dir := os.Getenv("CODETWIN_UPDATE_STATE_DIR")
	if dir == "" {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil || dir == "" {
			dir = os.TempDir()
		}
		dir = filepath.Join(dir, "codetwin")
	}
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "update_state.json")
}

func readUpdateState(path string) updateState {
	var st updateState
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func writeUpdateState(path string, st updateState) {
	if b, err := json.MarshalIndent(st, "", "  "); err == nil {
		_ = os.WriteFile(path, append(b, '\n'), 0o644)
	}
}

// noUpdateCheck reports whether the notifier is disabled by environment.
func noUpdateCheck() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(noUpdateCheckEnv))) {
	case "1", "true", "yes", "y", "on":
		return true
	}
	return false
}

// updateAvailable reports whether latest names a release other than the
// running build. Local builds ("dev" or dev-*) never see a notice — they
// are ahead of or beside releases, not behind them.
func updateAvailable(current, latest string) bool {
	if latest == "" || current == "dev" || strings.HasPrefix(current, "dev-") {
		return false
	}
	return latest != current
}

// checkIsDue reports whether the daily background check should run:
// missing or unparseable timestamp, or one older than ttl.
func checkIsDue(lastCheck string, now time.Time, ttl time.Duration) bool {
	t, err := time.Parse(time.RFC3339, lastCheck)
	if err != nil {
		return true
	}
	return now.Sub(t) >= ttl
}

// runUpdateNotifier runs at the top of user-facing commands. Best-effort
// and non-blocking by construction: it prints a one-line stderr notice
// when a newer release is already cached, and at most once a day spawns
// the detached background check that does the actual network round-trip.
func runUpdateNotifier() {
	if noUpdateCheck() {
		return
	}
	if strings.HasPrefix(buildVersion, "dev") {
		return // local builds have no release to compare against
	}
	statePath := updateStatePath()
	st := readUpdateState(statePath)

	if updateAvailable(buildVersion, st.LatestVersion) {
		fmt.Fprintf(os.Stderr, "codetwin: a new version is available (%s → %s); run `codetwin update` to upgrade.\n",
			buildVersion, st.LatestVersion)
	}

	if checkIsDue(st.LastCheck, time.Now().UTC(), updateCheckTTL) {
		// Stamp before spawning so concurrent runs today don't each
		// launch a checker.
		st.LastCheck = time.Now().UTC().Format(time.RFC3339)
		writeUpdateState(statePath, st)
		spawnDetachedCheck()
	}
}

// spawnDetachedCheck launches the hidden checker in its own session so
// it outlives this process and never delays it.
func spawnDetachedCheck() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, updateCheckCmdName)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.Env = append(os.Environ(), noUpdateCheckEnv+"=1")
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
}

// runBackgroundCheck is the detached worker: resolve the latest tag and
// cache it for the next foreground run to report. Notify-only — the
// binary swap is always the user's explicit `codetwin update`. Every
// failure is swallowed; nobody is watching this process.
func runBackgroundCheck() {
	statePath := updateStatePath()
	st := readUpdateState(statePath)
	st.LastCheck = time.Now().UTC().Format(time.RFC3339)
	if latest, err := latestReleaseTag(); err == nil {
		st.LatestVersion = latest
	}
	writeUpdateState(statePath, st)
}
