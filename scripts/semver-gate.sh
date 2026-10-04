#!/usr/bin/env bash
# The semver gate of spec 00 §6.3 for a pull request (docs/RELEASING.md).
#
#   PR_LABELS="a,b" scripts/semver-gate.sh BASE_REF
#
# BASE_REF is the branch the pull request merges into (origin/main in CI).
# PR_LABELS is the comma-separated list of the pull request's labels; CI
# fills it from github.event.pull_request.labels.
#
# When the pull request touches vectors/testdata/*.json, each changed file
# is classified by comparing the base and head versions by case name:
#
#   behaviour change  a new or removed file, a removed case, an existing
#                     case whose input or expected changed, an existing
#                     case that lost an owner (that owner's vector test
#                     stops running it), or a changed header key other
#                     than description, source and generated (fixtures,
#                     policy, tolerance, units, owners, utm_commit are
#                     what judgements read).
#   additive          cases were added and nothing above changed: new
#                     cases that existing behaviour passes (00 §6.3).
#   editorial         only why, source, description or generated text,
#                     or the order of cases or of a case's owners, changed.
#
# Every changed file needs a CHANGELOG.md line added by this pull request,
# `vectors: <file> (<clause>)`, naming the regulation or standard clause,
# and vectors/testdata/VERSION must pin a different uspace_lab_commit than
# the base (the file came from a lab sync; check-vectors.sh proves the
# bytes match the new pin) unless the file is in local_files.
#
# A behaviour change also needs:
#   - a CHANGELOG heading `## [X.Y.Z]` for a new major relative to the
#     latest release tag merged into the base (before v1.0.0: a new minor,
#     or 1.0.0), not yet tagged, with the file's `vectors:` line under it;
#     the heading may come from an earlier unreleased pull request;
#   - the label `behaviour-change`.
#
# It prints what it decided and why (LESSONS E-04) and exits 0 on pass, 1
# on fail, 2 on a usage or tool error.
set -euo pipefail

if [[ $# -ne 1 || -z "$1" ]]; then
  echo "usage: PR_LABELS=a,b $0 BASE_REF" >&2
  exit 2
fi
base_ref="$1"
label_required="behaviour-change"
root="$(git rev-parse --show-toplevel)"
cd "$root"
command -v jq >/dev/null || { echo "semver-gate: jq is required" >&2; exit 2; }
# jq without carriage returns (a Windows jq writes CRLF).
jqr() { jq "$@" | tr -d '\r'; }

mb="$(git merge-base "$base_ref" HEAD)" || { echo "semver-gate: no merge base with $base_ref" >&2; exit 2; }
head="$(git rev-parse HEAD)"
echo "semver-gate: base $base_ref (merge base ${mb:0:12}), head ${head:0:12}"

mapfile -t changes < <(git diff --name-status --no-renames "$mb" "$head" -- 'vectors/testdata/*.json')
if [[ ${#changes[@]} -eq 0 ]]; then
  echo "decision: PASS - no file under vectors/testdata/*.json changed; nothing to gate"
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
problems=()
fail() { problems+=("$1"); echo "  FAIL: $1"; }

# Labels as a set.
declare -A labels=()
IFS=',' read -r -a label_list <<< "${PR_LABELS:-}"
for l in "${label_list[@]}"; do
  l="${l#"${l%%[![:space:]]*}"}"; l="${l%"${l##*[![:space:]]}"}"
  if [[ -n "$l" ]]; then labels["$l"]=1; fi
done

# Classify each changed file.
compare='
  def body: del(.description, .source, .generated, .cases);
  def bycase: map({key: .name, value: {input, expected}}) | from_entries;
  def owners: map({key: .name, value: (.owner // [] | if type == "array" then . else [.] end | unique)}) | from_entries;
  def lost($a; $b): [$a | keys[] | . as $k | select($b | has($k)) | ($a[$k] - $b[$k]) as $d
    | select($d | length > 0) | "\($k) (\($d | map(tostring) | join(" ")))"];
  $old[0] as $o | $new[0] as $n
  | ($o.cases | bycase) as $oc | ($n.cases | bycase) as $nc
  | ($o | body) as $oh | ($n | body) as $nh
  | ($o.cases | owners) as $oo | ($n.cases | owners) as $no
  | {
      dup: ((($o.cases | length) != ($oc | length)) or (($n.cases | length) != ($nc | length))),
      removed: [$oc | keys[] | . as $k | select($nc | has($k) | not)],
      changed: [$oc | keys[] | . as $k | select(($nc | has($k)) and $nc[$k] != $oc[$k])],
      added: [$nc | keys[] | . as $k | select($oc | has($k) | not)],
      owner_removed: lost($oo; $no),
      header: [($oh + $nh) | keys[] | . as $k | select($oh[$k] != $nh[$k])],
      old_count: ($o.cases | length), new_count: ($n.cases | length)
    }'
list() { local s; s="$(jqr -r "$1 | join(\", \")" <<< "$2")"; echo "${s:-none}"; }

declare -A kind=()
files=()
echo "vector files changed: ${#changes[@]}"
for line in "${changes[@]}"; do
  st="${line%%$'\t'*}"; path="${line#*$'\t'}"; f="$(basename "$path")"
  files+=("$f")
  case "$st" in
    A) kind[$f]=behaviour; echo "  $f: behaviour change (a new vector file)"; continue ;;
    D) kind[$f]=behaviour; echo "  $f: behaviour change (the file was removed)"; continue ;;
  esac
  git show "$mb:$path" > "$tmp/old.json"
  git show "$head:$path" > "$tmp/new.json"
  if ! r="$(jqr -n -c --slurpfile old "$tmp/old.json" --slurpfile new "$tmp/new.json" "$compare" 2>"$tmp/err")"; then
    kind[$f]=behaviour
    echo "  $f: behaviour change (cannot compare by case: $(head -c 200 "$tmp/err" | tr '\n' ' '))"
    continue
  fi
  counts="cases $(jqr .old_count <<< "$r") -> $(jqr .new_count <<< "$r")"
  if [[ "$(jqr .dup <<< "$r")" == "true" ]]; then
    kind[$f]=behaviour; echo "  $f: behaviour change (duplicate case names; cannot compare by name; $counts)"
  elif [[ "$(jqr '(.removed + .changed + .owner_removed + .header) | length' <<< "$r")" != "0" ]]; then
    kind[$f]=behaviour
    echo "  $f: behaviour change ($counts; removed: $(list .removed "$r"); input or expected changed: $(list .changed "$r"); owners removed: $(list .owner_removed "$r"); header keys changed: $(list .header "$r"))"
  elif [[ "$(jqr '.added | length' <<< "$r")" != "0" ]]; then
    kind[$f]=additive
    echo "  $f: additive ($counts; added: $(list .added "$r"); no existing case's input or expected changed)"
  else
    kind[$f]=editorial
    echo "  $f: editorial (same cases, inputs, expected values and owners; only text or order changed)"
  fi
done

# CHANGELOG: lines added by this pull request, and sections at head.
git show "$mb:CHANGELOG.md" > "$tmp/cl.base" 2>/dev/null || : > "$tmp/cl.base"
git show "$head:CHANGELOG.md" > "$tmp/cl.head" 2>/dev/null || : > "$tmp/cl.head"
git diff --no-color -U0 "$mb" "$head" -- CHANGELOG.md | sed -n 's/^+\([^+]\)/\1/p' > "$tmp/cl.added" || true
version_re='^## \[([0-9]+)\.([0-9]+)\.([0-9]+)\]'
# Every line at head, prefixed with the version heading it sits under
# ("Unreleased", or "none" outside a version section).
awk '
  /^## / { sec = "none"; if (match($0, /^## \[[0-9]+\.[0-9]+\.[0-9]+\]/)) sec = substr($0, 5, RLENGTH - 5); else if ($0 ~ /^## \[Unreleased\]/) sec = "Unreleased" }
  { print sec "\t" $0 }' "$tmp/cl.head" > "$tmp/cl.sections"

# The CHANGELOG line for a vector file: "vectors: <file> (<clause>)".
line_re() { printf 'vectors: %s \\([^)]+\\)' "$(sed 's/[.]/[.]/g' <<< "$1")"; }
has_line() { # FILE: a line added by this pull request names FILE with a clause
  grep -qE "$(line_re "$1")" "$tmp/cl.added"
}
line_under() { # FILE VERSION: an added line for FILE sits under heading VERSION
  local re l
  re="$(line_re "$1")"
  while IFS= read -r l; do
    if [[ "$l" =~ $re ]] && grep -qxF -- "$l" "$tmp/cl.added"; then return 0; fi
  done < <(awk -F'\t' -v s="$2" '$1 == s { sub(/^[^\t]*\t/, ""); print }' "$tmp/cl.sections")
  return 1
}

# VERSION pin.
pin() { git show "$1:vectors/testdata/VERSION" 2>/dev/null | sed -n "s/^$2 *= *//p"; }
pin_base="$(pin "$mb" uspace_lab_commit)"; pin_head="$(pin "$head" uspace_lab_commit)"
local_head=" $(pin "$head" local_files) "

echo "checks:"
need_major=0
for f in "${files[@]}"; do
  if [[ "${kind[$f]}" == behaviour ]]; then need_major=1; fi
  if has_line "$f"; then
    echo "  ok: CHANGELOG adds a line 'vectors: $f (<clause>)'"
  else
    fail "CHANGELOG.md adds no line 'vectors: $f (<clause>)' naming the regulation or standard clause"
  fi
  if [[ "$local_head" == *" $f "* ]]; then
    echo "  ok: $f is in local_files; no lab pin required"
  elif [[ -n "$pin_head" && "$pin_head" != "$pin_base" ]]; then
    echo "  ok: $f comes with a new lab pin (uspace_lab_commit ${pin_base:0:12} -> ${pin_head:0:12}); check-vectors.sh proves the bytes"
  else
    fail "$f changed but vectors/testdata/VERSION still pins uspace_lab_commit ${pin_base:0:12}: sync it from the lab (scripts/sync-vectors.sh), never edit it by hand"
  fi
done

if [[ "$need_major" == 1 ]]; then
  # Latest release: the highest vX.Y.Z tag merged into the base, else the
  # highest version heading in the base CHANGELOG, else 0.0.0.
  last="$(git tag --merged "$mb" --list 'v[0-9]*.[0-9]*.[0-9]*' | sed 's/^v//' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n 1 || true)"
  last_src="tag v$last"
  if [[ -z "$last" ]]; then
    last="$(grep -oE "$version_re" "$tmp/cl.base" | sed -E 's/^## \[(.*)\]/\1/' | sort -V | tail -n 1 || true)"
    last_src="base CHANGELOG heading"
    if [[ -z "$last" ]]; then last="0.0.0"; last_src="no release yet"; fi
  fi
  IFS=. read -r lM lm _ <<< "$last"
  if [[ "$lM" -ge 1 ]]; then want="a new major ($((lM + 1)).0.0 or later, X.0.0)"; else want="a new minor before v1 (0.$((lm + 1)).0 or later, X.Y.0) or 1.0.0"; fi
  echo "  behaviour change: last release $last ($last_src); needs $want"
  qualifying=()
  while read -r v; do
    IFS=. read -r M m p <<< "$v"
    if git rev-parse -q --verify "refs/tags/v$v" >/dev/null; then continue; fi
    if [[ "$lM" -ge 1 ]]; then
      if [[ "$M" -gt "$lM" && "$m" == 0 && "$p" == 0 ]]; then qualifying+=("$v"); fi
    elif [[ "$M" -ge 1 ]]; then
      if [[ "$m" == 0 && "$p" == 0 ]]; then qualifying+=("$v"); fi
    elif [[ "$m" -gt "$lm" && "$p" == 0 ]]; then
      qualifying+=("$v")
    fi
  done < <(grep -oE "$version_re" "$tmp/cl.head" | sed -E 's/^## \[(.*)\]/\1/')
  if [[ ${#qualifying[@]} -eq 0 ]]; then
    fail "CHANGELOG.md has no untagged heading '## [X.Y.Z]' for $want"
  else
    echo "  ok: CHANGELOG heading(s) for the next major: ${qualifying[*]}"
    for f in "${files[@]}"; do
      if [[ "${kind[$f]}" != behaviour ]]; then continue; fi
      found=""
      for v in "${qualifying[@]}"; do
        if line_under "$f" "$v"; then found="$v"; break; fi
      done
      if [[ -n "$found" ]]; then
        echo "  ok: 'vectors: $f (<clause>)' is under [$found]"
      else
        fail "the line 'vectors: $f (<clause>)' is not under the heading [${qualifying[0]}] (a behaviour change ships in a new major)"
      fi
    done
  fi
  if [[ -n "${labels[$label_required]:-}" ]]; then
    echo "  ok: the pull request carries the label $label_required"
  else
    fail "the pull request lacks the label $label_required (labels: ${PR_LABELS:-none})"
  fi
else
  echo "  no behaviour change: no new major and no label required"
fi

if [[ ${#problems[@]} -gt 0 ]]; then
  echo "decision: FAIL - ${#problems[@]} problem(s):"
  for p in "${problems[@]}"; do echo "  - $p"; done
  exit 1
fi
if [[ "$need_major" == 1 ]]; then
  echo "decision: PASS - behaviour change with a new major heading, the clause and the label"
else
  echo "decision: PASS - additive or editorial vector change with its CHANGELOG line"
fi
