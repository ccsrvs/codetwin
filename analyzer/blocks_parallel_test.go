package analyzer

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
)

// Block detection runs on a worker pool; its output must not depend on
// the worker count or on scheduling.
func TestDetectBlockClonesIsIndependentOfWorkerCount(t *testing.T) {
	files, err := filepath.Glob("../testdata/bench/blocks/positive/*/*")
	if err != nil || len(files) < 4 {
		t.Fatalf("fixtures: %v (%d files)", err, len(files))
	}
	snippets, warnings := scan.ProcessFiles(files, 3, nil, nil, "", scan.GranularityFunction, nil)
	if len(warnings) > 0 {
		t.Fatalf("scan warnings: %v", warnings)
	}
	var candidates [][2]int
	for i := range snippets {
		for j := i + 1; j < len(snippets); j++ {
			candidates = append(candidates, [2]int{i, j})
		}
	}
	noProgress := func(Progress) {}

	serial, err := detectBlockClonesWorkers(context.Background(), candidates, snippets, 8, nil, noProgress, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(serial) == 0 {
		t.Fatal("fixtures produced no block clones; the comparison would be vacuous")
	}
	for _, workers := range []int{2, 7, 64} {
		for run := 0; run < 3; run++ {
			got, err := detectBlockClonesWorkers(context.Background(), candidates, snippets, 8, nil, noProgress, workers)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, serial) {
				t.Fatalf("workers=%d run=%d: %d clones differ from the serial %d", workers, run, len(got), len(serial))
			}
		}
	}
}

func TestDedupeBlockClonesKeepsFirstOverlapPerFilePair(t *testing.T) {
	clone := func(fa, fb string, as, ae, bs, be int) report.BlockClone {
		return report.BlockClone{FileA: fa, FileB: fb, AStartLine: as, AEndLine: ae, BStartLine: bs, BEndLine: be,
			LinesA: ae - as + 1, LinesB: be - bs + 1, Containment: 1}
	}
	in := []report.BlockClone{
		clone("a.go", "b.go", 1, 20, 1, 20),
		clone("a.go", "b.go", 5, 15, 5, 15),   // overlaps the first on both sides
		clone("a.go", "b.go", 5, 15, 40, 50),  // overlaps only on A
		clone("a.go", "c.go", 1, 20, 1, 20),   // different file pair
		clone("a.go", "b.go", 30, 45, 60, 75), // disjoint
	}
	got := dedupeBlockClones(in)
	if len(got) != 4 {
		t.Fatalf("kept %d clones, want 4: %+v", len(got), got)
	}
	for _, c := range got {
		if c.FileB == "b.go" && c.AStartLine == 5 && c.BStartLine == 5 {
			t.Errorf("clone overlapping a kept clone on both sides survived: %+v", c)
		}
	}
}
