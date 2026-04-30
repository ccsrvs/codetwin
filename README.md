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

The git-aware features (`--since`, `--blame`, `--sort age`) require git on
`PATH` and a git repository in the working directory; without them codetwin
runs the same in any directory.

## Install

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

# CI gate: fail on any new strong clone introduced since main
codetwin --since main --threshold 0.85 --json ./src

# Annotate findings with git provenance, newest clones first
codetwin --blame --sort age --limit 10 ./src
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `--threshold` | `0.30` | Minimum score to report (0.0–1.0) |
| `--plain` | false | Disable ANSI colors (CI-safe) |
| `--json` | false | JSON output |
| `--verbose` | false | Show all pairs including weak |
| `--min-lines` | `3` | Skip chunks shorter than N non-blank lines |
| `--eps` | `0.45` | DBSCAN epsilon (cluster density threshold) |
| `--min-pts` | `2` | DBSCAN minimum cluster size |
| `--preview` | false | Show line-numbered code excerpts under each finding |
| `--preview-lines` | `10` | Max lines per preview; `0` = show whole snippet |
| `--sort` | `score` | Result ordering: `score`, `score-asc`, `size`, `size-asc`, `name` |
| `--limit` | `0` | Cap pairs and clusters at N items each (0 = no limit) |
| `--min-confidence-lines` | `0` | Dampen pair scores when `min(LinesA, LinesB) < N` (0 = off). See [Scoring](#scoring). |
| `--no-progress` | false | Suppress the live progress indicator on stderr |
| `--no-cache` | false | Skip reading and writing `.codetwin-cache.bin` |
| `--rebuild-cache` | false | Ignore any existing cache and rebuild from scratch |
| `--debug` | false | Print phase checkpoints with elapsed time to stderr |
| `--cross-lang-only` | false | Report only pairs whose two snippets are in different languages |
| `--since` | `""` | PR-delta mode: keep only findings overlapping lines changed since `<ref>` (requires git) |
| `--blame` | false | Annotate findings with git provenance (introduced, by whom, last touched) (requires git) |
| `--skill` | false | Print the full skill guide (embedded in the binary) and exit |
| `--guide` | false | Print the report interpretation guide and exit |

## Scoring

| Score | Label | Recommended action |
|---|---|---|
| > 95% | Exact clone | Extract shared utility, delete one |
| > 85% | Near clone | Virtually identical; treat as a clone unless intentional |
| > 65% | Strong clone | Parameterize differing parts |
| > 45% | Refactor target | Evaluate shared abstraction |
| < 45% | Weak similarity | Probably coincidental |

Final score is `0.5 × structural (Jaccard) + 0.5 × semantic (cosine TF-IDF)`.
For a longer walk-through of what the score means, what the
`structural`/`semantic` sub-scores below each pair tell you, and how
pairs differ from clusters, run `codetwin --guide`.

### Short-snippet confidence

Two 5-line snippets that share their entire token shape and two 25-line
snippets that do the same both score 100%, but the first is much weaker
evidence — short snippets are forced into a shared shape by their API
surface (e.g. test scaffolding that has to call one function and assert
on the result). `--min-confidence-lines N` opts into a length-aware
dampener: the combined score is multiplied by `0.5 + 0.5 · min(LinesA, LinesB) / N`
(capped at 1.0), so matches under N non-blank lines lose proportional
score. The dampener is applied once at the scoring layer, so it also
affects DBSCAN cluster boundaries — short-snippet matches that drop
below the eps threshold don't cluster. A common starting point is
`--min-confidence-lines 20` — enough to push test boilerplate out of
the "exact clone" bucket while leaving real multi-line refactor
targets unaffected.

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
    "min_confidence_lines": 20
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
│   └── main.go                  # CLI: flag parsing, file collection, orchestration
└── internal/
    ├── tokenizer/               # Language-aware lexing + normalization
    ├── splitter/                # Function/class-level chunking per language
    ├── fingerprint/             # Winnowing algorithm (structural similarity)
    ├── similarity/              # TF-IDF vectors + cosine similarity (semantic)
    ├── cluster/                 # DBSCAN clustering
    ├── report/                  # ANSI terminal + plain text rendering
    ├── config/                  # .codetwin.json loading + ignore matching
    ├── cache/                   # .codetwin-cache.bin persistence
    ├── scan/                    # Per-file pipeline + parallel orchestrator (split → tokenize → fingerprint)
    ├── git/                     # Optional git integration: repo detection, diff parsing, blame
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
Breaks each file into per-definition chunks: every Python `def`, Go `func`,
JS / TS / JSX / TSX `function` / `const arrow` / `class`, Rust `fn`, and Java
method. Each chunk is then compared independently. A 500-line module with
one duplicated 20-line helper now scores high on that helper instead of being
washed out by 480 lines of unrelated code. Elixir falls back to whole-file
chunks (it needs a language-specific splitter; PRs welcome).

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

**Cluster** (`internal/cluster`)
DBSCAN over the combined similarity matrix. Rather than reporting O(n²) pairs,
it groups families of similar snippets into clusters. Each cluster is one
refactoring task. Noise points (unique snippets) are omitted.

**Report** (`internal/report`)
Renders results to stdout with ANSI colour-coded labels and cluster membership.
Sort, threshold filter, and limit run in a shared `Prepare()` helper so
terminal and JSON output reflect the same set of findings. `--plain` disables
colour for CI pipelines. `--json` emits machine-readable output.

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

```
 codetwin · code similarity report
────────────────────────────────────────────────────────────

 SIMILARITY PAIRS

  [EXACT CLONE     ]  100%
    src/utils/sum.go:3-9 SumSlice
         3 │ func SumSlice(nums []int) int {
         4 │     total := 0
         5 │     for i := 0; i < len(nums); i++ {
         6 │         total += nums[i]
         7 │     }
         8 │     return total
         9 │ }
    src/aggregate.go:14-20 SumAll
        14 │ func SumAll(values []int) int {
        15 │     total := 0
        16 │     for i := 0; i < len(values); i++ {
        17 │         total += values[i]
        18 │     }
        19 │     return total
        20 │ }
  structural: 100%  semantic: 100%

 REFACTORING CLUSTERS

  Cluster 1 — 2 snippets
    · src/utils/sum.go:3-9 SumSlice
    · src/aggregate.go:14-20 SumAll

 SUMMARY
────────────────────────────────────────────────────────────
  Pairs shown       1
  Exact clones      1
  Strong clones     0
  Refactor targets  0
  Clusters found    1
```

## Recipes

```bash
# Find the five biggest refactor opportunities in your repo
codetwin --sort size --limit 5 --preview ./src

# Triage borderline cases — pairs that ALMOST cleared the threshold
codetwin --sort score-asc --threshold 0.40 ./src

# Suppress noisy short-snippet matches (test boilerplate, tiny wrappers)
codetwin --min-confidence-lines 20 --threshold 0.50 ./src

# Strict CI gate — fail if any exact clones exist
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
