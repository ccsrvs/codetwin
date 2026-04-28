# codetwin — interpreting the output

This guide explains what the report is showing you and how to read it. For
flag reference run `codetwin --help`; for the workflow-oriented skill that
agents trigger on, run `codetwin --skill`.

## The combined score

Each pair's headline percentage is `0.5 × structural + 0.5 × semantic`,
shown as a label and a number. Bands use strict `>` thresholds:

| Label | Score | What it usually means |
|---|---|---|
| EXACT CLONE | > 95% | Token-for-token equivalent (after the tokenizer's `VAR` / `STR` / `NUM` normalization). Almost certainly copy-paste. Extract a shared utility and delete one. |
| NEAR CLONE | > 85% | Virtually identical with one or two token-level edits (a swapped literal, a different default arg). Treat as a clone unless the difference is intentional. |
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
| structural low, semantic high | "Functionally similar but written differently" — same problem, different shape. Often the most interesting refactor target, less interesting as a literal clone. |
| both moderate | Usually noise from shared idioms — test scaffolding, lifecycle methods. `--min-confidence-lines` exists to demote these. |

## Pairs vs clusters

The report has two sections.

**SIMILARITY PAIRS** are individual two-snippet matches above the
threshold. Each pair is one finding, scored independently.

**REFACTORING CLUSTERS** are families of similar snippets grouped by
DBSCAN. A cluster requires at least `--min-pts` (default 2) mutually
similar snippets within distance `--eps` (default 0.45). One cluster =
one refactoring task that consolidates several files at once.

Address clusters first when triaging — they represent the highest-value
consolidation opportunities. A pair that doesn't appear in any cluster
is an isolated duplicate that doesn't generalize beyond two callers.

## What moves the labels

- `--threshold N` filters which pairs are *reported*. Doesn't change the math, just hides anything below.
- `--min-confidence-lines N` dampens the combined `Score` for short
  snippets (multiplier ramps linearly from 0.5× at 0 lines to 1.0× at
  N lines). The dampener is applied **once, at the scoring layer** —
  before the score reaches the matrix that DBSCAN clusters from and
  before the threshold filter. Practical consequences:
  - Two 5-line snippets that look identical earn ~60-65% instead of 100%,
    reflecting how little evidence five lines of overlap actually carries.
  - **Cluster membership respects the dampener too.** A short-snippet
    match that drops below the eps boundary doesn't get clustered. So
    setting `--min-confidence-lines 20` doesn't just demote tiny pairs
    in the report — it dissolves clusters built on tiny-snippet noise.
  - The `structural` and `semantic` sub-scores stay raw. Only the
    combined `Score` (and the matrix DBSCAN sees) is adjusted.
  - `min(LinesA, LinesB) ≥ N` snippets are unaffected (multiplier 1.0×).
- `--verbose` includes weak similarities in addition to the labelled tiers.
- `--eps` only affects clusters. Stricter eps means tighter clusters with fewer members each.

## Things the score can't see (and judgment calls you still own)

The labels tell you what's *similar*, not what's *wrong*. Some 100%
scores reflect intentional duplication that you should NOT refactor:

- **Sibling test cases.** Two short tests of the same parser with
  different inputs read as exact clones. Use `pytest.parametrize` or its
  equivalent only if the cases are short and exhaustive.
- **Adapter classes for parallel APIs.** Kafka and Rabbit message
  handlers with the same lifecycle but different broker semantics —
  whether to extract a base class depends on how often the parallel
  APIs diverge. A 100% match here is a *signal* to look, not a verdict.
- **Boilerplate forced by the framework.** ASGI middleware, FastAPI /
  Flask route handlers. The shape is the framework's, not yours.
  Usually leave alone.

The judgment of "is this duplication worth removing?" is yours; the
tool's job is to surface candidates.

## A note on config

Some `.codetwin.json` knobs change what the tool *sees* before it
scores — `ignore_patterns` strips matching lines (often logging) before
tokenization, and `ignore_paths` excludes whole files from the scan.
Both can explain "why did this score lower than I expected?" or "why
is this pair missing from the report?" Run `codetwin --skill` for the
full config schema and ignore-pattern semantics.
