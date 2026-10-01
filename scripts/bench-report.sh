#!/usr/bin/env bash
# Print a Markdown table of benchmark results beside the ns/op targets of
# docs/PLAN.md section 8.6. Reported, never gated: a miss prints a
# warning line. Targets are matched by benchmark name prefix.
#   scripts/bench-report.sh bench.txt
set -euo pipefail
file="${1:-bench.txt}"
here="$(cd "$(dirname "$0")/.." && pwd)"
targets="$here/docs/bench-targets.txt"

echo "## Benchmarks"
echo
echo "| benchmark | ns/op | target ns/op | status |"
echo "|---|---:|---:|---|"
while read -r name iters ns unit rest; do
  [[ "$name" == Benchmark* ]] || continue
  [[ "$unit" == "ns/op" ]] || continue
  short="${name%-*}"
  target=""
  if [[ -f "$targets" ]]; then
    target="$(awk -v n="$short" '$1!~/^#/ && index(n,$1)==1 {print $2; exit}' "$targets")"
  fi
  status="-"
  if [[ -n "$target" ]]; then
    if awk -v a="$ns" -v b="$target" 'BEGIN{exit !(a<=b)}'; then status="ok"; else status="over target"; fi
  fi
  echo "| $short | $ns | ${target:--} | $status |"
done < "$file"
