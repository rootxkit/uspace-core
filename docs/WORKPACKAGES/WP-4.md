# WP-4: `regnum`, `serial`, `sources`

Branch `feat/WP-4-regnum-serial-sources`. Milestone G-M1. Owns `regnum/`,
`serial/`, `sources/` exclusively. Depends on WP-0 only. Consumers:
`identify` (WP-7, waits for this), `alerting` (WP-10, `sources`), the
systems' registries and adapters. Small; open the PR fast.

## Read first

1. `CLAUDE.md`, `docs/PLAN.md §3.6`, `§6`, `§8`.
2. `core/errors.go`, `core/counters.go`.
3. `vectors/testdata/serials_and_registration.json` (header
   `cta2063_rule`, all 33 cases) and `source_control.json` (8 cases).
4. LESSONS G-04, G-05, G-06, G-07, B-09, B-10, B-11, E-10.
5. Spec `04 §3.6` (`source/control`), `03 §6` (identifiers), `06 §5`
   (secret part hashed, compared on the public part), `08` Q5.
6. Reference only: `utm/common/uas_identity.py`,
   `utm/gateway/registry_projection.py` (`operator_key`),
   `utm/common/sources.py`.

## What to build

### `regnum`

```go
const DefaultPattern = `^[A-Z]{3}[A-Za-z0-9]{8,16}$`
type Validator struct{ re *regexp.Regexp }
func NewValidator(pattern string) (*Validator, error)        // empty -> DefaultPattern; invalid regexp -> error
func (v *Validator) Validate(value string) error             // *core.FieldError{Field:"registration_number"}
func PublicPart(value string) string
func CompareKey(value string) string
func Public(value string) (public, compareKey string)
```

Rules: `Validate` trims, refuses empty, refuses a value containing `-`
(the secret is never registered; the vector `registration-FIN87astrdge12k8-xyz`
is invalid), then matches the pattern (`fin87...` lower-case country is
invalid; `FIN87` too short). `problem_contains` phrases in the vectors
must appear in the error. `PublicPart`: trim; if the value ends in `-`
plus exactly three alphanumerics, strip them (any three: `GEO-OP-ABC`
becomes `GEO-OP`, the G-04 pitfall, documented); otherwise unchanged
(`FIN87astrdge12k8-` stays, `-xyz` becomes `-xyz`? read
`public-part--xyz` and `public-part-FIN87astrdge12k8-x!z`: a non-
alphanumeric tail is not stripped). `CompareKey = ToUpper(PublicPart)`.

### `serial`

```go
const MaxLen = 20
func ValidateCTA2063A(serial string) error      // *core.FieldError{Field:"serial"}
func ValidateForClass(serial, classLabel string) error
func RequiresCTA(classLabel string) bool        // C1 C2 C3 C5 C6
func Normalize(serial string) string            // strings.TrimSpace, case kept
func FoldKey(serial string) string              // strings.ToUpper(Normalize)
```

CTA-2063-A (header `cta2063_rule`): 4-character manufacturer code, one
length character (`1`-`9` = 1-9, `A`-`F` = 10-15), then exactly that
many characters, alphabet `0-9A-Z` without `O` and `I`, upper-case only
(`1a2b1x` invalid), total at most 20 (`1A2BG...` invalid: `G` is not a
length character; `1A2BF123456789ABCDEF` is 20 and valid). The error
reason must contain the phrases the vectors pin (`problem_contains`:
"not a CTA", "says 2", "says 3" for the length mismatch cases: "length
character says 2, 3 follow"). `ValidateForClass`: classes needing CTA
produce "class C1 requires a CTA-2063-A serial; " + the CTA reason; C0,
C4, "" need a non-empty serial ("a serial number is required").

### `sources`

```go
type Control struct{ SourceType string; InstanceID *string; Enabled bool }
type State struct{ Controls []Control; DefaultDeny bool; Version uint64; Epoch string }
type Why string; const (WhyType Why = "type"; WhyInstance Why = "instance"; WhyDefaultDeny Why = "default_deny")
type Decision struct{ Enabled bool; WhyDisabled *Why }
func (s State) Query(sourceType string, instanceID *string) Decision
type Follower struct{...}
func NewFollower() *Follower
func (f *Follower) Apply(in State) bool
func (f *Follower) State() (State, bool)        // false when nothing applied yet
func (f *Follower) Query(sourceType string, instanceID *string) Decision
func (f *Follower) Counters() *core.Counters    // applied, ignored_older_version, new_epoch
```

Query rules (the 8 vectors): a type row (`InstanceID nil`) that is off
disables every instance of the type (`why type`); an instance row that is
off disables that instance (`why instance`) even when the type row is on;
a type row on does not re-enable an instance off; with no matching row,
enabled unless `DefaultDeny` (`why default_deny`); an explicit instance
row on overrides default deny; a whole-type query (`instanceID nil`) is
decided by the type row only. Apply rules (B-09): first state always
applied; same epoch: only a strictly higher version; different epoch: any
version; return whether applied and count. `Follower.Query` with no
state: everything enabled (never fail closed). Document for callers
(B-10): refuse a disabled source with 503 + `Retry-After`, never 401/403.

## Vector tests

`serial/vectors_test.go` loads `serials_and_registration.json` (the
manifest names `serial`; it imports `regnum` for the two registration
kinds): switch on `kind`: `cta2063` (`serial` -> valid, `problem` or
`problem_contains`), `serial_for_class` (`serial`, `class_label`),
`registration_number` (`value`, `pattern`), `public_registration_number`
(`value` -> `public`, `compare_key`). Compare `valid` exactly; where
`problem` is a full string, assert `strings.Contains(err, problem_contains)`
when present else compare the full phrase loosely (log a diff; only
`problem_contains` is binding per the file).

`sources/vectors_test.go`: `controls`, `default_deny`, `query` ->
`enabled`, `why_disabled` (nil vs null).

## Other tests

- E-01 pairs per refusal; `PublicPart` table incl. the pitfall.
- `Follower.Apply` sequences: higher, equal, lower version in one epoch;
  new epoch with lower version; counters read back (E-02).
- Concurrency: `Follower` under `-race` with concurrent Apply/Query.
- Benchmarks `BenchmarkCompareKey`, `BenchmarkValidateCTA`,
  `BenchmarkQuery` (50 controls).

## Done when

- [ ] Lint run locally before every push, with the pinned linters:
  `make tools` (once; installs golangci-lint v2.14.0 and staticcheck
  v0.8.1, the versions CI runs) then `make lint` (gofmt, vet,
  staticcheck, golangci-lint; it refuses any other golangci-lint
  version) prints no issue. Paste its last lines into the PR.
- [ ] 33 + 8 vector cases pass; `-race -shuffle=on` green; coverage >= 90 % each.
- [ ] Lint clean; three `doc.go` rewritten; CHANGELOG line; PR with outputs.

## Commits

`feat(serial): validate CTA-2063-A serials and class requirements [WP-4 G-M1]`,
`feat(regnum): validate registration numbers and derive the compare key [WP-4 G-M1]`,
`feat(sources): decide source switches and apply states by version and epoch [WP-4 G-M1]`,
`test(serial): run the serials, registration and source-control vectors [WP-4 G-M1]`.
