# codetwin — interpreting the output

This guide explains what the report is showing you and how to read it. For
flag reference run `codetwin --help`; for the workflow-oriented skill that
agents trigger on, run `codetwin --skill`.

## The combined score

Each pair's headline percentage is `0.5 × structural + 0.5 × semantic`,
shown as a label and a number. Bands use strict `>` thresholds:

| Label | Score | What it usually means |
|---|---|---|
| EXACT CLONE | > 95% | Token-for-token equivalent (after the tokenizer's `VAR` / `STR` / `NUM` normalization). Almost certainly copy-paste. Extract a shared utility and delete one. Evidence-gated: both snippets must span ≥ 10 non-blank lines; a shorter pair renders as NEAR CLONE even at a perfect score, because tiny functions can share their whole token shape by API force alone. |
| NEAR CLONE | > 85% | Virtually identical with one or two token-level edits (a swapped literal, a different default arg). Treat as a clone unless the difference is intentional. |
| STRUCTURAL TWIN | > 85%, lexical < 20% | Same token shape, different content: the pair's raw identifier/string vocabulary barely overlaps, so this is likely parallel boilerplate (table tests, per-field validators, generated handlers) rather than copy-paste. See "Structural twins" below. |
| STRONG CLONE | > 65% | Same shape and most of the same structure, with substantive divergences. Parameterize the differing parts. |
| REFACTOR TARGET | > 45% | Same general approach to the same problem, with real differences in execution. Evaluate whether a shared abstraction reduces duplication; sometimes "no" is the right answer. |
| WEAK SIMILARITY | ≤ 45% | Probably coincidental token overlap. Hidden by default; visible with `--verbose`. |

## The two sub-scores

Below each pair the report shows `structural: NN%  semantic: NN%`. They
measure different things and the disagreement is informative.

**Structural (Jaccard over Winnowing fingerprints).** The tokenizer
normalizes identifiers to `VAR`, strings to `STR`, numbers to `NUM`; a
sliding window picks the minimum hash of each k-gram, producing a
fingerprint set. Jaccard = `|A ∩ B| / |A ∪ B|`. High when two snippets
share long contiguous *sequences* of normalized tokens. Insensitive to
renaming; sensitive to reordering.

**Semantic (cosine over TF-IDF vectors).** Each snippet becomes a
bag-of-tokens weighted by inverse document frequency over the whole
corpus, then compared by cosine angle. High when two snippets *use the
same vocabulary in similar proportions*, regardless of order. Catches
functionally similar code with different control flow (a Python `for`
vs a Go `range` loop) and "uses the same rare keywords" matches that
aren't structurally similar.

Reading the combinations:

| Pattern | Reading |
|---|---|
| structural ≈ semantic, both high | Real clone. Same tokens, same order. |
| structural high, semantic low | Rare. Short snippets where order matters but the token bag differs. |
| structural low, semantic high | Depends on language. **Cross-language:** the designed match shape — "same logic, different keyword surface"; this is what the 0.2/0.8 blend exists for. **Same-language:** shared idiom, not clone — the structural layer had every chance to fire and didn't, so the combined score is capped at 45% when structural < 0.20 (ramping out by 0.35) and the pair stays out of the default report. |
| both moderate | Usually noise from shared idioms — test scaffolding, lifecycle methods. The default `--min-confidence-lines 10` dampener demotes the short ones; raise it to demote more. |

## Structural twins

The tokenizer's normalization (identifiers → `VAR`, strings → `STR`)
is what makes the score rename-invariant — and it means identifiers
and string literals contribute nothing to it. Two table-driven tests
with entirely different test names, fields, and expected strings
genuinely tokenize identically and score 100%.

To separate that case from real copy-paste, pairs scoring above the
near-clone band (> 85%) get a third, label-only sub-score: **lexical**,
the Jaccard overlap of the two snippets' RAW-code vocabulary —
identifier and string-literal words, split on camelCase/snake_case,
lowercased, with keywords and comments excluded. When lexical falls
below 20%, the pair renders as STRUCTURAL TWIN
(`"structural_twin"` in JSON): same shape, different content. Twins
are usually parallel boilerplate — leave them alone, or parameterize
the shape if the family keeps growing; they are not "delete one copy"
findings.

What the lexical gate never does:

- It never changes the numeric score, and it never touches pairs at or
  below 85% — it only re-labels the top bands.
- It never demotes renames. A typical rename keeps most vocabulary
  (helper calls, field names, string literals), which keeps its lexical
  overlap well above the floor; the benchmark's renamed-clone fixtures
  pin this. A rename so total that *every* identifier and string
  differs is lexically indistinguishable from parallel boilerplate —
  codetwin sides with "twin" there, and the 100% score is still
  reported either way.
- It never judges pairs with fewer than 8 lexical terms per side
  (Jaccard over a five-word vocabulary is a coin flip); those fall
  through to the ordinary bands and the exact-clone length gate.

The lexical percentage renders under each top-band pair next to
`structural`/`semantic`, and appears as `lexical` on the pair in JSON
(only where computed). Precedence with the short-snippet gate: the
content check runs first — a short, content-divergent pair is a
STRUCTURAL TWIN (the more specific finding), not a "near clone
(short)".

## Clusters, relations, and pairs

The terminal report is cluster-first and has up to three sections.

**REFACTORING CLUSTERS** are families of similar snippets grouped by
DBSCAN. A cluster requires at least `--min-pts` (default 2) mutually
similar snippets within distance `--eps` (default 0.35 — pairs link at
score ≥ 65%, the "strong clone" band). One cluster = one refactoring
task that consolidates several files at once. A family of n members
implies n·(n-1)/2 pairs; those pairs are collapsed into the cluster
instead of being listed individually (the summary counts them as
"In-cluster pairs").

Each cluster header shows two numbers: the **avg similarity** across
all internal pairs and the **cohesion** — the *weakest* internal pair
(`min_score` in JSON). DBSCAN links transitively (A~B and B~C pull C
in even when A~C is weak), so a large gap between the two is the tell
that a family was chained together rather than uniformly similar. When
a cluster's cohesion falls below `--threshold`, codetwin re-links its
members at threshold strength and splits it into tighter families;
members left without a threshold-strength partner drop out as noise.

**RELATED CLUSTERS** aggregates pairs that bridge two different
clusters — `Cluster 3 ↔ Cluster 7 — 44 pairs, up to 61%` means the two
families resemble each other and might consolidate together.

**SIMILARITY PAIRS** lists the remaining individual matches: pairs
where at least one endpoint belongs to no cluster. Each is one finding,
scored independently.

Address clusters first when triaging — they represent the highest-value
consolidation opportunities. A pair that doesn't appear in any cluster
is an isolated duplicate that doesn't generalize beyond two callers.

`--flat` restores the flat pre-collapse listing (every pair
individually, pairs before clusters). `--json` output is always flat —
machine consumers see every pair regardless.

## Partial clones (PARTIAL CLONES section)

Everything above scores *whole functions* against each other, and that
has a known blind spot: a copied 15-line block inside two large,
otherwise-unrelated functions dilutes below any sane threshold — the
more unrelated code around the block, the lower the pair scores. The
`PARTIAL CLONES` section is a second detection channel for exactly
that case. Findings look like:

```
  [PARTIAL CLONE   ]  92% contained · 15 lines
    orders.go:120-134 ⊂ ProcessOrders
    invoices.go:88-102 ⊂ SummarizeInvoices
```

Read it as: lines 120–134 of `orders.go` (inside `ProcessOrders`) and
lines 88–102 of `invoices.go` (inside `SummarizeInvoices`) are the same
block of code. **Containment** is the fraction of the smaller side's
block tokens exactly matched on the other side, after the same
normalization the rest of the tool uses — so a systematic rename still
counts as matched, while an edited line inside the block lowers the
percentage. 100% contained = the block is verbatim (modulo renames).

Partial clones deliberately have **no combined score**: the enclosing
pair scored *below* your threshold (that's why the block channel looked
at it at all), so a pair-style percentage would be misleading. Their
quality bar is containment (≥ 0.85, enforced by the detector) plus the
`--min-block-lines` floor (default 8): at least that many source lines
must carry matched tokens on both sides. `--threshold` never filters
them; `--limit` caps them; `--min-block-lines 0` turns the channel off.
Test↔test partial clones are suppressed by default like test↔test
pairs (see below).

Acting on one is usually the easiest refactor in the report: the block
is contiguous on both sides, so extract it into a helper and call it
from both hosts.

## Class-level findings

For Python, Java, and JS/TS, classes are chunked twice: once per method
and once as a whole class span (named `path:start-end ClassName`). A
class↔class finding means the *container* matches — a copied class,
renamed, possibly with its methods reordered — which method-level
pairs alone underreport (each method pair looks small and independent).
Class chunks are only ever compared against other class chunks: a class
never pairs with a loose function or a single method across files
(container-vs-part comparisons are dilution noise, not clones), and a
class never pairs with its own methods (same-file nesting suppression).
So when you see both a class↔class finding and several method pairs
between the same two files, they're the same duplication reported at
two granularities: fix it at the class level. `--suggest` on a class
pair is rejected with a note — extraction targets functions/methods,
so run it on the method pairs inside.

## Test code segregation (default)

Files matching each language's test convention (`*_test.go`,
`test_*.py` / `*_test.py` / `tests/`, `*.spec.*` / `*.test.*` /
`__tests__/`, `src/test/`, Rust `tests/`, `*_test.exs` / `test/`) are
classified as test code by path. Test↔test pairs and clusters whose
members are all test snippets are suppressed from the default report
and replaced with one summary line each, e.g.
`1,874 test↔test pairs suppressed (--include-tests to show)`.
Test↔production pairs and mixed clusters always render — copy-paste
across the prod/test boundary is a real finding.

Why: test functions are short and forced into a common shape by the API
under test; they differ mostly in identifiers and string literals,
which normalization erases. They really are token-clones — just rarely
actionable ones, and on test-heavy repos they can be ~98% of the report.

`--include-tests` restores the full listing. In JSON, suppression
removes the findings from `pairs`/`clusters` and adds a top-level
`suppressed` object with the counts; with `--include-tests` the JSON is
identical to the pre-segregation schema. Scores and clustering are
unchanged — this is purely a presentation filter, applied after the
threshold and before `--limit`.

## What moves the labels

- `--threshold N` filters which pairs are *reported*. Doesn't change the math, just hides anything below.
- `--min-confidence-lines N` dampens the combined `Score` for short
  snippets (multiplier ramps linearly from 0.5× at 0 lines to 1.0× at
  N lines). **On by default with N = 10**; pass `0` to turn it off.
  The dampener is applied **once, at the scoring layer** — before the
  score reaches the matrix that DBSCAN clusters from and before the
  threshold filter. Practical consequences:
  - Two 5-line snippets that look identical earn 75% instead of 100%
    at the default N, reflecting how little evidence five lines of
    overlap actually carries; a 4-line shape-coincidence at 60% raw
    drops to 42% and out of the default report.
  - **Cluster membership respects the dampener too.** A short-snippet
    match that drops below the eps boundary doesn't get clustered. So
    the dampener doesn't just demote tiny pairs in the report — it
    dissolves clusters built on tiny-snippet noise.
  - The `structural` and `semantic` sub-scores stay raw. Only the
    combined `Score` (and the matrix DBSCAN sees) is adjusted.
  - `min(LinesA, LinesB) ≥ N` snippets are unaffected (multiplier 1.0×).
- Separately from the score, the EXACT CLONE **label** requires
  `min(LinesA, LinesB) ≥ 10`; shorter pairs demote one band to NEAR
  CLONE. Terminal and JSON labels always agree.
- Also label-only: pairs above 85% whose lexical overlap is below 20%
  render as STRUCTURAL TWIN (see "Structural twins" above). The
  content check takes precedence over the length gate.
- `--verbose` includes weak similarities in addition to the labelled
  tiers. For memory reasons pairs are only materialized down to
  `max(0.30, threshold − 0.20)`, so even `--verbose` bottoms out there.
- `--include-tests` restores test↔test pairs and test-only clusters,
  which are suppressed by default (see "Test code segregation" above).
- `--eps` only affects clusters. Stricter (lower) eps means tighter clusters
  with fewer members each; looser (higher) eps admits weaker pairs and grows
  chains. The default 0.35 keeps cluster linking aligned with the
  "strong clone" label (score ≥ 65%).

## Things the score can't see (and judgment calls you still own)

The labels tell you what's *similar*, not what's *wrong*. Some 100%
scores reflect intentional duplication that you should NOT refactor:

- **Sibling test cases.** Two short tests of the same parser with
  different inputs read as clones (usually STRUCTURAL TWIN when their
  vocabulary diverges enough for the lexical gate to see it). Use
  `pytest.parametrize` or its equivalent only if the cases are short
  and exhaustive.
- **Adapter classes for parallel APIs.** Kafka and Rabbit message
  handlers with the same lifecycle but different broker semantics —
  whether to extract a base class depends on how often the parallel
  APIs diverge. A 100% match here is a *signal* to look, not a verdict.
- **Boilerplate forced by the framework.** ASGI middleware, FastAPI /
  Flask route handlers. The shape is the framework's, not yours.
  Usually leave alone.

The judgment of "is this duplication worth removing?" is yours; the
tool's job is to surface candidates.

## Git-aware modes

Three optional flags layer git context on top of the report:

- **`--cross-lang-only`** — drops same-language pairs. The semantic
  scorer already pairs across languages (a Python loop and a Go loop
  with the same vocabulary will match), but most reports are dominated
  by within-language clones. Use this in polyglot repos to surface logic
  duplicated between, say, a Go service and its TypeScript dashboard.
- **`--since <ref>`** — keeps only pairs and clusters where ≥1 endpoint
  overlaps lines changed since `<ref>` (committed or unstaged).
  Designed as a CI ratchet: a team with existing duplication can adopt
  `codetwin --since main --threshold 0.85` as a gate and only fail
  builds that introduce *new* duplication. Requires git on PATH.
- **`--blame`** — calls `git blame` per snippet and attaches an
  "introduced YYYY-MM-DD by Author (sha)" line under each match (and
  `provenance_a` / `provenance_b` blocks in JSON). Pair with
  `--sort age` to surface the freshest clones first ("which duplication
  did we add this quarter?"). Requires git on PATH.

If `--since` or `--blame` is set in a directory that isn't a git repo,
or on a system without git installed, codetwin exits 1 with a clear
error rather than silently degrading — the user explicitly opted in to
a git-dependent feature, so silent fallback would hide the real problem.

## Refactor suggestions

`--suggest <pair-id>` emits a unified diff that *adds* a starter
helper to the file containing snippet A. The helper is a literal copy
of A's body, prefaced by a `Divergences (B vs A):` comment block
listing exactly what differs (`//` for Go, `#` for Python). Codetwin
doesn't rewrite the call sites — it plants a starting point so a human
(or the Claude skill) can finish the extraction with full visibility
on every divergence.

A few things worth knowing:

- **Applying the diff.** `--suggest` writes to stdout and never touches
  your files — the contract is "emit, don't apply" because the primary
  consumer is an LLM agent that decides how to land the change. If you
  trust the diff yourself, pipe it: `codetwin --suggest <id> | git
  apply`. Use `git apply --check` first for a dry-run that exits
  non-zero if the hunk won't land cleanly.
- **Confidence** is `commonLines / max(linesA, linesB)`. A 1.0
  confidence means every line of A is shared with B (literal
  duplication); 0.5 means about half overlap. v1 doesn't gate on
  confidence — even a low-confidence suggestion can be useful as a
  diff to read — but `--suggest-all --json` exposes the number so
  consumers can filter.
- **Pair IDs** are 8-char hex digests of `sha1(min(NameA,NameB) + "|"
  + max(NameA,NameB))`. They're stable across runs and order-invariant
  (the same pair has the same ID regardless of which side is "A").
- **Language coverage in v1.** All six supported languages emit
  helpers — Go, Python, Java, JavaScript/TypeScript, Rust, and Elixir.
  The synthesizer needs language-specific logic to spot the function
  header and produce a sensible helper body. For Java, the helper is
  appended at file scope (after the wrapping class's closing `}`) and
  carries a `// NOTE: appended at file scope` comment; the human moves
  it into the appropriate class before compiling. For
  JavaScript/TypeScript, ES6+ class methods are unwrapped and emitted
  as free `function` helpers; when the body references `this`, the
  helper carries a `// NOTE: extracted as a free function from a
  class-method context…` comment flagging that `this` must be wired
  at call sites. For Rust, impl methods are emitted as free `fn`
  helpers carrying `&self` as an explicit parameter; when the body
  references `self`, the helper carries a `// NOTE: extracted as a
  free function with &self carried as an explicit parameter…`
  comment. For Elixir, every common def shape is supported: `def`/`defp`/
  `defmacro`/`defmacrop` block-form, `, do:` shorthand (single-line
  and split forms), multi-line wrapping headers, pattern-matched
  args, and `when` guards. The helper preserves the input's keyword
  form and shorthand-vs-block style and ALWAYS carries a `# NOTE:
  appended at file scope; Elixir defs must live inside a defmodule…`
  comment, since Elixir cannot have free-standing defs.

## A note on config

Some `.codetwin.json` knobs change what the tool *sees* before it
scores — `ignore_patterns` strips matching lines (often logging) before
tokenization, and `ignore_paths` excludes whole files from the scan.
Both can explain "why did this score lower than I expected?" or "why
is this pair missing from the report?" Run `codetwin --skill` for the
full config schema and ignore-pattern semantics.
