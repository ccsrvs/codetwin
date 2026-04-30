package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ccsrvs/codetwin/internal/report"
)

func TestRequestedGitFlags_LabelsAndVerbsAreGrammatical(t *testing.T) {
	cases := []struct {
		name      string
		since     string
		blame     bool
		wantLabel string
		wantVerb  string
	}{
		{"since only", "main", false, "--since", "requires"},
		{"blame only", "", true, "--blame", "requires"},
		{"both", "main", true, "--since and --blame", "require"},
		// Defensive: the function should still produce a sensible
		// label even when called with no flags set, since the
		// caller is responsible for not invoking it in that case.
		{"neither", "", false, "git-dependent flags", "require"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			label, verb := requestedGitFlags(c.since, c.blame)
			if label != c.wantLabel {
				t.Errorf("label = %q, want %q", label, c.wantLabel)
			}
			if verb != c.wantVerb {
				t.Errorf("verb = %q, want %q", verb, c.wantVerb)
			}
		})
	}
}

func TestAttachProvenance_AppliesByName(t *testing.T) {
	t1 := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	pairs := []report.Pair{
		{NameA: "a", NameB: "b"},
		{NameA: "c", NameB: "d"},
	}
	provs := map[string]*report.Provenance{
		"a": {FirstCommit: "deadbeef", FirstAuthor: "Alice", FirstTime: t1},
		"d": {FirstCommit: "cafebabe", FirstAuthor: "Dan", FirstTime: t1},
	}

	attachProvenance(pairs, provs)

	if pairs[0].ProvenanceA == nil || pairs[0].ProvenanceA.FirstCommit != "deadbeef" {
		t.Errorf("pair 0 ProvenanceA = %+v, want Alice/deadbeef", pairs[0].ProvenanceA)
	}
	if pairs[0].ProvenanceB != nil {
		t.Errorf("pair 0 ProvenanceB = %+v, want nil (b has no provenance)", pairs[0].ProvenanceB)
	}
	if pairs[1].ProvenanceA != nil {
		t.Errorf("pair 1 ProvenanceA = %+v, want nil (c has no provenance)", pairs[1].ProvenanceA)
	}
	if pairs[1].ProvenanceB == nil || pairs[1].ProvenanceB.FirstAuthor != "Dan" {
		t.Errorf("pair 1 ProvenanceB = %+v, want Dan/cafebabe", pairs[1].ProvenanceB)
	}
}

func TestAttachProvenance_EmptyMapLeavesPairsUntouched(t *testing.T) {
	pairs := []report.Pair{{NameA: "a", NameB: "b"}}
	attachProvenance(pairs, map[string]*report.Provenance{})
	if pairs[0].ProvenanceA != nil || pairs[0].ProvenanceB != nil {
		t.Errorf("pair should remain bare, got %+v", pairs[0])
	}
}

func TestToJSONProvenance_NilInputReturnsNil(t *testing.T) {
	if got := toJSONProvenance(nil); got != nil {
		t.Errorf("toJSONProvenance(nil) = %+v, want nil", got)
	}
}

func TestToJSONProvenance_OmitsLastWhenSameAsFirst(t *testing.T) {
	t1 := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	prov := &report.Provenance{
		FirstCommit: "abc123def",
		FirstAuthor: "Alice",
		FirstTime:   t1,
		LastCommit:  "abc123def", // same as First → "untouched since"
		LastAuthor:  "Alice",
		LastTime:    t1,
	}
	got := toJSONProvenance(prov)
	if got.FirstDate != "2025-06-01" {
		t.Errorf("FirstDate = %q, want 2025-06-01", got.FirstDate)
	}
	if got.LastCommit != "" || got.LastAuthor != "" || got.LastDate != "" {
		t.Errorf("Last fields should be empty when same as First, got %+v", got)
	}
}

func TestToJSONProvenance_PopulatesLastWhenDistinct(t *testing.T) {
	first := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	prov := &report.Provenance{
		FirstCommit: "aaa1111", FirstAuthor: "Alice", FirstTime: first,
		LastCommit: "bbb2222", LastAuthor: "Bob", LastTime: last,
	}
	got := toJSONProvenance(prov)
	if got.FirstDate != "2024-01-01" {
		t.Errorf("FirstDate = %q, want 2024-01-01", got.FirstDate)
	}
	if got.LastDate != "2025-06-01" {
		t.Errorf("LastDate = %q, want 2025-06-01", got.LastDate)
	}
	if got.LastAuthor != "Bob" || got.LastCommit != "bbb2222" {
		t.Errorf("Last fields wrong: %+v", got)
	}
}

func TestToJSONProvenance_FirstDateNormalizedToUTC(t *testing.T) {
	// FirstTime in a non-UTC zone should still serialize as the UTC
	// calendar date so the JSON output is reproducible regardless of
	// the user's local clock.
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("Asia/Tokyo zoneinfo not available: %v", err)
	}
	tokyoMidnight := time.Date(2025, 6, 1, 0, 0, 0, 0, loc) // = 2025-05-31T15:00 UTC
	prov := &report.Provenance{FirstCommit: "abc", FirstTime: tokyoMidnight}
	got := toJSONProvenance(prov)
	if got.FirstDate != "2025-05-31" {
		t.Errorf("FirstDate = %q, want 2025-05-31 (UTC of Tokyo midnight)", got.FirstDate)
	}
	// Sanity check we didn't break formatting.
	if !strings.HasPrefix(got.FirstDate, "2025-") {
		t.Errorf("FirstDate %q malformed", got.FirstDate)
	}
}
