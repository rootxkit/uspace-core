#!/usr/bin/env bash
# The release gate a v* tag must pass before its GitHub release is made
# (docs/RELEASING.md). CI runs it in the `tag` job of ci.yml.
#
#   scripts/release-check.sh vX.Y.Z               verify
#   scripts/release-check.sh --notes vX.Y.Z       print the CHANGELOG section
#   scripts/release-check.sh --no-vectors vX.Y.Z  verify without Go or the
#                                                 network (fixture tests only)
#
# Checks, each printed with its result (LESSONS E-04):
#   - the tag is vMAJOR.MINOR.PATCH;
#   - the first version heading of CHANGELOG.md is `## [X.Y.Z]` for this
#     tag, does not say "unreleased", and its section is not empty;
#   - nothing is left under `## [Unreleased]`;
#   - the go.mod module path carries /vN exactly when N >= 2;
#   - from v1.0.0, vectors/testdata/VERSION has an empty local_files;
#   - when the tag exists, it points at HEAD and HEAD is on origin/main or
#     origin/release/vN;
#   - every manifest file is loaded by a vector test (STRICT), and the
#     vendored vectors match uspace-lab at the pin (REQUIRE_LAB=1).
# `go test ./...` and the vector tests run in the jobs the tag job needs.
set -euo pipefail

mode=check
vectors=1
while [[ $# -gt 1 ]]; do
  case "$1" in
    --notes) mode=notes ;;
    --no-vectors) vectors=0 ;;
    *) echo "usage: $0 [--notes|--no-vectors] vX.Y.Z" >&2; exit 2 ;;
  esac
  shift
done
tag="${1:-}"
if [[ ! "$tag" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  echo "release-check: '$tag' is not a release tag vMAJOR.MINOR.PATCH" >&2
  exit 2
fi
ver="${tag#v}"
major="${BASH_REMATCH[1]}"
root="$(git rev-parse --show-toplevel)"
cd "$root"
cl=CHANGELOG.md

# The lines under `## [X.Y.Z]` up to the next level-2 heading.
section() {
  awk -v h="## [$1]" '
    found && /^## / { exit }
    found { print }
    index($0, h) == 1 { found = 1 }' "$cl"
}
nonblank() { grep -q '[^[:space:]]'; }

if [[ "$mode" == notes ]]; then
  body="$(section "$ver")"
  if ! nonblank <<< "$body"; then
    echo "release-check: CHANGELOG.md has no section for [$ver]" >&2
    exit 1
  fi
  # Trim leading and trailing blank lines.
  awk 'NF { for (; blank > 0; blank--) if (started) print ""; print; started = 1; next } { blank++ }' <<< "$body"
  exit 0
fi

problems=()
ok() { echo "  ok: $1"; }
fail() { problems+=("$1"); echo "  FAIL: $1"; }
echo "release-check: $tag at $(git rev-parse --short HEAD)"

top_line="$(grep -E '^## \[[0-9]+\.[0-9]+\.[0-9]+\]' "$cl" | head -n 1 || true)"
top="$(sed -E 's/^## \[([^]]*)\].*/\1/' <<< "$top_line")"
if [[ "$top" == "$ver" ]]; then
  ok "the top CHANGELOG version heading is [$ver]"
else
  fail "the top CHANGELOG version heading is [${top:-none}], not [$ver]"
fi
if [[ "${top_line,,}" == *unreleased* ]]; then
  fail "the heading '$top_line' still says unreleased"
fi
if section "$ver" | nonblank; then
  ok "the [$ver] section has entries"
else
  fail "the [$ver] section is empty or missing"
fi
if section Unreleased | nonblank; then
  fail "entries are left under [Unreleased]; move them under [$ver]"
else
  ok "nothing is left under [Unreleased]"
fi

module="$(sed -n 's/^module[[:space:]]*//p' go.mod | tr -d '\r')"
if [[ "$major" -ge 2 ]]; then
  if [[ "$module" == */v"$major" ]]; then ok "module path $module ends in /v$major"
  else fail "module path $module must end in /v$major for a v$major tag (Go major-version rule)"; fi
else
  if [[ "$module" =~ /v[0-9]+$ ]]; then fail "module path $module has a major suffix for a v$major tag"
  else ok "module path $module has no major suffix"; fi
fi

local_files="$(sed -n 's/^local_files *= *//p' vectors/testdata/VERSION 2>/dev/null | tr -d '\r' | xargs || true)"
if [[ "$major" -ge 1 ]]; then
  if [[ -z "$local_files" ]]; then ok "local_files is empty: every vector comes from the lab"
  else fail "local_files lists '$local_files': from v1 every vector comes from uspace-lab"; fi
else
  ok "local_files '${local_files}' allowed before v1"
fi

if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  tc="$(git rev-parse "$tag^{commit}")"
  if [[ "$tc" == "$(git rev-parse HEAD)" ]]; then ok "$tag points at HEAD"
  else fail "$tag points at ${tc:0:12}, not at HEAD"; fi
  on=""
  for b in origin/main "origin/release/v$major"; do
    if git rev-parse -q --verify "refs/remotes/$b" >/dev/null && git merge-base --is-ancestor "$tc" "$b"; then on="$b"; break; fi
  done
  if [[ -n "$on" ]]; then ok "$tag is on $on"
  else fail "$tag is on neither origin/main nor origin/release/v$major"; fi
else
  echo "  note: tag $tag does not exist here; checked the working tree"
fi

if [[ "$vectors" == 1 ]]; then
  if STRICT=1 scripts/manifest-coverage.sh; then ok "every manifest file has a vector test"
  else fail "a manifest file has no vector test"; fi
  if REQUIRE_LAB=1 scripts/check-vectors.sh; then ok "vendored vectors match the lab pin"
  else fail "vendored vectors do not match the lab pin"; fi
else
  echo "  skipped: manifest coverage and the lab diff (--no-vectors)"
fi

if [[ ${#problems[@]} -gt 0 ]]; then
  echo "decision: FAIL - $tag is not releasable (${#problems[@]} problem(s)):"
  for p in "${problems[@]}"; do echo "  - $p"; done
  exit 1
fi
echo "decision: PASS - $tag is releasable"
