# WP-0: plan and scaffold (done on `plan/initial`)

Status: complete. This brief records what WP-0 delivered so that every
other work package knows what it may rely on and what it may not touch.

## Delivered

| Item | Where | Frozen? |
|---|---|---|
| Shared base types | `core/` (`geo.go`, `vertical.go`, `time.go`, `enums.go`, `ident.go`, `counters.go`, `errors.go`), tests | Yes. Additions need a plan change (edit `docs/PLAN.md §3.1` in the same PR, and say so in the PR title). |
| ODID message structs and enums | `odid/types.go` | Yes. WP-3 adds the codec in other files. |
| Vector harness | `vectors/` (`vectors.go`, `manifest.go`, tests) | API frozen (`docs/PLAN.md §3.2`). Bug fixes allowed. |
| Vendored vectors | `vectors/testdata/*.json`, `VERSION`, `SHA256SUMS` | Only through `scripts/sync-vectors.sh`. |
| Scripts | `scripts/sync-vectors.sh`, `check-vectors.sh`, `fuzz-smoke.sh`, `bench-report.sh`, `manifest-list/` | Fix-forward allowed. |
| CI, lint, Makefile | `.github/workflows/ci.yml`, `.golangci.yml`, `Makefile` | WP-13 extends CI with the semver gate. |
| Rules | `CLAUDE.md`, `SECURITY.md`, `CHANGELOG.md`, `README.md` | - |
| Package stubs | every package's `doc.go` | Each WP rewrites its own `doc.go`. |

## Verified on `plan/initial`

```
gofmt -l .            -> nothing
go build ./...        -> ok
go vet ./...          -> ok
go test -count=1 -cover ./core/ ./odid/ ./vectors/
    core 92.9 %, odid 100 %, vectors 91.8 %
scripts/check-vectors.sh
    offline: 16 files OK; online: match uspace-lab@c6b7f33
scripts/fuzz-smoke.sh -> no targets yet (expected)
```

golangci-lint and staticcheck were not run locally (not installed on the
authoring machine); CI runs them. If `.golangci.yml` needs a fix for the
installed linter version, the first WP to hit it fixes it in a separate
`ci:` commit and says so in its PR.

## What every WP inherits

- `go.mod` at Go 1.27, no dependencies. Only WP-11 adds one (`jwx/v3`).
- The harness pattern:

```go
func TestVectorsGeodesy(t *testing.T) {
    f := vectors.Load(t, "geodesy.json")
    const tolDistanceM = 0.001 // mirrors the header
    if tol, ok := f.FloatTolerance("vincenty distance_m"); !ok || tol != tolDistanceM {
        t.Fatalf("header tolerance changed: %v %v", tol, ok)
    }
    f.Run(t, func(t *testing.T, c vectors.Case) {
        var in struct{ Function string `json:"function"` /* ... */ }
        c.Decode(t, &in, nil)
        switch in.Function { /* ... */ }
    })
}
```

- Benchmark names pre-assigned in `docs/bench-targets.txt`.
- Commit format `type(scope): subject [WP-k G-Mn]`; no AI attribution.
