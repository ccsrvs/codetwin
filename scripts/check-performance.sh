#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <go-benchmark-output>" >&2
  exit 2
fi

report=$1
if [[ ! -f "$report" ]]; then
  echo "benchmark report not found: $report" >&2
  exit 2
fi

# Ratios compare each path with the dense reference in the same run, so
# the gate remains meaningful across different GitHub-hosted runner hardware.
#
# "sparse" is the default pipeline. It scores every comparable pair, as the
# dense reference does, so it cannot be much faster; it must stay close in
# time (measured ~1.1x) while using clearly less memory (measured ~0.6x).
# "incremental" is the opt-in --reuse-scores path. A cold run persists one
# score per pair and is inherently expensive (measured ~3.3x time, ~14.5x
# memory), which is why it is opt-in; its bounds only catch it getting
# worse. A warm run must still beat recomputing from scratch.
awk \
  -v sparse_time_max="${SPARSE_TIME_MAX:-1.25}" \
  -v sparse_memory_max="${SPARSE_MEMORY_MAX:-0.85}" \
  -v cold_time_max="${COLD_TIME_MAX:-4.00}" \
  -v cold_memory_max="${COLD_MEMORY_MAX:-17.0}" \
  -v warm_time_max="${WARM_TIME_MAX:-0.75}" \
  -v warm_memory_max="${WARM_MEMORY_MAX:-0.85}" '
function record(name, time, memory) {
  times[name] += time
  memories[name] += memory
  samples[name]++
}

/^BenchmarkSimilarityStoragePipeline\/(dense|sparse|incremental-cold|incremental-warm)(-[0-9]+)?[[:space:]]/ {
  name = $1
  sub(/^BenchmarkSimilarityStoragePipeline\//, "", name)
  sub(/-[0-9]+$/, "", name)
  time = memory = ""
  for (i = 2; i <= NF; i++) {
    if ($i == "ns/op") time = $(i - 1)
    if ($i == "B/op") memory = $(i - 1)
  }
  if (time != "" && memory != "") record(name, time, memory)
}

function check(label, actual, maximum) {
  printf "%s ratio: %.3f (maximum %.3f)\n", label, actual, maximum
  if (actual > maximum) failed = 1
}

END {
  required[1] = "dense"
  required[2] = "sparse"
  required[3] = "incremental-cold"
  required[4] = "incremental-warm"
  missing = ""
  for (i = 1; i <= 4; i++) {
    name = required[i]
    if (!samples[name]) missing = missing (missing == "" ? "" : ", ") name
  }
  if (missing != "") {
    print "missing benchmark metrics: " missing > "/dev/stderr"
    exit 1
  }

  for (i = 1; i <= 4; i++) {
    name = required[i]
    average_time[name] = times[name] / samples[name]
    average_memory[name] = memories[name] / samples[name]
  }
  if (average_time["dense"] <= 0 || average_memory["dense"] <= 0) {
    print "invalid dense benchmark metrics" > "/dev/stderr"
    exit 1
  }

  check("sparse time", average_time["sparse"] / average_time["dense"], sparse_time_max)
  check("sparse memory", average_memory["sparse"] / average_memory["dense"], sparse_memory_max)
  check("incremental-cold time", average_time["incremental-cold"] / average_time["dense"], cold_time_max)
  check("incremental-cold memory", average_memory["incremental-cold"] / average_memory["dense"], cold_memory_max)
  check("incremental-warm time", average_time["incremental-warm"] / average_time["dense"], warm_time_max)
  check("incremental-warm memory", average_memory["incremental-warm"] / average_memory["dense"], warm_memory_max)

  if (failed) {
    print "performance gate failed" > "/dev/stderr"
    exit 1
  }
  print "performance gate passed"
}
' "$report"
