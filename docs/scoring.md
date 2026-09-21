# Scoring internals

| Score | Label | Recommended action |
|---|---|---|
| > 95% | Exact clone | Extract shared utility, delete one |
| > 85% | Near clone | Virtually identical; treat as a clone unless intentional |
| > 85% + lexical < 20% | Structural twin | Same shape, different content — likely parallel boilerplate, not copy-paste |
| > 65% | Strong clone | Parameterize differing parts |
| > 45% | Refactor target | Evaluate shared abstraction |
| < 45% | Weak similarity | Probably coincidental |

The "Exact clone" label is additionally evidence-gated: it requires both
snippets to span at least 10 non-blank lines. A shorter pair renders as
a near clone even at a perfect score (the numeric score is unchanged —
only the label demotes), because two tiny functions can share their
entire token shape by API force alone.

### Structural twins

Normalization erases identifiers and string literals (`VAR`/`STR`) —
that's what makes the score rename-invariant, and it's also why two
table-driven tests with completely different test names, fields, and
expected strings can score 100%: they really are token-clones, just not
copy-paste. To tell the two apart, codetwin keeps a third, label-only
**lexical** sub-score: Jaccard over each snippet's raw identifier and
string-literal vocabulary (camelCase/snake_case split, lowercased,
keywords and comments excluded). A pair in the exact/near bands
(> 85%) whose lexical overlap is below 20% renders as **STRUCTURAL
TWIN** (`"structural_twin"` in JSON, with the `lexical` sub-score
exposed on the pair): same shape, different content — parallel
boilerplate to leave alone or parameterize, not duplication to delete.

The lexical score never feeds the numeric score, so rename detection is
untouched: a typical rename keeps most of its vocabulary (helper calls,
field names, string literals) and stays comfortably above the floor,
which is pinned by the benchmark's renamed-clone fixtures. Pairs ≤ 85%
are never modified, and pairs whose snippets carry fewer than 8 lexical
terms are never demoted (too little vocabulary to judge content either
way).

Final score is `0.5 × structural (Jaccard) + 0.5 × semantic (cosine TF-IDF
over token trigrams)` for same-language pairs. Cross-language pairs use
`0.2 × structural + 0.8 × semantic`: winnowing fingerprints hash raw keyword
sequences, so identical logic in two languages shares almost no fingerprints,
and the semantic layer — which canonicalizes cross-language keywords
(`func`/`def`/`fn`, `nil`/`None`/`null`, …) — carries the weight instead.
For a longer walk-through of what the score means, what the
`structural`/`semantic` sub-scores below each pair tell you, and how
pairs differ from clusters, run `codetwin --guide`.

**Same-language pairs additionally require structural corroboration.**
Trigram cosine saturates on shared language idioms — two unrelated
map-building loops, two comprehension-plus-guard functions, two
async/try-catch wrappers — because normalization erases the
identifiers that distinguish them. For a same-language pair the
winnowing layer had every chance to fire, so near-zero structural
evidence means idiom, not clone: when structural is below 0.20 the
combined score is capped at 0.45 (just under the report band), with
the cap ramping out linearly by structural 0.35, where it can no
longer bind. Cross-language pairs are exempt — structural absence is
expected there, which is the whole point of the 0.2/0.8 blend.

### Short-snippet confidence

Two 5-line snippets that share their entire token shape and two 25-line
snippets that do the same both score identically, but the first is much
weaker evidence — short snippets are forced into a shared shape by
their API surface (e.g. test scaffolding that has to call one function
and assert on the result). `--min-confidence-lines N` is a length-aware
dampener, **on by default at N = 10**: the combined score is multiplied
by `0.5 + 0.5 · min(LinesA, LinesB) / N` (capped at 1.0), so matches
under N non-blank lines lose proportional score. At the default, a
10-line exact clone keeps its full 100% score, while a 4-line
shape-coincidence scoring 60% raw dampens to 42% and drops below the
default threshold. The dampener is applied once at the scoring layer,
so it also affects DBSCAN cluster boundaries — short-snippet matches
that drop below the eps threshold don't cluster. Raise it (e.g.
`--min-confidence-lines 20`) to push more test boilerplate out of the
report, or pass `--min-confidence-lines 0` to turn it off and restore
raw scores.

[Back to README](../README.md#scoring)

