# Comparative algorithms review — effectiveness, noise, and multi-granularity dedup

_Date: 2026-07-14. Scope: the scoring pipeline (tokenizer → splitter →
winnowing/Jaccard → TF-IDF trigram cosine → language-aware blend →
DBSCAN), evaluated against the `internal/bench` ground-truth suite and
empirical scans of this repository. All numbers below are reproducible
with the commands shown._

> **Implementation status (2026-07-14):** R1, R2, R4, and R5 — the
> "Release N" SNR items from §6 — are implemented on this branch.
> Measured effect on the default self-scan of `./internal ./cmd`:
> **1,887 pairs / 77 clusters → 36 pairs / 8 clusters** (1,538
> test↔test pairs and 70 test-only clusters folded into summary lines;
> `--include-tests` restores the full listing). Largest cluster
> 47 → 6 members; visible "exact clone" labels 241 → 1 (short pairs
> demoted by the evidence gate); materialized pairs 83% → ~4% of all
> comparisons. `TestBench_GroundTruth` green throughout, with a new
> `negative-short` bench category pinning the short-snippet contract.
>
> **Release N+1 (same date):** R3 and R6 are implemented test-first —
> each landed its failing bench contract before the fix. **R3**: same-
> language pairs with structural < 0.20 are capped at 0.45 (linear
> ramp-out by 0.35), pinned by the new `bench/negative-idiom` category;
> semantic-only same-language pairs on the self-scan went 110 → 0 while
> all `synthesize*` production findings survive. **R6**: a label-only
> band modifier — pairs > 0.85 whose raw-code vocabulary overlap
> (lexical Jaccard) is below 0.20 render as **structural twins** rather
> than exact/near clones; pinned by the new `bench/twins` category plus
> explicit rename-invariance guards (`go-renamed`, `python-renamed`,
> and the new `go-renamed-rich` must never demote). Combined effect on
> the default self-scan: **34 pairs / 8 clusters**, all production
> findings.
>
> **§5.3 block-level detection is now grounded**: `testdata/bench/blocks`
> (5 positives, 3 boilerplate negatives) + `TestBlockClones_GroundTruth`
> define the acceptance contract (skipped until the detector lands), and
> `TestBlockClones_FixturesAreInvisibleAtFunctionLevel` runs live to
> enforce that every positive stays under the function-level threshold.
> The detector implementation (§5.3) and §5.1/§5.2 granularity modes
> remain open.

## Executive summary

The core comparison machinery is sound and well-tuned for what it
measures: the ground-truth benchmark passes with headroom on positives,
formatting/rename invariance is total (structural = 1.0 on those
cases), and precision on **production code** is genuinely good — the
non-test findings on a self-scan are almost all real duplication (the
per-language `synthesize*` emitter family, the `compile*Matcher` trio,
`filterPairsBySince`/`filterClustersBySince`).

The noise problem is real, but it is **concentrated, not diffuse**.
Four sources account for nearly all of it, in order of magnitude:

1. **Test scaffolding** — 98.2% of all reported pairs on a self-scan
   have at least one `_test.go` endpoint (98.3% of "exact clones").
2. **Short snippets** — the worst benchmark noise pairs are 4-line
   functions scoring 0.60, above the default 0.50 threshold; the
   existing `--min-confidence-lines` dampener fixes exactly this but is
   **off by default** and its 0.5× floor still lets 5-line "exact
   clones" through.
3. **Semantic-only same-language matches** — 119 self-scan pairs ≥ 0.50
   are carried purely by trigram cosine (structural < 0.10). These are
   shared idioms, not clones.
4. **DBSCAN transitive chaining** — the largest self-scan cluster has
   47 members; density chaining at eps 0.45 merges distinct clone
   families into one "refactoring task".

Each has a cheap, targeted fix (§4). Together they should cut default
report volume by ~95% on test-heavy repos without touching recall on
the benchmark positives.

On granularity (§5): **file-level and class-level dedup are small
lifts** (days to ~2 weeks). **Sub-function block-level dedup is the
one that matters and the one that's genuinely missing** — a verbatim
15-line block embedded in two otherwise-different ~45-line functions
scores **0.37 combined and is invisible at every default**. Roughly
70% of the plumbing it needs (positional fingerprints, match-range
extraction, range-based naming, preview highlighting) already exists;
estimate 3–6 weeks for a shippable v1 including bench fixtures and
report/`--suggest` integration.

---

## 1. What was reviewed

- `internal/tokenizer` — normalization (VAR/STR/NUM collapse, comment +
  import stripping, single-rune punctuation tokens)
- `internal/splitter` — function-level chunking per language
- `internal/fingerprint` — winnowing (k=10, w=4), Jaccard, positional sets
- `internal/similarity` — canonicalized token-trigram TF-IDF (sublinear
  TF, 4-term evidence floor), cosine, language-aware blend (0.5/0.5 same
  language, 0.2/0.8 cross-language), `LengthDampen`
- `internal/similarity/matrix.go` — inverted-index candidate pruning,
  nested-chunk suppression, `PairNoiseFloor`
- `internal/cluster` — DBSCAN (eps 0.45, minPts 2)
- `internal/bench` — the ground-truth tuning contract

Empirical runs: `TestBench_GroundTruth -v`, self-scans of
`./internal ./cmd` (51 files → 723 snippets → 261,003 comparisons,
~0.5 s), and synthetic block-clone fixtures.

## 2. What is working well (keep it)

| Property | Evidence |
|---|---|
| Formatting/rename invariance | `positive/go-formatting`, `go-renamed`, `python-renamed` all score structural **1.00** — the single-rune-punctuation tokenization does its job completely. |
| Winnowing correctness | Short-stream guarantee (whole-sequence window when hashes < w) means no snippet can have an empty fingerprint set; positional sets make matches locatable. |
| Benchmark discipline | `internal/bench` encodes positives, hard negatives, and a noise-floor p95 assertion. This is the right way to tune — every change below should be validated against it. |
| Production-code precision | On the self-scan, the non-test pair list (34 pairs) is nearly all true duplication worth a look. |
| Cross-language recall | `crosslang-sum` scores 0.64 via the canonicalized semantic layer with structural 0.00 — the 0.2/0.8 cross-language blend is doing exactly what it was designed for. |
| Determinism & pruning | Sorted-key cosine, stable snippet ordering, inverted-index Jaccard skip — all verified in code and consistent across runs. |

The PR #7 overhaul (trigram terms, sublinear TF, evidence floor,
k 5→10, threshold 0.30→0.50) already took report volume from 171,969
pairs to ~1,900 on this repo. This review is about the remaining
~1,900.

## 3. Anatomy of the remaining noise (empirical)

Self-scan at defaults: `./codetwin --json --no-cache ./internal ./cmd`
→ **1,887 pairs, 77 clusters** (241 "exact", 72 "near", 479 "strong",
1,095 "refactor target").

### 3.1 Test scaffolding dominates everything — 98% of pairs

```
pairs with a *_test.go endpoint:        1853 / 1887  (98.2%)
"exact clones" with a test endpoint:     237 / 241   (98.3%)
```

Root cause: the normalizer's greatest strength is also the failure
mode. Test functions are short, forced into a common shape by the API
under test, and differ mostly in **identifiers and string literals —
precisely the two token classes normalization erases** (VAR/STR).
`TestSplit_GoMethodReceiver` vs `TestSplit_PythonAsyncDef` genuinely
tokenize identically, so the algorithm is *correct* that they're
token-clones; they're just not *actionable* findings.

### 3.2 Short snippets — the dampener exists but is off

The benchmark's worst noise pairs are 4-line Elixir clauses
(`handle_cast` vs `parse`) at **0.60 combined** — above the default
threshold. Noise p95 is 0.27, but the max is 0.60, and everything in
that tail is a sub-10-line snippet.

`--min-confidence-lines 20` drops the self-scan from 1,887 → 1,005
pairs, and combined with `--threshold 0.65` → 399. But:

- it defaults to **0** (off), so nobody gets this protection;
- the multiplier floor is 0.5×, so a 5-line 100% match still scores
  0.625 and clears the default threshold;
- report labels are score-only, so a dampened short match that survives
  still renders as if it were strong evidence.

### 3.3 Semantic saturation on same-language pairs

119 self-scan pairs ≥ 0.50 have structural < 0.10 in the same
language. Examples: `cluster.Groups` vs `similarity.buildHashIndex`
(0.66 — both are 8-line map-append loops), `fingerprint.Hashes` vs
`similarity.Normalize` (0.56), two 3-line `Less` methods at 0.60 with
semantic 1.00. Trigrams over VAR-normalized streams still saturate for
idiom-shaped code. For **same-language** pairs, high semantic with no
structural corroboration is nearly always idiom, not clone — the
semantic layer earns its keep only cross-language (where structural
*can't* fire) and as a partial-rewrite catcher on top of nonzero
structural signal.

Relatedly, the hard negatives (`go-handlers`, `js-handlers`) sit at
**0.44 combined against a 0.45 report boundary** — a one-point margin,
both carried by semantic 0.71–0.76. The contract holds today but is
fragile, and the negative corpus is only 5 cases.

### 3.4 DBSCAN chaining merges distinct families

Cluster size distribution on the self-scan: **47, 42, 36, 16, 15, 14,
13, 13, …**. The 47-member cluster spans Elixir GenServer tests, Rust
tests, and JS synthesis tests — linked transitively (A~B ≥ 0.55,
B~C ≥ 0.55, A~C weak). The report's framing "each cluster is one
refactoring task" breaks at that size. eps 0.45 means any pair at
0.55+ links, which is below the "strong clone" band the report itself
says is where parameterization becomes sensible.

### 3.5 Memory noise: `PairNoiseFloor` barely filters

On the self-scan, **215,752 of 261,003 pairs (83%)** survive the 0.05
materialization floor. The floor predates the trigram overhaul; with
today's scoring, almost every pair scores above 0.05 semantic-blend.
Harmless at 723 snippets, but it's an O(n²) heap allocation on big
repos — and it's pure noise, since nothing below `--threshold` renders
(only `--suggest` reads sub-threshold pairs, and it could tolerate a
higher floor).

### 3.6 Minor observations (no action urgently needed)

- `crossLangCanon` never fires for Java methods (no `func`-like
  keyword), so Java cross-language pairs lean entirely on body
  trigrams — slightly weaker than the other five languages.
- Jaccard punishes size asymmetry by design (union-normalized). That's
  correct for same-granularity pairs but is exactly why containment
  (small function verbatim-inlined into a big one, across files) is
  invisible — see §5.3; the fix belongs to block-level detection, not
  to Jaccard.
- `kgrams` builds each k-gram by string concatenation (O(k) allocs per
  gram). A rolling hash would cut scan-phase allocations
  substantially; only worth it when someone complains about scan time.
- Roadmap already flags: cache version doesn't encode k/w, and
  `Unknown↔Unknown` pairs get the cross-language blend. Both still
  true, both still latent.

## 4. Recommended noise cuts, ranked

Ordered by (report-noise removed) ÷ (effort). Every item must keep
`TestBench_GroundTruth` green; items 1, 2, 4 need new bench cases first
(the current suite has **no test-scaffolding negatives** — that's the
gap that let this class of noise stay invisible to the contract).

### R1 — Segregate test code by default (biggest single win, ~2–3 days)

Classify files by well-known test conventions (`*_test.go`,
`test_*.py`/`*_test.py`, `*.spec.*`/`*.test.*`, `__tests__/`,
`src/test/java/`, Rust `#[cfg(test)]` modules as a stretch). Then:

- **test↔production pairs: keep** (copy-paste from prod into tests is
  a real finding),
- **test↔test pairs: fold into a one-line summary** ("1,819 test↔test
  pairs suppressed; --include-tests to show") or a separate trailing
  section.

This is presentation-layer only — no scoring change, no bench risk,
and it removes ~98% of default-report volume on repos like this one.
A `--include-tests` flag restores today's behavior. This beats asking
users to write `ignore_paths` because it's on by default and it
preserves the cross-boundary findings that `ignore_paths` would drop.

### R2 — Turn on length-aware confidence by default (~1 day + retune)

Default `--min-confidence-lines` to ~15 and make the ramp harsher at
the bottom end (e.g. multiplier `(min/N)^0.75` clamped to [0.35, 1.0],
or simply a hard sub-threshold for < 5-line snippets). Also gate the
**"exact clone" label** on evidence, not just score: require
`min(lines) ≥ 10` or fingerprint-intersection ≥ some floor to render
the top band; otherwise cap the label at "near clone (short)". The
benchmark's short-positive floor (0.50 for < 10 lines) already
anticipates exactly this contract; add the Elixir 4-line
`handle_cast`-style case as a labeled negative so the tail is pinned.

### R3 — Require structural corroboration for same-language pairs (~2 days + retune)

For same-language pairs, cap the combined score when structural
evidence is absent: e.g. `if sameLang && structural < 0.15 { combined =
min(combined, 0.45) }` (tune the constants against the bench). This
single rule removes the `Groups`/`buildHashIndex` class of idiom noise
*and* converts the fragile 0.44-vs-0.45 negative margin into a
structural guarantee, while leaving cross-language scoring — where
structural absence is expected — untouched. Add the current top-10
semantic-only self-scan pairs as bench negatives first.

### R4 — Tame cluster chaining (~2–3 days)

Two independently useful changes:

- Default `--eps` 0.45 → 0.35 so linking requires ≥ 0.65 — the same
  band the report calls "strong clone". Keeping cluster semantics
  aligned with label semantics is easy to explain and to document.
- Report **cluster cohesion** (min internal pair score) alongside the
  existing average, and either flag or split clusters whose min
  internal score < threshold. Splitting = run single-linkage inside
  the cluster at the stricter bound; cheap since the matrix is already
  in memory.

### R5 — Raise the materialization floor (~half a day)

`PairNoiseFloor` 0.05 → `max(0.30, threshold − 0.20)`. Keeps
`--suggest`'s "target a sub-threshold pair" workflow (it only needs a
modest band below threshold, not the 83% of all pairs kept today) and
bounds the pair slice on large repos. Matrix/DBSCAN are unaffected —
they don't read the materialized list.

### R6 — Reintroduce lexical evidence as a tie-breaker (~1 week, the only scoring-model change)

The deeper fix for §3.1: identifiers and string literals are currently
0% of the signal, which is why table tests are indistinguishable from
real clones. Add a third, *lightweight* sub-score — e.g. Jaccard over
the snippet's raw identifier + string-literal multiset (lowercased,
camelCase/snake_case split) — and use it **only to modulate the top
bands**: an "exact clone" whose lexical overlap is near zero demotes to
"structural twin" (new label), because it's shape-identical but
content-different. Do **not** blend it into the base score (that would
break `go-renamed`/`python-renamed`, which are *supposed* to score 1.0
— rename-invariance is the product's core promise). As a band
modifier, renamed positives keep passing (their band floor is 0.65,
and demotion only applies at > 0.85) while test scaffolding stops
being reported as the most severe finding class. This is the highest
value change per line of scoring code, but it needs the most bench
work: add table-test fixtures in Go/Python/JS as labeled
"structural-twin, not exact" cases.

Sequencing note: R1 + R2 + R4 are independent and could ship in one
minor release; R3 and R6 change scores and should each land with their
bench extensions in separate PRs so regressions bisect cleanly.

## 5. Dedup beyond function level — what it would take

"Function-level" today means: splitter emits per-definition chunks
(with whole-file fallback), and everything downstream is
granularity-agnostic — it just sees `Snippet`s with token streams,
positions, and line ranges. That downstream neutrality is the key
asset: **fingerprint, similarity, cluster, report, cache, git layers
all work unchanged on any chunk shape.** The lift for each granularity
is almost entirely in (a) producing the chunks and (b) not drowning in
the extra pairs.

### 5.1 File/module level — trivial (2–4 days)

A `--granularity file` mode that skips the splitter is nearly free
(the whole-file fallback path already exists and is exercised by
Elixir-before-splitter history). Useful for "these two files should be
one module" and for languages without a splitter. Cost: pair counts
shrink (fewer, bigger chunks) so this is noise-*reducing*; the main
work is flag plumbing, docs, and a couple of subprocess tests.
Value is modest — most file-level dupes surface today as many
function-level pairs — but it's cheap enough to bundle with any other
granularity work.

### 5.2 Class/type level — small (1–2 weeks)

Java/JS splitters deliberately reject class headers so methods
dominate; Python and Go have no class/struct chunks at all. Emitting a
class-span chunk **in addition to** method chunks is a per-language
splitter change plus fixtures. Same-file nesting suppression
(`chunksNestedSameFile`) already prevents the class-vs-own-method false
positives, and it's already same-file-scoped, so cross-file
class-to-class matches still fire. Watch: class chunks re-introduce
the "washed out by unrelated code" dilution the splitter was built to
avoid, so class-level pairs should render as their own section or only
when method-level evidence corroborates. Estimate: ~1 week for
Python/Java/JS + fixtures, +2–3 days if Go struct+methodset grouping
is wanted (Go methods live outside the type block, so "class-level" for
Go means symbol-grouping, not span-grouping).

### 5.3 Sub-function block level — the real gap, moderate lift (3–6 weeks)

**Why it matters (measured):** a verbatim 15-line block placed inside
two ~45-line functions with unrelated surrounding code scores
**combined 0.37 (structural 0.17, semantic 0.57)** — under every
default and even under `--verbose`'s practical floor. The same block in
~28-line hosts scores 0.63. Function-level Jaccard is
union-normalized, so shared content is diluted quadratically as host
functions grow. Every classic clone detector that handles Type-1/2
partial clones (CPD, NiCad, CCFinder) wins exactly this case against
codetwin today; it is the biggest recall hole in the product.

**Why codetwin is unusually well-positioned:** the pieces already
exist —

- `fingerprint.PositionalSet` keeps the token position of every
  selected hash; `MatchRange` already computes the span of shared
  fingerprints between two snippets (built for previews).
- Winnowing guarantees any shared token run of ≥ k+w−1 = 13 tokens
  (≈ 1.5–2 source lines at this tokenizer's density) selects at least
  one common fingerprint — so shared-fingerprint runs are a complete
  candidate generator for blocks ≥ ~3 lines, no new indexing needed.
- `TokenizeWithLines` maps tokens → source lines, so a token-range
  block converts to `path:start-end` directly, and `Chunk.Name()`
  already renders symbol-less ranges.
- The preview renderer already highlights matched line ranges.

**Proposed shape (v1):** a second-pass refinement, not a granularity
explosion. For every pair in a "gray band" (e.g. combined ∈ [0.20,
threshold) with nonzero structural), walk the shared fingerprint
positions on both sides, coalesce runs whose gaps are < g tokens, and
promote runs ≥ `--min-block-lines` (default ~8–10) on **both** sides to
a synthetic block pair scored by containment
(`intersection / min(|A|, |B|)`) over the block's own fingerprints,
then re-verified with exact token comparison for the Type-1 case.
Report them in a `PARTIAL CLONES` section as
`a.go:120-134 ⊂ ProcessOrders ↔ b.go:88-102 ⊂ SummarizeInvoices`.

This design deliberately avoids the two failure modes of naive
block-level scanning:

- **No n² blowup**: candidates come only from gray-band pairs that
  already share fingerprints (the inverted index prunes the rest), so
  the second pass is proportional to near-miss pairs, not to a 3–5×
  larger snippet universe. (A true fixed-window block *chunking*
  approach would multiply n by 3–5× and the dense matrix by 10–25× —
  the current `[][]float64` matrix is the scale ceiling; avoid that
  path, or make it contingent on a sparse-matrix refactor.)
- **No boilerplate flood**: a high `min-block-lines` floor plus exact
  token verification keeps `if err != nil { return err }` chains and
  import-adjacent noise out; R2's short-snippet rules apply to blocks
  doubly.

**Work items and estimate:**

| Item | Estimate |
|---|---|
| Run-coalescing + containment scoring over `PositionalSet` (new `internal/fingerprint` or `internal/blocks` code, pure functions, highly testable) | 1 week |
| Pipeline integration (gray-band second pass in/after `BuildMatrix`), `--min-block-lines`, block-pair report section, JSON schema | 1 week |
| Bench extension: positive block fixtures (verbatim, renamed, cross-file containment) + boilerplate negatives (err-check chains, logging blocks); tune g, floor, band | 1 week |
| `--suggest` for block pairs (actually the *easiest* suggest case — verbatim blocks extract cleanly; reuse the existing align/synth pipeline on the block spans) | 3–5 days |
| Cache schema bump (encode k/w while at it — closes the standing roadmap P3), `--since`/`--blame` line-range plumbing (works as-is since blocks carry real line ranges; verify), docs/skill/guide updates | 3–5 days |

**Total: ~3–4 weeks focused, 6 weeks with review/iteration buffer.**
Risk is concentrated in tuning (the bench extension is the mitigation)
and in report ergonomics, not in algorithmic feasibility.

**Ground truth landed:** `testdata/bench/blocks` +
`TestBlockClones_GroundTruth` (internal/bench/blocks_test.go) define
the acceptance contract — positives (verbatim, renamed, containment,
gapped; Go and Python) must produce block matches at
`min-block-lines 8` with containment ≥ 0.8 overlapping the line ranges
stated in each fixture header; negatives (err-check chains, logging
runs, import-adjacent setup) must produce none. The contract test skips
until an implementation is wired into the `detectBlocks` seam; fixture
invisibility at function level (every positive's best whole-function
pair scores 0.36–0.43, below the 0.50 default threshold) is enforced
by a live companion test so the fixtures can't drift into
function-level-detectable territory.

**Status: implemented.** The block-level detector landed as
`internal/blocks` (seed–extend–chain over shared fingerprint positions,
containment ≥ 0.85 + matched-line floor verification), wired through a
gray-band candidate channel in `BuildMatrix` into the `PARTIAL CLONES`
report section and `partial_clones` JSON array (`--min-block-lines`,
default 8); the contract test above is un-skipped and green.

### 5.4 Not recommended as "granularity": cross-repo

Cross-repo scanning (roadmap bet #6) is sometimes framed as a
granularity level; it's really namespacing + report grouping and is
independent of everything above. No change to the comparative
algorithms.

## 6. Suggested sequencing

1. **Release N (1–2 weeks):** R1 test segregation + R2 dampener default
   + R4 eps/cohesion + R5 floor. Pure SNR release; headline: default
   scans go from ~1,900 findings to a few dozen on a repo like this
   one, with nothing real lost (the 34 production findings all
   survive).
2. **Release N+1 (1–2 weeks):** R3 structural corroboration + R6
   lexical band-modifier, each with its bench extension. This is the
   scoring-model release; it hardens the negative margin and fixes the
   "test tables are exact clones" misclassification for anyone using
   `--include-tests`.
3. **Release N+2 (3–6 weeks):** block-level partial-clone detection
   (§5.3), with file-level (§5.1) bundled as a freebie and class-level
   (§5.2) optional behind it. This is the recall release and the
   headline feature — no OSS tool combines function-level cross-language
   matching *and* sub-function partial clones with provenance and
   PR-delta gating.

## Appendix: reproduction commands

```bash
make build && make test

# Ground truth + noise floor
go test ./internal/bench/ -run TestBench_GroundTruth -v

# Self-scan (default settings)
./codetwin --json --no-cache ./internal ./cmd > self.json
jq '.pairs | length' self.json                                   # 1887
jq '[.pairs[] | select((.file_a|test("_test\\.go")) or
                       (.file_b|test("_test\\.go")))] | length' self.json   # 1853
jq '[.pairs[] | select(.structural < 0.10 and .lang_a == .lang_b)]
    | length' self.json                                          # 119
jq '.clusters | map(.members|length) | sort | reverse | .[0:8]' self.json
                                                                 # 47,42,36,...

# Dampener effect
./codetwin --json --no-cache --min-confidence-lines 20 ./internal ./cmd \
  | jq '.pairs | length'                                         # 1005

# Materialization floor (83% of pairs kept at 0.05)
./codetwin --debug --no-cache --json ./internal ./cmd >/dev/null
# → comparing 723 × 723 = 261003 pairs
# → similarity.BuildMatrix: 215752 pairs above noise floor

# Block-dilution demonstration: identical 15-line block inside two
# ~45-line functions with unrelated surrounding code scores 0.37
# (build fixtures per §5.3 or see the review PR description).
```
