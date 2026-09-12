package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// Three same-language functions with genuine but weak overlap: with the
// default settings sumPositive↔joinNames scores ≈0.42, sumPositive↔meanOf
// ≈0.33, and meanOf↔joinNames ≈0.21.
var weakPairFixture = map[string]string{
	"a.go": "package sample\n\nfunc sumPositive(xs []int) int {\n\ttotal := 0\n\tfor _, x := range xs {\n\t\tif x > 0 {\n\t\t\ttotal += x\n\t\t}\n\t}\n\treturn total\n}\n",
	"b.go": "package sample\n\nfunc meanOf(xs []float64) float64 {\n\ttotal := 0.0\n\tcount := 0\n\tfor _, x := range xs {\n\t\ttotal += x\n\t\tcount++\n\t}\n\tif count == 0 {\n\t\treturn 0\n\t}\n\treturn total / float64(count)\n}\n",
	"c.go": "package sample\n\nfunc joinNames(names []string, sep string) string {\n\tout := \"\"\n\tfor i, n := range names {\n\t\tif i > 0 {\n\t\t\tout += sep\n\t\t}\n\t\tout += n\n\t}\n\treturn out\n}\n",
}

// Two functions that share no fingerprint and no vocabulary: their pair
// scores exactly 0 and carries no evidence of duplication.
var zeroEvidenceFixture = map[string]string{
	"a.go": "package sample\nfunc first() {\n return\n}\n",
	"b.go": "package sample\nfunc second() {\n return\n}\n",
}

func pairScores(t *testing.T, bin string, flags []string, dir string) []float64 {
	t.Helper()
	args := append([]string{"--json", "--no-cache", "--no-progress"}, flags...)
	args = append(args, dir)
	out, err := exec.Command(bin, args...).Output()
	if err != nil {
		t.Fatalf("run %v: %v\n%s", flags, err, out)
	}
	var doc struct {
		Pairs []struct {
			Score float64 `json:"score"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	scores := make([]float64, 0, len(doc.Pairs))
	for _, p := range doc.Pairs {
		scores = append(scores, p.Score)
	}
	return scores
}

func TestWeakPairs_VerboseReachesDownToMinimumFloor(t *testing.T) {
	bin := subprocessBin(t)
	dir := t.TempDir()
	mustWriteFiles(t, dir, weakPairFixture)

	// --verbose bypasses the threshold-relative band (floor 0.75 at
	// threshold 0.95) down to the 0.30 minimum: two of the three pairs.
	scores := pairScores(t, bin, []string{"--threshold", "0.95", "--verbose"}, dir)
	if len(scores) != 2 {
		t.Fatalf("--threshold 0.95 --verbose: got %d pairs %v, want the two pairs scoring ≥ 0.30", len(scores), scores)
	}
	for _, s := range scores {
		if s < 0.30 || s >= 0.95 {
			t.Errorf("--verbose surfaced a pair outside [0.30, 0.95): %v", s)
		}
	}

	// --threshold 0 is an explicit request for everything with evidence.
	scores = pairScores(t, bin, []string{"--threshold", "0"}, dir)
	if len(scores) != 3 {
		t.Fatalf("--threshold 0: got %d pairs %v, want all three", len(scores), scores)
	}
}

func TestWeakPairs_ZeroScorePairsNeverReported(t *testing.T) {
	bin := subprocessBin(t)
	dir := t.TempDir()
	mustWriteFiles(t, dir, zeroEvidenceFixture)
	for _, flags := range [][]string{{"--threshold", "0"}, {"--threshold", "0.95", "--verbose"}, {"--verbose"}} {
		flags = append(flags, "--min-lines", "3") // admit the 3-line fixture
		if scores := pairScores(t, bin, flags, dir); len(scores) != 0 {
			t.Errorf("%v: reported %v for a pair with no shared evidence", flags, scores)
		}
	}
}

func TestSimilarity_SingleFileWithTwoFunctions(t *testing.T) {
	bin := subprocessBin(t)
	dir := t.TempDir()
	mustWriteFiles(t, dir, map[string]string{
		"a.go": "package p\nfunc first() {\n println(1)\n println(2)\n println(3)\n}\nfunc second() {\n println(1)\n println(2)\n println(3)\n}\n",
	})
	out, err := exec.Command(bin, "--json", "--no-cache", "--no-progress", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	var doc struct {
		Pairs []json.RawMessage `json:"pairs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Pairs) != 1 {
		t.Fatalf("got %d pairs, want one same-file clone\n%s", len(doc.Pairs), out)
	}
}

func TestIgnoreFlag_SkipsMatchingPaths(t *testing.T) {
	bin := subprocessBin(t)
	dir := t.TempDir()
	files := map[string]string{}
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		files[name] = weakPairFixture[name]
	}
	files["gen/a.go"] = weakPairFixture["a.go"]
	files["gen/c.go"] = weakPairFixture["c.go"]
	mustWriteFiles(t, dir, files)

	out, err := exec.Command(bin, "--json", "--no-cache", "--no-progress", "--threshold", "0",
		"--ignore", "gen", "--ignore", "b.go", dir).Output()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	var doc struct {
		Pairs []struct {
			FileA string `json:"file_a"`
			FileB string `json:"file_b"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Pairs) != 1 {
		t.Fatalf("got %d pairs, want only a.go↔c.go\n%s", len(doc.Pairs), out)
	}
	for _, name := range []string{doc.Pairs[0].FileA, doc.Pairs[0].FileB} {
		if strings.Contains(name, "gen/") || strings.Contains(name, "b.go") {
			t.Errorf("--ignore did not exclude %s", name)
		}
	}
}
