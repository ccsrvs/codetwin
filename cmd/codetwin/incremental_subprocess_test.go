package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

var incrementalStatsPattern = regexp.MustCompile(`incremental scoring: (\d+) reused, (\d+) recomputed`)

func TestIncrementalScoring_WarmRunReusesPersistedScoresWithIdenticalOutput(t *testing.T) {
	bin := subprocessBin(t)
	dir := t.TempDir()
	files := []string{
		filepath.Join(dir, "sum_a.js"),
		filepath.Join(dir, "sum_b.js"),
		filepath.Join(dir, "fetch_user.js"),
	}
	copyFixtureFile(t, "../../testdata/sum_a.js", files[0])
	copyFixtureFile(t, "../../testdata/sum_b.js", files[1])
	copyFixtureFile(t, "../../testdata/fetch_user.js", files[2])

	run := func() ([]byte, int, int) {
		cmd := exec.Command(bin, append([]string{"--json", "--debug", "--no-progress"}, files...)...)
		cmd.Dir = dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("incremental run: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.Bytes(), stderr.Bytes())
		}
		match := incrementalStatsPattern.FindSubmatch(stderr.Bytes())
		if match == nil {
			t.Fatalf("missing incremental stats in stderr:\n%s", stderr.Bytes())
		}
		hits, _ := strconv.Atoi(string(match[1]))
		misses, _ := strconv.Atoi(string(match[2]))
		return stdout.Bytes(), hits, misses
	}

	coldOut, coldHits, coldMisses := run()
	if coldHits != 0 || coldMisses == 0 {
		t.Fatalf("cold incremental stats = %d hits/%d misses, want 0/>0", coldHits, coldMisses)
	}
	warmOut, warmHits, warmMisses := run()
	if warmHits != coldMisses || warmMisses != 0 {
		t.Fatalf("warm incremental stats = %d hits/%d misses, want %d/0", warmHits, warmMisses, coldMisses)
	}
	if !bytes.Equal(warmOut, coldOut) {
		t.Fatal("warm incremental output differs from cold output")
	}
}
