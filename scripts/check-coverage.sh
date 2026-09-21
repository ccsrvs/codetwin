#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <go-cover-func-report> <minimum-percent>" >&2
  exit 2
fi

report=$1
minimum=$2

if [[ ! -f "$report" ]]; then
  echo "coverage report not found: $report" >&2
  exit 2
fi

total=$(awk '$1 == "total:" { print $3; found = 1 } END { if (!found) exit 1 }' "$report") || {
  echo "total coverage not found in $report" >&2
  exit 1
}

if [[ ! $total =~ ^[0-9]+([.][0-9]+)?%$ ]]; then
  echo "invalid total coverage: $total" >&2
  exit 1
fi
if [[ ! $minimum =~ ^[0-9]+([.][0-9]+)?$ ]]; then
  echo "invalid minimum coverage: $minimum" >&2
  exit 2
fi

percentage=${total%%%}
if awk -v actual="$percentage" -v floor="$minimum" 'BEGIN { exit !(actual + 0 >= floor + 0) }'; then
  echo "coverage gate passed: ${percentage}% meets ${minimum}% minimum"
else
  echo "coverage gate failed: ${percentage}% is below ${minimum}% minimum" >&2
  exit 1
fi
