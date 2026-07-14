package report

import (
	"strings"
	"testing"
)

func mkBlock(fileA string, aStart, aEnd int, symA, fileB string, bStart, bEnd int, symB string, cont float64, lines int) BlockClone {
	b := BlockClone{
		FileA: fileA, SymbolA: symA, AStartLine: aStart, AEndLine: aEnd,
		FileB: fileB, SymbolB: symB, BStartLine: bStart, BEndLine: bEnd,
		Containment: cont, LinesA: lines, LinesB: lines,
	}
	b.ID = PairID(b.RangeNameA(), b.RangeNameB())
	return b
}

func TestPrepareBlocks_SuppressesTestTestByDefault(t *testing.T) {
	prod := mkBlock("a.go", 10, 20, "F", "b.go", 30, 40, "G", 0.9, 10)
	testTest := mkBlock("a_test.go", 10, 20, "TestF", "b_test.go", 30, 40, "TestG", 0.95, 10)
	testTest.IsTestA, testTest.IsTestB = true, true
	mixed := mkBlock("c_test.go", 5, 15, "TestH", "d.go", 5, 15, "H", 0.92, 10)
	mixed.IsTestA = true

	visible, suppressed := PrepareBlocks([]BlockClone{prod, testTest, mixed}, Options{})
	if suppressed != 1 {
		t.Errorf("suppressed = %d, want 1 (only the test↔test block)", suppressed)
	}
	if len(visible) != 2 {
		t.Fatalf("visible = %d, want 2 (production + mixed)", len(visible))
	}
	for _, b := range visible {
		if b.IsTestA && b.IsTestB {
			t.Errorf("test↔test block leaked into visible set: %+v", b)
		}
	}

	visible, suppressed = PrepareBlocks([]BlockClone{prod, testTest, mixed}, Options{IncludeTests: true})
	if suppressed != 0 || len(visible) != 3 {
		t.Errorf("--include-tests: visible=%d suppressed=%d, want 3/0", len(visible), suppressed)
	}
}

func TestPrepareBlocks_SortsByContainmentAndAppliesLimit(t *testing.T) {
	blocks := []BlockClone{
		mkBlock("a.go", 1, 10, "A", "b.go", 1, 10, "B", 0.86, 8),
		mkBlock("c.go", 1, 20, "C", "d.go", 1, 20, "D", 1.0, 18),
		mkBlock("e.go", 1, 12, "E", "f.go", 1, 12, "F", 0.93, 10),
	}
	visible, _ := PrepareBlocks(blocks, Options{})
	if visible[0].Containment != 1.0 || visible[2].Containment != 0.86 {
		t.Errorf("blocks not sorted by containment desc: %+v", visible)
	}

	limited, _ := PrepareBlocks(blocks, Options{Limit: 1})
	if len(limited) != 1 || limited[0].Containment != 1.0 {
		t.Errorf("limit 1 should keep only the best block, got %+v", limited)
	}
}

func TestPrepareBlocks_ThresholdDoesNotFilter(t *testing.T) {
	// Containment is the block quality bar; the pair-score threshold
	// must never drop a block finding.
	b := mkBlock("a.go", 1, 10, "A", "b.go", 1, 10, "B", 0.85, 8)
	visible, _ := PrepareBlocks([]BlockClone{b}, Options{Threshold: 0.99})
	if len(visible) != 1 {
		t.Errorf("threshold 0.99 dropped a block finding; containment is the quality bar")
	}
}

func TestRender_PartialClonesSection(t *testing.T) {
	b := mkBlock("orders.go", 120, 134, "ProcessOrders", "invoices.go", 88, 102, "SummarizeInvoices", 0.92, 14)
	opts := Options{Plain: true, Threshold: 0.5, PartialClones: []BlockClone{b}}
	var sb strings.Builder
	Render(&sb, nil, nil, opts)
	out := sb.String()

	for _, want := range []string{
		"PARTIAL CLONES",
		"[PARTIAL CLONE   ]",
		"92% contained",
		"orders.go:120-134 ⊂ ProcessOrders",
		"invoices.go:88-102 ⊂ SummarizeInvoices",
		"Partial clones",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered report missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "No similarities found") {
		t.Errorf("block-only report must not print the empty banner:\n%s", out)
	}
}

func TestRender_NoBlocksNoSection(t *testing.T) {
	var sb strings.Builder
	Render(&sb, nil, nil, Options{Plain: true, Threshold: 0.5})
	out := sb.String()
	if strings.Contains(out, "PARTIAL CLONES") || strings.Contains(out, "Partial clones") {
		t.Errorf("empty report must not mention partial clones:\n%s", out)
	}
	if !strings.Contains(out, "No similarities found") {
		t.Errorf("expected the empty banner:\n%s", out)
	}
}

func TestRender_SuppressedBlocksLine(t *testing.T) {
	var sb strings.Builder
	opts := Options{Plain: true, Threshold: 0.5,
		Suppressed: Suppressed{TestTestBlocks: 7}}
	Render(&sb, nil, nil, opts)
	out := sb.String()
	if !strings.Contains(out, "7 test↔test partial clones suppressed (--include-tests to show)") {
		t.Errorf("expected suppressed partial-clones line:\n%s", out)
	}
}

func TestRender_SymbollessBlockSideOmitsContainer(t *testing.T) {
	b := mkBlock("a.go", 1, 10, "", "b.go", 1, 10, "G", 0.9, 10)
	var sb strings.Builder
	Render(&sb, nil, nil, Options{Plain: true, Threshold: 0.5, PartialClones: []BlockClone{b}})
	out := sb.String()
	if !strings.Contains(out, "a.go:1-10\n") {
		t.Errorf("symbol-less side should render bare range:\n%s", out)
	}
	if !strings.Contains(out, "b.go:1-10 ⊂ G") {
		t.Errorf("symbol side should render container:\n%s", out)
	}
}
