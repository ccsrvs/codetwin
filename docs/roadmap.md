# codetwin roadmap — unique-niche bets

_Last updated: 2026-07-14 (bet #6 shipped). Sources: planning conversation that shipped
_Last updated: 2026-07-14. Sources: planning conversation that shipped
commits `159a298`, `59fe97f`, `f53a739` on
`claude/explore-unique-features-4rInJ`; detection-quality overhaul
merged as PR #7 (`fix/matching-pipeline-review`)._

## Status at a glance

| # | Bet | Status | Surface |
|---|---|---|---|
| 1 | Git provenance | **Shipped** | `--blame`, `--sort age`, `--sort age-asc` |
| 2 | PR-delta mode | **Shipped** | `--since <ref>` |
| 3 | Cross-language as the headline | **Shipped** | `--cross-lang-only`, `lang_{a,b}` JSON |
| 4 | Refactor patch emission | **Shipped (all 6 languages)** | `--suggest <pair-id>`, `--suggest-all`, `id` + `suggested_patch` in JSON |
| 5 | Clone watchlist + drift alerts | **Shipped** | `--update-baseline`, `--baseline`, `drift` JSON array |
| 6 | Cross-repo / org-level scanning | **Shipped** | automatic on ≥2 directory roots; `--cross-repo-only`, `repo_a`/`repo_b`/`member_repos`/`cross_repo` JSON, per-repo cluster grouping + `cross-repo` tag |
| 7 | Behavioural / runtime equivalence | Flagged longshot | — |
| — | Detection quality + report SNR (PR #7) | **Shipped** | `internal/bench` ground-truth benchmark, retuned scoring defaults, cluster-first report, `--flat` |

### Per-language emitter status (Bet #4)

| Language | Synthesizer | Notes |
|---|---|---|
| Go | **Shipped** | Starter helper + divergence comment block. |
| Python | **Shipped** | Starter helper with `#`-comment divergence block; class methods carried through as top-level helpers with `self`/`cls` as ordinary parameters. Multi-line (Black-formatted) `def` signatures are carried whole — name rewritten on the first line, continuation params / default args / annotations / the closing `) -> Ret:` line verbatim (fixture: `realworld-multiline-sig`). |
| Java | **Shipped** | Starter helper with `//`-comment divergence block; modifiers/generics/`throws` preserved verbatim; helper is appended at file scope after the wrapping class's closing `}` and carries a `// NOTE: appended at file scope…` placement comment (file won't compile until a human moves the helper into the appropriate class — the v1 "starter, human finishes" boundary). Control-flow keyword set extended with `throw`. |
| JavaScript / TypeScript | **Shipped** | Starter helper with `//`-comment divergence block. Recognises four definition shapes: `function name(...)` (incl. `async` / `export default`), arrow assignments `const|let|var name = (...) => {…}`, `const|let|var name = async function(...) {…}`, and ES6+ class methods. The JS splitter was lifted to method-level granularity in the same commit (matching Python and Java) so detection itself runs on individual methods rather than swallowing whole class bodies. When a method references `this.`, the helper carries a `// NOTE:` line flagging that `this` must be wired at call sites. Control-flow keyword set extended with `throw` (mirrors Java). TypeScript-specific header shapes are handled (fixture: `realworld-typescript`, real `.ts` files): parameter/return-type annotations and generics are carried onto the helper verbatim (never stripped — plain-JS pairs never contain them, so plain-JS output is byte-identical), arrow return annotations (`(x: string): Foo => {`) survive the free-function rewrite, and class-method access modifiers (`public`/`private`/`protected`/`readonly`) are dropped (invalid on a free function) while `async`/`static` are preserved. Interface declarations and type aliases remain out of scope — the splitter never chunks them as functions. |
| Python | **Shipped** | Starter helper with `#`-comment divergence block; class methods carried through as top-level helpers with `self`/`cls` as ordinary parameters. |
| Java | **Shipped** | Starter helper with `//`-comment divergence block; modifiers/generics/`throws` preserved verbatim; the patch inserts the helper inside the innermost class/interface/enum/record enclosing A's chunk (immediately before its closing `}`, indented like a sibling member) so the file compiles as emitted. Defensive fallback: when no enclosing type is found the helper appends at file scope with a `// NOTE: appended at file scope…` placement comment. Control-flow keyword set extended with `throw`. |
| JavaScript / TypeScript | **Shipped** | Starter helper with `//`-comment divergence block. Recognises four definition shapes: `function name(...)` (incl. `async` / `export default`), arrow assignments `const|let|var name = (...) => {…}`, `const|let|var name = async function(...) {…}`, and ES6+ class methods. The JS splitter was lifted to method-level granularity in the same commit (matching Python and Java) so detection itself runs on individual methods rather than swallowing whole class bodies. When a method references `this.`, the helper carries a `// NOTE:` line flagging that `this` must be wired at call sites. Control-flow keyword set extended with `throw` (mirrors Java). |
| Rust | **Shipped** | Starter helper with `//`-comment divergence block. Recognises `fn name(...)` headers with any combination of `pub` / `pub(crate)` / `async` / `unsafe` / `const` / `extern` modifiers; preserves generics, lifetimes, return types, and `where` clauses verbatim. Impl methods come through the splitter as method-level chunks (the splitter was already method-granular for Rust). When the body references the standalone `self` keyword, the helper carries a `// NOTE: extracted as a free function with &self carried as an explicit parameter…` block flagging that the receiver must be bound at call sites. Control-flow keyword set extended with `panic` so `panic!(…)` macro asymmetry triggers rejection (mirrors Java's `throw`). |
| Elixir | **Shipped (v2)** | Starter helper with `#`-comment divergence block. Recognises every common def shape: `def`/`defp`/`defmacro`/`defmacrop` block-form headers (`do … end`), `, do:` shorthand (single-line and split-across-lines forms), multi-line wrapping headers (Phoenix-style `def update(\n  conn,\n  …\n) do`), pattern-matched args (`{:ok, value}`, `%{"id" => id}`), and `when` guards. Splitter is method-granular over `defmodule` bodies (mirroring Python's behaviour). Helper preserves the input's keyword (def vs defp vs defmacro/defmacrop), guards, and shorthand vs block form. The patch inserts the helper inside the innermost defmodule enclosing A's chunk (immediately before its closing `end`, indented like a sibling def) so the file compiles as emitted; when no defmodule encloses the chunk (defensive — Elixir cannot have free-standing defs) the helper appends at file scope with a `# NOTE: appended at file scope…` block. Control-flow keyword set is `["raise", "throw", "exit"]` (Elixir has no `return`/`break`/`continue`; functions return their last expression). Real-world fixtures: GenServer (`@impl`, do: shorthand alongside block form, nested `case`), multi-clause pattern-matched defs (`def parse({:ok, _}, ...)` etc.), and `defmacro` DSL builders. |

## Context

The question that drove this roadmap: what areas could codetwin take on
that put it in space no other clone detector occupies?

Today's landscape: jscpd, PMD CPD, Simian, dupl, NiCad/Deckard/CCFinder,
SonarQube duplications, Sourcery (Python only), Semgrep / ast-grep
(pattern-matching, not clone discovery). All of them stop at "here are
the duplicated regions". None of them pair clone detection with
provenance, refactor patches, PR-delta gating, or cross-language
matching at the function level.

Today's codetwin: function-level chunks across 8 languages, structural
(Winnowing/Jaccard, k=10 over whitespace-invariant token streams) +
semantic (sublinear TF-IDF cosine over canonicalized token trigrams)
scoring with language-aware blending, DBSCAN clusters, cluster-first
terminal report, inverted-index pruning, per-file cache, Claude-skill
packaged. CLI-only, one-shot, no external deps.

## Ranked unique-niche bets

The bets below are ranked by **(a)** how cleanly they fit codetwin's
existing architecture, **(b)** how clearly no other tool does it, and
**(c)** practical lift.

### 1. Git provenance — when, by whom, which is the original
**Status: Shipped (commit `159a298`).**

**Why nobody has it:** Existing tools answer "where", not "when" or
"who". Provenance turns codetwin from a static linter into a forensic
tool: "this duplication was introduced in PR #842 last week" or "all
five members of cluster 3 trace back to a copy-paste event in 2023-Q1".

**What landed:**
- `--blame` flag: runs `git blame --line-porcelain` per snippet,
  attaches `*report.Provenance` to each pair.
- `--sort age` / `--sort age-asc`: order pairs by introduction date
  (newer-of-the-two-endpoints' `FirstTime`).
- Terminal render adds `introduced YYYY-MM-DD by Author (sha)` under
  each match; `--json` adds `provenance_a` / `provenance_b` blocks.
- `internal/git/blame.go` — porcelain parser that aggregates per-line
  blame into a single `BlameRange` (oldest + newest commit/author/time).

**Verified:** unit + integration tests in
`internal/git/blame_test.go`, `internal/report/report_test.go`,
`cmd/codetwin/blame_test.go`. Smoke-tested on the live repo —
`./codetwin --blame --sort age --threshold 0.85 ./internal/report`
prints the introduction date and short SHA for every endpoint.

### 2. PR-delta mode — fail only on *new* duplication
**Status: Shipped (commit `159a298`).**

**Why nobody has it:** SonarQube has new-code-period scoping but it's
heavyweight, server-side, and file-grained. None of the OSS clone
detectors do this at the function level. Most repos can't adopt clone
gating because their existing tech debt would fail the gate forever;
PR-delta makes a clean ratchet possible.

**What landed:**
- `--since <ref>` flag: keep only pairs and clusters whose endpoints
  overlap lines changed between `<ref>` and the working tree
  (committed + unstaged).
- `internal/git/diff.go` — parses `git diff --unified=0` into a
  `path → []LineRange` map; `DiffMap.Touches` is the snippet-overlap
  predicate.
- `cmd/codetwin/main.go`: `filterPairsBySince` and
  `filterClustersBySince` apply between `BuildMatrix`/DBSCAN and
  `report.Prepare`.

**Canonical CI invocation:**
```bash
codetwin --since main --threshold 0.85 --json ./src \
  | jq '.pairs | length' | xargs -I{} test {} -eq 0
```

**Verified:** `internal/git/diff_test.go` (parser + integration with
real `git diff`), `cmd/codetwin/since_test.go` (filter).

### 3. Cross-language as the headline feature
**Status: Shipped (commit `159a298`).**

**Why it matters:** The semantic layer already paired Python↔Go in
`internal/similarity/matrix.go:71-72`, but the JSON output, README, and
skill manifest never surfaced it. Polyglot monorepos (Go service + TS
dashboard + Python ETL sharing logic) are the new normal — and no
other tool finds duplicate logic across them.

**What landed:**
- `report.Pair.LangA` / `LangB`, populated by `BuildMatrix` from
  `Snippet.Lang`.
- `Options.CrossLangOnly` + `--cross-lang-only` flag: filters in
  `report.Prepare` to drop same-language and unknown-language pairs.
- JSON output gains `lang_a` / `lang_b` (omitempty).
- README headline now leads with the cross-language story; skill.md
  and guide.md document the flag and a polyglot recipe.

**Verified:** `internal/report/report_test.go` (filter behavior),
`internal/similarity/matrix_test.go` (Lang population).

### 4. Refactor patch emission — turn detection into action
**Status: Shipped (v1, Go-only).**

**What landed:**
- `report.Pair.ID`: stable, order-invariant 8-char hex digest of
  `sha1(min(NameA,NameB) + "|" + max(NameA,NameB))`. Populated in
  `similarity.BuildMatrix`; surfaced as `id` in JSON.
- `--suggest <pair-id>`: looks up the pair across all materialized
  pairs (so users can address a sub-threshold pair without retuning
  `--threshold`), runs the refactor pipeline, and prints a unified
  diff to stdout. Rejection cases print `note: <reason>` on stderr
  and exit 1.
- `--suggest-all`: with `--json`, populates `suggested_patch` on every
  visible pair. Off by default — synthesis cost scales with pair
  count.
- New package `internal/refactor`:
  - `align.go` — language-agnostic line-level LCS alignment producing
    `Common []LineSpan` + `Holes []Hole`. Operates on raw source
    rather than normalized tokens (the tokenizer collapses literals
    so token-level alignment can't see literal-only differences).
  - `synth.go` — Go emitter. Dispatch returns
    `unsupported language: <lang>` for Python/JS/TS/Rust/Java/Elixir.
  - `patch.go` — unified-diff builder; appends the helper at the end
    of A's file with 3 lines of trailing context.

**v1 scope choice:** the helper is a *literal copy* of A's body with a
`// Divergences (B vs A):` comment block listing every divergence;
codetwin doesn't rewrite either call site. Full parameterization would
require per-language type inference, which is unsafe without an AST.
The starter-helper approach makes the boundary explicit — codetwin
gets you 80% of the way, the human (or the Claude skill) finishes.

**Verified:**
- `internal/refactor/{align,synth,patch}_test.go` — table-driven over
  21 fixtures across 6 languages × 3 complexity tiers + 3 Go rejection
  cases.
- `internal/refactor/patch_test.go` round-trips a fixture through
  `git apply --check`.
- `cmd/codetwin/suggest_test.go` covers the CLI hooks
  (`emitSuggestion`, `buildSuggestionMap`).

**Follow-up commits (one per language):** Python, JS/TS, Rust, Java,
Elixir emitters. Fixtures are already in place under
`testdata/refactor/<lang>/{simple,medium,advanced}/`.

### 5. Clone watchlist + drift alerts
**Status: Shipped.**

**Why nobody has it:** Clone families *evolve* — once detected, members
gradually drift apart, fixing a bug in one but not the others. No tool
tracks this.

**What landed:**
- `--update-baseline <file>`: after the normal scan (report still
  prints), write a snapshot of the visible clusters and exit 0.
- `--baseline <file>`: compare the scan's clusters against the
  snapshot; drift events print to stderr one line each
  (`drift: <kind> cluster <n>: <detail>`); any drift exits 1 — the CI
  gate. No drift exits 0 silently. `--json` adds a `drift` array
  (omitted when empty, so the schema is unchanged for non-watchlist
  consumers). The two flags are mutually exclusive.
- Five event kinds: `member-added`, `member-removed`,
  `member-changed` (body changed but still clusters — detected via a
  per-member normalized-token hash), `cluster-appeared`,
  `cluster-dissolved`.
- New package `internal/baseline`: versioned JSON snapshot
  (`schema_version` 1; mismatch = explicit "regenerate" error), the
  scan params that gate comparability (threshold / eps / min-pts /
  granularity / include-tests — a mismatch is a clear pre-scan error,
  not drift), and the drift diff.
- **Member identity across runs** reuses the exact ignore_pairs
  normalization (`config.ParseSnippetName`): line ranges are stripped
  and paths are made relative to the scan roots, so ordinary edits and
  different scan directories never read as drift. Duplicate keys
  inside a cluster (Elixir multi-clause defs) merge with a combined
  hash.
- **Cluster matching** is greedy highest-Jaccard over member keys with
  a documented floor (overlap coefficient ≥ 0.5 — the two clusters
  share at least half the smaller one's members, so grown clusters
  still match), ties broken by first member key; below the floor a
  pair reads as dissolved + appeared.
- **Determinism:** the snapshot has NO timestamp (deliberate — VCS
  history dates it); clusters/members are written sorted, so two
  `--update-baseline` runs over the same tree are byte-identical.
- Baselines snapshot the post-suppression *visible* clusters, so test
  segregation and `--include-tests` compose naturally: you baseline
  what you see.

**Verified:** all five test layers per the plan below —
`internal/baseline/baseline_test.go` (round-trip, byte determinism,
schema/params errors, all five kinds on synthetic sets, floor/greedy/
tie-break); fixture-driven `cmd/codetwin/baseline_test.go` over
`testdata/baseline/{before,after}` (member-added / member-removed /
member-changed each fire exactly once through the real pipeline; a
line-shifting comment pins that identical bodies never drift);
subprocess `cmd/codetwin/baseline_subprocess_test.go` (stderr lines +
exit codes end-to-end, JSON `drift` array, mutual exclusion, schema and
params mismatch errors, byte-determinism through the binary); self-host
`TestSelfHost_BaselineZeroDriftOnInternal` (snapshot `./internal`,
re-compare unchanged, zero drift).

### 6. Cross-repo / org-level scanning
**Status: Shipped (2026-07-14).**

**Why nobody has it:** Existing tools are repo-scoped. Platform teams
have no good way to find logic that should be a shared library across N
service repos. Codetwin's cache makes incremental org-scale scanning
viable.

**What landed:**
- **Automatic on ≥2 directory roots** — `codetwin ../svc-a ../svc-b
  ../svc-c` treats each root as a "repo"; no opt-in flag. (The
  `--repos repos.txt` file form was dropped as redundant — shells
  expand `$(cat repos.txt)` fine.) Single-root and file-argument
  invocations are byte-identical to before; that compatibility
  contract is pinned by subprocess tests.
- **Repo labels & namespacing** — label = base name of the root's
  absolute path; duplicate base names disambiguate by input order
  (`api`, `api~2`). Snippet names become `repo:relpath:start-end Sym`
  (root-relative path). Assigned post-scan in `cmd/codetwin/repos.go`,
  so the cache stays repo-agnostic.
- **Per-repo cluster grouping** — clusters spanning ≥2 repos get a
  `cross-repo` header tag and members grouped under one
  `repo — N snippets` line per repo; single-repo clusters render flat.
- **JSON** — pairs and `partial_clones` gain `repo_a`/`repo_b`,
  clusters gain `member_repos` + `cross_repo`; all omitempty.
- **`--cross-repo-only`** (`report.Options.CrossRepoOnly`, mirroring
  `--cross-lang-only`'s plumbing; the two compose) — keeps pairs/blocks
  with two distinct repo labels and clusters spanning ≥2 repos; errors
  out with <2 directory roots.
- **Interactions** — per-repo test conventions still classify
  (cross-repo test↔test pairs suppressed by default); `ignore_pairs`
  endpoints match the UN-prefixed root-relative name; `--suggest` pair
  IDs resolve in multi-root runs; absolute-path cache keys make org
  rescans incremental; `--since`/`--blame` fail fast with a clear
  error when roots live in different git repositories (documented
  limitation — per-repo provenance is future work).

**Verified:** `internal/report/crossrepo_test.go` +
`cmd/codetwin/repos_test.go` (unit), `testdata/multirepo/svc-{a,b,c}`
fixture, `cmd/codetwin/multirepo_subprocess_test.go` (13 subprocess
cases incl. the roadmap's jq acceptance check and the single-root
compatibility pins), `cmd/codetwin/multirepo_perf_test.go` (6 sibling
repos: cold ~0.4s, cache-warm ~0.3s, identical output).

**Behavior change:** multi-root invocations that predate this bet
(e.g. `codetwin ./internal ./cmd`) now report namespaced names
(`internal:…`, `cmd:…`) and repo JSON fields. Deliberate — the
prefixes make multi-root reports unambiguous — and called out in the
README's Cross-repo scanning section.

### 7. Behavioural / runtime equivalence (longshot, highest novelty)
**Status: Flagged longshot.** Not on the next-quarter list.

**Why nobody has it:** Confirms two clones are *observably* equivalent
by fuzzing inputs and comparing outputs. Closest prior art is
differential testing (research-y). Would let codetwin distinguish
"these are textually similar but behave differently" from "extract
this safely".

**Fit:** Poor for a no-deps single binary. Needs language-specific
sandboxes. Worth flagging only as a future research direction.

## Detection quality + report SNR overhaul (PR #7, 2026-07-02)

Not one of the original bets, but a prerequisite for all of them: a
pipeline review found real bugs, and quantifying the "noisy defaults"
complaint led to a scoring overhaul. Five commits on
`fix/matching-pipeline-review`.

**Bugs fixed (`69a43f0`):**
- Winnowing short-stream gap — snippets with fewer k-grams than one
  window got an *empty* fingerprint set and could never match
  structurally, even identical copies.
- Python splitter — a column-0 `#` comment inside a function body
  terminated the chunk early, hiding the rest of the body.
- Symlinked paths (macOS `/var` → `/private/var`) broke `--blame`
  (error) and made `--since` silently drop every pair.

**Tokenization (`7ee9dc3`):** punctuation is emitted as single-rune
tokens so formatting never changes the token stream (`x=a+b` ==
`x = a + b` == minified). `Jaccard(empty, empty)` is 0, not a vacuous
1.0. Elixir Detect heuristic requires a word-boundary `do`.

**Scoring (`583c1e8`):** the old defaults reported 171,969 pairs on
codetwin's own repo — unigram TF-IDF over VAR-normalized streams
scores *unrelated* code at cosine 0.7–0.98, and the 0.30 threshold sat
below that floor. Changes, each validated against the new benchmark:
- semantic terms are token **trigrams** with **sublinear TF** and an
  **evidence floor** (< 4 terms → empty vector, cosine 0)
- semantic stream drops punctuation and canonicalizes cross-language
  keywords (`func`/`def`/`fn` → `FN`, `range` → `in`, `nil`/`None`/
  `null` → `NIL`, …)
- **language-aware blend**: same-language 0.5/0.5; cross-language
  0.2 structural / 0.8 semantic (winnowing can't match across keyword
  sets)
- structural k 5 → 10 (denser token streams), default `--threshold`
  0.30 → 0.50, cache schema Version 2

Result: self-host 171,969 → ~1,700 pairs; gin (24k LOC) hand-verified
at both ends — the 50–55% band surfaces gin's real `Bind`/`BindBody`
copy-paste across four binding backends (3-line methods only the
semantic layer can see).

**Cluster-first report (`31a5ffc`, `3b92bc5`):** clusters render first
with avg similarity; intra-cluster pairs collapse into the cluster;
cross-cluster pairs aggregate into one `RELATED CLUSTERS` line per
family pair; `SIMILARITY PAIRS` lists only pairs with an unclustered
endpoint. `--flat` restores the flat listing; `--json` is always flat
(CI contract unchanged). Summary tiers classify *all* visible pairs so
collapsed exact clones still show in totals. On gin: 13,312 report
lines → 3,348.

### The benchmark is the tuning contract

`internal/bench` + `testdata/bench/` is a labeled ground-truth
benchmark: positives (every refactor fixture pair + formatting-variant
/ renamed / cross-language cases), hard negatives (test stubs, HTTP
handlers sharing only error-handling idiom, unrelated logic), and a
noise-floor assertion (unrelated-pair p95 ≤ 0.30). **Any change to
tokenizer/fingerprint/similarity must keep `TestBench_GroundTruth`
green — tune against it, not eyeballed report output.** It logs a
per-case score table and the worst noise pairs by name under `-v`.

### Deferred follow-ups (P3s from the PR #7 review)

- **`cache.Version` doesn't encode fingerprint k/w or tokenizer
  schema** — a future `DefaultK` retune silently serves stale
  fingerprints unless the version is manually bumped. Encode the
  parameters in the version/key, or validate the cached per-chunk `K`
  against `fingerprint.DefaultK` on load.
- **`Unknown`↔`Unknown` language pairs get the cross-language 0.2/0.8
  blend** in `BuildMatrix`. Unreachable today (scan gates on supported
  extensions) but a latent trap; two unrecognized files are more
  likely the *same* language.
- **`buildPreviews` builds previews for collapsed intra-cluster pairs**
  that the cluster-first layout never renders — wasted work with
  `--preview` on big repos.
- **Cross-paradigm cross-language matching is weak by design** — an
  index loop vs `Enum.reduce` shares few trigrams even after keyword
  canonicalization (integration tests assert ranking, not absolutes).
  Deeper canonicalization of iteration idioms would be its own bet.

## Recommendation (original) and what shipped

The original recommendation was **1 + 2 + 3** as the headline narrative:
*"codetwin finds duplicate logic — across languages, across the git
history, and only complains about the duplication you just introduced."*
**That triad is now shipped.**

Bet **4** (refactor patches) is **fully shipped** — Go, Python, Java,
JavaScript/TypeScript, Rust, and Elixir all emit starter helpers via
`--suggest`. Codetwin goes from reporter to *starter generator*: it
emits a unified diff that adds a helper extracted from a clone pair,
with a comment block listing every divergence. The Elixir commit also
built the long-standing missing function-level splitter so detection
runs at def granularity rather than swallowing whole modules. The Java
emitter established the `cmd/codetwin/refactor_subprocess_test.go`
convention required by the "Testing layers" section below — every
future emitter should add subprocess cases there. The JS/TS emitter
added the first `cmd/codetwin/main_selfhost_test.go` cases, partially
closing the standing 25.2% coverage gap on cmd/codetwin.

### Bet #4 deferred follow-ups

These were carved out of the v1 emitter implementations and remain
worth pursuing if a real-world fixture surfaces or a user requests
them:

- **Elixir `@spec` / `@doc` propagation** — **Shipped.** The emitter
  now re-reads the source file at synthesis time and carries the
  symbol-scoped @doc/@spec block above the def's first clause into
  the helper (@spec renamed to the helper's name, heredocs verbatim,
  conflicting B-spec surfaced as a one-line `# NOTE:`); see
  `exGroupForSnippet` and the `realworld-spec` fixture tier.
- **Elixir multi-clause grouping** — **Shipped.** At synthesis time
  (`--suggest`/`--suggest-all` only — detection chunks stay
  clause-granular) adjacent sibling clauses of the endpoint symbol
  (same name + arity, contiguous apart from blanks/comments/
  attributes) are emitted as one multi-clause helper, renamed
  consistently; clause-count mismatch adds a `# NOTE:` line.
- **Auto-insertion inside the enclosing `defmodule`** — Elixir and
  Java helpers both append at file scope and ask the user to relocate.
  Detecting the chunk's parent container and inserting before its
  closing `end`/`}` would make the patch immediately compilable.
- ~~**Python multi-line `def` signatures**~~ — **Shipped**: `pythonHelperHeader` now carries Black-formatted multi-line signatures verbatim (see the Python row in the per-language table above).
- ~~**TypeScript-specific syntax in the JS emitter**~~ — **Shipped**: annotations/generics carried verbatim, access modifiers dropped on the method-to-free-function rewrite; interfaces/type aliases documented out of scope (see the JS/TS row above).
**Shipped from this list:** auto-insertion inside the enclosing
container — `--suggest` patches now insert Java/Elixir helpers inside
the innermost enclosing class/defmodule so they compile as emitted
(the file-scope NOTE survives only on the no-container fallback).

- **Elixir `@spec` / `@doc` propagation** — module attributes sitting
  above a def are skipped by `exHelperHeader` and not carried into the
  emitted helper. If the contract or docstring is part of the
  duplication's value, it should propagate. (Multi-clause defs
  inherit any preceding `@spec` because Elixir attaches it to the
  function name, not the individual clause — propagation needs to be
  symbol-scoped, not chunk-scoped.)
- **Elixir multi-clause grouping** — currently each `def parse(...)`
  clause is its own chunk (good for clone detection at clause
  granularity). The next-level feature would be the option to group
  adjacent clauses by symbol so `--suggest` could produce a single
  multi-clause helper. Pure ergonomics; no correctness gap.
- **Python multi-line `def` signatures** — flagged as a TODO at
  `pythonHelperHeader`. v1 fixtures don't exercise it.
- **TypeScript-specific syntax in the JS emitter** — return-type
  annotations (`fn(): T {`), interface declarations, etc. The shared
  JS emitter handles plain TS today but doesn't strip type
  annotations.

Bet **5** (clone watchlist + drift alerts) is **shipped** (2026-07-14):
codetwin now tracks clone families *over time* — snapshot with
`--update-baseline`, gate CI with `--baseline`, and get one stderr line
per drift event when a family gains a copy, loses one, or a member's
body changes while its siblings don't. The next bet to consider is
**6** (cross-repo / org-level scanning): surface "promote to library"
candidates across N repos.

Bet **6** (cross-repo / org-level scanning) shipped 2026-07-14: two or
more directory roots automatically become "repos", cross-repo clusters
are tagged and grouped per repo, and `--cross-repo-only` isolates the
shared-library candidates. The next bet to consider is **5** (clone
watchlist + drift alerts) — the last unstarted item on the
next-quarter list.

## Coverage of shipped code

After bet #6 (cross-repo scanning, 2026-07-14):

| Package | Coverage |
|---|---|
| `internal/refactor` | 99.1% |
| `internal/baseline` | 94.8% _(added 2026-07-14 by bet #5)_ |

| `internal/tokenizer` | 98.7% |
| `internal/fingerprint` | 97.6% |
| `internal/git` | 96.7% |
| `internal/pathutil` | 96.4% |
| `internal/scan` | 95.8% |
| `internal/similarity` | 94.7% |
| `internal/report` | 94.4% |
| `internal/splitter` | 94.2% |
| `internal/cluster` | 93.9% |
| `internal/config` | 93.9% |
| `internal/cache` | 89.7% |
| `internal/blocks` | 85.9% |
| `cmd/codetwin` | 28.7% (`main()` body still un-unit-tested; covered by subprocess tests, which don't count toward `-cover`) |

(`internal/bench` reports no coverage — it is a test-only package; its
`TestBench_GroundTruth` is the detection-quality gate described above.)

The biggest standing gap is `cmd/codetwin/main.go`'s top-level
orchestration: `main()`, `printJSON`, `applyConfigDefaults`,
`computeProvenance`, `usage`, etc. all sit at 0% because they're
only reachable by spawning the binary. **Closing that gap is the
charter of the integration-test layer described below** — every
future bet should add a subprocess test that runs `./codetwin
<new-flag>` against a fixture and asserts on stdout/exit code.

## Testing layers (required for every bet)

Each layer answers a different question. Skipping any one is what
lets bugs slip through.

| Layer | Question it answers | Where it lives | Example |
|---|---|---|---|
| **Unit** | Does this function compute the right value for crafted inputs? | `*_test.go` next to the function | `rejectControlFlowAsymmetryWithKeywords` table-driven cases |
| **Fixture-driven** | Does the real splitter/tokenizer/aligner pipeline produce the right result on representative source? | `testdata/<feature>/<tier>/` + a test that feeds it through the in-process pipeline | `TestSynthesize_PythonRealworld_Decorated` reads `testdata/refactor/python/realworld-decorated/{a,b}.py` through `splitter.Split → Align → Synthesize` |
| **Round-trip** | Does the emitted artefact (diff, JSON, baseline file) round-trip through the tool that consumes it? | Subprocess test invoking the consuming tool | `TestBuildPatch_GoMethodRealworld_AppliesClean` shells out to `git apply --check` and `git apply` against a tempdir repo |
| **Subprocess CLI** | Does the binary itself produce the right stdout/stderr/exit code for the documented invocation? | `cmd/codetwin/*_subprocess_test.go` | `TestSuggest_JavaSimple_ExitsZeroAndPrintsDiff` runs `./codetwin --suggest <id> testdata/refactor/java/simple` and asserts exit 0 plus `extracted_priceWithTaxA_` in the diff; `TestSuggest_JavaRejectThrow_ExitsNonZeroWithNote` asserts exit 1 + stderr note on rejection. Established by Bet #4's Java commit. |
| **Self-host** | Does the tool work on its own source tree without crashing or producing pathological output? | `cmd/codetwin/main_selfhost_test.go` — short-circuits in `-short` mode | `TestSelfHost_RunsCleanOnInternal` runs `./codetwin --threshold 0.85 --json ../../internal` and asserts exit 0 + valid JSON. `TestSelfHost_SuggestAllRunsCleanOnInternal` adds `--suggest-all` to guard the cross-feature interaction. Added by Bet #4's JavaScript commit. |

### What "true integration" requires that unit tests don't catch

- **Flag wiring:** does the CLI flag actually plumb through to the
  internal package call? Unit tests on the helper don't catch a
  typo in `flag.String(...)`.
- **stdout/stderr discipline:** does the tool print the diff to
  stdout and the rejection note to stderr? Round-trip tests that
  call `BuildPatch` directly bypass the print routing.
- **Exit codes:** does `--suggest <unknown-id>` exit 1, not 0?
  Unknown-ID error returns are unit-tested at the `emitSuggestion`
  level, but not via `os.Exit`.
- **Cross-feature interaction:** `--since main --suggest-all
  --json` combines a git-diff filter with the suggestion map. Each
  half is unit-tested; the combined behaviour is not.
- **JSON schema stability:** the `suggested_patch` field shape
  is asserted on once in `TestBuildSuggestionMap_*_PopulatesPatch`,
  but no test parses the actual `./codetwin --json` output and
  checks every documented field is present.

## Integration test plan per remaining bet

### Bet #4 follow-ups (JS/TS, Rust, Java, Elixir emitters)

Per language, before merging:

- **Fixture-driven:** add `testdata/refactor/<lang>/{simple,medium,
  advanced}/{a,b}.<ext>` and the equivalent `realworld-*` tier
  exercising whatever the language-specific surface is (e.g.
  `realworld-trait` for Rust, `realworld-static` for Java).
- **Round-trip:** mirror `TestBuildPatch_PythonSimple_AppliesClean`
  — synthesize, build patch, `git apply --check` in a tempdir.
- **Subprocess:** `./codetwin --suggest <id> testdata/refactor/
  <lang>/simple` exits 0 and prints a diff containing the helper
  signature; `--suggest-all --json testdata/refactor/<lang>/simple
  | jq '.pairs[0].suggested_patch.unified_diff'` is non-empty.
- **Cross-language regression:** confirm the new emitter doesn't
  perturb the Go/Python emitters by re-running their tier tests
  unchanged.

Elixir specifically also needs splitter coverage: function-level
chunks must come out of `splitter.Split` before any emitter work
starts. Add `internal/splitter/elixir_test.go` with the same
shape as `python_test.go`.

### Bet #5 — clone watchlist + drift alerts

- **Unit:** baseline serialization round-trip (encode/decode), drift
  detection on a synthetic `before/after` cluster pair.
- **Fixture-driven:** `testdata/baseline/before/` and `after/` —
  two snapshots of the same code where one cluster has gained a
  member, one has lost a member, one has a member whose body
  changed. Assert each drift event fires exactly once.
- **Round-trip:** `--baseline f.json` writes a file that
  `--baseline f.json` on a subsequent run can read; mismatched
  schema versions surface a clear error.
- **Subprocess:** end-to-end. `./codetwin --update-baseline f.json
  testdata/baseline/before` then `./codetwin --baseline f.json
  testdata/baseline/after` — assert the drift events appear in
  stderr and the exit code is 1 (or whatever the gating contract
  is).
- **Self-host:** baseline against codetwin's own `internal/`,
  re-run with no changes, confirm zero drift events.

### Bet #6 — cross-repo / org-level scanning

- **Fixture-driven:** `testdata/multirepo/svc-a/`, `svc-b/`,
  `svc-c/` — each a small directory tree with one shared clone
  family and unique code.
- **Subprocess:** `./codetwin svc-a svc-b svc-c --json | jq
  '.clusters[0].members'` returns members from at least two
  repos; per-repo cluster grouping in the terminal output
  visually separates the repos.
- **Performance smoke test:** running on N=10 sibling clones of
  `internal/` should complete in a sane time (target: cache hit on
  second run, no exponential blowup with repo count).

## Verification checklist (template for any future bet)

1. `make test` — unit + fixture-driven layers green.
2. `make build` — binary compiles.
3. `./codetwin <new-flag> testdata/<fixture>` — new behavior
   produces the documented output, including in `--json`.
4. Subprocess test in `cmd/codetwin/*_subprocess_test.go` asserts
   on stdout, stderr, and exit code for at least the happy path
   and one rejection/error path.
5. Self-host: `./codetwin <new-flag> ./internal` exits cleanly.
6. Update `--skill` and `--guide` embedded text so Claude knows
   about the new capability.
7. Update this roadmap's status table and coverage block.
