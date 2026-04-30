package refactor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// loadSnippets reads the {a,b}.<ext> pair under the given fixture
// directory, runs the same pipeline scan.ProcessFile uses (split →
// tokenize → fingerprint), and returns the *function-level* chunk
// from each. v1 fixtures are crafted so the function under test is
// either the first chunk or the only top-level function of interest;
// loadSnippetFromFile picks the chunk whose Symbol starts with the
// fixture's prefix when there are multiple, falling back to the first.
func loadSnippets(t *testing.T, dir string) (scan.Snippet, scan.Snippet) {
	t.Helper()
	// Special case: reject-anon fixtures want the goroutine chunk, not
	// the outer wrapper function. The wrapper exists only because Go
	// requires goroutines inside a function body.
	if strings.HasSuffix(dir, "reject-anon") {
		return loadSnippetsByPredicate(t, dir, func(c splitter.Chunk) bool {
			return strings.HasPrefix(c.Symbol, "goroutine@")
		})
	}
	return loadSnippetsByPredicate(t, dir, nil)
}

func loadSnippetsByPredicate(t *testing.T, dir string, pick func(splitter.Chunk) bool) (scan.Snippet, scan.Snippet) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var aPath, bPath string
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "a.") || name == "A.java":
			aPath = filepath.Join(dir, name)
		case strings.HasPrefix(name, "b.") || name == "B.java":
			bPath = filepath.Join(dir, name)
		}
	}
	if aPath == "" || bPath == "" {
		t.Fatalf("fixture %s missing a/b file (entries: %v)", dir, entries)
	}
	return loadSnippetFromFile(t, aPath, "a", pick), loadSnippetFromFile(t, bPath, "b", pick)
}

func loadSnippetFromFile(t *testing.T, path, prefer string, pick func(splitter.Chunk) bool) scan.Snippet {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	code := string(data)
	lang := tokenizer.Detect(path, code)
	chunks := splitter.Split(path, code, lang)
	if len(chunks) == 0 {
		t.Fatalf("no chunks from %s", path)
	}
	var ch splitter.Chunk
	picked := false
	if pick != nil {
		for _, c := range chunks {
			if pick(c) {
				ch = c
				picked = true
				break
			}
		}
	}
	// Default: prefer a chunk whose Symbol matches the fixture's
	// a/b suffix (priceWithTaxA, formatAdminB, ...); fall back to the
	// longest chunk so we don't pick up a tiny helper like round2.
	if !picked {
		for _, c := range chunks {
			if c.Symbol != "" && strings.Contains(strings.ToLower(c.Symbol), prefer) {
				ch = c
				picked = true
				break
			}
		}
	}
	if !picked {
		ch = longestChunk(chunks)
	}
	tokens, lines := tokenizer.TokenizeWithLines(ch.Code, lang)
	ps := fingerprint.GeneratePositional(tokens, fingerprint.DefaultK, fingerprint.DefaultW)
	return scan.Snippet{
		Name:      ch.Name(),
		Path:      path,
		Lang:      lang,
		Code:      ch.Code,
		StartLine: ch.StartLine,
		EndLine:   ch.EndLine,
		Tokens:    tokens,
		Lines:     lines,
		Fps:       ps,
	}
}

func longestChunk(chunks []splitter.Chunk) splitter.Chunk {
	out := chunks[0]
	for _, c := range chunks[1:] {
		if len(c.Code) > len(out.Code) {
			out = c
		}
	}
	return out
}

// alignmentExpectation describes what we assert about Align's output
// for a fixture. We don't assert on exact line indices because those
// are sensitive to the LCS tie-break; we assert on *shape* — common
// line ratio, number of holes, and what each hole's source text
// contains.
type alignmentExpectation struct {
	dir            string
	minCommonRatio float64  // CommonLines / max(linesA, linesB)
	maxHoles       int      // Holes count must be in [1..maxHoles]; 0 to skip
	holeAContains  []string // each value must appear in *some* hole's AText
	holeBContains  []string
}

func TestAlign_FixturesAcrossLanguages(t *testing.T) {
	cases := []alignmentExpectation{
		{
			dir:            "../../testdata/refactor/go/simple",
			minCommonRatio: 0.5,
			maxHoles:       2,
			holeAContains:  []string{"0.07"},
			holeBContains:  []string{"0.085"},
		},
		{
			dir:            "../../testdata/refactor/go/medium",
			minCommonRatio: 0.4,
			maxHoles:       4,
			holeAContains:  []string{"formatUserA", `"user:"`, `"(active)"`},
			holeBContains:  []string{"formatAdminB", `"admin:"`, `"(privileged)"`},
		},
		{
			dir:            "../../testdata/refactor/go/advanced",
			minCommonRatio: 0.4,
			maxHoles:       4,
			holeAContains:  []string{"backoffStepA", "base * 2"},
			holeBContains:  []string{"backoffStepB", "base + 5"},
		},
		{
			dir:            "../../testdata/refactor/python/simple",
			minCommonRatio: 0.4,
			maxHoles:       3,
			holeAContains:  []string{"price_with_tax_a", "0.07"},
			holeBContains:  []string{"price_with_tax_b", "0.085"},
		},
		{
			dir:            "../../testdata/refactor/js/simple",
			minCommonRatio: 0.5,
			maxHoles:       3,
			holeAContains:  []string{"priceWithTaxA", "0.07"},
			holeBContains:  []string{"priceWithTaxB", "0.085"},
		},
		{
			dir:            "../../testdata/refactor/rust/simple",
			minCommonRatio: 0.5,
			maxHoles:       3,
			holeAContains:  []string{"price_with_tax_a", "0.07"},
			holeBContains:  []string{"price_with_tax_b", "0.085"},
		},
		{
			dir:            "../../testdata/refactor/java/simple",
			minCommonRatio: 0.4,
			maxHoles:       3,
			holeAContains:  []string{"priceWithTaxA", "0.07"},
			holeBContains:  []string{"priceWithTaxB", "0.085"},
		},
		{
			// After the Elixir splitter shipped, chunks are the inner
			// `def price_with_tax(amount) do` (identical between A and B)
			// — so the wrapper module names `TaxA`/`TaxB` no longer
			// appear in the alignment. The only divergence is the tax
			// rate literal.
			dir:            "../../testdata/refactor/elixir/simple",
			minCommonRatio: 0.4,
			maxHoles:       3,
			holeAContains:  []string{"0.07"},
			holeBContains:  []string{"0.085"},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.dir, func(t *testing.T) {
			a, b := loadSnippets(t, c.dir)
			al := Align(a, b)

			aLines := strings.Count(strings.TrimRight(a.Code, "\n"), "\n") + 1
			bLines := strings.Count(strings.TrimRight(b.Code, "\n"), "\n") + 1
			maxLines := aLines
			if bLines > maxLines {
				maxLines = bLines
			}
			ratio := float64(al.CommonLines()) / float64(maxLines)
			if ratio < c.minCommonRatio {
				t.Errorf("CommonLines ratio = %.2f, want >= %.2f (a=%d, b=%d, common=%d)",
					ratio, c.minCommonRatio, aLines, bLines, al.CommonLines())
			}

			if c.maxHoles > 0 && (len(al.Holes) < 1 || len(al.Holes) > c.maxHoles) {
				t.Errorf("hole count = %d, want 1..%d", len(al.Holes), c.maxHoles)
				for i, h := range al.Holes {
					t.Logf("  hole[%d] A=%q B=%q", i, h.AText, h.BText)
				}
				return
			}

			for _, want := range c.holeAContains {
				if !anyHoleContains(al.Holes, true, want) {
					t.Errorf("no hole AText contains %q. holes:", want)
					for i, h := range al.Holes {
						t.Logf("  hole[%d] AText=%q", i, h.AText)
					}
				}
			}
			for _, want := range c.holeBContains {
				if !anyHoleContains(al.Holes, false, want) {
					t.Errorf("no hole BText contains %q. holes:", want)
					for i, h := range al.Holes {
						t.Logf("  hole[%d] BText=%q", i, h.BText)
					}
				}
			}
		})
	}
}

func anyHoleContains(holes []Hole, sideA bool, want string) bool {
	for _, h := range holes {
		text := h.BText
		if sideA {
			text = h.AText
		}
		if strings.Contains(text, want) {
			return true
		}
	}
	return false
}

func TestAlign_IdenticalSnippets_NoHoles(t *testing.T) {
	a, _ := loadSnippets(t, "../../testdata/refactor/go/simple")
	al := Align(a, a)
	if len(al.Holes) != 0 {
		t.Errorf("identical snippets should produce 0 holes, got %d", len(al.Holes))
	}
	aLines := strings.Count(strings.TrimRight(a.Code, "\n"), "\n") + 1
	if al.CommonLines() != aLines {
		t.Errorf("identical snippets should have CommonLines == lines (%d), got %d",
			aLines, al.CommonLines())
	}
}

func TestAlign_EmptyCode_OneHole(t *testing.T) {
	a := scan.Snippet{Code: "", Tokens: nil, Lines: nil}
	b := scan.Snippet{Code: "x\ny", Tokens: []string{"a", "b"}, Lines: []int{1, 2}}
	al := Align(a, b)
	if len(al.Holes) != 1 {
		t.Errorf("empty A vs nonempty B: want 1 hole, got %d", len(al.Holes))
	}
}
