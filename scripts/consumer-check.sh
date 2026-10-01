#!/usr/bin/env bash
# Prove that the vectors and their testdata travel with the module (plan
# section 6, section 11 gap 12): a scratch module in a temp dir requires
# github.com/rootxkit/uspace-core, runs its own vector test through the
# harness, and runs every vector test of uspace-core from inside it, as a
# system's CI does (spec 05 §7).
#
#   scripts/consumer-check.sh           replace the module with this checkout
#   scripts/consumer-check.sh vX.Y.Z    fetch the published tag from GitHub
#                                       (GOPRIVATE, so straight from git and
#                                       into a fresh module cache): the real
#                                       module zip, as a system gets it
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
# The native path for go.mod on Windows (Git Bash); the same path elsewhere.
here_native="$(cd "$here" && { pwd -W 2>/dev/null || pwd; })"
GO="${GO:-go}"
mod=github.com/rootxkit/uspace-core
ver="${1:-}"

tmp="$(mktemp -d)"
cleanup() { chmod -R u+w "$tmp" 2>/dev/null || true; rm -rf "$tmp"; }
trap cleanup EXIT
mkdir -p "$tmp/consumer"
cd "$tmp/consumer"
goversion="$(sed -n 's/^go //p' "$here/go.mod" | tr -d '\r')"

if [[ -n "$ver" ]]; then
  export GOMODCACHE="$tmp/modcache" GOPRIVATE="$mod" GOFLAGS=-mod=mod
  printf 'module example.com/consumer\n\ngo %s\n\nrequire %s %s\n' "$goversion" "$mod" "$ver" > go.mod
  echo "consumer-check: $mod@$ver from the published tag (module cache $GOMODCACHE)"
else
  export GOFLAGS=-mod=mod
  printf 'module example.com/consumer\n\ngo %s\n\nrequire %s v0.0.0\n\nreplace %s => %s\n' \
    "$goversion" "$mod" "$mod" "$here_native" > go.mod
  echo "consumer-check: $mod replaced by the checkout $here_native"
fi

cat > consumer_test.go <<'EOF'
package consumer_test

import (
	"testing"

	"github.com/rootxkit/uspace-core/vectors"
)

// A system's own vector test: the ussp cases of cpa.json, loaded through
// the harness from wherever the module lives.
func TestVectorsFromConsumer(t *testing.T) {
	f := vectors.Load(t, "cpa.json")
	ran := 0
	f.RunOwned(t, "ussp", func(t *testing.T, c vectors.Case) {
		ran++
		if len(c.Input) == 0 || len(c.Expected) == 0 {
			t.Errorf("case %s has no input or expected", c.Name)
		}
	})
	if ran == 0 {
		t.Fatal("no ussp case ran")
	}
	t.Logf("consumer ran %d ussp cases of cpa.json from %s", ran, vectors.Dir())
}
EOF

"$GO" mod download "$mod"
"$GO" test -count=1 -v -run Vectors . 2>&1 | tee own.log
grep -q -- '--- PASS: TestVectorsFromConsumer' own.log || { echo "consumer-check: the consumer's vector test did not pass"; exit 1; }
dir="$(sed -n 's/.*consumer ran [0-9]* ussp cases of cpa.json from //p' own.log | head -n 1 | tr -d '\r')"
echo "consumer-check: vectors.Dir() is $dir"

set +e
"$GO" test -count=1 -v -run Vectors "$mod/..." > core.log 2>&1
rc=$?
set -e
grep -E '^(ok|FAIL|---)|no test files' core.log | grep -vE '^    ' | grep -E '^(ok|FAIL|--- (FAIL|SKIP): TestVectors)' || true
if [[ "$rc" != 0 ]]; then
  echo "consumer-check: FAIL - go test -run Vectors $mod/... exited $rc"
  tail -n 40 core.log
  exit 1
fi

# Every package that owns a manifest file ran its vector test here.
missing=0
pkgs="$("$GO" run "$mod/scripts/manifest-list" | awk '{print $2}' | tr -d '\r' | sort -u)"
for p in $pkgs; do
  line="$(grep -E "^ok[[:space:]]+$mod/$p[[:space:]]" core.log || true)"
  if [[ -n "$line" && "$line" != *"no tests to run"* ]]; then
    echo "  ok: $mod/$p"
  else
    echo "  FAIL: $mod/$p did not run a vector test (${line:-no ok line})"
    missing=1
  fi
done
passed="$(grep -cE -- '^--- PASS: TestVectors' core.log || true)"
skipped="$(grep -cE -- '--- SKIP' core.log || true)"
if [[ "$missing" != 0 || "$passed" -eq 0 ]]; then
  echo "consumer-check: FAIL - not every vector package ran from the consumer"
  exit 1
fi
echo "consumer-check: PASS - $passed TestVectors* tests passed from a consumer module; $skipped tests or subtests skipped (listed in the log)"
