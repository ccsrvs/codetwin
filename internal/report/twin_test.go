package report

import (
	"strings"
	"testing"
)

// Structural-twin label gate: pairs above StructuralTwinMinScore whose
// lexical overlap is below StructuralTwinMaxLexical render as
// STRUCTURAL TWIN. The numeric score is never altered; pairs at or
// below the band boundary are never modified.

func TestJSONLabel_StructuralTwinGate(t *testing.T) {
	cases := []struct {
		name    string
		score   float64
		lexical float64
		lexOK   bool // LexicalComputed
		want    string
	}{
		{"exact band, disjoint vocabulary", 0.99, 0.05, true, "structural_twin"},
		{"exact band, measured zero lexical", 0.99, 0.0, true, "structural_twin"},
		{"near band, disjoint vocabulary", 0.90, 0.05, true, "structural_twin"},
		{"exact band, shared vocabulary", 0.99, 0.50, true, "exact_clone"},
		{"exact band, lexical exactly at floor keeps label", 0.99, StructuralTwinMaxLexical, true, "exact_clone"},
		{"exact band, lexical not computed", 0.99, 0.0, false, "exact_clone"},
		{"at the band boundary, never modified", 0.85, 0.0, true, "strong_clone"},
		{"below the band, never modified", 0.70, 0.0, true, "strong_clone"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Pair{
				Score:  c.score,
				LinesA: 30, LinesB: 30, // above the exact-clone evidence gate
				Lexical: c.lexical, LexicalComputed: c.lexOK,
			}
			if got := JSONLabel(p); got != c.want {
				t.Errorf("JSONLabel(score=%.2f, lex=%.2f, computed=%v) = %q; want %q",
					c.score, c.lexical, c.lexOK, got, c.want)
			}
		})
	}
}

// Precedence: the content check runs before the length gate — a short,
// content-divergent pair is a structural twin (the more specific
// finding), not a "near clone (short)". A short pair with shared
// vocabulary still demotes exact → near under the length gate.
func TestJSONLabel_TwinGatePrecedesLengthGate(t *testing.T) {
	shortDivergent := Pair{Score: 1.0, LinesA: 5, LinesB: 5, Lexical: 0.05, LexicalComputed: true}
	if got := JSONLabel(shortDivergent); got != "structural_twin" {
		t.Errorf("short content-divergent pair = %q; want structural_twin (content check first)", got)
	}
	shortShared := Pair{Score: 1.0, LinesA: 5, LinesB: 5, Lexical: 0.80, LexicalComputed: true}
	if got := JSONLabel(shortShared); got != "near_clone" {
		t.Errorf("short shared-vocabulary pair = %q; want near_clone (length gate)", got)
	}
}

func TestRender_StructuralTwinLabelAndSummary(t *testing.T) {
	var buf strings.Builder
	pairs := []Pair{{
		NameA: "a.go:1-20 TestParse", NameB: "b.go:1-20 TestBadge",
		Score: 1.0, Structural: 1.0, Semantic: 1.0,
		LinesA: 20, LinesB: 20,
		Lexical: 0.07, LexicalComputed: true,
	}}
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.50})
	out := buf.String()
	if !strings.Contains(out, "STRUCTURAL TWIN") {
		t.Errorf("expected STRUCTURAL TWIN label in output:\n%s", out)
	}
	if !strings.Contains(out, "Structural twins") {
		t.Errorf("expected a 'Structural twins' summary bucket:\n%s", out)
	}
	if strings.Contains(out, "EXACT CLONE") {
		t.Errorf("content-divergent pair must not render as EXACT CLONE:\n%s", out)
	}
	if !strings.Contains(out, "lexical:   7%") {
		t.Errorf("expected the lexical sub-score line:\n%s", out)
	}
}

// Pairs whose lexical score was never computed must render the
// pre-lexical sub-score line unchanged.
func TestRender_NoLexicalLineWhenNotComputed(t *testing.T) {
	var buf strings.Builder
	pairs := []Pair{{NameA: "a.go", NameB: "b.go", Score: 0.70, Structural: 0.7, Semantic: 0.7, LinesA: 20, LinesB: 20}}
	Render(&buf, pairs, nil, Options{Plain: true, Threshold: 0.50})
	if strings.Contains(buf.String(), "lexical:") {
		t.Errorf("uncomputed lexical must not render:\n%s", buf.String())
	}
}
