#!/usr/bin/env bash
# Fetch GeographicLib's EGM96 and EGM2008 geoid grids into DIR, so the
# GeographicLib reference cases in vectors/testdata/terrain_geoid.json run
# against the real grids (USPACE_GEOID_DIR=DIR) instead of being skipped.
#
#   scripts/fetch-geoid.sh DIR
#
# Both the downloaded archive and the extracted .pgm are pinned by SHA-256:
# a changed download, or a corrupted or tampered cache entry, fails here
# instead of quietly moving every Remote ID altitude up or down. A grid
# already in DIR with the pinned hash is kept, so a CI cache hit makes no
# network request at all.
#
# Source: GeographicLib's distribution of the NGA models (public domain).
# Needs curl, tar with bzip2, and sha256sum.
set -euo pipefail

dir="${1:?usage: scripts/fetch-geoid.sh DIR}"
base=https://downloads.sourceforge.net/project/geographiclib/geoids-distrib

# model  archive sha256  pgm sha256
grids="
egm96-15 8b1ebad1ebae0a045502d0edb9cc51553da1d3914f01e07470c11b3bed75048e 2a12f13b6df65cdea52432c7fa1b43f34b007148eb817bf032af5af710905caa
egm2008-2_5 d602e13446a4a4a23f39aecfe6a2a0760a1bc6c1b497482c2ebc9f7d513be699 fab040a55dfabe782be89a89b2ba7e4a73183513a9813e24a3f80e7b6ed61dbf
"

for tool in curl tar sha256sum; do
  command -v "$tool" >/dev/null || { echo "fetch-geoid: $tool not found" >&2; exit 1; }
done

sha() { sha256sum "$1" | cut -d' ' -f1; }

mkdir -p "$dir"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

while read -r model archive_sha pgm_sha; do
  [ -n "$model" ] || continue
  pgm="$dir/$model.pgm"
  if [ -f "$pgm" ]; then
    if [ "$(sha "$pgm")" = "$pgm_sha" ]; then
      echo "fetch-geoid: $model.pgm present, sha256 ok"
      continue
    fi
    echo "fetch-geoid: $model.pgm has the wrong sha256; fetching again" >&2
    rm -f "$pgm"
  fi
  curl -fsSL --retry 3 --retry-delay 5 --max-time 600 \
    -o "$work/$model.tar.bz2" "$base/$model.tar.bz2"
  got="$(sha "$work/$model.tar.bz2")"
  if [ "$got" != "$archive_sha" ]; then
    echo "fetch-geoid: $model archive sha256 $got, expected $archive_sha" >&2
    exit 1
  fi
  tar -xjf "$work/$model.tar.bz2" -C "$work"
  found="$(find "$work" -name "$model.pgm" -print -quit)"
  [ -n "$found" ] || { echo "fetch-geoid: $model.pgm not in the archive" >&2; exit 1; }
  got="$(sha "$found")"
  if [ "$got" != "$pgm_sha" ]; then
    echo "fetch-geoid: $model.pgm sha256 $got, expected $pgm_sha" >&2
    exit 1
  fi
  mv "$found" "$pgm.part"
  mv "$pgm.part" "$pgm"
  echo "fetch-geoid: wrote $pgm"
done <<< "$grids"
