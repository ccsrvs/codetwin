package main

// Performance smoke test for cross-repo scanning (roadmap bet #6):
// scanning N=6 sibling copies of a fixture tree completes sanely, the
// cache-warm second run is faster than the cold run, and output is
// identical between the two. Skipped in -short mode (no subprocess
// binary is built there).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// perfSeedFiles are real source files copied into each sibling repo —
// big enough that the scan phase (the part the cache eliminates) is a
// measurable slice of the cold run, small enough that the O(n²)
// comparison keeps the whole test comfortably under a second.
var perfSeedFiles = []string{
	"../../internal/report/report.go",
	"../../internal/scan/scan.go",
	"../../internal/cache/cache.go",
	"../../internal/refactor/align.go",
	"../../internal/refactor/synth.go",
	"../../internal/refactor/patch.go",
	"../../internal/similarity/matrix.go",
}

func TestMultirepo_PerfSmoke_SixSiblingReposCacheWarmRunIsFaster(t *testing.T) {
	bin := subprocessBin(t)
	if testing.Short() {
		t.Skip("perf smoke skipped in -short mode")
	}

	tmp := t.TempDir()
	cwd := filepath.Join(tmp, "cwd") // holds .codetwin-cache.bin
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}
	const n = 6
	roots := make([]string, n)
	for i := 0; i < n; i++ {
		roots[i] = filepath.Join(tmp, fmt.Sprintf("repo-%d", i+1))
		for _, src := range perfSeedFiles {
			copyFixtureFile(t, src, filepath.Join(roots[i], filepath.Base(src)))
		}
	}

	run := func() (time.Duration, string) {
		cmd := exec.Command(bin, append([]string{"--json", "--no-progress"}, roots...)...)
		cmd.Dir = cwd
		start := time.Now()
		out, err := cmd.Output()
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("perf run: %v\nstdout:\n%s", err, out)
		}
		return elapsed, string(out)
	}

	cold, coldOut := run()
	if _, err := os.Stat(filepath.Join(cwd, ".codetwin-cache.bin")); err != nil {
		t.Fatalf("cold run did not write the cache: %v", err)
	}
	warm, warmOut := run()

	t.Logf("perf smoke: %d roots × %d files — cold %v, warm %v", n, len(perfSeedFiles), cold, warm)

	if warmOut != coldOut {
		t.Error("cache-warm output differs from cold output")
	}
	// Sanity bound: no blowup with root count. The comparison phase is
	// O(snippets²) by design; anything beyond this bound on a tree this
	// size means something regressed pathologically.
	if cold > 30*time.Second {
		t.Errorf("cold run took %v — pathological for %d small roots", cold, n)
	}
	// The warm run skips split/tokenize/fingerprint via the cache and
	// must be faster. One re-measure absorbs scheduler noise before we
	// call it a regression.
	if warm >= cold {
		warm2, _ := run()
		t.Logf("perf smoke: warm re-measure %v", warm2)
		if warm2 >= cold {
			t.Errorf("cache-warm runs (%v, %v) were not faster than the cold run (%v)", warm, warm2, cold)
		}
	}
}
