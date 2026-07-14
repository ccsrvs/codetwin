---
name: codetwin
description: >
  Run codetwin — a multi-language code similarity and duplicate detection CLI — via bash_tool.
  Use this skill whenever the user asks to find duplicate code, detect clones, identify refactoring
  opportunities, check for similar functions across files, or scan a codebase for copy-paste across
  Go, JavaScript, TypeScript, Python, Java, Rust, or Elixir. Also trigger when the user says things
  like "find repeated code", "what can be refactored", "check for duplicates", "scan my project
  for similar functions", or "watch/track clone drift" (baseline snapshots + CI drift gating via
  --update-baseline / --baseline).
---

# codetwin Skill

`codetwin` is a CLI that finds duplicate and structurally similar code across
Go, JavaScript/TypeScript, Python, Java, Rust, and Elixir. Function-level
chunks (plus class-kind chunks for Python/Java/JS classes, Elixir
defmodules, Rust impl blocks, and Go struct+methodset groups, matched
class↔class only),
structural (Winnowing/Jaccard) + semantic (TF-IDF/cosine) scoring,
DBSCAN clusters.

## How to use this skill

The full usage guide is **embedded in the binary** to keep this skill file
small. Fetch it on demand instead of loading it up-front:

```bash
codetwin --help     # one-line description of every flag
codetwin --skill    # full skill guide: flags, recipes, scoring, troubleshooting
codetwin --guide    # interpretation guide: what scores mean, pairs vs clusters
```

Run those before you do anything non-trivial — they cover sorting, the
`.codetwin.json` config, ignore patterns, the cache, score interpretation,
and JSON output.

## Locating the binary

```bash
which codetwin || ls ./codetwin 2>/dev/null
```

If neither finds it, build from the codetwin repo:

```bash
cd <path-to-codetwin-repo> && make build        # produces ./codetwin
# or
go install github.com/ccsrvs/codetwin/cmd/codetwin@latest
```

## Quick start

```bash
codetwin --threshold 0.40 <path>            # default scan
codetwin --preview --threshold 0.40 <path>  # with line-numbered previews
codetwin --json --threshold 0.40 <path>     # JSON for piping into jq
codetwin ../svc-a ../svc-b ../svc-c         # cross-repo scan: >=2 directory
                                            # roots => each root is a "repo";
                                            # --cross-repo-only keeps only
                                            # repo-spanning findings
```

For anything beyond that — sort modes, limits, ignore rules, the
`--min-confidence-lines` short-snippet dampener, troubleshooting — run
`codetwin --skill`.
