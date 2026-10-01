#!/usr/bin/env bash
# Copy knowledge/vectors/*.json from a uspace-lab checkout into
# vectors/testdata/ at the checkout's HEAD commit, and record the pin.
#
#   scripts/sync-vectors.sh ../uspace-lab
#
# Refusals (E-04: never report an inference as an observation):
#   - the lab checkout is dirty under knowledge/vectors  -> refuse
#   - a vector file lacks utm_commit, or files disagree  -> refuse
# The copy is a reviewed change in this repository: commit it with the
# reason and, per 00 §6.3, bump the major version when a changed vector
# changes behaviour.
set -euo pipefail

lab="${1:-}"
if [[ -z "$lab" || ! -d "$lab/knowledge/vectors" ]]; then
  echo "usage: $0 <path to uspace-lab checkout>" >&2
  exit 2
fi
here="$(cd "$(dirname "$0")/.." && pwd)"
dst="$here/vectors/testdata"
src="$lab/knowledge/vectors"

if [[ -n "$(git -C "$lab" status --porcelain -- knowledge/vectors)" ]]; then
  echo "refusing: $src has uncommitted changes" >&2
  exit 1
fi
commit="$(git -C "$lab" rev-parse HEAD)"
remote="$(git -C "$lab" remote get-url origin 2>/dev/null || echo unknown)"

utm=""
for f in "$src"/*.json; do
  c="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1],encoding="utf-8")).get("utm_commit",""))' "$f")"
  if [[ -z "$c" ]]; then echo "refusing: $(basename "$f") has no utm_commit" >&2; exit 1; fi
  if [[ -z "$utm" ]]; then utm="$c"; elif [[ "$utm" != "$c" ]]; then
    echo "refusing: utm_commit differs between files ($utm vs $c in $(basename "$f"))" >&2; exit 1
  fi
done

local_files="$(sed -n 's/^local_files *= *//p' "$dst/VERSION" 2>/dev/null || true)"
# A local file the lab now carries is the lab's from here on: it is copied
# like the others and leaves local_files.
still_local=""
for lf in $local_files; do
  if [[ -e "$src/$lf" ]]; then
    echo "upstreamed: $lf is now in the lab and leaves local_files"
  else
    still_local="${still_local:+$still_local }$lf"
  fi
done
local_files="$still_local"
for f in "$dst"/*.json; do
  keep=0
  for lf in $local_files; do [[ "$(basename "$f")" == "$lf" ]] && keep=1; done
  [[ "$keep" == "1" ]] || rm -f "$f"
done
cp "$src"/*.json "$dst"/
# Text-mode lines ("<hash>  <file>") on every platform: a Windows
# sha256sum marks binary mode with "*", which would churn the file.
(cd "$dst" && sha256sum ./*.json | sed -E 's#^([0-9a-f]+) [ *]\./#\1  #' > SHA256SUMS)
cat > "$dst/VERSION" <<EOF
# Pinned source of the vendored knowledge vectors. Written by
# scripts/sync-vectors.sh; never edited by hand. CI compares the files
# against SHA256SUMS always, and against the lab checkout at this commit
# when it can reach it.
uspace_lab_repo = ${remote}
uspace_lab_commit = ${commit}
uspace_lab_path = knowledge/vectors
utm_commit = ${utm}
synced_at = $(date -u +%F)
# Files written in this repository until the lab merges them; excluded
# from the online diff and from the utm_commit check, still in SHA256SUMS.
local_files = ${local_files}
EOF
echo "synced $(ls "$dst"/*.json | wc -l) files from uspace-lab@${commit:0:12} (utm ${utm:0:12})"
echo "now: go test ./vectors/ && update vectors/manifest.go if counts changed"
