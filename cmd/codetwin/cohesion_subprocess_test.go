package main

// Subprocess CLI tests for cluster cohesion (R4): the terminal cluster
// header must show the cohesion (minimum internal pair score) alongside
// the average, and --json clusters must carry a min_score field. The
// go/medium refactor fixture yields one 2-member cluster, so cohesion
// equals the average there — the assertions target presence and wiring,
// not the split math (unit-tested in cohesion_test.go).

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestReport_ClusterHeaderShowsCohesion(t *testing.T) {
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
		t.Fatalf("expected cluster section:\n%s", s)
	}
	// Header shape: "Cluster 1 — N snippets · avg similarity NN% · cohesion NN%"
	header := regexp.MustCompile(`Cluster 1 — \d+ snippets · avg similarity\s+\d+% · cohesion\s+\d+%`)
	if !header.MatchString(s) {
		t.Errorf("cluster header should show avg similarity and cohesion:\n%s", s)
	}
}

func TestReport_JSONClustersCarryMinScore(t *testing.T) {
	bin := subprocessBin(t)

	out, err := exec.Command(bin,
		"--json", "--no-cache", "--no-progress",
		"../../testdata/refactor/go/medium",
	).Output()
	if err != nil {
		t.Fatalf("run: %v\nstdout:\n%s", err, out)
	}

	var doc struct {
		Clusters []struct {
			ID       int      `json:"id"`
			Members  []string `json:"members"`
			Score    float64  `json:"score"`
			MinScore *float64 `json:"min_score"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("JSON parse: %v\noutput:\n%s", err, out)
	}
	if len(doc.Clusters) == 0 {
		t.Fatal("fixture should produce at least one cluster")
	}
	for _, c := range doc.Clusters {
		if c.MinScore == nil {
			t.Fatalf("cluster %d missing min_score field:\n%s", c.ID, out)
		}
		if *c.MinScore <= 0 || *c.MinScore > 1.0 {
			t.Errorf("cluster %d min_score = %v; want in (0, 1]", c.ID, *c.MinScore)
		}
		if *c.MinScore > c.Score+1e-9 {
			t.Errorf("cluster %d min_score %v exceeds avg score %v", c.ID, *c.MinScore, c.Score)
		}
	}
}
