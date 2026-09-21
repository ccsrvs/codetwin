# Performance and caching

codetwin is designed to handle large repositories. A few mechanisms in
play:

**Parallel candidate scoring.** Similarity work shards candidate rows across
`runtime.NumCPU()` goroutines. Exact scoring runs only for the union of
structural and semantic candidates.

**Inverted-index pair pruning.** Before computing scores, codetwin
builds a `fingerprint-hash → snippet-indices` map and a bounded TF-IDF
term index. Their union preserves structural and cross-language
semantic-only candidates without all-pairs cosine scoring.

**Persistent incremental cache.** The expensive per-file work (split →
tokenize → fingerprint with positions) and exact candidate-pair scores are
persisted to `.codetwin-cache.bin` in the working directory. File cache keys are
`sha256(absPath ‖ contentHash ‖ patternsHash)` so any of those
changing invalidates the relevant entry automatically. On a warm rerun
unchanged files skip the parsing pipeline and unchanged candidate pairs reuse
their exact scores. A changed file, corpus-dependent TF-IDF weight, scoring
parameter, or snippet property invalidates the affected document identity and
recomputes only incident candidate scores. Add `.codetwin-cache.bin` to your
`.gitignore`. Use `--no-cache` to skip caching entirely or
`--rebuild-cache` to force a fresh build. Persistence is behind the
`cache.Storage` interface; the default CGO-free gob implementation uses a
synced temporary file plus atomic rename and explicit schema validation.

**Live progress.** While the matrix is computing, codetwin prints a
counter to stderr (`comparing snippets: N/M (X%)`). Auto-suppressed
when stderr isn't a TTY so CI logs stay clean. Use `--no-progress` to
force off.

[Back to README](../README.md#performance)

