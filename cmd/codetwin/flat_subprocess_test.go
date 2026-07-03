package main

// Subprocess CLI tests for the cluster-first default report and the
// --flat escape hatch. The go/medium refactor fixture yields one clone
// pair whose two endpoints DBSCAN groups into one cluster, so the
// default report must collapse the pair into the cluster and --flat
// must list it.

import (
	"os/exec"
	"strings"
	"testing"
)

func TestReport_DefaultIsClusterFirst(t *testing.T) {
	bin := subprocessBin(t)

	out, err := exec.Command(bin,
		"--plain", "--no-cache", "--no-progress",
		"../../testdata/refactor/go/medium",
	).Output()
	if err != nil {
		t.Fatalf("run: %v\nstdout:\n%s", err, out)
	}
	s := string(out)

	if !strings.Contains(s, "REFACTORING CLUSTERS") {
		t.Fatalf("expected cluster section in default report:\n%s", s)
	}
	if strings.Contains(s, "SIMILARITY PAIRS") {
		t.Errorf("intra-cluster pair should be collapsed out of the pairs section:\n%s", s)
	}
	if !strings.Contains(s, "In-cluster pairs") {
		t.Errorf("summary should count collapsed pairs:\n%s", s)
	}
}

func TestReport_FlatListsPairs(t *testing.T) {
	bin := subprocessBin(t)

	out, err := exec.Command(bin,
		"--plain", "--flat", "--no-cache", "--no-progress",
		"../../testdata/refactor/go/medium",
	).Output()
	if err != nil {
		t.Fatalf("run: %v\nstdout:\n%s", err, out)
	}
	s := string(out)

	if !strings.Contains(s, "SIMILARITY PAIRS") {
		t.Fatalf("--flat should render the pairs section:\n%s", s)
	}
	if strings.Contains(s, "In-cluster pairs") {
		t.Errorf("--flat summary should not report collapsed pairs:\n%s", s)
	}
}

func TestReport_JSONAlwaysFlat(t *testing.T) {
	bin := subprocessBin(t)

	out, err := exec.Command(bin,
		"--json", "--no-cache", "--no-progress",
		"../../testdata/refactor/go/medium",
	).Output()
	if err != nil {
		t.Fatalf("run: %v\nstdout:\n%s", err, out)
	}
	// JSON consumers keep the full pair list regardless of the
	// terminal collapse.
	if !strings.Contains(string(out), `"pairs"`) || !strings.Contains(string(out), `"score"`) {
		t.Errorf("JSON output should contain the full pairs array:\n%s", out)
	}
}
