# codetwin roadmap — unique-niche bets

_Last updated: 2026-04-30. Source: planning conversation that shipped
commits `159a298`, `59fe97f`, `f53a739` on
`claude/explore-unique-features-4rInJ`._

## Status at a glance

| # | Bet | Status | Surface |
|---|---|---|---|
| 1 | Git provenance | **Shipped** | `--blame`, `--sort age`, `--sort age-asc` |
| 2 | PR-delta mode | **Shipped** | `--since <ref>` |
| 3 | Cross-language as the headline | **Shipped** | `--cross-lang-only`, `lang_{a,b}` JSON |
| 4 | Refactor patch emission | **Shipped (v1, Go)** | `--suggest <pair-id>`, `--suggest-all`, `id` + `suggested_patch` in JSON |
| 5 | Clone watchlist + drift alerts | Not started | (proposed: `--baseline`) |
| 6 | Cross-repo / org-level scanning | Not started | (existing CLI already accepts multiple roots; needs namespacing + per-repo cluster grouping) |
| 7 | Behavioural / runtime equivalence | Flagged longshot | — |

### Per-language emitter status (Bet #4)

| Language | Synthesizer | Notes |
|---|---|---|
| Go | **Shipped** | Starter helper + divergence comment block. |
| Python | **Shipped** | Starter helper with `#`-comment divergence block; class methods carried through as top-level helpers with `self`/`cls` as ordinary parameters. |
| Java | **Shipped** | Starter helper with `//`-comment divergence block; modifiers/generics/`throws` preserved verbatim; helper is appended at file scope after the wrapping class's closing `}` and carries a `// NOTE: appended at file scope…` placement comment (file won't compile until a human moves the helper into the appropriate class — the v1 "starter, human finishes" boundary). Control-flow keyword set extended with `throw`. |
| JavaScript / TypeScript | **Shipped** | Starter helper with `//`-comment divergence block. Recognises four definition shapes: `function name(...)` (incl. `async` / `export default`), arrow assignments `const|let|var name = (...) => {…}`, `const|let|var name = async function(...) {…}`, and ES6+ class methods. The JS splitter was lifted to method-level granularity in the same commit (matching Python and Java) so detection itself runs on individual methods rather than swallowing whole class bodies. When a method references `this.`, the helper carries a `// NOTE:` line flagging that `this` must be wired at call sites. Control-flow keyword set extended with `throw` (mirrors Java). |
| Rust | **Shipped** | Starter helper with `//`-comment divergence block. Recognises `fn name(...)` headers with any combination of `pub` / `pub(crate)` / `async` / `unsafe` / `const` / `extern` modifiers; preserves generics, lifetimes, return types, and `where` clauses verbatim. Impl methods come through the splitter as method-level chunks (the splitter was already method-granular for Rust). When the body references the standalone `self` keyword, the helper carries a `// NOTE: extracted as a free function with &self carried as an explicit parameter…` block flagging that the receiver must be bound at call sites. Control-flow keyword set extended with `panic` so `panic!(…)` macro asymmetry triggers rejection (mirrors Java's `throw`). |
| Elixir | Fixture in place | Returns `unsupported language: elixir`; splitter still falls back to whole-file for Elixir. |

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
(Winnowing/Jaccard) + semantic (TF-IDF cosine) scoring, DBSCAN clusters,
inverted-index pruning, per-file cache, Claude-skill packaged. CLI-only,
one-shot, no external deps.

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
**Status: Not started.**

**Why nobody has it:** Clone families *evolve* — once detected, members
gradually drift apart, fixing a bug in one but not the others. No tool
tracks this. Codetwin's cache infrastructure is one annotation away
from supporting a watchlist.

**Fit:** Good. Persist clusters detected on a baseline run; on each
subsequent run, compare and emit `drift: <cluster> member <N> diverged`
events.

**Proposed surface:**
- `codetwin --baseline .codetwin-baseline.json ./src`
- `codetwin --update-baseline ./src`

**Critical files (proposed):** new `internal/baseline/baseline.go`,
hooks in `cmd/codetwin/main.go` after DBSCAN.

### 6. Cross-repo / org-level scanning
**Status: Not started.** The CLI already accepts multiple roots and the
matrix operates on a flat snippet list — what's missing is repo-aware
namespacing of snippet IDs and per-repo cluster grouping.

**Why nobody has it:** Existing tools are repo-scoped. Platform teams
have no good way to find logic that should be a shared library across N
service repos. Codetwin's cache makes incremental org-scale scanning
viable.

**Proposed surface:**
- `codetwin --repos repos.txt` or `codetwin ../svc-a ../svc-b ../svc-c`.
- Cluster output groups by repo to make "promote to library"
  candidates obvious.

### 7. Behavioural / runtime equivalence (longshot, highest novelty)
**Status: Flagged longshot.** Not on the next-quarter list.

**Why nobody has it:** Confirms two clones are *observably* equivalent
by fuzzing inputs and comparing outputs. Closest prior art is
differential testing (research-y). Would let codetwin distinguish
"these are textually similar but behave differently" from "extract
this safely".

**Fit:** Poor for a no-deps single binary. Needs language-specific
sandboxes. Worth flagging only as a future research direction.

## Recommendation (original) and what shipped

The original recommendation was **1 + 2 + 3** as the headline narrative:
*"codetwin finds duplicate logic — across languages, across the git
history, and only complains about the duplication you just introduced."*
**That triad is now shipped.**

Bet **4** (refactor patches) shipped Go in v1, then Python, then Java,
then JavaScript/TypeScript, and now Rust — codetwin goes from reporter
to *starter generator*: it emits a unified diff that adds a helper
extracted from a clone pair, with a comment block listing every
divergence. Elixir is the only remaining emitter on Bet #4, and it
additionally needs a function-level splitter (currently whole-file
fallback). The Java emitter established the
`cmd/codetwin/refactor_subprocess_test.go` convention required by the
"Testing layers" section below — every future emitter should add
subprocess cases there. The JS/TS emitter added the first
`cmd/codetwin/main_selfhost_test.go` cases, partially closing the
standing 25.2% coverage gap on cmd/codetwin.

The next bet to consider is **5** (clone watchlist + drift alerts) or
**6** (cross-repo / org-level scanning), depending on whether the
priority is lifecycle (track clone families over time) or scale
(surface "promote to library" candidates across N repos).

## Coverage of shipped code

After the Python emitter test pass (commits `d032c0d`, `c6ff2b6`,
`d42f4d5`):

| Package | Coverage |
|---|---|
| `internal/refactor` | **99.7%** (residual is one provably-unreachable defensive break) |
| `internal/fingerprint` | 97.3% |
| `internal/pathutil` | 96.4% |
| `internal/similarity` | 95.6% |
| `internal/scan` | 94.3% |
| `internal/tokenizer` | 94.4% |
| `internal/config` | 93.9% |
| `internal/git` | 93.8% |
| `internal/cluster` | 93.2% |
| `internal/report` | 91.7% |
| `internal/splitter` | 90.3% |
| `internal/cache` | 79.4% |
| `cmd/codetwin` | 25.2% (`main()` body still un-unit-tested; new helpers at 75–100%) |

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
