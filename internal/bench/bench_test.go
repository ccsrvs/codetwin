// Package bench is a labeled ground-truth benchmark for duplicate
// detection quality. Positive cases are pairs a human would call clones
// (including every refactor fixture pair, which are clones by
// construction); negative cases are pairs that merely share language
// boilerplate — the classic false-positive shapes. The assertions here
// are the contract the default scoring must satisfy; tune scoring
// against this file rather than eyeballing report output.
package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/fingerprint"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
	"github.com/ccsrvs/codetwin/internal/similarity"
	"github.com/ccsrvs/codetwin/internal/splitter"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

const (
	// positiveMin is the combined score every same-language positive
	// pair must reach: the "strong clone" report band.
	positiveMin = 0.65
	// shortPositiveMin is the floor for positives whose smaller snippet
	// is under shortLines non-blank lines. Short snippets carry less
	// evidence in both scoring layers (few fingerprints, few semantic
	// terms), so the contract for them is "surfaced at the default
	// threshold", not "labeled a strong clone".
	shortPositiveMin = 0.50
	shortLines       = 10
	// crossLangMin is the (laxer) floor for cross-language positives —
	// keyword vocabulary differs between languages, so identical logic
	// scores lower than a same-language clone.
	crossLangMin = 0.50
	// negativeMax is the ceiling every negative pair must stay under:
	// the "refactor target" band boundary. A negative scoring above
	// this would appear in default report output as a finding.
	negativeMax = 0.45
	// noiseP95Max bounds the 95th percentile of scores between
	// unrelated snippets drawn from different cases. This is the
	// report-noise proxy: at the default threshold, most unrelated
	// pairs must fall below it.
	noiseP95Max = 0.30
	// defaultThreshold mirrors the CLI --threshold default: the score a
	// pair must reach to appear in a default report.
	defaultThreshold = 0.50

	// twinMin is the combined score every structural-twin case must
	// reach: the near-clone band boundary. Twins are real token-clones
	// by construction — shape-identical after VAR/STR/NUM normalization
	// — which is exactly what makes their label contract meaningful: a
	// case below this band would never have carried a top-band label in
	// the first place.
	twinMin = 0.85

	minLines = 3 // mirror the CLI default
)

type benchCase struct {
	name     string
	dir      string
	positive bool
	minWant  float64 // for positives

	// shortNegative marks sub-10-line noise pairs (API-forced token
	// shapes, e.g. 4-line Elixir clauses) whose RAW combined score is
	// allowed above negativeMax — real noise of this class scores ~0.60
	// raw. Their contract is instead that the default-on length
	// dampener (--min-confidence-lines 10) pushes them below the
	// default report threshold.
	shortNegative bool

	// twin marks structural-twin pairs: shape-identical code with
	// disjoint identifier/string vocabulary (table tests, per-field
	// boilerplate). Their contract is that they score in the exact/near
	// bands (> twinMin — they ARE token-clones) but render with the
	// structural_twin label instead of exact/near clone, because the
	// lexical sub-score exposes that no content was actually copied.
	twin bool
}

// collectCases discovers labeled pairs: testdata/bench/{positive,negative}
// plus every testdata/refactor fixture tier (clone pairs by construction).
func collectCases(t *testing.T) []benchCase {
	t.Helper()
	root := "../../testdata"
	var cases []benchCase

	addDir := func(base string, positive bool, minWant float64) {
		entries, err := os.ReadDir(base)
		if err != nil {
			t.Fatalf("read %s: %v", base, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			cases = append(cases, benchCase{
				name:     filepath.Base(base) + "/" + e.Name(),
				dir:      filepath.Join(base, e.Name()),
				positive: positive,
				minWant:  minWant,
			})
		}
	}

	addDir(filepath.Join(root, "bench", "negative"), false, 0)

	// Idiom negatives: same-language pairs that share only a language
	// idiom (map-accumulator loops, comprehension+guard, async/try
	// shape), not logic — the semantic-saturation noise class from the
	// algorithms review §3.3. Standard negative ceiling applies: with
	// structural corroboration required for same-language pairs, no
	// amount of trigram-cosine saturation may push these into the
	// default report band.
	addDir(filepath.Join(root, "bench", "negative-idiom"), false, 0)

	// Short negatives: labeled noise pairs under 10 non-blank lines,
	// asserted against the dampened score rather than the raw one.
	shortBase := filepath.Join(root, "bench", "negative-short")
	shortEntries, err := os.ReadDir(shortBase)
	if err != nil {
		t.Fatalf("read %s: %v", shortBase, err)
	}
	for _, e := range shortEntries {
		if !e.IsDir() {
			continue
		}
		cases = append(cases, benchCase{
			name:          "negative-short/" + e.Name(),
			dir:           filepath.Join(shortBase, e.Name()),
			positive:      false,
			shortNegative: true,
		})
	}
	// Structural twins: token-clone pairs with disjoint vocabulary,
	// labeled structural_twin rather than exact/near clone.
	twinBase := filepath.Join(root, "bench", "twins")
	twinEntries, err := os.ReadDir(twinBase)
	if err != nil {
		t.Fatalf("read %s: %v", twinBase, err)
	}
	for _, e := range twinEntries {
		if !e.IsDir() {
			continue
		}
		cases = append(cases, benchCase{
			name: "twins/" + e.Name(),
			dir:  filepath.Join(twinBase, e.Name()),
			twin: true,
		})
	}
	// Bench positives: crosslang-* pairs get the cross-language floor.
	posBase := filepath.Join(root, "bench", "positive")
	entries, err := os.ReadDir(posBase)
	if err != nil {
		t.Fatalf("read %s: %v", posBase, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		want := float64(positiveMin)
		if strings.HasPrefix(e.Name(), "crosslang") {
			want = crossLangMin
		}
		cases = append(cases, benchCase{
			name:     "positive/" + e.Name(),
			dir:      filepath.Join(posBase, e.Name()),
			positive: true,
			minWant:  want,
		})
	}

	// Refactor fixtures: every non-reject tier is a designed clone pair.
	langs, err := os.ReadDir(filepath.Join(root, "refactor"))
	if err != nil {
		t.Fatalf("read refactor fixtures: %v", err)
	}
	for _, lang := range langs {
		if !lang.IsDir() {
			continue
		}
		tiers, err := os.ReadDir(filepath.Join(root, "refactor", lang.Name()))
		if err != nil {
			t.Fatalf("read refactor/%s: %v", lang.Name(), err)
		}
		for _, tier := range tiers {
			if !tier.IsDir() || strings.HasPrefix(tier.Name(), "reject-") {
				continue
			}
			cases = append(cases, benchCase{
				name:     "refactor/" + lang.Name() + "/" + tier.Name(),
				dir:      filepath.Join(root, "refactor", lang.Name(), tier.Name()),
				positive: true,
				minWant:  positiveMin,
			})
		}
	}
	return cases
}

// caseSnippets loads the a.* and b.* file of a case through the real
// scan pipeline (splitter → tokenizer → fingerprint).
func caseSnippets(t *testing.T, dir string) (a, b []scan.Snippet) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	c := cache.New()
	for _, m := range matches {
		base := strings.ToLower(filepath.Base(m)) // Java fixtures are A.java/B.java
		snips, warn := scan.ProcessFile(m, minLines, nil, c, "")
		if warn != "" {
			t.Fatalf("scan %s: %s", m, warn)
		}
		switch {
		case strings.HasPrefix(base, "a."):
			a = snips
		case strings.HasPrefix(base, "b."):
			b = snips
		}
	}
	if len(a) == 0 || len(b) == 0 {
		t.Fatalf("case %s: missing snippets (a=%d b=%d)", dir, len(a), len(b))
	}
	return a, b
}

type scored struct {
	structural, semantic, combined float64
}

func pairScore(a, b scan.Snippet, va, vb similarity.NormalizedVector) scored {
	structural := fingerprint.Jaccard(a.Fps.Set, b.Fps.Set)
	semantic := similarity.CosineFromNormalized(va, vb)
	sameLang := a.Lang == b.Lang && a.Lang != tokenizer.Unknown
	return scored{structural, semantic, similarity.CombinedForLangs(structural, semantic, sameLang)}
}

func TestBench_GroundTruth(t *testing.T) {
	cases := collectCases(t)

	// One corpus across every snippet in the benchmark so IDF weights
	// resemble a real multi-file scan rather than a two-file corpus.
	type loaded struct {
		benchCase
		a, b []scan.Snippet
	}
	var all []loaded
	var streams [][]string
	for _, c := range cases {
		a, b := caseSnippets(t, c.dir)
		all = append(all, loaded{c, a, b})
		for _, s := range append(append([]scan.Snippet{}, a...), b...) {
			streams = append(streams, s.Tokens)
		}
	}
	corpus := similarity.NewCorpus(streams)
	vec := func(s scan.Snippet) similarity.NormalizedVector {
		return similarity.Normalize(corpus.Vectorize(s.Tokens))
	}

	failures := 0
	topBandLabels := make(map[string]string)
	for _, c := range all {
		// Case score: best cross-file pair, mirroring how the report
		// would surface the case's duplication. Track the pair's two
		// snippets for the short-positive floor and the label contract.
		var best scored
		var bestA, bestB scan.Snippet
		for _, sa := range c.a {
			va := vec(sa)
			for _, sb := range c.b {
				if s := pairScore(sa, sb, va, vec(sb)); s.combined > best.combined {
					best = s
					bestA, bestB = sa, sb
				}
			}
		}
		bestMinLines := bestA.NonBlankLn
		if bestB.NonBlankLn < bestMinLines {
			bestMinLines = bestB.NonBlankLn
		}
		minWant := c.minWant
		if c.positive && minWant == positiveMin && bestMinLines < shortLines {
			minWant = shortPositiveMin
		}
		status := "ok"
		if c.positive && best.combined < minWant {
			status = "FAIL(<" + fmtF(minWant) + ")"
			failures++
		}
		// Label contract (R6): the report label is a function of the
		// dampened score, the line counts (exact-clone evidence gate),
		// and the lexical sub-score. Compute it exactly as the pipeline
		// would so the assertions below pin real report behaviour.
		label, lexical := labelForBest(best, bestA, bestB)
		if c.positive && !c.twin && similarity.LengthDampen(
			best.combined, bestA.NonBlankLn, bestB.NonBlankLn,
			similarity.DefaultMinConfidenceLines) > twinMin {
			// Rename-invariance guard: every positive in the exact/near
			// bands must KEEP its exact/near label — systematic renames
			// lower lexical overlap, but a typical rename shares most of
			// its vocabulary (helper calls, field names, string
			// literals) and must stay above the structural-twin floor.
			topBandLabels[c.name] = label
			if label != "exact_clone" && label != "near_clone" {
				status = "FAIL(label " + label + ", want exact/near clone; lex=" + fmtF(lexical) + ")"
				failures++
			}
		}
		switch {
		case c.twin:
			// Twins must be real token-clones (the label contract is
			// vacuous below the near band) AND render as structural
			// twins: same shape, disjoint content.
			switch {
			case best.combined <= twinMin:
				status = "FAIL(raw " + fmtF(best.combined) + " <= " + fmtF(twinMin) + ": case is not a token-clone, label contract is vacuous)"
				failures++
			case label != "structural_twin":
				status = "FAIL(label " + label + ", want structural_twin; lex=" + fmtF(lexical) + ")"
				failures++
			default:
				status = "ok (structural_twin, lex " + fmtF(lexical) + ")"
			}
		case c.shortNegative:
			// Contract for short noise: the raw score is real report
			// noise (it WOULD render at the default threshold — that's
			// what makes the case meaningful), and the default-on
			// dampener is what keeps it out of the default report.
			dampened := similarity.LengthDampen(
				best.combined, bestMinLines, bestMinLines,
				similarity.DefaultMinConfidenceLines)
			if best.combined < defaultThreshold {
				status = "FAIL(raw<" + fmtF(defaultThreshold) + ": case no longer exercises the dampener)"
				failures++
			} else if dampened >= defaultThreshold {
				status = "FAIL(dampened " + fmtF(dampened) + " >= " + fmtF(defaultThreshold) + ")"
				failures++
			} else {
				status = "ok (dampened " + fmtF(dampened) + ")"
			}
		case !c.positive && best.combined > negativeMax:
			status = "FAIL(>" + fmtF(negativeMax) + ")"
			failures++
		}
		t.Logf("%-40s struct=%.2f sem=%.2f combined=%.2f lex=%.2f %s",
			c.name, best.structural, best.semantic, best.combined, lexical, status)
		if strings.HasPrefix(status, "FAIL") {
			t.Fail()
		}
	}

	// Rename-invariance is the product's core promise: the renamed
	// positives score 1.0 by design, and the structural-twin band
	// modifier must never demote them. Assert their presence in the
	// exact/near bands explicitly so the in-loop guard can't go vacuous
	// if a fixture edit drops them below 0.85.
	for _, name := range []string{"positive/go-renamed", "positive/python-renamed", "positive/go-renamed-rich"} {
		label, ok := topBandLabels[name]
		if !ok {
			t.Errorf("%s: expected the renamed positive to score above %.2f (rename invariance)", name, twinMin)
			continue
		}
		if label != "exact_clone" && label != "near_clone" {
			t.Errorf("%s: label = %q, want exact_clone or near_clone (renamed positives must not demote to structural_twin)", name, label)
		}
	}

	// Noise floor: snippet pairs drawn from different cases are
	// unrelated — except refactor fixtures of the same tier, which
	// implement the same example logic in each language (go/medium and
	// python/medium are true cross-language clones, not noise). Their
	// score distribution predicts how much noise a real scan produces
	// at a given threshold.
	sameLogic := func(a, b benchCase) bool {
		return strings.HasPrefix(a.name, "refactor/") &&
			strings.HasPrefix(b.name, "refactor/") &&
			filepath.Base(a.dir) == filepath.Base(b.dir)
	}
	type tagged struct {
		caseIdx int
		v       similarity.NormalizedVector
		s       scan.Snippet
	}
	var pool []tagged
	for i, c := range all {
		for _, s := range append(append([]scan.Snippet{}, c.a...), c.b...) {
			pool = append(pool, tagged{i, vec(s), s})
		}
	}
	type noisePair struct {
		score float64
		a, b  string
	}
	var noise []float64
	var worst []noisePair
	for i := 0; i < len(pool); i++ {
		for j := i + 1; j < len(pool); j++ {
			if pool[i].caseIdx == pool[j].caseIdx ||
				sameLogic(all[pool[i].caseIdx].benchCase, all[pool[j].caseIdx].benchCase) {
				continue
			}
			s := pairScore(pool[i].s, pool[j].s, pool[i].v, pool[j].v).combined
			noise = append(noise, s)
			worst = append(worst, noisePair{s, pool[i].s.Name, pool[j].s.Name})
		}
	}
	sort.Slice(worst, func(i, j int) bool { return worst[i].score > worst[j].score })
	for _, w := range worst[:3] {
		t.Logf("worst noise %.2f: %s <-> %s", w.score, filepath.Base(filepath.Dir(w.a))+"/"+filepath.Base(w.a), filepath.Base(filepath.Dir(w.b))+"/"+filepath.Base(w.b))
	}
	sort.Float64s(noise)
	p50 := noise[len(noise)/2]
	p95 := noise[len(noise)*95/100]
	max := noise[len(noise)-1]
	t.Logf("noise floor over %d unrelated pairs: p50=%.2f p95=%.2f max=%.2f", len(noise), p50, p95, max)
	if p95 > noiseP95Max {
		t.Errorf("unrelated-pair p95 = %.2f, want <= %.2f — default scans will drown in noise", p95, noiseP95Max)
	}
}

// labelForBest computes the report label the pipeline would render for
// a case's best pair, mirroring BuildMatrix + report.JSONLabel: the
// combined score is length-dampened, and the lexical Jaccard over the
// two snippets' raw-code term sets feeds the structural-twin band
// modifier. Returns the label and the lexical score.
func labelForBest(s scored, a, b scan.Snippet) (string, float64) {
	lex := similarity.LexicalJaccard(a.LexTerms, b.LexTerms)
	computed := len(a.LexTerms) >= similarity.MinLexicalTerms &&
		len(b.LexTerms) >= similarity.MinLexicalTerms
	score := similarity.LengthDampen(
		s.combined, a.NonBlankLn, b.NonBlankLn, similarity.DefaultMinConfidenceLines)
	p := report.Pair{
		Score:  score,
		LinesA: a.NonBlankLn, LinesB: b.NonBlankLn,
		Lexical: lex, LexicalComputed: computed,
	}
	return report.JSONLabel(p), lex
}

func fmtF(f float64) string {
	return fmt.Sprintf("%.2f", f)
}

// ── Class-level granularity (§5.2) ───────────────────────────────────────────

// classCasePairs runs one testdata/bench/classes case through the real
// matrix pipeline — BuildMatrix applies the mixed-kind gate, the
// same-file nesting filter, and the length dampener exactly as a scan
// would — and returns the snippets plus materialized pairs.
func classCasePairs(t *testing.T, name string) ([]scan.Snippet, []report.Pair) {
	t.Helper()
	a, b := caseSnippets(t, filepath.Join("../../testdata/bench/classes", name))
	snips := append(append([]scan.Snippet{}, a...), b...)
	streams := make([][]string, len(snips))
	for i, s := range snips {
		streams[i] = s.Tokens
	}
	corpus := similarity.NewCorpus(streams)
	vectors := make([]similarity.NormalizedVector, len(snips))
	for i, s := range snips {
		vectors[i] = similarity.Normalize(corpus.Vectorize(s.Tokens))
	}
	_, pairs, _ := similarity.BuildMatrix(
		snips, vectors, similarity.DefaultMinConfidenceLines, defaultThreshold, nil)
	return snips, pairs
}

// classNames returns the Name set of the case's class-kind snippets.
func classNames(snips []scan.Snippet) map[string]bool {
	out := map[string]bool{}
	for _, s := range snips {
		if s.Kind == splitter.KindClass {
			out[s.Name] = true
		}
	}
	return out
}

// TestBench_ClassGranularity is the §5.2 contract: cross-file
// class↔class pairs must surface as strong clones (the case
// method-level granularity underreports — a renamed class with slightly
// reordered methods), while class↔function pairs across files are
// mixed-kind noise and must never materialize.
func TestBench_ClassGranularity(t *testing.T) {
	// Positive: same class renamed, methods slightly reordered.
	for _, name := range []string{"python-class-clone", "java-class-clone"} {
		snips, pairs := classCasePairs(t, name)
		classes := classNames(snips)
		if len(classes) != 2 {
			t.Fatalf("%s: expected 2 class chunks (one per file), got %d: %v", name, len(classes), classes)
		}
		var best float64
		found := false
		for _, p := range pairs {
			if classes[p.NameA] && classes[p.NameB] {
				found = true
				if p.Score > best {
					best = p.Score
				}
			}
		}
		if !found {
			t.Errorf("%s: no class↔class pair materialized", name)
		} else if best < positiveMin {
			t.Errorf("%s: class↔class pair score = %s, want >= %s", name, fmtF(best), fmtF(positiveMin))
		} else {
			t.Logf("%-40s class pair score=%s ok", "classes/"+name, fmtF(best))
		}
	}

	// Negative: a class in a.js vs the same methods as loose functions
	// in b.js. The class↔function pairs are mixed-kind and must produce
	// NO pair at all; the methods inside the class must still match the
	// loose functions individually.
	snips, pairs := classCasePairs(t, "js-class-vs-loose-funcs")
	classes := classNames(snips)
	if len(classes) != 1 {
		t.Fatalf("js-class-vs-loose-funcs: expected exactly 1 class chunk (in a.js), got %d: %v", len(classes), classes)
	}
	var methodBest float64
	for _, p := range pairs {
		if classes[p.NameA] || classes[p.NameB] {
			t.Errorf("js-class-vs-loose-funcs: class chunk must not pair with anything, got %s <-> %s (%s)",
				p.NameA, p.NameB, fmtF(p.Score))
		}
		if !classes[p.NameA] && !classes[p.NameB] && p.Score > methodBest {
			methodBest = p.Score
		}
	}
	if methodBest < positiveMin {
		t.Errorf("js-class-vs-loose-funcs: best method↔function pair = %s, want >= %s (method-level matching must keep working)",
			fmtF(methodBest), fmtF(positiveMin))
	} else {
		t.Logf("%-40s no class pair; method pair score=%s ok", "classes/js-class-vs-loose-funcs", fmtF(methodBest))
	}
}
