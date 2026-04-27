# codetwin

Multi-language code similarity detector — finds duplicate and refactorable code
across `.go`, `.js`, `.ts`, `.py`, `.java`, `.rs`, and `.ex`/`.exs` files with zero external
dependencies.

## Install

```bash
go install github.com/codetwin/codetwin/cmd/codetwin@latest
```

Or build from source:

```bash
git clone https://github.com/codetwin/codetwin
cd codetwin
make build          # produces ./codetwin binary
make test           # run all unit + integration tests
```

## Usage

```bash
# Scan a directory recursively
codetwin ./src

# Compare specific files
codetwin ./utils/a.go ./utils/b.go

# Only report pairs with >= 60% similarity
codetwin --threshold 0.6 ./pkg

# Plain text for CI pipelines / file redirection
codetwin --plain ./src > report.txt

# JSON output (pipe-friendly)
codetwin --json ./src | jq '.pairs[] | select(.score > 0.8)'

# Show everything including weak matches
codetwin --verbose ./src
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `--threshold` | `0.30` | Minimum score to report (0.0–1.0) |
| `--plain` | false | Disable ANSI colors (CI-safe) |
| `--json` | false | JSON output |
| `--verbose` | false | Show all pairs including weak |
| `--min-lines` | `3` | Skip files shorter than N non-blank lines |
| `--eps` | `0.45` | DBSCAN epsilon (cluster density threshold) |
| `--min-pts` | `2` | DBSCAN minimum cluster size |

## Scoring

| Score | Label | Recommended action |
|---|---|---|
| > 85% | Exact clone | Extract shared utility, delete one |
| > 65% | Strong clone | Parameterize differing parts |
| > 45% | Refactor target | Evaluate shared abstraction |
| < 45% | Weak similarity | Probably coincidental |

## Architecture

```
codetwin/
├── cmd/codetwin/
│   └── main.go                  # CLI: flag parsing, file collection, orchestration
└── internal/
    ├── tokenizer/               # Language-aware lexing + normalization
    │   ├── tokenizer.go
    │   └── tokenizer_test.go
    ├── fingerprint/             # Winnowing algorithm (structural similarity)
    │   ├── fingerprint.go
    │   └── fingerprint_test.go
    ├── similarity/              # TF-IDF vectors + cosine similarity (semantic)
    │   ├── similarity.go
    │   └── similarity_test.go
    ├── cluster/                 # DBSCAN clustering
    │   ├── cluster.go
    │   └── cluster_test.go
    └── report/                  # ANSI terminal + plain text rendering
        ├── report.go
        └── report_test.go
```

### How each layer works

**Tokenizer** (`internal/tokenizer`)
Language-aware normalization before comparison. Comments are stripped. String
literals become `STR`, numbers become `NUM`, all non-keyword identifiers become
`VAR`. This means `sumArray(arr)` and `addNumbers(nums)` normalize to the same
token stream — only structure matters.

**Fingerprint** (`internal/fingerprint`)
Implements the Winnowing algorithm. Slides a window over k-gram hashes and
selects the minimum hash in each window as a "fingerprint". Jaccard similarity
between two fingerprint sets gives the **structural score** — fast and exact for
near-duplicate detection.

**Similarity** (`internal/similarity`)
Builds TF-IDF weighted token vectors across the full corpus and computes cosine
similarity. This is the **semantic score** — it catches functionally similar code
even when structure differs (e.g. a Python loop vs a Go loop with different
control flow patterns).

**Cluster** (`internal/cluster`)
DBSCAN over the combined similarity matrix. Rather than reporting O(n²) pairs, it
groups families of similar snippets into clusters. Each cluster is one refactoring
opportunity. Noise points (unique files) are omitted.

**Report** (`internal/report`)
Renders results to stdout with ANSI colour-coded labels, a similarity matrix
summary, and cluster membership. `--plain` disables colour for CI pipelines.
`--json` emits machine-readable output.

### Final score

```
score = 0.5 × structural (Jaccard) + 0.5 × semantic (cosine TF-IDF)
```

The 50/50 weight can be tuned via code — raise the structural weight if you care
more about copy-paste detection; raise semantic if you want to catch logic
equivalence across different coding styles.

## Adding a new language

1. Add a `langPatterns` entry in `internal/tokenizer/tokenizer.go`:

```go
Ruby: {
    keywords: []string{"def", "end", "class", "return", "if", "else", ...},
    comments: regexp.MustCompile(`#[^\n]*`),
    strings:  regexp.MustCompile(`'[^']*'|"[^"]*"`),
    numbers:  regexp.MustCompile(`\b\d+(\.\d+)?\b`),
},
```

2. Add the file extension in `internal/tokenizer/tokenizer.go` `Detect()` and in
   `cmd/codetwin/main.go` `supportedExts`.

3. Add a test in `internal/tokenizer/tokenizer_test.go`.

That's it — the fingerprint, similarity, cluster, and report layers are fully
language-agnostic.

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

  [EXACT CLONE     ]  91%
  │  JS: sumArray
  │  JS: addNumbers
  │  structural: 100%  semantic: 82%

  [STRONG CLONE    ]  74%
  │  JS: sumArray
  │  Python: sum_list
  │  structural: 68%  semantic: 80%

  [REFACTOR TARGET ]  61%
  │  JS: sumArray
  │  Go: SumSlice
  │  structural: 55%  semantic: 67%

 REFACTORING CLUSTERS

  Cluster 1 — 4 snippets
    · JS: sumArray
    · JS: addNumbers
    · Python: sum_list
    · Go: SumSlice

 SUMMARY
────────────────────────────────────────────────────────────
  Exact clones      1
  Strong clones     1
  Refactor targets  2
  Clusters found    1
```
