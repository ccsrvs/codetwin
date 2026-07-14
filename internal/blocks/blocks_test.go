package blocks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// snippetFrom runs code through the real tokenizer + fingerprint
// pipeline, mirroring what scan.ProcessFile produces for one chunk.
func snippetFrom(t *testing.T, code string) scan.Snippet {
	t.Helper()
	tokens, lines := tokenizer.TokenizeWithLines(code, tokenizer.Go)
	if len(tokens) == 0 {
		t.Fatal("no tokens produced")
	}
	fps := fingerprint.GeneratePositional(tokens, fingerprint.DefaultK, fingerprint.DefaultW)
	return scan.Snippet{
		Name: "test.go:1-999", Code: code, StartLine: 1,
		Lang: tokenizer.Go, Tokens: tokens, Lines: lines, Fps: fps,
	}
}

// sharedBlock is a 10-non-blank-line body reused by the end-to-end
// tests below.
const sharedBlock = `	if cfg == nil {
		return errNil
	}
	total := 0
	for _, it := range cfg.Items {
		if it.Count < 0 {
			return errNegative
		}
		total += it.Count
	}`

func TestDetect_VerbatimBlockInDistinctHosts(t *testing.T) {
	a := snippetFrom(t, `func hostA(cfg *Config, w io.Writer) error {
`+sharedBlock+`
	fmt.Fprintf(w, "total=%d rows=%d", total, len(cfg.Items))
	w.Write(headerBytes)
	return flushBuffers(w, total)
}`)
	b := snippetFrom(t, `func hostB(cfg *Config, db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
`+sharedBlock+`
	if _, execErr := tx.Exec(updateTotals, total); execErr != nil {
		return execErr
	}
	return tx.Commit()
}`)
	got := Detect(a, b, 8)
	if len(got) != 1 {
		t.Fatalf("Detect = %d matches, want 1: %+v", len(got), got)
	}
	m := got[0]
	// Block body sits at a lines 2-11, b lines 6-15 (±1 for boundary
	// tokens shared with the surrounding syntax).
	if m.AStartLine > 3 || m.AEndLine < 10 || m.BStartLine > 7 || m.BEndLine < 14 {
		t.Errorf("match ranges a:%d-%d b:%d-%d do not cover the shared block", m.AStartLine, m.AEndLine, m.BStartLine, m.BEndLine)
	}
	if m.Containment < 0.95 {
		t.Errorf("verbatim containment = %.2f, want >= 0.95", m.Containment)
	}
	if m.ALines < 8 || m.BLines < 8 {
		t.Errorf("non-blank line counts %d/%d, want >= 8", m.ALines, m.BLines)
	}
}

func TestDetect_LineFloorBothSides(t *testing.T) {
	a := snippetFrom(t, `func hostA(cfg *Config, w io.Writer) error {
`+sharedBlock+`
	fmt.Fprintf(w, "total=%d rows=%d", total, len(cfg.Items))
	w.Write(headerBytes)
	return flushBuffers(w, total)
}`)
	b := snippetFrom(t, `func hostB(cfg *Config, db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
`+sharedBlock+`
	if _, execErr := tx.Exec(updateTotals, total); execErr != nil {
		return execErr
	}
	return tx.Commit()
}`)
	// The shared block is ~10-12 non-blank lines including boundary
	// overlap; a floor above that must reject it on both sides.
	if got := Detect(a, b, 20); len(got) != 0 {
		t.Errorf("Detect with min-block-lines 20 = %d matches, want 0: %+v", len(got), got)
	}
	// A floor of 0 disables detection entirely.
	if got := Detect(a, b, 0); got != nil {
		t.Errorf("Detect with min-block-lines 0 = %v, want nil", got)
	}
}

func TestDetect_GapBridgedAcrossOneDivergentLine(t *testing.T) {
	// Same block in both hosts except one edited middle line: the two
	// exact segments must chain into a single match covering the whole
	// block, with containment above the verification floor.
	mk := func(mid string) scan.Snippet {
		return snippetFrom(t, `func host(cfg *Config) error {
	if cfg == nil {
		return errNil
	}
	total := 0
	for _, it := range cfg.Items {
		if it.Count < 0 {
			return errNegative
		}
		`+mid+`
		total += it.Count
	}
	if total > cfg.Ceiling {
		return errOverflow
	}
	return nil
}`)
	}
	a := mk(`audit.Record(it.SKU, it.Count)`)
	b := mk(`metrics.ObserveBatch(it.Region, float64(it.Count), clock.Now())`)
	got := Detect(a, b, 8)
	if len(got) != 1 {
		t.Fatalf("Detect = %d matches, want 1 bridged match: %+v", len(got), got)
	}
	m := got[0]
	if m.AStartLine > 2 || m.AEndLine < 15 {
		t.Errorf("bridged match a:%d-%d does not span the divergent line", m.AStartLine, m.AEndLine)
	}
	if m.Containment < MinContainment {
		t.Errorf("bridged containment = %.2f, want >= %.2f", m.Containment, MinContainment)
	}
}

func TestDetect_WideDivergenceDoesNotChain(t *testing.T) {
	// Two shared 5-line runs separated by a wide stretch of genuinely
	// divergent logic (different normalized token shapes, well over the
	// gap budget): the runs must not chain, and since neither run alone
	// reaches the 8-line floor, no match may be emitted.
	mk := func(mid string) scan.Snippet {
		return snippetFrom(t, `func host(cfg *Config, w *Worker) error {
	if cfg == nil || cfg.Region == "" {
		return errNilConfigValue
	}
	limit := cfg.Burst * cfg.RefillRate
	acquireQuota(cfg.Region, limit)
`+mid+`
	if depth := traceDepth(cfg); depth > maxTraceDepth {
		return errTraceTooDeep
	}
	commitQuota(cfg.Region, limit)
	return nil
}`)
	}
	a := mk(`	switch cfg.Mode {
	case modeFast:
		w.enableTurbo()
	case modeSlow:
		w.throttle(cfg.Delay)
	default:
		return errUnknownMode
	}`)
	b := mk(`	done := make(chan error, len(cfg.Endpoints))
	for _, ep := range cfg.Endpoints {
		go probe(ep, done)
	}
	for range cfg.Endpoints {
		if perr := <-done; perr != nil {
			return perr
		}
	}`)
	if got := Detect(a, b, 8); len(got) != 0 {
		t.Errorf("Detect = %d matches, want 0 (runs must not chain across a wide divergence): %+v", len(got), got)
	}
}

func TestDetect_Deterministic(t *testing.T) {
	a := snippetFrom(t, `func hostA(cfg *Config, w io.Writer) error {
`+sharedBlock+`
	fmt.Fprintf(w, "total=%d", total)
	return nil
}`)
	b := snippetFrom(t, `func hostB(cfg *Config) (int, error) {
	started := clock.Now()
`+sharedBlock+`
	observeDuration(clock.Since(started))
	return total, nil
}`)
	first := Detect(a, b, 8)
	for i := 0; i < 10; i++ {
		if got := Detect(a, b, 8); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs: %+v vs %+v", i, got, first)
		}
	}
}

func TestExtendSeed_GrowsToMaximalRun(t *testing.T) {
	ta := strings.Fields("x x A B C D E F y y")
	tb := strings.Fields("z A B C D E F w")
	// Seed on the 3-gram "C D E" (a: pos 4, b: pos 3).
	s := extendSeed(ta, tb, 4, 3, 3)
	want := segment{aStart: 2, bStart: 1, length: 6}
	if s != want {
		t.Errorf("extendSeed = %+v, want %+v", s, want)
	}
}

func TestChainable_GapBudget(t *testing.T) {
	x := segment{aStart: 0, bStart: 0, length: 10} // ends at 9/9
	cases := []struct {
		name string
		y    segment
		gap  int
		want bool
	}{
		{"adjacent", segment{aStart: 10, bStart: 10, length: 5}, 0, true},
		{"within budget", segment{aStart: 15, bStart: 12, length: 5}, 5, true},
		{"a-side gap too wide", segment{aStart: 16, bStart: 12, length: 5}, 5, false},
		{"b-side gap too wide", segment{aStart: 12, bStart: 16, length: 5}, 5, false},
		{"overlap on a", segment{aStart: 9, bStart: 12, length: 5}, 5, false},
		{"overlap on b", segment{aStart: 12, bStart: 9, length: 5}, 5, false},
	}
	for _, c := range cases {
		if got := chainable(x, c.y, c.gap); got != c.want {
			t.Errorf("%s: chainable = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestExtractBestChain_PicksMaxMatchedTokens(t *testing.T) {
	segs := []segment{
		{aStart: 0, bStart: 50, length: 12},  // crossing alignment, alone
		{aStart: 0, bStart: 0, length: 10},   // chain 1 start
		{aStart: 12, bStart: 12, length: 20}, // chain 1 middle
		{aStart: 35, bStart: 35, length: 15}, // chain 1 end
	}
	// segs must be sorted by aStart for the DP.
	sortSegs := []segment{segs[1], segs[0], segs[2], segs[3]}
	chain, rest := extractBestChain(sortSegs, 5)
	if len(chain) != 3 || len(rest) != 1 {
		t.Fatalf("chain/rest sizes = %d/%d, want 3/1", len(chain), len(rest))
	}
	total := 0
	for _, s := range chain {
		total += s.length
	}
	if total != 45 {
		t.Errorf("best chain matched tokens = %d, want 45", total)
	}
	if rest[0].bStart != 50 {
		t.Errorf("leftover segment = %+v, want the crossing alignment", rest[0])
	}
}

func TestDedupeOverlapping_KeepsBestOfOverlappingPair(t *testing.T) {
	strong := candidate{
		Match:   Match{AStartLine: 10, AEndLine: 25, BStartLine: 10, BEndLine: 25, Containment: 0.95},
		matched: 100,
	}
	shifted := candidate{
		Match:   Match{AStartLine: 12, AEndLine: 22, BStartLine: 14, BEndLine: 24, Containment: 0.9},
		matched: 60,
	}
	elsewhere := candidate{
		Match:   Match{AStartLine: 10, AEndLine: 25, BStartLine: 60, BEndLine: 75, Containment: 0.9},
		matched: 80,
	}
	got := dedupeOverlapping([]candidate{shifted, strong, elsewhere})
	if len(got) != 2 {
		t.Fatalf("dedupe kept %d, want 2 (shifted overlap dropped, distinct B target kept): %+v", len(got), got)
	}
	// Output is position-sorted; both keepers start at line 10 on A.
	if got[0].matched != 100 || got[1].matched != 80 {
		t.Errorf("kept wrong candidates: %+v", got)
	}
}
