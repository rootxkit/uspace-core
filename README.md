# uspace-core

Shared Go module of the U-space systems (`uspace-authority`, `uspace-cisp`,
`uspace-ussp`, `uspace-ansp`, `uspace-lab`): ODID, geodesy, terrain and
geoid, ED-269/ED-318, F3411/F3548 types, identification, zone judgement,
CPA and alerting, time placement, source control, JWT and receiver
verification.

A library, not a service: no process, no port, no database. Each system
compiles it in and pins a semver tag. Every safety judgement lives here
once and is pinned by the knowledge vectors of `uspace-lab`
(`vectors/testdata/`, 18 files, 682 cases).

- Plan and architecture: [`docs/PLAN.md`](docs/PLAN.md)
- Work packages: [`docs/WORKPACKAGES/`](docs/WORKPACKAGES/)
- Rules for contributors and agents: [`CLAUDE.md`](CLAUDE.md)
- Versioning and history: [`CHANGELOG.md`](CHANGELOG.md)
- Releases, behaviour changes and upgrading: [`docs/RELEASING.md`](docs/RELEASING.md)

```
make build vet lint    # gofmt, go vet, staticcheck, golangci-lint
make race              # go test -race
make vectors           # the knowledge vectors against every package
make fuzz-smoke bench  # decoders fuzzed; hot functions timed
```

Go 1.27. No cgo. Module path `github.com/rootxkit/uspace-core`.
