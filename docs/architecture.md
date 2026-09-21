# Architecture

```
codetwin/
├── analyzer/                    # Public cancellable analysis API
├── cmd/codetwin/
│   ├── main.go                  # Thin CLI adapter: flags, file collection, rendering
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
    ├── cache/                   # Pluggable cache storage; atomic gob default
    ├── paircache/               # Persisted exact-score snapshot contract
    ├── scan/                    # Per-file pipeline + parallel orchestrator (split → tokenize → fingerprint)
    ├── git/                     # Optional git integration: repo detection, diff parsing, blame
    ├── bench/                   # Test-only ground-truth benchmark (detection-quality gate)
    └── pathutil/                # Lexical path helpers (Dedupe, Contains)
```

### Reusable Go API

`analyzer.Analyzer` runs the compute pipeline without CLI globals. Callers
provide an explicit file set and `context.Context`; cancellation propagates
through parallel scanning, exact scoring, block detection, clustering, and
the context-aware git helpers used by the CLI.

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

result, err := (analyzer.Analyzer{}).Run(ctx, analyzer.Request{
    Files:              []string{"service/a.go", "service/b.go"},
    MinLines:           5,
    Threshold:          0.50,
    Epsilon:            0.35,
    MinPoints:          2,
    MinConfidenceLines: 10,
    Granularity:        analyzer.GranularityFunction,
})
if err != nil {
    return err
}
fmt.Printf("%d pairs in %d clusters\n", len(result.Pairs), len(result.Clusters))
```

`Result` also exposes snippets, partial clones, dead-code findings, warnings,
and candidate/cache statistics. `Request.OnProgress` receives structured stage
updates for IDE, daemon, or agent integrations. `Analyzer` is stateless and
safe for concurrent use; each run owns its cache lifecycle.

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
[Class-level matching](../README.md#class-level-matching).

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
with different control flow patterns). Candidate retrieval unions shared
Winnowing fingerprints with a deterministic inverted index over each
snippet's 16 highest-weight TF-IDF terms, then computes the exact structural,
cosine, and combined scores for every selected pair. The bounded semantic
index is approximate only at retrieval; final scores are never approximate.
Combined nonzero scores are stored as edges in a sparse graph, where an absent
edge has score zero. The legacy `BuildMatrix` API remains an exhaustive dense
compatibility wrapper and regression oracle.

**Blocks** (`internal/blocks`)
The sub-function partial-clone detector behind `--min-block-lines`.
`BuildGraph` hands it the "gray band" — same-language pairs that share
fingerprints but score below the report threshold — and for each candidate it
seeds on shared fingerprint positions, extends them to maximal
exactly-matching token runs, chains runs across small gaps, and verifies each
block with exact token comparison (containment ≥ 0.85 plus the matched-line
floor on both sides). `cmd/codetwin/blocks.go` dedupes and packages the
findings for the `PARTIAL CLONES` section / `partial_clones` JSON array.

**Cluster** (`internal/cluster`)
DBSCAN over the combined sparse similarity graph. Rather than reporting O(n²) pairs,
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
between BuildGraph and DBSCAN.

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

[Back to README](../README.md#architecture)

