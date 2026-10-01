#!/usr/bin/env bash
# Regenerate the f3411 and f3548 types in a scratch copy and diff them
# against the committed files. Needs the network (go generate fetches the
# pinned OpenAPI files); when they cannot be reached it says so and exits
# 0 as "skipped", never as "ok" (E-04). Not run by CI, which never runs go
# generate (owner decision on PR #16); the offline check is
# TestGeneratedTypesUnchanged.
#   scripts/check-generated.sh
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
GO="${GO:-go}"
url="$(sed -n 's/^spec_url *= *//p' "$here/f3411/SOURCE")"
if ! curl -sSfL --max-time 20 -o /dev/null "$url" 2>/dev/null; then
  echo "skipped: cannot reach $url (offline?); the generated types were not re-checked"
  exit 0
fi
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
cp -R "$here/." "$tmp/"
(cd "$tmp" && $GO generate ./f3411 ./f3548)
status=0
for f in f3411/types.gen.go f3411/SOURCE f3548/types.gen.go f3548/SOURCE; do
  if ! diff -u --strip-trailing-cr "$here/$f" "$tmp/$f"; then
    echo "differs after go generate: $f" >&2
    status=1
  fi
done
if [[ "$status" == "0" ]]; then
  echo "ok: go generate reproduces the committed f3411 and f3548 types"
fi
exit $status
