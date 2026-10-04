# Releasing uspace-core

How a version of `github.com/rootxkit/uspace-core` is cut, how a change to
a judgement is made, and how a system moves to a new version. The rules
are spec `00 §6.3` (versioning and compatibility), `00 §7` (compatibility
policy), `04 §4` (vector pinning) and `docs/PLAN.md` §10. CI enforces
what it can; this page says what it checks and what is left to people.

## 1. What a version promises

| Version change | What it may contain |
|---|---|
| patch `v1.2.3 -> v1.2.4` | fixes that change no judgement and no exported API: a refusal that was a panic, a counter that was not incremented, documentation, performance |
| minor `v1.2 -> v1.3` | additions: new functions, types, optional fields, constants; new vector cases that existing behaviour already passes (a new vector file is a major, see 3.2) |
| major `v1 -> v2` | any change to what a judgement returns for some input, even with no signature change: a changed `expected`, a removed case, an owner removed from a case (that owner's vector test stops running it), a changed header (policy, tolerance, fixtures); a removed or renamed exported identifier; a new version of F3411, F3548 or ED-318 (`04 §4`) |

Before `v1.0.0` the same rule applied one level down: a change to a
vector's expected value bumped the minor.

The stable API from `v1.0.0`, and the parts deliberately left unstable,
are listed in `CHANGELOG.md` under `[1.0.0]`.

A system on an old major keeps passing the vectors of that major: the
vectors are in the module zip (`vectors/testdata/`), so
`go test -run Vectors github.com/rootxkit/uspace-core/...` at the pinned
version runs the vectors of that version. `scripts/consumer-check.sh`
proves it on every tag.

## 2. Cutting a release

Releases are tagged from `main` (or from `release/vN` for a fix to an
older major, section 4). Only the owner, or whoever the owner delegates
to, creates a `v*` tag.

1. **Release pull request.** On a branch, move the entries under
   `## [Unreleased]` in `CHANGELOG.md` to a new heading
   `## [X.Y.Z] - YYYY-MM-DD (milestone)` directly below it, leaving
   `## [Unreleased]` empty. The date may be left out (`## [X.Y.Z]
   (milestone)`); the tag and the GitHub release carry it. The heading
   must not say "unreleased". Merge it with every required check green.
2. **Check before tagging** (optional, the tag job repeats it):
   `make release-check TAG=vX.Y.Z` on the merge commit.
3. **Tag the merge commit** with an annotated tag and push it:

   ```sh
   git fetch origin
   git tag -a vX.Y.Z -m "uspace-core vX.Y.Z (milestone): one line" origin/main
   git push origin vX.Y.Z
   ```

4. **The `tag` job** of `ci.yml` runs on the tag after every gating job
   (build, vet, lint, `go test -race ./...`, the vectors, fuzz smoke,
   govulncheck, gitleaks) has passed on the tagged commit. It runs:
   - `scripts/release-check.sh vX.Y.Z`: the tag is the first version
     heading of `CHANGELOG.md`, that section has entries, nothing is
     left under `[Unreleased]`, the module path carries `/vN` exactly
     when N >= 2, `local_files` in `vectors/testdata/VERSION` is empty
     from v1, the tag points at the checked-out commit and is on
     `origin/main` or `origin/release/vN`, every manifest file is loaded
     by a vector test (`STRICT=1 scripts/manifest-coverage.sh`), and the
     vendored vectors are byte-identical to `uspace-lab` at the pin
     (`REQUIRE_LAB=1 scripts/check-vectors.sh`);
   - `scripts/consumer-check.sh vX.Y.Z`: fetches the published tag
     straight from GitHub into an empty module cache, runs a consumer's
     own `RunOwned(t, "ussp", ...)` test on `cpa.json`, then every
     `TestVectors*` of the module from inside the consumer;
   - creates the GitHub release `vX.Y.Z` with the `CHANGELOG.md` section
     as its notes (`scripts/release-check.sh --notes`). An existing
     release is left unchanged.

   The `geoid` workflow also runs on the tag and runs the 30
   GeographicLib reference cases against the real grids; check it is
   green before announcing the release.
5. **If the tag job fails**, nothing is released. Fix on `main` with a
   pull request; do not move or reuse the tag. Tag the next patch
   (`vX.Y.Z+1`) with its own CHANGELOG heading. A tag that was pushed is
   never deleted: a Go module version, once fetched by the proxy, is
   permanent.

## 3. Making a behaviour change

A vector is law (CLAUDE.md rule 2). A change to a judgement starts in
the lab, never in this repository.

### 3.1 The order

1. **Lab first.** A pull request to `rootxkit/uspace-lab` changes
   `knowledge/vectors/<file>.json` through its generator (or, for a
   file written here such as `jwt_verify.json`, through the generator in
   this repository, copied byte for byte), with the regulation or
   standard clause behind it in the case's `decision`, `why` or
   `source`. It merges there first.
2. **Sync.** In this repository, on a branch, sync from a clean lab
   checkout at the merge commit. Use a clone with LF line endings:

   ```sh
   git -c core.autocrlf=false clone https://github.com/rootxkit/uspace-lab.git ../lab-sync
   git -C ../lab-sync checkout <lab merge commit>
   scripts/sync-vectors.sh ../lab-sync
   ```

   The script records the lab commit in `vectors/testdata/VERSION` and
   rewrites `SHA256SUMS`. Never edit either, or a vector, by hand. Update
   `vectors/manifest.go` if a count changed.
3. **Code.** Change the package so its vector test passes the new file
   as written, with no override.
4. **CHANGELOG.** Under the heading of the next major (add
   `## [N+1.0.0]` below `## [Unreleased]` if no pull request has added
   it since the last release), one line per changed file:

   ```
   - vectors: zones_vertical.json (Regulation (EU) 2019/947 Art. 4(1)(c)): what changed and why
   ```

5. **Label** the pull request `behaviour-change`.
6. **Release.** The major is tagged as in section 2, after
   `release/vN` is cut (section 4).

### 3.2 What the semver gate checks

The `semver-gate` workflow runs `scripts/semver-gate.sh origin/main` on
every pull request (opened, pushed, labelled or unlabelled) and prints
its decision and the reason for each file. For every
`vectors/testdata/*.json` the pull request changes, it compares the base
and head versions by case name (`jq`):

| Change | Class | Requires |
|---|---|---|
| none under `vectors/testdata/*.json` | - | nothing |
| only `why`, `source`, `description` or `generated` text, case order, or the order of a case's `owner` list | editorial | a CHANGELOG line, a new lab pin |
| cases added, no existing case's `input` or `expected` changed, header unchanged | additive (`00 §6.3`) | a CHANGELOG line, a new lab pin |
| an existing case's `input` or `expected` changed, an owner removed from an existing case's `owner` list, a case removed, a header key changed (policy, tolerance, fixtures, units, owners, `utm_commit`), a new file, a removed file, or a file that is not valid JSON | behaviour change | the above, plus a CHANGELOG heading for a new major that is not yet tagged with the file's line under it, plus the label `behaviour-change` |

- "A CHANGELOG line" is a line added by the pull request matching
  `vectors: <file> (<clause>)`; the clause is not optional.
- "A new lab pin" is a `uspace_lab_commit` in `vectors/testdata/VERSION`
  that differs from the base. The gate does not reach the lab; the
  `vectors` job's `scripts/check-vectors.sh` proves the files match the
  new pin byte for byte (required on `main`). A file listed in
  `local_files` is exempt.
- "A new major" is relative to the highest `vX.Y.Z` tag merged into the
  base: from v1, `X+1.0.0` or later with minor and patch 0; before v1,
  a new minor `0.Y+1.0` or `1.0.0`. An untagged heading added by an
  earlier pull request counts, so several behaviour changes can share
  one major before it is released.
- A new file counts as a behaviour change because its expected values
  are new judgements no earlier version promised.
- An owner removed from a case is a behaviour change: `RunOwned` for that
  owner stops running the case, so a judgement that owner was held to is
  no longer checked. The gate compares each case's `owner` list as a set
  and prints `owners removed: <case> (<owner>)`.
- Additive means the cases are new; it does not prove that existing
  behaviour passes them. The vector tests do, in the same pull request.

The gate's own tests, `scripts/test-release-gates.sh` (`make
release-gates`), build fixture repositories in a temp dir and check
each outcome above and its twin; the workflow runs them before the gate.

The label is read from `github.event.pull_request.labels`. That is why
the gate is its own workflow rather than a job of `ci.yml`: adding the
label must start a new run with the new labels (a re-run replays the
old event), and only this short workflow reruns on `labeled` and
`unlabeled`.

## 4. Two majors in parallel: `release/vN`

When `vN+1.0.0` is about to be tagged, cut `release/vN` from the last
`vN` tag:

```sh
git push origin vN.Y.Z^{commit}:refs/heads/release/vN
```

- `release/vN` is maintained for six months after `vN+1.0.0` (`00
  §6.3`), then frozen. Its end date goes in the `vN+1.0.0` CHANGELOG
  section.
- It takes fixes only: security fixes, refusals that were panics,
  counters, documentation. A fix lands on `main` first and is
  cherry-picked (`git cherry-pick -x`) in a pull request against
  `release/vN`, with a `## [N.Y.Z+1]` heading in that branch's
  CHANGELOG. Tags `vN.Y.Z+1` are cut from `release/vN`; the tag job
  accepts a tag on `origin/release/vN`.
- No behaviour change lands on `release/vN`: its vectors stay those of
  vN. The semver gate runs on pull requests to it the same way; a
  vector change there fails unless it is a new major, which belongs on
  `main`.
- **Dual publishing for the systems.** A system moves to `vN+1` on its
  own schedule (`00 §6.3`). Where the changed judgement shows in a
  message (`04 §4`), the system publishing it dual-publishes the old and
  the new major of that message for one release, running both versions
  of the judgement: from v2 the module path differs (section 6), so both
  can be in one build (`github.com/rootxkit/uspace-core` and
  `github.com/rootxkit/uspace-core/v2`). Consumers declare the majors
  they accept and drop the old one when the window closes.

## 5. How a system upgrades

1. Read the `CHANGELOG.md` sections between the pinned version and the
   target. A major lists every changed vector file with its clause.
2. Bump the requirement (`go get github.com/rootxkit/uspace-core@vX.Y.Z`,
   or `.../v2@v2.Y.Z` with the import paths rewritten at v2).
3. Run, in the system's CI, the module's vectors and the system's own
   adapters:

   ```sh
   go test -run Vectors github.com/rootxkit/uspace-core/...
   go test ./...      # the system's RunOwned(t, "<system>", ...) tests
   ```

4. For a major, update the system's message versions as in section 4
   and its own scenario expectations.

## 6. The Go major-version path rule

Go requires the module path of major 2 and later to end in `/vN`. At
the second major:

- `go.mod` becomes `module github.com/rootxkit/uspace-core/v2`, and
  every import inside the module is rewritten to `.../v2/...` in the
  same pull request;
- `release/v1` keeps the old path;
- `scripts/release-check.sh` refuses a `v2.x.y` tag whose module path
  does not end in `/v2`, and a `v1.x.y` tag whose path does.

## 7. Branch protection (for the owner to apply)

CI cannot set this. On `main`, and on every `release/v*` branch when
one is cut, require a pull request before merging and these status
checks, by their exact names:

| Required check | Workflow | Why |
|---|---|---|
| `build, vet, fmt, lint` | ci | formatting, vet, staticcheck, golangci-lint, `go mod tidy` |
| `test with the race detector` | ci | `go test -race -shuffle=on ./...` |
| `knowledge vectors` | ci | every vector test, SHA256SUMS, the lab diff |
| `fuzz smoke` | ci | every decoder's fuzz target |
| `gitleaks` | ci | secrets (`06 §4`) |
| `semver-gate` | semver-gate | the vector rule of `00 §6.3` |

Also set:

- "Require branches to be up to date before merging", so the semver
  gate compares against the current base.
- "Do not allow bypassing the above settings".
- No force pushes and no deletions on `main` and `release/v*`.
- A tag ruleset for `v*`: only the owner (and delegates) may create,
  nobody may update or delete.
- The label `behaviour-change` must exist in the repository for the
  gate to be satisfiable: `gh label create behaviour-change --color
  B60205 --description "Changes a judgement: needs a new major"`.

Not required, on purpose:

- `benchmarks (reported, not gated)`: noisy runners; design budgets.
- `govulncheck`: it fails on a newly published advisory whatever the
  pull request changes; it still runs on every pull request and on
  `main`, and a finding is fixed in its own pull request.
- `geoid reference cases`: path-filtered, so it does not run on every
  pull request, and a required check that does not run blocks the merge.
- `tag`: runs on tags only.
