#!/usr/bin/env bash
# Fixture tests for the CI helper scripts that are not release gates
# (those are in test-release-gates.sh): check-vectors.sh and
# fuzz-smoke.sh. Each case runs the script in a temp dir against a small
# fixture, with a recording wrapper or a stand-in for git and go where
# the real one cannot be driven, and checks the exit status and what it
# prints. Every failing case has a passing twin (LESSONS E-01).
#
#   scripts/test-scripts.sh
#
# Needs bash, git, sha256sum and base64; no network and no Go.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
passed=0
failed=0
real_git="$(command -v git)"

# check NAME WANT RC PHRASE OUTPUT: WANT is pass (rc 0) or fail (rc 1);
# PHRASE must be in OUTPUT.
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
    echo "FAIL $1: want $2 with \"$4\", got rc $3:"
    sed 's/^/     /' <<< "$5"
  fi
}

# absent NAME NEEDLE HAYSTACK: NEEDLE must not occur in HAYSTACK.
absent() {
  if grep -qF -- "$2" <<< "$3"; then
    failed=$((failed + 1))
    echo "FAIL $1: found what must not be there:"
    grep -nF -- "$2" <<< "$3" | sed 's/^/     /'
  else
    passed=$((passed + 1))
    echo "ok   $1: absent"
  fi
}

# --- check-vectors.sh -------------------------------------------------

token="tok-FIXTURE-$$-secret"
basic="$(printf 'x-access-token:%s' "$token" | base64 | tr -d '\n')"

# vecfix NAME: a checkout with check-vectors.sh, one vendored file, its
# SHA256SUMS and a VERSION whose lab repository nothing listens on.
vecfix() {
  local r="$work/$1"
  mkdir -p "$r/scripts" "$r/vectors/testdata" "$r/bin"
  cp "$here/check-vectors.sh" "$r/scripts/"
  (cd "$r/vectors/testdata" && echo '{}' > a.json && sha256sum a.json > SHA256SUMS)
  printf '%s\n' "uspace_lab_repo = https://127.0.0.1:9/lab.git" \
    "uspace_lab_commit = 0123456789abcdef0123456789abcdef01234567" \
    "uspace_lab_path = knowledge/vectors" "local_files =" > "$r/vectors/testdata/VERSION"
  echo "$r"
}

# recgit DIR MODE: a git on PATH that logs every argument vector, the
# extra configuration it was given and, after a real run, the temp
# clone's .git/config; MODE real runs the real git, MODE leak fails a
# fetch printing all of that, as a verbose git might.
recgit() {
  cat > "$1/bin/git" <<WRAP
#!/usr/bin/env bash
log="$1/git.log"
{ printf 'argv:'; printf ' %s' "\$@"; echo
  for ((i = 0; i < \${GIT_CONFIG_COUNT:-0}; i++)); do
    k="GIT_CONFIG_KEY_\$i"; v="GIT_CONFIG_VALUE_\$i"; echo "config: \${!k}=\${!v}"
  done; } >> "\$log"
if [[ "$2" == leak && " \$* " == *" fetch "* ]]; then
  grep -E '^(config:|argv: .* remote add)' "\$log" >&2
  echo "fatal: unable to access" >&2; exit 128
fi
rc=0; "$real_git" "\$@" || rc=\$?
if [[ "\$1" == -C && -f "\$2/.git/config" ]]; then
  sed 's/^/localconfig: /' "\$2/.git/config" >> "\$log"
fi
exit "\$rc"
WRAP
  chmod +x "$1/bin/git"
}

runvec() { # runvec DIR [ENV...]: run check-vectors.sh in DIR with ENV
  local d="$1"; shift
  (cd "$d" && env PATH="$d/bin:$PATH" "$@" bash scripts/check-vectors.sh 2>&1)
}

r="$(vecfix vec-token)"; recgit "$r" real
rc=0; out="$(runvec "$r" LAB_READ_TOKEN="$token")" || rc=$?
check vec-token-unreachable pass "$rc" "unverified: could not fetch" "$out"
absent vec-token-not-printed "$token" "$out"
absent vec-token-not-in-argv "$token" "$(grep '^argv:' "$r/git.log")"
check vec-token-as-header pass 0 "config: http.https://127.0.0.1:9/lab.git.extraheader=AUTHORIZATION: basic $basic" "$(cat "$r/git.log")"
check vec-clone-config-logged pass 0 "localconfig: [remote \"origin\"]" "$(cat "$r/git.log")"
absent vec-token-not-in-clone-config "$token" "$(grep '^localconfig:' "$r/git.log")"

r="$(vecfix vec-token-required)"; recgit "$r" real
rc=0; out="$(runvec "$r" LAB_READ_TOKEN="$token" REQUIRE_LAB=1)" || rc=$?
check vec-token-required fail "$rc" "unverified: could not fetch" "$out"
absent vec-token-required-not-printed "$token" "$out"

r="$(vecfix vec-no-token)"; recgit "$r" real
rc=0; out="$(runvec "$r")" || rc=$?
check vec-no-token pass "$rc" "unverified: could not fetch" "$out"
absent vec-no-token-no-header "extraheader" "$(cat "$r/git.log")"

r="$(vecfix vec-leaky-git)"; recgit "$r" leak
rc=0; out="$(runvec "$r" LAB_READ_TOKEN="$token")" || rc=$?
check vec-leaky-git pass "$rc" "unverified: could not fetch" "$out"
check vec-leaky-git-redacted pass 0 "<redacted>" "$out"
absent vec-leaky-git-token "$token" "$out"
absent vec-leaky-git-header "$basic" "$out"

# --- fuzz-smoke.sh ----------------------------------------------------

# fuzzfix NAME TARGETS: a checkout with fuzz-smoke.sh and a stand-in go
# whose one package lists TARGETS (space-separated, may be empty) and
# whose fuzz runs pass.
fuzzfix() {
  local r="$work/$1"
  mkdir -p "$r/scripts"
  cp "$here/fuzz-smoke.sh" "$r/scripts/"
  cat > "$r/go" <<STUB
#!/usr/bin/env bash
case "\$1 \$2" in
  "list ./...") echo example.com/fixture/pkg ;;
  "test -list") for t in $2; do echo "\$t"; done; echo "ok  example.com/fixture/pkg 0.01s" ;;
  "test -run") echo "fuzz: elapsed: 0s, PASS" ;;
  *) echo "stand-in go: unexpected \$*" >&2; exit 2 ;;
esac
STUB
  chmod +x "$r/go"
  echo "$r"
}

r="$(fuzzfix fuzz-none "")"
rc=0; out="$(GO="$r/go" FUZZTIME=1s bash "$r/scripts/fuzz-smoke.sh" 2>&1)" || rc=$?
check fuzz-none fail "$rc" "fuzz-smoke: no fuzz target found" "$out"

r="$(fuzzfix fuzz-one "FuzzDecode")"
rc=0; out="$(GO="$r/go" FUZZTIME=1s bash "$r/scripts/fuzz-smoke.sh" 2>&1)" || rc=$?
check fuzz-one pass "$rc" "== example.com/fixture/pkg FuzzDecode (1s)" "$out"

echo "== $passed passed, $failed failed"
[[ "$failed" == 0 ]]
