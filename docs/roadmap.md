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
| 4 | Refactor patch emission | Not started | (proposed: `--suggest`) |
| 5 | Clone watchlist + drift alerts | Not started | (proposed: `--baseline`) |
| 6 | Cross-repo / org-level scanning | Not started | (existing CLI already accepts multiple roots; needs namespacing + per-repo cluster grouping) |
| 7 | Behavioural / runtime equivalence | Flagged longshot | — |

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
**Status: Not started.** Natural next step now that the detection
trio is in place.

**Why nobody has it:** Every clone detector stops at "here are the
matches". Codetwin already aligns matched fingerprints by token
position (`internal/fingerprint`'s `PositionalSet`), so it can compute
which tokens differ between two clones and propose an extracted helper
parameterized on those differences — no LLM required.

**Fit:** Medium. Needs new code (template-based emission per language)
but builds on data the fingerprint layer already has. Pairs naturally
with the Claude skill: codetwin emits the candidate, Claude applies it.

**Proposed surface:**
- `codetwin --suggest <pair-id>` prints a unified diff that introduces
  a shared helper and rewrites both call sites.
- JSON gets a `suggested_patch` field (opt-in via `--suggest`).

**Critical files (proposed):** new `internal/refactor/extract.go`,
hooks in `cmd/codetwin/main.go`.

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

Bet **4** (refactor patches) remains the natural follow-up: it converts
codetwin from a reporter into an actor and makes the Claude skill
substantially more useful.

## Coverage of shipped code

After the test pass in commit `f53a739`:

| Package | Coverage |
|---|---|
| `internal/git` (new) | 93.8% |
| `internal/similarity` | 95.6% |
| `internal/report` | 91.4% |
| `cmd/codetwin` | 19.9% (`main()` body still un-unit-tested; new helpers at 88–100%) |

Uncovered surface worth knowing about: `cmd/codetwin/main.go`'s
`computeProvenance` is a thin orchestrator over `git.Blame` and would
need a fixture repo to exercise meaningfully.

## Verification recipe (template for future bets)

For whichever direction is picked next, the end-to-end check is:

- `make test` for unit coverage of the new package.
- `./codetwin <new-flag> testdata/` and confirm the new fields appear
  in `--json` output.
- Run codetwin against its own repo with the new flag and sanity-check
  the output against `git log` / `git blame` / `git diff` as relevant.
- Update `--skill` and `--guide` embedded text so Claude knows about
  the new capability.
- Update this roadmap's status table.
