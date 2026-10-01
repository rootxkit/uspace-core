#!/usr/bin/env bash
# Run every Fuzz* target in the module for a short time. Go accepts one
# -fuzz pattern per package invocation, so each target gets its own run.
#   FUZZTIME=10s scripts/fuzz-smoke.sh
set -euo pipefail
cd "$(dirname "$0")/.."
GO="${GO:-go}"
fuzztime="${FUZZTIME:-10s}"
status=0
found=0
for pkg in $($GO list ./...); do
  targets="$($GO test -list '^Fuzz' "$pkg" 2>/dev/null | grep '^Fuzz' || true)"
  for t in $targets; do
    found=1
    echo "== $pkg $t ($fuzztime)"
    if ! $GO test -run '^$' -fuzz "^${t}\$" -fuzztime "$fuzztime" "$pkg"; then
      status=1
    fi
  done
done
if [[ "$found" == "0" ]]; then
  echo "no fuzz targets found yet (the decoders of WP-3, WP-5, WP-11 add them)"
fi
exit $status
