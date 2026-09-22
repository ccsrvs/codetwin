package main

import (
	"bytes"
	"os"
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
		cmd := exec.Command(bin, append([]string{"--json", "--debug", "--no-progress", "--reuse-scores"}, files...)...)
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

// Pair-score reuse is opt-in: the persisted table holds one entry per
// scored pair and grows with the square of the snippet count, so on
// anything but a tiny repo decoding it costs more than recomputing the
// scores. A default run must neither reuse nor keep one, and must shrink
// a cache file that an earlier --reuse-scores run filled.
func TestIncrementalScoring_OffByDefaultAndDropsPersistedScores(t *testing.T) {
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
	cacheFile := filepath.Join(dir, ".codetwin-cache.bin")

	run := func(extra ...string) (stdout []byte, hits, misses int) {
		args := append([]string{"--json", "--debug", "--no-progress"}, extra...)
		cmd := exec.Command(bin, append(args, files...)...)
		cmd.Dir = dir
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			t.Fatalf("run %v: %v\n%s", extra, err, errOut.Bytes())
		}
		match := incrementalStatsPattern.FindSubmatch(errOut.Bytes())
		if match == nil {
			t.Fatalf("missing incremental stats in stderr:\n%s", errOut.Bytes())
		}
		hits, _ = strconv.Atoi(string(match[1]))
		misses, _ = strconv.Atoi(string(match[2]))
		return out.Bytes(), hits, misses
	}
	size := func() int64 {
		info, err := os.Stat(cacheFile)
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}

	plainOut, hits, misses := run()
	if hits != 0 || misses != 0 {
		t.Fatalf("default run touched the score cache: %d hits/%d misses", hits, misses)
	}
	plainSize := size()

	reuseOut, _, _ := run("--reuse-scores")
	if !bytes.Equal(reuseOut, plainOut) {
		t.Fatal("--reuse-scores changed the report")
	}
	if size() <= plainSize {
		t.Fatalf("--reuse-scores did not persist scores: cache %d bytes, plain %d", size(), plainSize)
	}

	if _, hits, _ := run(); hits != 0 {
		t.Fatalf("default run reused %d persisted scores", hits)
	}
	if got := size(); got != plainSize {
		t.Fatalf("default run kept the persisted scores: cache %d bytes, want %d", got, plainSize)
	}
}
