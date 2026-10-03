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
  # A package whose tests do not build lists nothing; that is a failure,
  # not a package without fuzz targets.
  if ! list="$($GO test -list '^Fuzz' "$pkg" 2>&1)"; then
    echo "$list"
    echo "fuzz-smoke: go test -list failed for $pkg"
    exit 1
  fi
  targets="$(grep '^Fuzz' <<<"$list" || true)"
  for t in $targets; do
    found=1
    echo "== $pkg $t ($fuzztime)"
    if ! $GO test -run '^$' -fuzz "^${t}\$" -fuzztime "$fuzztime" "$pkg"; then
      status=1
    fi
  done
done
# The module has Fuzz targets (auth, geodesy, the decoders), so finding
# none means the listing is broken, not that there is nothing to run.
if [[ "$found" == "0" ]]; then
  echo "fuzz-smoke: no fuzz target found; go list or go test -list matched nothing"
  exit 1
fi
exit $status
