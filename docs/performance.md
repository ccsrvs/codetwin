# Performance and caching

codetwin is designed to handle large repositories. A few mechanisms in
play:

**Parallel scoring.** Similarity work shards rows across
`runtime.NumCPU()` goroutines. Every comparable pair gets an exact score;
a `fingerprint-hash → snippet-indices` map limits the structural Jaccard
computation to pairs that share at least one fingerprint.

**Parallel block detection.** Sub-function partial-clone detection runs
on the same worker count. Each candidate's matches are collected in
candidate order, so the report is identical for any core count.

**Optional semantic candidate retrieval.** The analyzer API's
`ApproximateCandidates` option skips semantic scoring for pairs that share
neither a fingerprint nor one of each side's 16 heaviest TF-IDF terms. It
is lossy: cross-language pairs whose shared vocabulary sits in mid-weight
terms are dropped even above threshold. On real repositories it pruned
under 10% of pairs and saved no time, so the CLI always scores
exhaustively.

**Persistent incremental cache.** The expensive per-file work (split →
tokenize → fingerprint with positions) is persisted to `.codetwin-cache.bin`
in the working directory. File cache keys are
`sha256(absPath ‖ contentHash ‖ patternsHash)` so any of those
changing invalidates the relevant entry automatically. On a warm rerun
unchanged files skip the parsing pipeline.

`--reuse-scores` additionally persists exact candidate-pair scores so
unchanged pairs skip scoring. A changed file, corpus-dependent TF-IDF weight,
scoring parameter, or snippet property invalidates the affected document
identity and recomputes only incident candidate scores. It is off by default:
the table holds one entry per scored pair, so it grows with the square of the
snippet count, and decoding it is single-threaded while scoring runs on every
core. Measured on a 16-core machine:

| Repo | Snippets | Cache with scores | Warm run, scores reused | Warm run, default |
|---|---|---|---|---|
| go-vss | 199 | 1 MB | 0.05 s | 0.08 s |
| codetwin | 1,622 | 40 MB | 0.77 s | 0.53 s |
| disknexus | 5,810 | 508 MB | 10.6 s | 6.6 s |

A run without the flag drops any stored score table, so the cache shrinks
back on the next default run. Add `.codetwin-cache.bin` to your
`.gitignore`. Use `--no-cache` to skip caching entirely or
`--rebuild-cache` to force a fresh build. Persistence is behind the
`cache.Storage` interface; the default CGO-free gob implementation uses a
synced temporary file plus atomic rename and explicit schema validation.

**Live progress.** While the matrix is computing, codetwin prints a
counter to stderr (`comparing snippets: N/M (X%)`). Auto-suppressed
when stderr isn't a TTY so CI logs stay clean. Use `--no-progress` to
force off.

[Back to README](../README.md#performance)

