# codetwin

Multi-language code similarity detector — finds duplicate and refactorable code
across `.go`, `.js`, `.ts`, `.jsx`, `.tsx`, `.py`, `.java`, `.rs`, and
`.ex`/`.exs` files. Function-level chunking, semantic + structural scoring,
DBSCAN clustering, no external dependencies.

What sets codetwin apart from other clone detectors:

- **Cross-language clones** — finds duplicate logic across a Go service and a
  TypeScript dashboard in the same monorepo (`--cross-lang-only`).
- **PR-delta CI gating** — fails only on duplication a PR introduces, not the
  whole tech-debt backlog (`--since main`). Lets teams ratchet down debt
  without rewriting history first.
- **Git provenance** — annotate every match with when, by whom, and which
  endpoint is the original (`--blame`). Sort by introduction date with
  `--sort age` for "newest clones first".
- **Refactor suggestions** — once a finding is identified, `--suggest <id>`
  emits a unified diff that adds a starter helper extracted from the matching
  pair *or partial-clone block*, with a comment block listing every
  divergence. Block suggestions wrap the shared statement run in a fresh
  helper (Go and Python), inserted right after the enclosing function.
  Unsupported languages report a structured `note` so a follow-up emitter
  has a clear contract.
- **Sub-function partial clones** — a copied block hiding inside two
  otherwise-unrelated functions is invisible to whole-function scoring
  (dilution grows quadratically with host size); the block channel finds it
  anyway and reports it with exact line ranges in a dedicated
  `PARTIAL CLONES` section. See
  [Partial clones (block level)](#partial-clones-block-level).
- **Cross-repo / org-level scanning** — point codetwin at N service repos at
  once (`codetwin ../svc-a ../svc-b ../svc-c`) and clusters that span repos
  are tagged `cross-repo` with members grouped per repo: the "promote to a
  shared library" candidates platform teams have no other way to find.
  `--cross-repo-only` keeps just those. See
  [Cross-repo scanning](#cross-repo-scanning).
- **Clone watchlist + drift alerts** — snapshot today's clone families with
  `--update-baseline`, then gate CI with `--baseline`: one stderr line per
  drift event (a new copy pasted in, a copy refactored away, a member's body
  edited while its siblings weren't) and exit 1 on any drift. See
  [Clone watchlist](#clone-watchlist-drift-alerts).
- **Test code segregation** — test↔test findings (API-forced scaffolding
  shape, rarely actionable) are suppressed by default and replaced with a
  one-line summary, while test↔production copy-paste still renders.
  `--include-tests` restores the full listing. See
  [Test code segregation](#test-code-segregation).
- **Dead code detection** — `--dead-code` reports definitions nothing in
  the scan references, tiered by confidence: `dead` (private, zero refs),
  `test-only` (production code only tests keep alive), `unused-in-scan`
  (exported; external consumers possible). Name-based and conservative —
  string-literal and import mentions count as alive, entry points and
  implicitly-dispatched methods are never reported. See
  [Dead code detection](#dead-code-detection---dead-code).

The git-aware features (`--since`, `--blame`, `--sort age`) require git on
`PATH` and a git repository in the working directory; without them codetwin
runs the same in any directory.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ccsrvs/codetwin/main/install.sh | bash
```

Installs the latest release for your platform into `~/.local/bin`, or updates an
existing `codetwin` in place. Re-run it to upgrade. Two knobs:

- `CODETWIN_VERSION=v0.3.1` — pin or roll back to a specific tag
- `CODETWIN_BIN_DIR=/usr/local/bin` — choose the install directory

Prebuilt binaries cover linux and macOS on amd64/arm64; Windows amd64 is
attached to each [release](https://github.com/ccsrvs/codetwin/releases) for
manual download.

Or with the Go toolchain:

```bash
go install github.com/ccsrvs/codetwin/cmd/codetwin@latest
```

Or build from source:

```bash
git clone https://github.com/ccsrvs/codetwin
cd codetwin
make build          # produces ./codetwin binary
make test           # run all unit + integration tests
```

### Install as a Claude Code skill

`codetwin-SKILL.md` at the repo root is a Claude Code skill manifest that
tells Claude when and how to invoke the CLI (find duplicate code, detect
clones, scan for refactor targets). To make it discoverable in your Claude
sessions, drop it into your user-level skills folder as `SKILL.md`:

```bash
mkdir -p ~/.claude/skills/codetwin
cp codetwin-SKILL.md ~/.claude/skills/codetwin/SKILL.md
```

The skill assumes `codetwin` is on `PATH`. The bundled
`./build_and_cp_cli.sh` builds the binary and copies it to `~/.local/bin`,
which is one easy way to satisfy that. Once both are in place, Claude can
locate the binary via `which codetwin` and run `codetwin --skill` /
`codetwin --guide` for the full guides embedded in the binary.

## Usage

```bash
# Scan a directory recursively
codetwin ./src

# Compare specific files
codetwin ./utils/a.go ./utils/b.go

# Multiple paths — nested ones get deduped automatically
codetwin ./src ./pkg

# Only report pairs with >= 60% similarity
codetwin --threshold 0.6 ./pkg

# Plain text for CI pipelines / file redirection
codetwin --plain ./src > report.txt

# JSON output (pipe-friendly)
codetwin --json ./src | jq '.pairs[] | select(.score > 0.8)'

# Show line-numbered code excerpts under each finding
codetwin --preview ./src

# Top 5 biggest refactor opportunities
codetwin --sort size --limit 5 ./src

# Show everything including weak matches
codetwin --verbose ./src

# Cross-language only — duplicate logic across Go service + TS dashboard
codetwin --cross-lang-only --threshold 0.50 ./

# Cross-repo scan — each directory root is a repo; clusters spanning repos
# are tagged cross-repo ("promote to a shared library" candidates)
codetwin ../svc-a ../svc-b ../svc-c

# Only findings whose endpoints live in different repos
codetwin --cross-repo-only ../svc-a ../svc-b ../svc-c

# CI gate: fail on any new strong clone introduced since main
codetwin --since main --threshold 0.85 --json ./src

# Clone watchlist: snapshot today's clusters, then alert when they drift
codetwin --update-baseline .codetwin-baseline.json ./src
codetwin --baseline .codetwin-baseline.json ./src   # exits 1 on drift

# Annotate findings with git provenance, newest clones first
codetwin --blame --sort age --limit 10 ./src

# Suggest a starter helper for one same-language pair (look up <id> in --json output)
codetwin --suggest <pair-id> ./src

# Dead code: definitions nothing in the scan references
codetwin --dead-code ./src
codetwin --dead-code --json ./src | jq '.dead_symbols[] | select(.verdict == "dead")'
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `--threshold` | `0.50` | Minimum score to report (0.0–1.0) |
| `--plain` | false | Disable ANSI colors (CI-safe) |
| `--json` | false | JSON output |
| `--verbose` | false | Show all pairs including weak |
| `--min-lines` | `3` | Skip chunks shorter than N non-blank lines |
| `--eps` | `0.35` | DBSCAN epsilon (cluster density threshold). The default links pairs scoring ≥ 0.65 — the "strong clone" band |
| `--min-pts` | `2` | DBSCAN minimum cluster size |
| `--preview` | false | Show line-numbered code excerpts under each finding |
| `--preview-lines` | `10` | Max lines per preview; `0` = show whole snippet |
| `--sort` | `score` | Result ordering: `score`, `score-asc`, `size`, `size-asc`, `name`, `age`, `age-asc` (age modes require `--blame`) |
| `--limit` | `0` | Cap pairs and clusters at N items each (0 = no limit) |
| `--min-confidence-lines` | `10` | Dampen pair scores when `min(LinesA, LinesB) < N` (0 = off). On by default. See [Scoring](#scoring). |
| `--min-block-lines` | `8` | Report sub-function partial clones spanning at least N matched lines on both sides (0 = off). See [Partial clones](#partial-clones-block-level). |
| `--granularity` | `function` | Chunking unit: `function` (per-definition chunks) or `file` (each source file is one whole-file snippet). See [Granularity](#granularity). |
| `--dead-code` | false | Report definitions nothing in the scan references, tiered by confidence. Requires `--granularity function`. See [Dead code detection](#dead-code-detection---dead-code). |
| `--no-progress` | false | Suppress the live progress indicator on stderr |
| `--no-cache` | false | Skip reading and writing `.codetwin-cache.bin` |
| `--rebuild-cache` | false | Ignore any existing cache and rebuild from scratch |
| `--debug` | false | Print phase checkpoints with elapsed time to stderr |
| `--cross-lang-only` | false | Report only pairs whose two snippets are in different languages |
| `--cross-repo-only` | false | Report only findings whose endpoints are in different repos; requires ≥ 2 directory roots. See [Cross-repo scanning](#cross-repo-scanning). |
| `--include-tests` | false | Include test↔test pairs and test-only clusters; by default they are suppressed and summarized in one line. See [Test code segregation](#test-code-segregation). |
| `--flat` | false | List every pair individually; by default intra-cluster pairs collapse into their cluster and cross-cluster pairs aggregate into relation lines |
| `--since` | `""` | PR-delta mode: keep only findings overlapping lines changed since `<ref>` (requires git) |
| `--update-baseline` | `""` | Write a clone-watchlist snapshot of the visible clusters to `<file>` after the scan. See [Clone watchlist](#clone-watchlist-drift-alerts). |
| `--baseline` | `""` | Compare this scan against the snapshot in `<file>`; drift events print to stderr and any drift exits 1 (CI gate). Mutually exclusive with `--update-baseline`. |
| `--blame` | false | Annotate findings with git provenance (introduced, by whom, last touched) (requires git) |
| `--suggest` | `""` | Print a unified diff that adds a starter helper for the pair or partial-clone block with the given 8-char ID. Pairs: all six languages; blocks: Go and Python. |
| `--suggest-all` | false | With `--json`: populate `suggested_patch` on every visible pair and partial clone. |
| `--skill` | false | Print the full skill guide (embedded in the binary) and exit |
| `--guide` | false | Print the report interpretation guide and exit |
| `--version` | false | Print the codetwin version and exit |

## Scoring

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

## Partial clones (block level)

Whole-function scoring has a structural blind spot: a verbatim 15-line
block pasted into two ~45-line functions with unrelated surrounding
code scores ~0.37 combined — Jaccard is union-normalized, so shared
content is diluted quadratically as the host functions grow. The block
channel closes that hole. For every same-language pair that shares
fingerprints but lands *below* the report threshold (the "gray band"),
codetwin walks the shared fingerprint positions, extends them to
maximal exactly-matching token runs, chains runs across small gaps
(so one edited line inside a copied block doesn't split the finding),
and verifies each candidate with exact token comparison.

Verified blocks render in their own section with real line ranges and
the enclosing function of each side:

```
 PARTIAL CLONES

  [PARTIAL CLONE   ]  92% contained · 15 lines
    orders.go:120-134 ⊂ ProcessOrders
    invoices.go:88-102 ⊂ SummarizeInvoices
```

"92% contained" means 92% of the smaller side's block tokens are
exactly matched (after normalization, so systematic renames still
count) on the other side. Partial clones have no combined score and
`--threshold` never filters them — containment plus the line floor is
their quality bar. `--limit` applies, and in JSON they appear as a
top-level `partial_clones` array.

Two floors keep boilerplate out: containment must reach 0.85 (err-check
chains and logging runs interleaved with divergent code fall below),
and at least `--min-block-lines` (default 8) source lines must carry
matched tokens on *both* sides — lines merely spanned (e.g. by a
multi-line string literal) don't count. Raise the floor to focus on
bigger extractions, or pass `--min-block-lines 0` to disable the
channel entirely. Test↔test partial clones follow the same
[test code segregation](#test-code-segregation) as pairs.

With `--preview` on, each side renders a line-numbered excerpt of its
exact block range (capped by `--preview-lines`); in JSON the
`previews` map gains entries keyed by each side's `file:start-end`
range name, so block previews never collide with the whole-chunk
preview of the same snippet.

Partial clones are also `--suggest` targets: pass the finding's 8-char
`id` from the JSON output and codetwin emits a unified diff that wraps
side A's block in a fresh helper (`extractedBlock_<id>` in Go,
`extracted_block_<id>` in Python), inserted right after the enclosing
function. The helper is a literal copy of the block — parameters are
left as a `TODO(codetwin)` comment listing the free identifiers the
block uses (a lexical heuristic; the human finishes the extraction).
Block suggestions ship for Go and Python; other languages print a
`note:` and exit 1. `--suggest-all --json` fills `suggested_patch` on
every visible partial clone under the same per-language scope.

## Granularity

By default codetwin compares per-definition chunks (functions, methods),
so a duplicated helper inside a big module scores on the helper, not the
module. `--granularity file` inverts that: the splitter is skipped and
each source file becomes one whole-file snippet, so the report answers
"which *files* are near-duplicates of each other" instead of "which
functions are".

Use file mode when:

- **Module-level consolidation** — two files carry the same functions
  (perhaps reordered, lightly edited) plus the same surrounding
  declarations. Function mode shows that as several mid-band pairs;
  file mode shows one strong whole-file pair, which is the actual
  refactoring unit ("these two files should be one module").
- **Unsupported languages** — files in languages without a splitter
  already fall back to whole-file chunks; file mode puts *every*
  language on that footing so mixed-language scans compare like with
  like.

Everything downstream is granularity-agnostic: scoring, clustering,
labels, partial-clone detection, test segregation, `--since`/`--blame`
all work unchanged on file-sized chunks. Expect fewer, bigger findings —
pair counts shrink because there are fewer chunks. The per-file cache
keys entries by granularity, so both modes stay cached side by side and
switching back and forth never rebuilds. Note `--min-confidence-lines`
rarely binds in file mode (whole files usually exceed 10 lines) — that
is expected, not a gap.

```bash
codetwin --granularity file ./src           # which files are near-duplicates?
codetwin --granularity file --preview ./lib # with whole-file previews
```

## Class-level matching

For the class-based languages the splitter emits **class-span chunks in
addition to** the method chunks inside them:

- **Python** — `class Foo:` blocks (indent-terminated, exactly like
  `def` chunks; decorated classes include their decorator block).
- **Java** — class / interface / enum / record bodies, including nested
  types (each nested type gets its own span).
- **JS / TS** — `class Foo { ... }` declarations. Class *expressions*
  (`const A = class { ... }`) are not span-chunked (their methods still
  are).
- **Elixir** — `defmodule Foo do ... end` blocks (block form only;
  nested modules each get their own span, like Java's nested types).
  A module wrapping fewer than two defs is not span-chunked — its span
  would just duplicate the single def plus `defmodule`/`end`
  boilerplate, and Elixir's pervasive one-callback modules would
  otherwise pair up as module↔module near-noise.
- **Rust** — `impl Foo { ... }` and `impl Trait for Foo { ... }` blocks.
  The symbol is always the TYPE name (`Foo`), so an inherent impl and
  the trait impls of one type share a symbol; each block is its own
  chunk. An impl declared inside a function body is not chunked (same
  rule as a JS class inside a function).
- **Go** — struct+methodset **groups**. Go methods live *outside* the
  type block, so there is no contiguous span to cut: instead the
  splitter builds one **synthetic** chunk per struct type declared in a
  file with **two or more in-file methods** — the type decl plus every
  `func (r Foo)` / `func (r *Foo)` method (pointer and value receivers
  unify), joined in file order. Source interleaved between them is
  excluded from the chunk's text, but the chunk's *line range* is the
  covering range (decl start to last method end), which
  over-approximates: `--since` overlap and blame treat the whole
  stretch as the chunk (a safe over-match), and previews render the
  joined text. Interfaces never group (no methodset can exist), and a
  methods-only file — the type declared in a sibling file — gets no
  group (grouping is decl-anchored).

This catches the case method-level granularity underreports: a whole
class copied and renamed with its methods slightly reordered surfaces
as one strong class↔class finding instead of a scatter of method pairs.

**Noise control.** Class chunks are whole-container spans, which is
exactly the "washed out by unrelated code" dilution the splitter exists
to avoid — so class chunks are only ever scored against **other class
chunks**. A big class weakly resembling a small function across files
is container-vs-part noise, not a clone; those mixed-kind comparisons
are skipped entirely (matrix cell stays 0, nothing materializes, no
cluster or block-candidate edges). Same-file class-vs-own-method
overlap was already suppressed by the nested-chunk filter — for Go
groups this extends to anything inside the covering range, including
unrelated functions interleaved between the methods (same-file
container findings are rarely the value; cross-file group↔group is).
Class↔class pairs participate in clusters like any other same-kind
pair, but not in the partial-clone block channel: every method inside
a container is already its own function chunk there, so container-level
block detection would only re-find the same text — and a Go group's
joined non-contiguous code would misreport block line ranges.

## Cross-repo scanning

Existing clone detectors are repo-scoped, so platform teams have no good
way to find logic that should be a shared library across N service
repos. Codetwin's matrix already operates on a flat snippet list, so
cross-repo scanning is just an invocation shape: **pass two or more
directory roots and each root is treated as a "repo"**. No flag — the
mode is automatic.

```bash
# The org-level recipe: check out the services side by side, then
git clone git@github.com:org/svc-a && git clone git@github.com:org/svc-b && git clone git@github.com:org/svc-c
codetwin svc-a svc-b svc-c

# Only the findings that span repos — the shared-library candidates
codetwin --cross-repo-only svc-a svc-b svc-c

# Machine-readable: which clusters cross repo boundaries?
codetwin --json svc-a svc-b svc-c | jq '.clusters[] | select(.cross_repo)'
```

What changes in cross-repo mode (and *only* then — single-root and
file-argument invocations are byte-identical to before):

- **Repo labels.** Each root is labelled by the base name of its
  absolute path (`../teams/payments/api` → `api`). Two roots with the
  same base name disambiguate deterministically by input order: `api`,
  `api~2`, `api~3` …
- **Namespaced snippet names.** Names become
  `repo:path:start-end Symbol`, with the file path shown *relative to
  its root*: `svc-a:src/handler.go:10-30 Parse`. Files passed directly
  as arguments alongside directory roots keep their plain names and no
  repo label.
- **Per-repo cluster grouping.** A cluster whose members span ≥ 2 repos
  gets a `cross-repo` tag in its header, and its members render grouped
  under one `repo — N snippets` line per repo:

  ```
    Cluster 1 — 2 snippets · avg similarity 100% · cohesion 100% · cross-repo
      svc-a — 1 snippet
        · svc-a:pricing.go:7-26 ApplyDiscount
      svc-b — 1 snippet
        · svc-b:billing.go:7-26 ApplyDiscount
  ```

  Clusters confined to one repo render flat, exactly as before (the
  name prefix already tells you the repo).
- **JSON fields.** Pairs and `partial_clones` entries gain
  `repo_a`/`repo_b`; clusters gain `member_repos` (parallel to
  `members`) and `cross_repo`. All are `omitempty`, so the single-root
  schema is untouched.
- **`--cross-repo-only`.** Keeps only findings whose endpoints live in
  different repos: pairs and partial clones with two distinct repo
  labels, clusters spanning ≥ 2 repos. Composes with
  `--cross-lang-only` (both filters apply). Errors out when fewer than
  two directory roots were given.

Interactions worth knowing:

- **`ignore_pairs` match the un-prefixed name.** Write endpoints
  without the repo label (`"src/handler.go Parse"`, matched against the
  root-relative path) — one config works for single-root and multi-root
  invocations alike.
- **The cache just works.** Cache keys are absolute-path based, so
  repeat org scans are incremental; only changed files pay the
  tokenize/fingerprint cost again.
- **`--since` / `--blame` require one git repository.** Both resolve a
  single repo, so they error out (fail-fast, with a clear message) when
  the directory roots live in *different* git repositories. Roots
  inside one repository — `codetwin --since main ./internal ./cmd` —
  keep working. Known limitation; per-repo provenance is future work.
- **Behavior change vs. pre-cross-repo versions:** any invocation with
  two or more directory roots now namespaces names (`codetwin ./internal
  ./cmd` reports `internal:…` / `cmd:…`). Scripts that scan multiple
  roots and parse names must account for the prefix; single-root
  invocations are unaffected.

## Test code segregation

Test scaffolding dominates clone reports: test functions are short,
forced into a common shape by the API under test, and differ mostly in
identifiers and string literals — exactly the token classes the
normalizer erases. On a self-scan of this repository, 98% of all
reported pairs had at least one `_test.go` endpoint. They are genuine
token-clones, but they are rarely actionable findings.

By default codetwin therefore classifies each file by its language's
test-file convention and:

- **keeps test↔production pairs** (copy-paste between prod and tests is
  a real finding) and **mixed clusters** (some test, some prod members);
- **suppresses test↔test pairs** and **clusters whose members are all
  test snippets**, replacing them with one summary line each:

```
  1,874 test↔test pairs suppressed (--include-tests to show)
  64 test-only clusters suppressed (--include-tests to show)
  12 test↔test partial clones suppressed (--include-tests to show)
```

In `--json` mode the suppressed findings are omitted from `pairs` /
`clusters` / `partial_clones` and a `"suppressed": {"test_test_pairs": N,
"test_only_clusters": M, "test_test_blocks": K}` object is added (zero
counts are omitted). `--include-tests` (or
`"include_tests": true` under `defaults` in `.codetwin.json`) restores
the previous behaviour exactly — full pair list, no `suppressed`
object — so existing CI contracts stay stable.

Classification is by path only (no file contents are read):

| Language | Test convention |
|---|---|
| Go | `*_test.go` |
| Python | `test_*.py`, `*_test.py`, or a `tests/` / `test/` directory component |
| JS / TS | `*.spec.*`, `*.test.*`, or a `__tests__/` directory component |
| Java | a `src/test/` path component sequence |
| Rust | a `tests/` directory component |
| Elixir | `*_test.exs`, or a `test/` directory component |

This is presentation-layer only: scores, the similarity matrix, and
clustering are unchanged, and suppression happens after threshold
filtering (the summary counts only findings that would have rendered)
and before `--limit` (the limit applies to what remains). Unlike adding
`**/*_test.go` to `ignore_paths`, segregation keeps test files in the
scan, so cross-boundary test↔production findings still surface.

## Dead code detection (--dead-code)

`--dead-code` adds a second, similarity-independent report channel:
every named definition (function, method, class) that nothing in the
scanned corpus references. It reuses the chunks the similarity pipeline
already extracts — a definition is *alive* if its name occurs anywhere
outside its own body (and outside the bodies of same-named
definitions), *dead* otherwise.

```bash
codetwin --dead-code ./src
codetwin --dead-code --json ./src | jq '.dead_symbols[] | select(.verdict == "dead")'
```

Findings are tiered by confidence:

| Verdict | Meaning |
|---|---|
| `dead` | Private/unexported, zero references — the strongest deletion candidates |
| `test-only` | Production code referenced only from test files — dead weight in the shipped artifact |
| `unused-in-scan` | Exported/public, zero references in the scan — advisory, since external consumers are invisible |

The analysis is name-based reachability, deliberately biased toward
false-alive rather than false-dead: a name mentioned in a string
literal (dynamic dispatch, reflection, registries) or in an import
(re-exports) keeps its symbol alive; comment mentions do not. Entry
points and implicitly-dispatched methods are never reported — `main`,
`init`, `TestXxx`, Go stdlib interface methods (`String`, `Error`,
`MarshalJSON`, `ServeHTTP`, ...), Python dunders, React lifecycle
methods, Java's `equals`/`hashCode`, Rust trait impls and operator
overloads, and Elixir OTP/Phoenix callbacks including `start_link`.
Per-language visibility conventions drive the `exported` split: Go
capitalization, Python leading underscore, Rust `pub`, Java `public`,
JS `export`, Elixir `def` vs `defp`.

What it cannot see — verify before deleting: consumers outside the
scanned roots (the whole `unused-in-scan` tier exists because of them),
build-tag variants, callers in generated code outside the scan, and
frameworks that discover handlers by annotation alone. Definitions
shorter than `--min-lines` (default 3) are not analyzed. Requires
`--granularity function`; `--threshold` and `--since` do not filter the
section, `--limit` caps it. In JSON, findings land in a top-level
`dead_symbols` array (omitted when the flag is off or nothing is dead).

## Clone watchlist (drift alerts)

Clone families evolve: members drift apart, a bug gets fixed in one
copy but not the others, a new copy gets pasted in. The watchlist
persists a baseline of the clusters a scan found and alerts on the
*changes* between runs — no other clone detector tracks families over
time.

```bash
# 1. Snapshot the current clusters (commit the file alongside your code)
codetwin --update-baseline .codetwin-baseline.json ./src
git add .codetwin-baseline.json

# 2. In CI: compare each build against the snapshot
codetwin --baseline .codetwin-baseline.json ./src
# exit 0  → no drift (stderr is silent)
# exit 1  → drift; one line per event on stderr:
#   drift: member-added cluster 0: src/billing/tax.go computeVAT
#   drift: member-changed cluster 2: src/api/parse.go ParseRecord

# 3. When the drift is intentional, refresh the snapshot
codetwin --update-baseline .codetwin-baseline.json ./src
```

Five event kinds:

| Event | Meaning |
|---|---|
| `member-added` | a cluster gained a member — a new copy was pasted in |
| `member-removed` | a cluster lost a member — deleted or refactored away |
| `member-changed` | a member's body changed but it still clusters — the classic "bug fixed in one copy" alarm: check its siblings |
| `cluster-appeared` | a new clone family with no baseline counterpart |
| `cluster-dissolved` | a baseline family no longer detected (numbered by its position in the *baseline*; the detail lists its members) |

The normal report still prints to stdout in both modes; drift events go
to stderr. With `--json`, a `drift` array (kind / cluster / detail) is
added to the document — omitted when empty, so the JSON schema is
unchanged for consumers that don't use baselines.

Details that make baselines durable:

- **Member identity survives edits.** Snapshot members are stored as
  line-range-stripped names (`path Symbol`, the same normalization as
  [`ignore_pairs`](#pair-ignores-ignore_pairs)) with paths relative to
  the scan roots — so editing code above a function, or running the
  scan from a different directory, never reads as drift. A body change
  is detected via a hash of the member's *normalized* token stream:
  formatting, comments, and renames don't trip it; structural edits do.
- **Clusters are matched by membership, not position.** A baseline
  cluster matches the current cluster it shares the most members with
  (highest Jaccard wins, deterministic tie-break), as long as they share
  at least half the smaller cluster's members; below that floor the
  pair reads as `cluster-dissolved` + `cluster-appeared`.
- **Snapshots are byte-deterministic.** Two `--update-baseline` runs
  over the same tree write byte-identical files — the schema
  deliberately has no timestamp (your VCS history dates it), so the
  file diffs cleanly in review.
- **Comparability is enforced.** The snapshot records the scan
  parameters that shape clusters (`threshold`, `eps`, `min-pts`,
  `granularity`, `include-tests`). Comparing with different flags is
  user error, not drift: codetwin lists the mismatched parameters and
  exits 1 before scanning. A `schema_version` field likewise rejects
  snapshots from an incompatible codetwin with a clear "regenerate"
  error. Baselines snapshot the *visible* (post-suppression) clusters,
  so [test segregation](#test-code-segregation) composes naturally —
  you baseline what you see, and `--include-tests` baselines require
  `--include-tests` comparisons.

## Sorting

`--sort` applies the same mode to both pairs and clusters, with each section
using its natural interpretation:

| Mode         | Pairs                          | Clusters                          |
|--------------|--------------------------------|-----------------------------------|
| `score`      | highest similarity first       | highest avg internal score first  |
| `score-asc`  | borderline cases first         | loosest clusters first            |
| `size`       | biggest snippets first         | most members first                |
| `size-asc`   | smallest snippets first        | smallest clusters first           |
| `name`       | alphabetical by file path      | alphabetical by first member      |
| `age`        | newest pair first (when introduced) | (clusters fall back to score) |
| `age-asc`    | oldest pair first              | (clusters fall back to score)     |

`--limit N` caps each section at N items independently, applied **after**
sort and threshold filtering — so `--limit 5` always yields up to 5 visible
items per section.

## Configuration

Drop a `.codetwin.json` file in the directory you run codetwin from to set
defaults, ignore files, strip lines before tokenization, or silence
individual false-positive pairs. CLI flags always win over config defaults.

```json
{
  "defaults": {
    "threshold": 0.5,
    "preview": true,
    "preview_lines": 15,
    "sort": "size",
    "limit": 20,
    "min_confidence_lines": 20,
    "min_block_lines": 8,
    "include_tests": false,
    "granularity": "function"
  },
  "ignore_paths": [
    "vendor/**",
    "**/*_test.go",
    "migrations/"
  ],
  "ignore_patterns": [
    "^\\s*log\\.(info|debug|warn|error)\\(",
    "^\\s*println!\\("
  ],
  "ignore_pairs": [
    {"a": "internal/foo/util.go",         "b": "internal/bar/util.go"},
    {"a": "auth/handler.go parseRequest", "b": "api/middleware.go parseRequest"}
  ]
}
```

### Path patterns (`ignore_paths`)

| Pattern              | Matches                                                  |
|----------------------|----------------------------------------------------------|
| `vendor`             | any path component named exactly `vendor`                |
| `vendor/lib`         | the multi-component path anywhere in the tree            |
| `vendor/`            | only when `vendor` is a directory (file `vendor` won't)  |
| `*_test.go`          | any file whose basename matches the glob, anywhere       |
| `vendor/**`          | anything under any `vendor` directory                    |
| `/build`             | leading `/` anchors the pattern to the scan root only    |

Globs use `*` (within a path component), `**` (across components), and
`?` (single character). Plain literals match path components, not loose
substrings — so `lib` matches `src/lib/x` but not `library`.

### Line patterns (`ignore_patterns`)

Lines matching any pattern are stripped before tokenization, like comments.
Useful for filtering out boilerplate that would otherwise inflate scores
(logging calls, debug prints, license headers). Patterns are Go regular
expressions with `(?m)` multi-line mode automatically applied so `^` and
`$` anchor on each line.

### Pair ignores (`ignore_pairs`)

Use this when a specific match is a confirmed false positive but you still
want both files scanned against everything else. Each entry names two
endpoints; a pair is suppressed when its two snippets match the endpoints
in either order. Suppression also prevents DBSCAN from grouping the two
snippets in a cluster.

| Endpoint               | Matches                                                       |
|------------------------|---------------------------------------------------------------|
| `path/to/file.go`      | any chunk in that file (path uses the same globs as `ignore_paths`) |
| `path/to/file.go Func` | only chunks where the splitter detected symbol `Func`         |
| `**/*_generated.go`    | any chunk in any generated file (glob on the path side)       |

**Do not include line ranges** (`:15-30`). Codetwin strips the line range
from snippet names before matching so your entries survive routine edits
that shift line numbers.

## Performance

codetwin is designed to handle large repositories. A few mechanisms in
play:

**Parallel matrix.** The all-pairs similarity computation shards rows
across `runtime.NumCPU()` goroutines. On an 8-core machine that's
roughly an 8x speedup on the dominant cost.

**Inverted-index pair pruning.** Before computing scores, codetwin
builds a `fingerprint-hash → snippet-indices` map. Pairs that share
zero fingerprints get structural=0 without paying for a Jaccard call —
on a typical big repo, most pairs are in that bucket. Cosine still
runs for every pair so cross-language semantic-only matches still
surface.

**Per-file cache.** The expensive per-file work (split → tokenize →
fingerprint with positions) is persisted to `.codetwin-cache.bin` in
the working directory. Cache keys are
`sha256(absPath ‖ contentHash ‖ patternsHash)` so any of those
changing invalidates the relevant entry automatically. On a warm rerun
unchanged files skip the entire pipeline. Add `.codetwin-cache.bin` to
your `.gitignore`. Use `--no-cache` to skip caching entirely or
`--rebuild-cache` to force a fresh build.

**Live progress.** While the matrix is computing, codetwin prints a
counter to stderr (`comparing snippets: N/M (X%)`). Auto-suppressed
when stderr isn't a TTY so CI logs stay clean. Use `--no-progress` to
force off.

## Architecture

```
codetwin/
├── cmd/codetwin/
│   ├── main.go                  # CLI: flag parsing, file collection, orchestration
│   ├── blocks.go                # Partial-clone orchestration + partial_clones JSON schema
│   ├── repos.go                 # Cross-repo mode: repo labels + snippet namespacing
│   └── baseline.go              # Clone-watchlist CLI glue (--update-baseline / --baseline)
└── internal/
    ├── tokenizer/               # Language-aware lexing + normalization
    ├── splitter/                # Function/class-level chunking per language
    ├── fingerprint/             # Winnowing algorithm (structural similarity)
    ├── similarity/              # TF-IDF vectors + cosine similarity (semantic); matrix + pair materialization
    ├── blocks/                  # Sub-function partial-clone detector (seed → extend → chain → verify)
    ├── cluster/                 # DBSCAN clustering
    ├── report/                  # ANSI terminal + plain text rendering
    ├── refactor/                # --suggest pipeline: align → synthesize → place → patch
    ├── baseline/                # Clone-watchlist snapshots + drift diffing
    ├── config/                  # .codetwin.json loading + ignore matching
    ├── cache/                   # .codetwin-cache.bin persistence
    ├── scan/                    # Per-file pipeline + parallel orchestrator (split → tokenize → fingerprint)
    ├── git/                     # Optional git integration: repo detection, diff parsing, blame
    ├── bench/                   # Test-only ground-truth benchmark (detection-quality gate)
    └── pathutil/                # Lexical path helpers (Dedupe, Contains)
```

### How each layer works

**Tokenizer** (`internal/tokenizer`)
Language-aware normalization before comparison. Comments and import statements
are stripped. String literals become `STR`, numbers become `NUM`, all
non-keyword identifiers become `VAR`. This means `sumArray(arr)` and
`addNumbers(nums)` normalize to the same token stream — only structure matters.

`TokenizeWithLines` returns each token's source line so the rendered preview
can show absolute file line numbers and the match-range slicer can find the
duplicated lines.

**Splitter** (`internal/splitter`)
Breaks each file into per-definition chunks: every Python `def`, Go `func`
(including closures/goroutines/defers), JS / TS / JSX / TSX `function` /
`const arrow` / class method, Rust `fn`, Java method/constructor, and
Elixir `def`/`defp`. Each chunk is then compared independently. A 500-line
module with one duplicated 20-line helper now scores high on that helper
instead of being washed out by 480 lines of unrelated code. For the
container languages (Python, Java, JS/TS, Elixir, Rust) the splitter ALSO
emits one class-span chunk per
`class`/`interface`/`enum`/`record`/`defmodule`/`impl` declaration, and
for Go one synthetic struct+methodset group per type with two or more
in-file methods — all tagged with a distinct chunk kind — see
"Class-level matching" below.

**Fingerprint** (`internal/fingerprint`)
Implements the Winnowing algorithm. Slides a window over k-gram hashes and
selects the minimum hash in each window as a "fingerprint". Jaccard similarity
between two fingerprint sets gives the **structural score** — fast and exact
for near-duplicate detection. `PositionalSet` retains the originating token
positions so the renderer can highlight which lines actually matched.

**Similarity** (`internal/similarity`)
Builds TF-IDF weighted token vectors across the full corpus and computes
cosine similarity. This is the **semantic score** — it catches functionally
similar code even when structure differs (e.g. a Python loop vs a Go loop
with different control flow patterns).

**Blocks** (`internal/blocks`)
The sub-function partial-clone detector behind `--min-block-lines`.
`BuildMatrix` hands it the "gray band" — same-language pairs that share
fingerprints but score below the report threshold — and for each candidate it
seeds on shared fingerprint positions, extends them to maximal
exactly-matching token runs, chains runs across small gaps, and verifies each
block with exact token comparison (containment ≥ 0.85 plus the matched-line
floor on both sides). `cmd/codetwin/blocks.go` dedupes and packages the
findings for the `PARTIAL CLONES` section / `partial_clones` JSON array.

**Cluster** (`internal/cluster`)
DBSCAN over the combined similarity matrix. Rather than reporting O(n²) pairs,
it groups families of similar snippets into clusters. Each cluster is one
refactoring task. Noise points (unique snippets) are omitted. DBSCAN links
transitively, so each cluster header reports both the average internal pair
score and its **cohesion** (the weakest internal pair — `min_score` in JSON);
clusters whose cohesion falls below `--threshold` are re-linked single-linkage
at threshold strength and split into tighter families (members left without a
threshold-strength partner drop out as noise).

**Report** (`internal/report`)
Renders results to stdout with ANSI colour-coded labels and cluster membership.
Sort, threshold filter, and limit run in a shared `Prepare()` helper so
terminal and JSON output reflect the same set of findings. `--plain` disables
colour for CI pipelines. `--json` emits machine-readable output.

**Refactor** (`internal/refactor`)
The `--suggest` / `--suggest-all` pipeline: `align.go` computes a line-level
LCS alignment over the raw source (common spans + divergence "holes"),
`synth.go` dispatches to a per-language emitter that produces a starter
helper (a literal copy of A's body with a divergence comment block),
`place.go` finds the innermost enclosing class/defmodule for Java/Elixir
placement, and `patch.go` wraps the helper in a unified diff. All six
languages have pair emitters (blocks: Go and Python); synthesis is rejected
with a structured note for cross-language pairs, class-level pairs,
control-flow-asymmetric holes, and chunks without a recognisable header.

**Baseline** (`internal/baseline`)
The clone watchlist behind `--update-baseline` / `--baseline`: versioned,
byte-deterministic JSON snapshots of the visible clusters (member keys are
line-range-stripped, root-relative names plus a normalized-token body hash)
and the drift diff that matches clusters by membership overlap and emits the
five drift event kinds.

**Config** (`internal/config`)
Loads `.codetwin.json` from the working directory. Compiles `ignore_paths`
into a glob/component matcher, `ignore_patterns` into regexes consumed by
the tokenizer, and `ignore_pairs` into a post-similarity matcher applied
between BuildMatrix and DBSCAN.

**Scan** (`internal/scan`)
Per-file pipeline that turns a source file into one or more `Snippet`s
(split → tokenize → fingerprint) plus the parallel orchestrator that runs it
across the file set. Sits between `cmd/codetwin/main.go` and the
splitter/tokenizer/fingerprint packages, and consults `internal/cache` so
unchanged files skip the work.

**Git** (`internal/git`)
Thin wrapper around the small set of git invocations the optional
features need: `Open(dir)` discovers the repo root and surfaces
`ErrGitNotInstalled` / `ErrNotARepo` so callers can degrade gracefully;
`(*Repo).ChangedSince(ref)` runs `git diff --unified=0` and parses the
hunks into a `path → []LineRange` map for the `--since` filter;
`(*Repo).Blame(file, start, end)` aggregates `git blame --line-porcelain`
into a single-record `BlameRange` for `--blame`. Used only when the
relevant flag is set; codetwin is otherwise git-independent.

**Pathutil** (`internal/pathutil`)
Pure lexical path helpers. `Dedupe` collapses duplicate input paths and drops
inputs already covered by another (e.g. `./src/utils` is dropped when `./src`
is also passed); `Contains` does an absolute-path containment check that
respects separator boundaries so `/foo` doesn't match `/foobar`.

## Adding a new language

1. Add a `langPatterns` entry in `internal/tokenizer/tokenizer.go`:

```go
Ruby: {
    keywords: []string{"def", "end", "class", "return", "if", "else", ...},
    comments: regexp.MustCompile(`#[^\n]*`),
    imports:  []*regexp.Regexp{regexp.MustCompile(`(?m)^[ \t]*require\s+['"][^'"]+['"]`)},
    strings:  regexp.MustCompile(`'[^']*'|"[^"]*"`),
    numbers:  regexp.MustCompile(`\b\d+(\.\d+)?\b`),
},
```

2. Add the file extension in `internal/tokenizer/tokenizer.go` `Detect()` and
   in `cmd/codetwin/main.go` `supportedExts`.

3. Add tests in `internal/tokenizer/tokenizer_test.go`.

4. (Optional but recommended) Add a splitter for the language in
   `internal/splitter/splitter.go` so chunks are function-sized rather than
   whole-file.

The fingerprint, similarity, cluster, and report layers are fully
language-agnostic — they don't need changes for a new language.

> **Heads up — Go's regex engine is RE2.** No lookaround (`(?=`, `(?!`) and
> no backreferences (`\1`). Use explicit alternation when you'd otherwise
> reach for those features.

## Running tests

```bash
make test                  # all packages
make test-verbose          # with per-test names (good during TDD)
make test-coverage         # generates coverage.html
go test -run TestNormalize # single test by name
```

## Example output

From `codetwin --preview testdata/sum_a.js testdata/sum_b.js`:

```
 codetwin · code similarity report
────────────────────────────────────────────────────────────

 REFACTORING CLUSTERS

  Cluster 1 — 2 snippets · avg similarity  85% · cohesion  85%
    · testdata/sum_a.js:1-7 sumArray
         1 │ function sumArray(arr) {
         2 │   let total = 0;
         3 │   for (let i = 0; i < arr.length; i++) {
         4 │     total += arr[i];
         5 │   }
         6 │   return total;
         7 │ }
    · testdata/sum_b.js:1-7 addNumbers
         1 │ function addNumbers(nums) {
         2 │   let result = 0;
         3 │   for (let i = 0; i < nums.length; i++) {
         4 │     result += nums[i];
         5 │   }
         6 │   return result;
         7 │ }

 SUMMARY
────────────────────────────────────────────────────────────
  Pairs shown       0
  In-cluster pairs  1 (inside the clusters above; --flat lists them)
  Exact clones      0
  Near clones       0
  Strong clones     1
  Refactor targets  0
  Clusters found    1
```

The default report is cluster-first: the pair between the two members
renders once, as the cluster (`--flat` lists it individually, with its
`structural: 100%  semantic: 100%` sub-score line). The two functions
are token-identical, but at 7 non-blank lines each the default
short-snippet dampener (`--min-confidence-lines 10`) scales the
combined score to 85%. Run with `--min-confidence-lines 0` to see the
raw 100% — it would render as a NEAR CLONE, since the EXACT CLONE
label needs ≥ 10 non-blank lines on both sides.

## Recipes

```bash
# Find the five biggest refactor opportunities in your repo
codetwin --sort size --limit 5 --preview ./src

# Triage borderline cases — pairs that ALMOST cleared the threshold
codetwin --sort score-asc --threshold 0.40 ./src

# Suppress MORE short-snippet noise than the default dampener (N=10) does
codetwin --min-confidence-lines 20 --threshold 0.50 ./src

# See raw, undampened scores (short-snippet dampening off)
codetwin --min-confidence-lines 0 ./src

# Strict CI gate — fail if any exact or near clones exist
codetwin --json --threshold 0.85 ./src | jq '.pairs | length' \
  | xargs -I{} test {} -eq 0

# Generate a markdown digest of clusters, sorted by impact
codetwin --json --sort size ./src \
  | jq -r '.clusters[] | "## Cluster \(.id+1) (\(.members|length) snippets)\n\n" + (.members | map("- `\(.)`") | join("\n"))'

# CI gate that ratchets: fail only on duplication this PR introduces
codetwin --since main --threshold 0.85 --json ./src \
  | jq '.pairs | length' | xargs -I{} test {} -eq 0

# Polyglot monorepo: find logic duplicated across languages
codetwin --cross-lang-only --threshold 0.5 --preview ./

# Triage: who introduced the freshest exact clone?
codetwin --blame --sort age --threshold 0.95 --limit 1 --json ./src \
  | jq '.pairs[0] | {a:.file_a,b:.file_b,intro:.provenance_b.first_date,by:.provenance_b.first_author}'

# See exactly which lines a partial clone covers, then get a starter helper
codetwin --preview ./src                       # PARTIAL CLONES with excerpts
codetwin --json ./src | jq '.partial_clones[0].id'
codetwin --suggest <block-id> ./src > extract-block.diff
```

## Git-aware modes

Three flags layer optional git integration on top of codetwin's
otherwise-self-contained scan. They all require `git` on `PATH` and a
git repository in the working directory; if either is missing, codetwin
exits 1 with a clear error rather than silently degrading.

- `--cross-lang-only` does **not** need git; included here for the
  positioning narrative only.
- `--since <ref>` filters pairs and clusters to those whose endpoints
  overlap lines changed between `<ref>` and the current working tree
  (uncommitted edits included). Use it as a CI gate that only complains
  about new duplication.
- `--blame` calls `git blame` once per snippet and attaches a
  `Provenance` record (`first_commit`, `first_author`, `first_date`,
  optionally `last_*`) to each pair. Adds a small per-snippet cost; pair
  with `--sort age` to surface the freshest clones first.

```bash
# Sample errors when git or repo is missing
$ codetwin --since main ./src
error: --since requires running inside a git repository

$ PATH=/var/empty codetwin --blame ./src
error: --blame requires the git binary on PATH

$ codetwin --since main --blame ./src
error: --since and --blame require running inside a git repository
```

## License

MIT — see [LICENSE](LICENSE).
