# codetwin Skill

Runs the `codetwin` CLI to find duplicate and structurally similar code across a multi-language
codebase. Reports pairs and clusters with optional line-numbered code previews.

## How the tool works

codetwin operates in five internal stages — all handled automatically:

1. **Chunk** — split each file into per-definition chunks (Python `def`,
   Go `func`, JS `function`/`class`/arrow, Rust `fn`). A 500-line module
   with one duplicated 20-line helper scores high on the helper rather
   than getting washed out by 480 lines of unrelated code.
2. **Tokenize & normalize** — strip comments, imports, replace literals/
   identifiers with canonical tokens
3. **Winnowing fingerprints** — structural (Jaccard) similarity via k-gram hashing
4. **TF-IDF vectors** — semantic (cosine) similarity across the full corpus
5. **DBSCAN clustering** — groups related findings into one refactoring opportunity each

Final score = `0.5 × structural + 0.5 × semantic`

## Step 1 — Locate or build the binary

```bash
which codetwin || ls ./codetwin 2>/dev/null
```

If no binary is on `PATH` or in the working directory, build from the codetwin repo:

```bash
cd <path-to-codetwin-repo> && make build        # produces ./codetwin
# or
go install github.com/ccsrvs/codetwin/cmd/codetwin@latest
```

## Step 2 — Run codetwin

### Basic scan (colorized terminal output)

```bash
codetwin --threshold 0.40 <TARGET_PATH>
```

### Common flag combinations

| Goal | Command |
|---|---|
| Default scan | `codetwin --threshold 0.40 <path>` |
| Only strong clones | `codetwin --threshold 0.70 <path>` |
| CI-safe plain text | `codetwin --plain --threshold 0.40 <path>` |
| Machine-readable | `codetwin --json --threshold 0.40 <path>` |
| Show everything | `codetwin --verbose --threshold 0.20 <path>` |
| Inline code previews | `codetwin --preview --threshold 0.40 <path>` |
| Filter noisy short-snippet matches | `codetwin --min-confidence-lines 20 --threshold 0.50 <path>` |
| Two specific files | `codetwin file_a.go file_b.go` |
| Multiple roots (nested deduped) | `codetwin ./src ./pkg` |
| Suggest a refactor (Go) | `codetwin --suggest <pair-id> <path>` |
| Suggest for every visible pair (JSON) | `codetwin --suggest-all --json <path>` |

### All flags

```
--threshold float       minimum score to report, 0.0–1.0 (default 0.30)
--plain                 no ANSI colors — use for piping or file output
--json                  JSON output
--verbose               show all pairs including weak similarities
--min-lines int         skip chunks shorter than N non-blank lines (default 3)
--eps float             DBSCAN epsilon — cluster density (default 0.45)
--min-pts int           DBSCAN min cluster size (default 2)
--preview               show a short code excerpt for each finding
--preview-lines int     max lines per preview; 0 = show whole snippet (default 10)
--sort string           result ordering: score | score-asc | size | size-asc | name | age | age-asc
                        (default score; age modes require --blame)
--limit int             show only the top N pairs and N clusters (0 = no limit)
--min-confidence-lines int  dampen pair scores when min(LinesA, LinesB) < N (0 = off);
                            multiplier ramps from 0.5× at 0 lines to 1.0× at N
--cross-lang-only       report only pairs whose two snippets are in different languages
                        (e.g. duplicate logic across a Go service and a TS dashboard)
--since string          PR-delta mode: keep only findings where ≥1 endpoint overlaps
                        lines changed since <ref> (e.g. main, HEAD~5, abc123)
--blame                 annotate findings with git provenance (when introduced, by whom,
                        last touched). Pairs --sort=age for "newest clones first".
--suggest string        print a unified diff that adds a starter helper extracted from the
                        matching pair (look up the 8-char pair ID in --json output). v1
                        supports Go, Python, and Java; other languages print a 'note'
                        explaining why.
--suggest-all           with --json: populate `suggested_patch` on every visible pair
--no-progress           suppress the live progress indicator on stderr
--no-cache              skip reading and writing .codetwin-cache.bin
--rebuild-cache         ignore any existing cache and rebuild from scratch
--skill                 print this skill guide and exit
--guide                 print the report interpretation guide and exit
```

### Git-aware modes

`--since`, `--blame`, and `--sort=age` all require git on `PATH` and a
git repository in the working directory. If either condition isn't met,
codetwin exits 1 with a clear error — there is no silent degradation,
because the user explicitly opted in. Without these flags codetwin
works the same in a non-git directory as it does in one.

| Goal | Command |
|---|---|
| CI gate: fail only on duplication this PR introduces | `codetwin --since main --threshold 0.85 --json <path>` |
| Find duplicate logic across languages in a polyglot repo | `codetwin --cross-lang-only --threshold 0.50 <path>` |
| Show the freshest clones (newest endpoint first) | `codetwin --blame --sort age --limit 10 <path>` |
| Annotate every match with origin metadata | `codetwin --blame --preview <path>` |
| Triage who introduced this clone | `codetwin --blame --json <path> \| jq '.pairs[] \| {a:.file_a,b:.file_b,intro_a:.provenance_a.first_date,intro_b:.provenance_b.first_date}'` |

`codetwin --help` prints the same flag list with one-line descriptions.
`codetwin --guide` walks through the score bands, structural/semantic
sub-scores, and pairs vs clusters in more depth.

### Refactor suggestions (`--suggest`)

Once a pair is identified, `--suggest` turns codetwin from a *reporter*
into a *starter generator*: it emits a unified diff that appends a
helper function — a literal copy of snippet A's body, prefaced by a
divergence comment block listing exactly how snippet B differs.

```bash
# 1. Run with --json to discover pair IDs.
codetwin --json --threshold 0.85 ./pkg | jq '.pairs[] | {id, file_a, file_b}'

# 2. Pick a same-language pair (Go or Python in v1) and emit its suggestion.
codetwin --suggest <pair-id> ./pkg > suggest.diff

# 3. Review, then apply.
git apply suggest.diff
```

The diff is *additive only* — it adds the helper at the end of A's
file without rewriting either call site. The reviewer (or a Claude
skill consuming the JSON output) finishes the refactor by hand.
Codetwin deliberately stops short of full parameterization: doing it
without a language AST would be unsafe.

Rejection cases (printed as a `note:` line on stderr; exit 1):

- Methods on different receiver types (Go)
- Anonymous/goroutine/defer chunks (Go)
- Cross-language pairs (v1 doesn't transpile)
- Unsupported language (v1 supports Go, Python, Java,
  JavaScript/TypeScript, Rust, and Elixir — every language with a
  splitter)
- Holes where one side has a control-flow keyword (`return`/`break`/
  `continue`, plus `raise`/`yield` for Python, `throw`/`yield` for
  Java and JavaScript/TypeScript, `panic` for Rust, and
  `raise`/`throw`/`exit` for Elixir) and the other doesn't — that
  asymmetry signals semantically different snippets

For Java specifically, the helper is appended at file scope (after the
wrapping class's closing `}`) so the diff applies cleanly via `git
apply`, but the file won't compile until a human moves the helper
inside an appropriate class. The helper carries a leading `// NOTE:
appended at file scope; move it into the appropriate Java class…`
comment to flag the placement step.

For JavaScript/TypeScript, ES6+ class methods are unwrapped from their
class chunk and emitted as free `function` helpers. When the body
references `this`, the helper carries a `// NOTE: extracted as a free
function from a class-method context; this references must be wired
at call sites…` comment so the user knows to bind via `.call(this, …)`
or pass `this` explicitly. Free-function and arrow-assignment sources
don't carry that NOTE.

For Rust, free functions and impl-method chunks are both emitted as
free `fn` helpers; modifiers (`pub`, `pub(crate)`, `async`, `unsafe`)
plus generics, lifetimes, return types, and `where` clauses are all
preserved verbatim. When the body references `&self`, the helper
carries a `// NOTE: extracted as a free function with &self carried as
an explicit parameter…` comment so the user knows to either bind a
receiver at call sites (e.g. `extracted_helper(&store, key)`) or move
the helper into an `impl` block.

For Elixir, defs (and `defp` private defs) inside a `defmodule` are
extracted as method-level chunks. The helper is emitted as a free
`def name(args) do … end` block and ALWAYS carries a `# NOTE: appended
at file scope; Elixir defs must live inside a defmodule…` comment —
Elixir cannot have free-standing defs, so the user must always move
the helper into an appropriate module before running.

`--suggest-all` with `--json` populates `suggested_patch` on every
pair, so a single run produces machine-readable suggestions across the
whole report.

### Sorting and limiting results

`--sort` applies the same ordering to both pairs and clusters, with each
section using its natural interpretation:

| Mode         | Pairs                            | Clusters                              |
|--------------|----------------------------------|---------------------------------------|
| `score`      | highest similarity first         | highest avg internal pair score first |
| `score-asc`  | borderline cases first           | loosest clusters first                |
| `size`       | biggest snippets first           | most members first ("biggest bang")   |
| `size-asc`   | smallest snippets first          | smallest clusters first ("quick wins")|
| `name`       | alphabetical by file path        | alphabetical by first member          |
| `age`        | newest pair first (when introduced) | (clusters fall back to score)      |
| `age-asc`    | oldest pair first                | (clusters fall back to score)         |

`age` and `age-asc` use git blame to determine when each pair was
introduced (= the later FirstTime of its two endpoints). They require
`--blame` and silently fall back to score sort for pairs without
provenance.

`--limit N` caps **each** section at N items independently (top N pairs and
top N clusters), applied after sort and threshold filtering. Use it together
with `--sort` to focus on what matters: e.g. `--sort size --limit 5` for the
five biggest refactor opportunities.

### Configuration file (`.codetwin.json`)

If a `.codetwin.json` exists in the current working directory, codetwin
reads it for default flag overrides, files to ignore, lines/regexes to
strip before tokenization, and individual false-positive pairs to silence.
CLI flags always win over the `defaults` block.

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
    {"a": "auth/handler.go parseRequest",
     "b": "api/middleware.go parseRequest"}
  ]
}
```

**`ignore_paths`** — gitignore-flavored:

- `vendor` — matches any path component named exactly `vendor`
- `vendor/lib` — matches that multi-component path anywhere in the tree
- `vendor/` — directory-only (file `vendor` won't match)
- `*_test.go` — basename glob, anywhere
- `vendor/**` — anything under any `vendor` directory
- `/build` — leading `/` anchors to the scan root only

**`ignore_patterns`** — Go regexes (with `(?m)` automatically applied).
Lines matching any pattern are stripped before tokenization, like comments.
Useful for filtering out logging boilerplate that would otherwise inflate
similarity scores.

**`ignore_pairs`** — silence one specific pair as a confirmed false positive
while keeping both files scannable against the rest of the corpus. Each
entry has two endpoints; a pair is dropped (from both pair output AND
clustering) when its two snippets match the endpoints in either order.

- `"path/to/file.go"` — any chunk in that file (path uses the same globs as `ignore_paths`)
- `"path/to/file.go Func"` — only chunks where the splitter detected symbol `Func`
- `"**/*_generated.go"` — any chunk in any generated file

Do NOT include line ranges (`:15-30`) — codetwin strips them before
matching so entries survive routine edits that shift line numbers.
Reach for `ignore_pairs` when `ignore_paths` is too coarse (the file has
legitimate matches against other files) and `ignore_patterns` doesn't
help (the noise isn't a per-line shape).

When you scan multiple paths and one is nested inside another (e.g.
`./src ./src/utils`), only the outer path is walked — no double-counting.

### Performance and the cache

codetwin parallelizes the all-pairs comparison across CPU cores and uses
an inverted index to skip Jaccard work for pairs that share no
fingerprints, so a fresh scan of a big repo runs in seconds.

The expensive per-file work (split → tokenize → fingerprint) is also
persisted to `.codetwin-cache.bin` in the working directory. On a
warm rerun any file whose content + ignore_patterns are unchanged is
served from cache, often making subsequent runs nearly instant. Add
the cache file to `.gitignore` (or let codetwin's installer do it for
you).

| Goal | Flag |
|---|---|
| Skip caching entirely | `--no-cache` |
| Force a fresh build, then re-cache | `--rebuild-cache` |
| Suppress the live progress counter | `--no-progress` |

A live `comparing snippets: N/M (X%)` indicator prints to stderr while
the matrix is computing. It's auto-suppressed when stderr isn't a TTY
(piping to a file or running under CI), so log capture stays clean.

## Step 3 — Interpret results

### Score thresholds

| Score | Label | What to do |
|---|---|---|
| > 95% | Exact clone | Extract shared utility, delete one immediately |
| > 85% | Near clone | Virtually identical with one or two token edits; treat as a clone unless the difference is intentional |
| > 65% | Strong clone | Parameterize the differing parts |
| > 45% | Refactor target | Evaluate whether a shared abstraction reduces duplication |
| < 45% | Weak similarity | Probably coincidental — review before acting |

For a fuller explanation of the score, the structural/semantic sub-scores,
and how pairs differ from clusters, run `codetwin --guide`.

### Short-snippet confidence

Two 5-line snippets with identical token shape and two 25-line snippets
with identical token shape both score 100%, but the first is much weaker
evidence — short snippets are forced into shared shapes by their API
surface (test scaffolding, trivial wrappers). `--min-confidence-lines N`
opts into a length-aware dampener: the combined score is scaled by
`0.5 + 0.5 · min(LinesA, LinesB) / N` (capped at 1.0). The dampener is
applied once at the scoring layer, so it also affects DBSCAN cluster
boundaries — setting it dissolves clusters built on tiny-snippet noise,
not just demoting individual pairs. Off by default. A typical starting
value is `--min-confidence-lines 20`.

### Clusters vs pairs

- **Clusters** = families of related snippets grouped by DBSCAN. One cluster = one refactoring task.
- **Pairs** = individual high-similarity findings not part of a larger cluster.
- Always address clusters first — they represent the highest-value consolidation opportunities.

### Cross-language clusters

When a cluster spans multiple languages (e.g. JS + Python + Go all implementing the same loop),
that's a strong signal for either:
- A shared service/API boundary (microservice or RPC endpoint)
- A canonical implementation in one language with thin client wrappers in the others

## Step 4 — Showing code previews

Use `--preview` to print a short line-numbered excerpt of each file directly under its path in
the report:

```bash
codetwin --preview --preview-lines 8 --threshold 0.40 <path>
```

In `--json` mode, `--preview` adds a top-level `previews` map keyed by file path so downstream
consumers can render snippets without re-reading the source:

```bash
codetwin --json --preview --threshold 0.50 <path> | jq '.previews'
```

## Supported languages

| Language | Extensions |
|---|---|
| Go | `.go` |
| JavaScript / TypeScript | `.js` `.ts` `.jsx` `.tsx` |
| Python | `.py` |
| Java | `.java` |
| Rust | `.rs` |
| Elixir | `.ex` `.exs` |

## Running tests

From the repository root:

```bash
go test ./...
```

## Worked example

```bash
codetwin --preview --threshold 0.40 ./testdata

# Expected output (chunk-level — note "path:start-end Symbol" naming):
#  SIMILARITY PAIRS
#
#   [EXACT CLONE     ]  100%
#     testdata/sum_a.js:1-7 sumArray
#          1 │ function sumArray(arr) {
#          2 │   let total = 0;
#          3 │   for (let i = 0; i < arr.length; i++) {
#          4 │     total += arr[i];
#          5 │   }
#          6 │   return total;
#          7 │ }
#     testdata/sum_b.js:1-7 addNumbers
#          1 │ function addNumbers(nums) {
#          ...
#
#  REFACTORING CLUSTERS
#   Cluster 1 — 2 snippets:
#     testdata/sum_a.js:1-7 sumArray
#     testdata/sum_b.js:1-7 addNumbers
```

## Troubleshooting

| Symptom | Fix |
|---|---|
| `not enough parseable files` | Target has < 2 files with supported extensions |
| All scores near 0% | Files may be too short — lower `--min-lines` |
| No clusters formed | Lower `--eps` (e.g. `--eps 0.35`) or `--min-pts 2` |
| Want to see source under findings | Add `--preview` (and tune `--preview-lines`) |
| Too many noisy pairs from imports/logging | Add `ignore_patterns` to `.codetwin.json` |
| Tests/vendored code dominating results | Add `ignore_paths` (e.g. `["**/*_test.go", "vendor/**"]`) |
| One specific pair is a confirmed false positive | Add `ignore_pairs` (keeps both files scannable against everything else) |
| 100% scores on tiny snippets that aren't real duplicates | Add `--min-confidence-lines 20` — short matches lose proportional score |
| `--since/--blame requires the git binary on PATH` | Install git, or drop the flag |
| `--since/--blame requires running inside a git repository` | `cd` into the repo, or run `git init` if the directory should be one |
| `--since <ref>` returns nothing | Confirm the ref exists (`git rev-parse <ref>`) and that the diff is non-empty (`git diff --stat <ref>`) |
| Build errors | Run `go test ./...` first to isolate the broken package |
