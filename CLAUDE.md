# uspace-core: rules for every contributor and agent

`uspace-core` is the shared Go module of the U-space system-of-systems
(`github.com/rootxkit/uspace-core`): a library with no process, no port and
no database, compiled into every system's binaries and pinned by a semver
tag. Every safety judgement (identification, zone, CPA, conformance, time
placement, pressure altitude, geodesy) exists once, here, and is pinned by
the knowledge vectors in `vectors/testdata/`. Read `docs/PLAN.md` before
changing anything; a work package brief is in `docs/WORKPACKAGES/`.

## Hard rules

1. **Nothing here commands an aircraft.** No type, function or dependency
   has a send path towards a vehicle (LESSONS INV-01).
2. **A vector is law.** A change that breaks a vector fails CI unless the
   vector changes in the same PR with the regulation or standard clause
   behind it, and that is a major version (spec `00 §6.3`). Never edit
   `vectors/testdata/*.json` by hand: run `scripts/sync-vectors.sh` from a
   clean `uspace-lab` checkout.
3. **Thresholds are data.** Separation minima, the height limit, the
   pressure margin, TTLs and tolerances are parameters of a `Policy` or
   `Settings` struct with documented defaults; never a literal in a
   judgement, never relaxed to make a test pass (INV-03).
4. **Units and datums in every name** (E-13): `alt_amsl_m`, `alt_hae_m`,
   `height_agl_m`, `speed_ms`, `timeout_s`; in Go `AltAMSLM`, `SpeedMS`,
   `TimeoutS` or `time.Duration`. Wire units are converted at the parser
   boundary only. AMSL and AGL never meet in one calculation (D-01).
5. **No panics on untrusted input.** Decoders and validators return a
   `*core.FieldError` (or a list) that names the field and the reason.
   Bounds (sizes, depths, counts) are explicit and tested past the bound
   (E-10). `golangci-lint` forbids `panic` outside tests.
6. **Everything refused, dropped or degraded is counted** in a
   `core.Counters` with a stable snake_case name (E-09). A library never
   logs or prints; the caller decides.
7. **English only**, in code, comments, commits and docs.
8. **Dependencies**: standard library first. A third-party module needs a
   one-line reason in the commit body and an entry in `docs/PLAN.md §4`.
   Allowed today: `github.com/lestrrat-go/jwx/v3` (spec `00 §6.2`).
   Nothing with cgo.

## Testing rules (from utm, LESSONS E-01 to E-04, E-10, E-11)

- **E-01 Test presence, not only absence.** Every test that asserts
  something does not happen (no alert, no refusal, no publish, nil) is
  paired with the test that makes it happen. Every `refuse-*` has its
  `accept-*` twin.
- **E-02 Run the branch that says nothing is wrong.** Exercise success,
  health and degraded paths deliberately: make the thing succeed and read
  what it says; take the dependency away (no geoid, no terrain, unknown
  ground) and check the exact degraded output and counter.
- **E-03 Never write a wire-format offset from memory.** ODID layouts come
  from opendroneid-core-c and are pinned by the reference frames in
  `odid_decode.json`; PGM and JWT layouts from their references. Derive an
  offset by diffing two frames that differ in one field, and pin it so.
- **E-04 Never report an inference as an observation.** In a PR
  description, "the vectors pass" means you ran `make vectors` and read
  the output. A skipped case (missing geoid file) is reported as skipped.
  A tool error is not evidence about the thing being checked.
- **E-10** every bounded structure (cache, nonce set, grid, problem list,
  ring size) has a test that exceeds its bound.
- **E-11** tests restore global state and pass under `-shuffle=on` and
  `-race`.
- Vector tests are named `TestVectors<File>` in `<pkg>/vectors_test.go`,
  decode strictly through `vectors.Case.Decode`, compare floats with the
  file's tolerance and times with `Equal`. A `null` is a nil pointer.
- Every decoder has a `Fuzz*` target seeded from the vectors; every hot
  function has a `Benchmark*` named in `docs/bench-targets.txt`.
- Coverage: a work package is done at >= 90% statement coverage of its
  packages, with every branch that produces a distinct counter or reason
  covered by a named test.

## Git conventions

- Branch per work package: `feat/WP-<k>-<slug>` (the slug is in the
  brief). Plan and docs branches: `plan/<slug>`, `docs/<slug>`.
- Conventional Commits, one logical change per commit, imperative subject
  under 72 characters, the work package and milestone in brackets at the
  end: `feat(geodesy): add the Vincenty inverse [WP-1 G-M1]`,
  `test(odid): run the 187 decode vectors [WP-3 G-M1]`,
  `fix(zones): judge a lower AGL limit at ground without a DEM [WP-8 G-M1]`.
  Types: `feat`, `fix`, `test`, `refactor`, `perf`, `docs`, `build`, `ci`,
  `chore`. Scope is the package.
- A commit that adds a dependency says why in the body.
- **No AI attribution of any kind**: no `Co-Authored-By`, no "generated
  by", no tool names in commits, PRs or code.
- Never force-push a shared branch; never commit to `main` directly.
- Do not push unless asked. The owner merges.

## Before you say a work package is done

Run, in this order, and paste the last lines of each into the PR:

```
make tools          # once per machine: the linter versions CI pins
make lint           # gofmt, vet, staticcheck, golangci-lint; must print no issue
make race
make vectors
make fuzz-smoke FUZZTIME=10s
make bench
```

Run `make lint` locally before every push, not only at the end: CI runs
golangci-lint v2.14.0 and staticcheck v0.8.1 (pinned in
`.github/workflows/ci.yml` and the `Makefile`), and `make tools` installs
exactly those. `make lint` refuses to run another golangci-lint version,
because a different version reports different issues. Without `make`:
`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`,
`go install honnef.co/go/tools/cmd/staticcheck@v0.8.1`, then `gofmt -l .`,
`go vet ./...`, `staticcheck ./...` and `golangci-lint run ./...`.

Then check the brief's done-when list item by item. If a vector cannot
pass without a behaviour the plan did not foresee, stop and write it down
in the PR as a spec gap; do not change the vector.
