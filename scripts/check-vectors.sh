#!/usr/bin/env bash
# Verify the vendored vectors.
#   1. Offline, always: sha256 of every file matches SHA256SUMS.
#   2. Online, when the lab can be reached: the files are byte-identical
#      to knowledge/vectors at the commit recorded in VERSION.
# Step 2 reports "unverified" rather than "ok" when the lab cannot be
# fetched (E-04: an unanswerable question is not a pass). Set
# REQUIRE_LAB=1 to make that a failure (the main-branch CI does).
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
dst="$here/vectors/testdata"

echo "== offline: SHA256SUMS"
(cd "$dst" && sha256sum --check --strict SHA256SUMS)

commit="$(sed -n 's/^uspace_lab_commit *= *//p' "$dst/VERSION")"
repo="$(sed -n 's/^uspace_lab_repo *= *//p' "$dst/VERSION")"
path="$(sed -n 's/^uspace_lab_path *= *//p' "$dst/VERSION")"
local_files="$(sed -n 's/^local_files *= *//p' "$dst/VERSION")"
excludes=(--exclude=VERSION --exclude=SHA256SUMS)
for lf in $local_files; do excludes+=("--exclude=$lf"); done
# The token reaches git as an HTTP header for this repository only,
# through the environment: never in the remote URL, the command line or
# the clone's .git/config, so neither git's messages nor a process
# listing carry it. Whatever git prints is redacted anyway.
basic=""
if [[ -n "${LAB_READ_TOKEN:-}" ]]; then
  basic="$(printf 'x-access-token:%s' "$LAB_READ_TOKEN" | base64 | tr -d '\r\n')"
  export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0="http.${repo}.extraheader" \
    GIT_CONFIG_VALUE_0="AUTHORIZATION: basic $basic"
fi
redact() {
  local s="$1"
  if [[ -n "${LAB_READ_TOKEN:-}" ]]; then
    s="${s//"$LAB_READ_TOKEN"/<redacted>}"
    s="${s//"$basic"/<redacted>}"
  fi
  printf '%s' "$s"
}

echo "== online: uspace-lab@${commit:0:12} $path"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
if ! ( git -C "$tmp" init -q && git -C "$tmp" remote add origin "$repo" \
       && git -C "$tmp" fetch -q --depth 1 origin "$commit" \
       && git -C "$tmp" checkout -q FETCH_HEAD -- "$path" ) 2>"$tmp/err"; then
  err="$(redact "$(tr '\n' ' ' < "$tmp/err")")"
  echo "unverified: could not fetch uspace-lab@${commit:0:12} (${err:0:200})"
  if [[ "${REQUIRE_LAB:-0}" == "1" ]]; then exit 1; fi
  exit 0
fi
if diff -r --strip-trailing-cr "${excludes[@]}" "$tmp/$path" "$dst"; then
  echo "ok: vendored vectors match uspace-lab@${commit:0:12}"
else
  echo "mismatch: vendored vectors differ from uspace-lab@${commit:0:12}; run scripts/sync-vectors.sh" >&2
  exit 1
fi
