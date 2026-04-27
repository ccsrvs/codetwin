---
name: codetwin
description: >
  Run codetwin — a multi-language code similarity and duplicate detection CLI — via bash_tool.
  Use this skill whenever the user asks to find duplicate code, detect clones, identify refactoring
  opportunities, check for similar functions across files, or scan a codebase for copy-paste across
  Go, JavaScript, TypeScript, Python, Java, Rust, or Elixir. Also trigger when the user says things
  like "find repeated code", "what can be refactored", "check for duplicates", or "scan my project
  for similar functions".
---

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
| Two specific files | `codetwin file_a.go file_b.go` |
| Multiple roots (nested deduped) | `codetwin ./src ./pkg` |

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
--sort string           result ordering: score | score-asc | size | size-asc | name (default score)
--limit int             show only the top N pairs and N clusters (0 = no limit)
```

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

`--limit N` caps **each** section at N items independently (top N pairs and
top N clusters), applied after sort and threshold filtering. Use it together
with `--sort` to focus on what matters: e.g. `--sort size --limit 5` for the
five biggest refactor opportunities.

### Configuration file (`.codetwin.json`)

If a `.codetwin.json` exists in the current working directory, codetwin
reads it for default flag overrides, files to ignore, and lines/regexes to
strip before tokenization. CLI flags always win over the `defaults` block.

```json
{
  "defaults": {
    "threshold": 0.5,
    "preview": true,
    "preview_lines": 15,
    "sort": "size",
    "limit": 20
  },
  "ignore_paths": [
    "vendor/**",
    "**/*_test.go",
    "migrations/"
  ],
  "ignore_patterns": [
    "^\\s*log\\.(info|debug|warn|error)\\(",
    "^\\s*println!\\("
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

When you scan multiple paths and one is nested inside another (e.g.
`./src ./src/utils`), only the outer path is walked — no double-counting.

## Step 3 — Interpret results

### Score thresholds

| Score | Label | What to do |
|---|---|---|
| > 85% | Exact clone | Extract shared utility, delete one immediately |
| > 65% | Strong clone | Parameterize the differing parts |
| > 45% | Refactor target | Evaluate whether a shared abstraction reduces duplication |
| < 45% | Weak similarity | Probably coincidental — review before acting |

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
| Build errors | Run `go test ./...` first to isolate the broken package |
