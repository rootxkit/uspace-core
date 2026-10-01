#!/usr/bin/env bash
# Every file in vectors.Manifest is loaded by a test of the package that
# owns it (G-M1 done-when): the file's name appears in one of the
# package's _test.go files.
#
#   scripts/manifest-coverage.sh            a miss is a warning
#   STRICT=1 scripts/manifest-coverage.sh   a miss fails (release tags)
set -euo pipefail
cd "$(dirname "$0")/.."
GO="${GO:-go}"

entries="$("$GO" run ./scripts/manifest-list | tr -d '\r')"
if [[ -z "$entries" ]]; then
  echo "manifest-coverage: the manifest is empty" >&2
  exit 1
fi
missing=0
total=0
while read -r file pkg; do
  total=$((total + 1))
  if grep -lq "\"$file\"" "$pkg"/*_test.go 2>/dev/null; then
    echo "ok: $file is loaded by a test in $pkg/"
  else
    echo "::warning::$file is not loaded by a test in $pkg/"
    missing=$((missing + 1))
  fi
done <<< "$entries"

if [[ "$missing" -gt 0 ]]; then
  echo "manifest-coverage: $missing of $total manifest files have no vector test"
  if [[ "${STRICT:-0}" == "1" ]]; then exit 1; fi
  exit 0
fi
echo "manifest-coverage: all $total manifest files have a vector test"
