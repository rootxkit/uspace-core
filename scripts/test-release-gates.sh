#!/usr/bin/env bash
# Fixture tests for scripts/semver-gate.sh and scripts/release-check.sh.
# Each case builds a small git repository in a temp dir, makes one change
# on a branch, runs the gate and checks both the exit status and the
# reason it prints. Every failing case has a passing twin (LESSONS E-01).
#
#   scripts/test-release-gates.sh
#
# Needs bash, git and jq; no network and no Go.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
gate="$here/semver-gate.sh"
release="$here/release-check.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
passed=0
failed=0
repo=""

g() {
  git -C "$repo" -c user.name=fixture -c user.email=fixture@example.invalid \
    -c core.autocrlf=false -c commit.gpgsign=false -c tag.gpgsign=false "$@"
}

# vec FILE NAME=EXPECTED...: a vector file; TOL and WHY vary the header and
# text, OWN_<name> the owner list of one case (default ["authority", "ussp"]).
vec() {
  local out="$repo/vectors/testdata/$1" sep="" c own
  shift
  {
    printf '{\n  "description": "fixture",\n  "tolerance": {"dist_m": %s},\n  "cases": [' "${TOL:-0.1}"
    for c in "$@"; do
      own="OWN_${c%%=*}"
      printf '%s\n    {"name": "%s", "owner": %s, "input": {"x": 1}, "expected": %s, "why": "%s"}' \
        "$sep" "${c%%=*}" "${!own:-[\"authority\", \"ussp\"]}" "${c#*=}" "${WHY:-why}"
      sep=","
    done
    printf '\n  ]\n}\n'
  } > "$out"
}
pin() { printf 'uspace_lab_commit = %s\nlocal_files = %s\n' "$1" "${2:-}" > "$repo/vectors/testdata/VERSION"; }
changelog() { printf '%s\n' "# Changelog" "" "$@" > "$repo/CHANGELOG.md"; }

# setup NAME RELEASE [EXTRA HEADING LINES...]: a base on main with
# cpa.json (cases a, b), the lab pin lab1, a CHANGELOG with [Unreleased]
# and the release heading, tagged vRELEASE; then branch pr.
setup() {
  repo="$work/$1"
  local rel="$2"
  shift 2
  mkdir -p "$repo/vectors/testdata"
  git init -q "$repo"
  g checkout -q -b main
  vec cpa.json a=1 b=2
  pin lab1
  changelog "## [Unreleased]" "" "$@" "## [$rel] - 2026-10-01" "" "- first release"
  g add -A
  g commit -q -m base
  g tag -a "v$rel" -m "v$rel"
  g checkout -q -b pr
}
commit() { g add -A; g commit -q -m change; }

# expect pass|fail PHRASE [LABELS]: run the gate on the current fixture.
expect() {
  local want="$1" phrase="$2" labels="${3:-}" out rc=0 name
  name="$(basename "$repo")"
  out="$(cd "$repo" && PR_LABELS="$labels" "$gate" main 2>&1)" || rc=$?
  check "$name" "$want" "$rc" "$phrase" "$out"
}

# check NAME WANT RC PHRASE OUTPUT
check() {
  local ok=1
  case "$2" in
    pass) [[ "$3" == 0 ]] || ok=0 ;;
    fail) [[ "$3" == 1 ]] || ok=0 ;;
  esac
  grep -qF -- "$4" <<< "$5" || ok=0
  if [[ "$ok" == 1 ]]; then
    passed=$((passed + 1))
    echo "ok   $1: $2 ($4)"
  else
    failed=$((failed + 1))
    echo "FAIL $1: wanted $2 with '$4', got exit $3:"
    sed 's/^/     | /' <<< "$5"
  fi
}

major_changelog() { # the heading [2.0.0] with the clause line, above [1.0.0]
  changelog "## [Unreleased]" "" "## [2.0.0]" "" \
    "- vectors: cpa.json (Regulation (EU) 2021/664 Art. 3(4))" "" \
    "## [1.0.0] - 2026-10-01" "" "- first release"
}

echo "== semver-gate"

# No vector change: nothing to gate.
setup no-vector-change 1.0.0
echo "readme" > "$repo/README.md"; commit
expect pass "nothing to gate"

# A changed expected value with no major heading fails; with the heading,
# the clause line under it, the label and a new pin it passes.
setup major-without-heading 1.0.0
vec cpa.json a=1 b=3; pin lab2
changelog "## [Unreleased]" "" "- vectors: cpa.json (ED-269 §2)" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect fail "no untagged heading '## [X.Y.Z]' for a new major" behaviour-change

setup major-with-heading 1.0.0
vec cpa.json a=1 b=3; pin lab2; major_changelog; commit
expect pass "behaviour change with a new major heading" behaviour-change
expect pass "input or expected changed: b" behaviour-change
expect pass "carries the label behaviour-change" "documentation, behaviour-change"

# The label is required for a behaviour change.
setup major-without-label 1.0.0
vec cpa.json a=1 b=3; pin lab2; major_changelog; commit
expect fail "lacks the label behaviour-change" "documentation"

# A stale VERSION pin fails; the same change listed in local_files passes.
setup major-stale-version 1.0.0
vec cpa.json a=1 b=3; major_changelog; commit
expect fail "still pins uspace_lab_commit lab1" behaviour-change

setup major-local-file 1.0.0
vec cpa.json a=1 b=3; pin lab1 cpa.json; major_changelog; commit
expect pass "cpa.json is in local_files" behaviour-change

# Added cases are additive: a CHANGELOG line, no major, no label.
setup additive-with-line 1.0.0
vec cpa.json a=1 b=2 c=4; pin lab2
changelog "## [Unreleased]" "" "- vectors: cpa.json (ASTM F3411-22a §5.4)" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect pass "additive (cases 2 -> 3; added: c; no existing case's input or expected changed)"
expect pass "no new major and no label required"

setup additive-without-line 1.0.0
vec cpa.json a=1 b=2 c=4; pin lab2; commit
expect fail "adds no line 'vectors: cpa.json (<clause>)'"

# A line without a clause does not count.
setup additive-without-clause 1.0.0
vec cpa.json a=1 b=2 c=4; pin lab2
changelog "## [Unreleased]" "" "- vectors: cpa.json" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect fail "adds no line 'vectors: cpa.json (<clause>)'"

# A removed case is a behaviour change, even with the cases count smaller.
setup removed-case 1.0.0
vec cpa.json a=1; pin lab2
changelog "## [Unreleased]" "" "- vectors: cpa.json (ED-269 §2)" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect fail "removed: b"

# Added cases plus one changed expected value: behaviour change, not additive.
setup added-and-changed 1.0.0
vec cpa.json a=1 b=5 c=4; pin lab2
changelog "## [Unreleased]" "" "- vectors: cpa.json (ED-269 §2)" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect fail "input or expected changed: b"

# A header key that judgements read (tolerance) is a behaviour change.
setup header-change 1.0.0
TOL=0.2 vec cpa.json a=1 b=2; pin lab2
changelog "## [Unreleased]" "" "- vectors: cpa.json (ED-269 §2)" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect fail "header keys changed: tolerance"

# Only the why text changed: editorial, a line and a pin suffice.
setup editorial 1.0.0
WHY=reworded vec cpa.json a=1 b=2; pin lab2
changelog "## [Unreleased]" "" "- vectors: cpa.json (LESSONS C-01)" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect pass "cpa.json: editorial"

# Removing an owner from a case is a behaviour change: that owner's
# vector test stops running the case. Reordering the owners is not.
setup owner-removed 1.0.0
OWN_b='["authority"]' vec cpa.json a=1 b=2; pin lab2
changelog "## [Unreleased]" "" "- vectors: cpa.json (ED-269 §2)" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect fail "owners removed: b (ussp)"
expect fail "lacks the label behaviour-change"

setup owner-removed-major 1.0.0
OWN_b='["authority"]' vec cpa.json a=1 b=2; pin lab2; major_changelog; commit
expect pass "behaviour change with a new major heading" behaviour-change

setup owner-reordered 1.0.0
OWN_b='["ussp", "authority"]' vec cpa.json a=1 b=2; pin lab2
changelog "## [Unreleased]" "" "- vectors: cpa.json (LESSONS C-01)" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect pass "cpa.json: editorial"

# A new vector file is a behaviour change.
setup new-file 1.0.0
vec geodesy.json g=1; pin lab2
changelog "## [Unreleased]" "" "## [2.0.0]" "" "- vectors: geodesy.json (EUROCAE ED-269 §3)" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect pass "geodesy.json: behaviour change (a new vector file)" behaviour-change

# The line must sit under the new major, not under [Unreleased].
setup line-in-wrong-section 1.0.0
vec cpa.json a=1 b=3; pin lab2
changelog "## [Unreleased]" "" "- vectors: cpa.json (ED-269 §2)" "" "## [2.0.0]" "" "- other" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect fail "is not under the heading [2.0.0]" behaviour-change

# A heading that is already tagged is not a new major.
setup heading-already-released 1.0.0
vec cpa.json a=1 b=3; pin lab2
changelog "## [Unreleased]" "" "## [1.0.0] - 2026-10-01" "" "- first release" "- vectors: cpa.json (ED-269 §2)"
commit
expect fail "no untagged heading" behaviour-change

# An unreleased [2.0.0] from an earlier pull request takes the next change.
setup major-heading-from-base 1.0.0 "## [2.0.0]" "" "- vectors: zones.json (ED-269 §4)" ""
vec cpa.json a=1 b=3; pin lab2
changelog "## [Unreleased]" "" "## [2.0.0]" "" "- vectors: zones.json (ED-269 §4)" \
  "- vectors: cpa.json (ED-269 §2)" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect pass "is under [2.0.0]" behaviour-change

# Before v1 a behaviour change is a new minor; a patch is not enough.
setup pre-v1-minor 0.2.0
vec cpa.json a=1 b=3; pin lab2
changelog "## [Unreleased]" "" "## [0.3.0]" "" "- vectors: cpa.json (ED-269 §2)" "" "## [0.2.0] - 2026-10-01" "" "- first release"
commit
expect pass "needs a new minor before v1" behaviour-change

setup pre-v1-patch 0.2.0
vec cpa.json a=1 b=3; pin lab2
changelog "## [Unreleased]" "" "## [0.2.1]" "" "- vectors: cpa.json (ED-269 §2)" "" "## [0.2.0] - 2026-10-01" "" "- first release"
commit
expect fail "no untagged heading '## [X.Y.Z]' for a new minor before v1" behaviour-change

# A file that is not JSON cannot be compared by case: behaviour change.
setup not-json 1.0.0
echo "not json" > "$repo/vectors/testdata/cpa.json"; pin lab2
changelog "## [Unreleased]" "" "- vectors: cpa.json (ED-269 §2)" "" "## [1.0.0] - 2026-10-01" "" "- first release"
commit
expect fail "cannot compare by case"

echo "== release-check"

# rcheck NAME WANT PHRASE TAG [--notes]: run release-check on the fixture.
rcheck() {
  local name="$1" want="$2" phrase="$3" tag="$4" mode="${5:---no-vectors}" out rc=0
  out="$(cd "$repo" && "$release" "$mode" "$tag" 2>&1)" || rc=$?
  check "$name" "$want" "$rc" "$phrase" "$out"
}
release_repo() { # release_repo NAME MODULE LOCAL_FILES CHANGELOG-LINES...
  repo="$work/$1"
  local module="$2" lf="$3"
  shift 3
  mkdir -p "$repo/vectors/testdata"
  git init -q "$repo"
  printf 'module %s\n\ngo 1.27\n' "$module" > "$repo/go.mod"
  pin lab1 "$lf"
  changelog "$@"
  g add -A
  g commit -q -m release
}
released=("## [Unreleased]" "" "## [1.0.0] (G-M3)" "" "- API declared stable" "" "## [0.2.0] - 2026-10-01" "" "- older")

release_repo top-heading github.com/x/m "" "${released[@]}"
rcheck top-heading pass "is releasable" v1.0.0
rcheck top-heading-mismatch fail "top CHANGELOG version heading is [1.0.0], not [1.0.1]" v1.0.1
rcheck top-heading-notes pass "- API declared stable" v1.0.0 --notes

release_repo heading-unreleased github.com/x/m "" "## [1.0.0] - unreleased" "" "- x"
rcheck heading-unreleased fail "still says unreleased" v1.0.0

release_repo unreleased-entries github.com/x/m "" "## [Unreleased]" "" "- pending" "" "## [1.0.0]" "" "- x"
rcheck unreleased-entries fail "entries are left under [Unreleased]" v1.0.0

release_repo empty-section github.com/x/m "" "## [1.0.0]" "" "## [0.2.0]" "" "- x"
rcheck empty-section fail "the [1.0.0] section is empty" v1.0.0

release_repo local-files-v1 github.com/x/m "ed318_roundtrip.json" "${released[@]}"
rcheck local-files-v1 fail "from v1 every vector comes from uspace-lab" v1.0.0

release_repo local-files-v0 github.com/x/m "ed318_roundtrip.json" "## [0.3.0]" "" "- x"
rcheck local-files-v0 pass "allowed before v1" v0.3.0

release_repo v2-without-suffix github.com/x/m "" "## [2.0.0]" "" "- vectors: cpa.json (ED-269 §2)"
rcheck v2-without-suffix fail "must end in /v2" v2.0.0

release_repo v2-with-suffix github.com/x/m/v2 "" "## [2.0.0]" "" "- vectors: cpa.json (ED-269 §2)"
rcheck v2-with-suffix pass "ends in /v2" v2.0.0

release_repo v1-with-suffix github.com/x/m/v2 "" "${released[@]}"
rcheck v1-with-suffix fail "has a major suffix" v1.0.0

# A tag that exists must point at HEAD.
release_repo tag-not-head github.com/x/m "" "${released[@]}"
g tag -a v1.0.0 -m v1.0.0
echo x > "$repo/extra"; g add -A; g commit -q -m later
rcheck tag-not-head fail "not at HEAD" v1.0.0

echo "== $passed passed, $failed failed"
[[ "$failed" == 0 ]]
