# uspace-core implementation plan

Status: plan for parallel implementation by independent agents. Branch
`plan/initial`. Inputs: the spec at `uspace-lab/docs/spec/` (especially
`00 §6`, `00 §6.3`, `04`, `05`, `07` G-M1..G-M3, `09`), the knowledge base
at `uspace-lab/knowledge/` (`LESSONS.md`, 16 vector files, `scenarios.md`,
`README.md`) and the predecessor `rootxkit/utm` (read-only reference). The
vectors are vendored at `uspace-lab@c6b7f33` (generated from
`utm@484cd22`).

Sections: 1 scope and decisions; 2 module layout and dependency graph;
3 public API per package; 4 third-party dependencies; 5 vectors and
lessons per package; 6 vector harness; 7 work packages; 8 engineering
standards; 9 CI; 10 versioning; 11 spec gaps and decisions for the owner;
12 the `v1.1.0` additive release (C1).

---

## 1. Scope and decisions

`uspace-core` is a Go module (`github.com/rootxkit/uspace-core`), Go 1.27,
no cgo, a library with no process, port or database (spec `00 §6.3`).
Every judgement the systems share lives here once and is pinned by the
knowledge vectors (`00 §6` hard rule; `06` T12).

Decisions taken in this plan (each justified where it is used):

| # | Decision | Why |
|---|---|---|
| D1 | Shared base types are frozen now in package `core` (positions, vertical references, altitude sources, the time triplet, trust, severity, zone type, the identification block, counters, field errors) and in `odid/types.go` (message structs). They compile and are tested on `plan/initial`. | Work packages start in parallel against real code, not prose. `core` imports nothing from the module; nothing in the module is imported by `core`. |
| D2 | Package split follows the spec table of `00 §6.3` with four deviations, all recorded in §11: the alert lifecycle is its own package `alerting` on top of `cpa`; the Remote ID identity tracker and pressure-altitude selection are a package `rid`; the spoofing guard (`fleet_match`) lives in `identify`, not `regnum`/`serial`; receiver HMAC lives in `auth`. | A dependency DAG without cycles and work packages that do not overlap. The import paths the spec names (`uspace-core/cpa`, `/auth`, `/zones`, ...) all exist and carry what the table promises for them. |
| D3 | Vectors are vendored into `vectors/testdata/` with a sync script, a `VERSION` pin, `SHA256SUMS` and a CI check against the lab at the pinned commit. Not a submodule. | §6. |
| D4 | uspace-core runs every vector case regardless of the case's `owner` list. Systems filter with `RunOwned`. | Core is the shared implementation; an owner list says who runs it downstream, not whose behaviour it is. |
| D5 | One third-party dependency at G-M1 (`lestrrat-go/jwx/v3`, mandated by `00 §6.2`), none with cgo. H3 is not in core at v0.x (§11 gap 3). | Standard library first; reproducible cross-platform builds; the H3 binding is cgo. |
| D6 | Terrain tiles are the PGM container the predecessor's tooling writes, not GeoTIFF. The GeoTIFF conversion stays in the lab's tooling. | A GeoTIFF reader is not pinned by any vector and would add a dependency to the hot path. The vector pins the PGM layout. |
| D7 | The first tag `v0.1.0` is G-M1; `v1.0.0` is G-M3. Between them, behaviour changes to a judgement already bump the major of the vector set (§10). | Spec `07` and `00 §6.3`. |

---

## 2. Module layout and dependency graph

```
github.com/rootxkit/uspace-core
├── core/          frozen base types (WP-0)                     deps: stdlib
├── vectors/       harness + testdata/*.json (WP-0)             deps: stdlib
├── geodesy/       WGS84 distance, bearing, tangent plane, containment (WP-1)   deps: core
├── geodesy/cell/  c5/c3 partition cells: names, parent, ring-1, bbox cover (WP-15, C1)  deps: core, geodesy
├── internal/pgm/  PGM P5 container (WP-2)                      deps: core
├── terrain/       DEM tiles, cell names, bilinear, index (WP-2)  deps: core, internal/pgm
├── geoid/         GeographicLib grids, HAE <-> AMSL (WP-2)     deps: core, internal/pgm
├── odid/          ODID codec; types.go frozen (WP-3)           deps: core
├── regnum/        registration numbers (WP-4)                  deps: core
├── serial/        CTA-2063-A serials (WP-4)                    deps: core
├── sources/       source control model (WP-4)                  deps: core
├── ed269/         strict ED-269 parse/export, applicability (WP-5)   deps: core
├── timeplace/     ts / rx_ts / captured_at placement (WP-6)   deps: core
├── rid/           identity tracker, altitude selection, NED velocity (WP-6)  deps: core, odid
├── identify/      identification resolution, spoofing guard (WP-7)   deps: core, regnum, serial, geodesy
├── zones/         zone judgement (WP-8)                        deps: core, geodesy, ed269
├── cpa/           CPA, conflict test, neighbour grid (WP-9)    deps: core, geodesy
├── alerting/      alert state machine (WP-10)                  deps: core, cpa, zones, ed269, sources
├── auth/          JWT/JWKS verification, receiver HMAC (WP-11) deps: core, jwx
├── ed318/         ED-318 model, ED-269 mapping (WP-12, G-M2)   deps: core, ed269, geodesy
├── f3411/         F3411 v22a types (WP-12, G-M2)               deps: core
├── f3548/         F3548 v21 types (WP-12, G-M2)                deps: core
├── docs/          this plan, work packages, bench targets
└── scripts/       sync/check vectors, fuzz smoke, bench report, manifest-list
```

Dependency DAG (edges point at what is imported). No cycles; `core` is
the single root; `vectors` is imported by tests only.

```
core <- geodesy <- identify, zones, cpa, ed318
core, geodesy <- geodesy/cell   (nothing in geodesy imports cell)
core <- internal/pgm <- terrain, geoid
core <- odid <- rid
core <- regnum, serial <- identify
core <- sources <- alerting
core <- ed269 <- zones, alerting, ed318
core <- timeplace
core <- cpa <- alerting ;  zones <- alerting
core <- auth (+ jwx)
core <- f3411, f3548
```

Rules: `core` never imports another package of the module. Packages do
not import `alerting`, `identify` or `zones` sideways; a shared enum they
both need goes to `core` (that is why `core.Identification` exists). No
package imports `vectors` outside `_test.go`. Nothing imports a system.

---

## 3. Public API per package

Signatures below are the contract between work packages. A work package
may add exported functions and types; it may not rename, remove or change
the meaning of what is listed here without updating this plan. Pointer
results mean "unknown / none" (a vector's `null`), never a zero value.

**Stable since `v1.0.0`.** This API, with what the work packages added to
it, is declared stable (G-M3). From `v1.0.0` the rule above is the semver
rule of §10: an addition is a minor, a removal, rename or change of
meaning is a major. `CHANGELOG.md` under `[1.0.0]` lists what is
deliberately left unstable.

### 3.1 `core` (frozen, written)

```go
type LatLon struct{ LatDeg, LonDeg float64 }       // Valid() bool
func WrapLonDeg(lonDeg float64) float64            // into (-180, 180]
const FeetToMetres = 0.3048; MeanEarthRadiusM = 6_371_008.8; WGS84SemiMajorM; WGS84Flattening
type VerticalRef string  // RefAGL, RefAMSL, RefWGS84; Valid()
type AltSource string    // AltGeodetic, AltPressure, AltNetwork, AltNone
type Altitude struct{ ValueM float64; Ref VerticalRef }
type TimeSource string   // TimeSourceClock, TimeBroadcast, TimeReceiver, TimeProvider, TimeSystem
type Times struct{ TS *time.Time; RxTS, CapturedAt time.Time; Source TimeSource; Backlog bool } // LagS()
type Trust string        // TrustAuthenticated, TrustProvider, TrustSurveillance, TrustBroadcast, TrustSensor, TrustSimulated
type Severity string     // SeverityInfo, SeverityWarning, SeverityCritical
type ZoneType string     // ZoneProhibited, ZoneReqAuthorization, ZoneConditional, ZoneNoRestriction, ZoneUSpace
                         // Valid(), ED269() string, IncidentZone() bool; ZoneTypeFromED269(string) (ZoneType, bool)
type IdentStatus string  // IdentRegistered, IdentSuspended, IdentUnknownOperator, IdentUnidentified; IncidentStatus()
type IdentReason string  // the 15 reasons of 04 §3.2 (ReasonMatched ... ReasonRegistryUnavailable)
type IdentBasis string   // BasisAuthenticated, BasisAsBroadcast
type Identification struct{ Status; Reason; Serial, OperatorReg, RegisteredOperatorReg *string; Mismatch bool; RegistryUASID *string; Basis }
type Counters struct{...} // Inc, Add, Get, Snapshot, Names; concurrency-safe
type FieldError struct{ Field, Reason string }; func Fieldf(field, format string, args ...any) *FieldError
```

### 3.2 `vectors` (written)

```go
type File struct{ Description string; Source []string; Units map[string]string; Tolerance map[string]any; Owners []string; Generated, UtmCommit string; Fixtures json.RawMessage; Cases []Case; Extra map[string]json.RawMessage; Name string }
type Case struct{ Name string; Owner []string; Input, Expected json.RawMessage; Why, Source string }
type TB interface{ Helper(); Logf; Errorf; Fatalf }   // *testing.T satisfies it
func Dir() string
func Load(t TB, name string) *File;  func Read(path string) (*File, error);  func Parse(raw []byte) (*File, error)
func (f *File) Run(t *testing.T, fn func(*testing.T, Case))            // all cases (core)
func (f *File) RunOwned(t *testing.T, owner string, fn ...)             // systems
func (f *File) Header(t TB, key string, v any);  func (f *File) FloatTolerance(key string) (float64, bool)
func (c Case) Decode(t TB, in, exp any);  func (c Case) ExpectedIsNull() bool
func Unmarshal(t TB, raw json.RawMessage, v any);  func StrictUnmarshal(raw []byte, v any) error
func Near(t TB, field string, got, want, tol float64);  func NearPtr(t TB, field string, got, want *float64, tol float64)
func EqualTime(t TB, field string, got, want time.Time);  func EqualTimePtr(...);  func EqualStrPtr(t TB, field string, got, want *string)
var Manifest []Entry;  const TotalCases = 682
```

### 3.3 `geodesy` (WP-1)

```go
// Vincenty inverse on WGS84. ErrNoConvergence for nearly antipodal points (D-09).
func Inverse(a, b core.LatLon) (distanceM, initialBearingDeg, finalBearingDeg float64, err error)
func DistanceM(a, b core.LatLon) (float64, error)           // Inverse, distance only
func HaversineM(a, b core.LatLon) float64                    // sphere core.MeanEarthRadiusM; spoof distance only (D-11)
// Local tangent plane about origin: north/east metres of p using the WGS84
// meridional and prime-vertical radii at the origin latitude; longitude
// difference wrapped (D-10).
func LocalOffsetM(origin, p core.LatLon) (northM, eastM float64)
func LocalOffsetAboutMidLatM(a, b core.LatLon) (northM, eastM float64) // the CPA projection: radii at (a.lat+b.lat)/2
// Shapes. Ring is lon/lat as GeoJSON stores it; Polygon is outer + holes.
type Ring []core.LatLon
type Polygon struct{ Rings []Ring }                 // Rings[0] outer, the rest holes
type Circle struct{ Center core.LatLon; RadiusM float64 }
type BBox struct{ MinLat, MinLon, MaxLat, MaxLon float64 } // Contains(p) bool; Pad(metres) BBox
func (p Polygon) Contains(pt core.LatLon) bool      // ray casting in degrees, holes honoured, antimeridian-safe for rings < 180 deg wide
func (p Polygon) BBox() BBox
func (c Circle) Contains(pt core.LatLon) (inside bool, distanceM float64, err error) // geodesic distance (D-09)
func (c Circle) BBox() BBox
func ValidRing(r Ring, maxVertices int) error        // closed, >= 4 positions, finite, in range, <= maxVertices (Z-06)
```

`geodesy/cell` (WP-15, C1, `v1.1.0`; M35): the named partition key of
spec `05 §3`, not H3 and not `cpa.Grid`. Indexes are
`floor((lat_deg + 90) * n)` and `floor((lon_deg + 180) * n)` with
`n` = 10 (`c5`) or 1 (`c3`), longitude in `[-180, 180)`, +90 in the last
row; names `c5:<lat_idx>:<lon_idx>` / `c3:<lat_idx>:<lon_idx>`.

```go
type Level int                       // Level5 (0.1 degree, "c5"), Level3 (1 degree, "c3"); the zero Level is invalid
func (l Level) StepDeg() float64     // 0.1, 1
func (l Level) Valid() bool
type ID struct{ Level Level; LatIdx, LonIdx int }
func Of(p core.LatLon, l Level) (ID, error)          // *core.FieldError "level", "lat_deg", "lon_deg"
func (c ID) String() string                          // "c5:1317:2248"; "" for an invalid ID
func Parse(s string) (ID, error)                     // strict inverse of String; *core.FieldError "cell"
func (c ID) Valid() bool
func (c ID) Parent() ID                              // c5 -> its c3; a c3 returns itself
func (c ID) Children() []ID                          // c3 -> its 100 c5 in row-major order; a c5 returns nil
func (c ID) Ring1() []ID                             // up to 8 neighbours, longitude wrapped, 5 on the polar rows, sorted
func (c ID) BBox() geodesy.BBox                      // [south, north) x [west, east); east 180 for the last column
func (c ID) Centre() core.LatLon
func Cover(b geodesy.BBox, l Level, maxCells int) ([]ID, error) // sorted; antimeridian split; MaxLon 180 adds column 0; > maxCells -> *core.FieldError "bbox", nil
const MaxCoverDefault = 10_000
```

### 3.4 `internal/pgm`, `terrain`, `geoid` (WP-2)

```go
// internal/pgm
type Grid struct{ Width, Height int; MaxVal int; Header map[string]string; samples []byte }
func Parse(data []byte, maxBytes int) (*Grid, error)   // P5, 16-bit big-endian only; "# Key value" comments; FieldError names the byte offset
func (g *Grid) Raw(ix, iy int) uint16
func (g *Grid) Number(key string) (float64, error)

// geoid
type Grid struct{...}
func Parse(data []byte) (*Grid, error);  func Load(path string) (*Grid, error)
func (g *Grid) UndulationM(p core.LatLon) (float64, error)   // bilinear, wraps in longitude; error outside [-90, 90]
func (g *Grid) Description() string                          // "EGM2008 2.5'" etc. from the header
func AMSLFromHAE(altHAEM float64, undulationM float64) float64 // alt_amsl_m = alt_hae_m - N
func HAEFromAMSL(altAMSLM float64, undulationM float64) float64
type Undulator interface{ UndulationM(core.LatLon) (float64, error) }  // what zones and rid take

// terrain
func CellName(p core.LatLon) string                          // "N41E044": floor of the south-west corner
type Tile struct{...}
func ParseTile(data []byte) (*Tile, error)                   // the lab tool's PGM: Offset -500, Scale 0.2, Dataset, LatFirst, LonFirst, LatStep, LonStep; nodata 0xFFFF
func (t *Tile) ElevationM(p core.LatLon) *float64            // bilinear; edge clamp; nil if any corner is nodata
func (t *Tile) SpacingM() float64;  func (t *Tile) Dataset() string
type Elevation struct{ ElevationM float64; Dataset string; SpacingM float64 }
type Index map[string]string                                 // cell name -> dataset or "sea"; from index.json
type Store struct{...}
func NewStore(dir string, index Index, opts StoreOptions) *Store   // StoreOptions{MaxTiles int; RetryAfter time.Duration; Open func(name string) ([]byte, error)}
func (s *Store) Elevation(p core.LatLon) (*Elevation, error)  // nil,nil = unknown (not indexed, nodata, or tile unreadable: counted, retried once per RetryAfter)
func (s *Store) Counters() *core.Counters
type Ground interface{ Elevation(core.LatLon) (*Elevation, error) } // what zones takes
const Attribution = "Copernicus DEM ..."                      // D-05
```

### 3.5 `odid` (WP-3; types frozen in `odid/types.go`)

```go
// Decode one 25-byte message or a message pack. Refusals are *core.FieldError
// with Field "frame" / "pack[i]" and the reason phrases the vectors pin.
func Decode(frame []byte, opts DecodeOptions) ([]Message, error)
func DecodeMessage(b [MessageSize]byte, opts DecodeOptions) (Message, error)
func Encode(m Message) ([MessageSize]byte, error)            // round-trips the reference frames byte for byte (R-02)
func EncodePack(ms []Message) ([]byte, error)                // refuses what Decode refuses (R-03)
func TypeOf(frame []byte) (MessageType, error)               // the type nibble, length-checked
// Wire constants (from opendroneid-core-c, pinned by reference frames, E-03):
const SpecialDirection = 361; SpecialSpeedH = 255; SpecialSpeedV = 63; SpecialAltitude = -1000.0; SpecialTimestamp = 0xFFFF
```

### 3.6 `regnum`, `serial`, `sources` (WP-4)

```go
// regnum
const DefaultPattern = `^[A-Z]{3}[A-Za-z0-9]{8,16}$`          // G-07, configuration
type Validator struct{...};  func NewValidator(pattern string) (*Validator, error)
func (v *Validator) Validate(value string) error             // *core.FieldError{Field:"registration_number"}; refuses a hyphen (the secret part is never registered)
func PublicPart(value string) string                         // trim; strip "-XYZ" (3 ASCII alphanumerics) only after a number under the pattern (G-04)
func CompareKey(value string) string                         // strings.ToUpper(PublicPart(value))
func Public(value string) (public, compareKey string)

// serial
const MaxLen = 20
func ValidateCTA2063A(serial string) error                   // the rule in the vector header; FieldError{Field:"serial"}
func ValidateForClass(serial, classLabel string) error       // C1,C2,C3,C5,C6 need CTA; C0,C4,"" need non-empty (G-06)
func Normalize(serial string) string                         // trim, case kept (G-05)
func FoldKey(serial string) string                           // upper-cased lookup key; callers accept a fold match only when unique
func RequiresCTA(classLabel string) bool

// sources
type Control struct{ SourceType string; InstanceID *string; Enabled bool }   // InstanceID nil = the whole type
type State struct{ Controls []Control; DefaultDeny bool; Version uint64; Epoch string }
type Why string  // WhyType "type", WhyInstance "instance", WhyDefaultDeny "default_deny"
type Decision struct{ Enabled bool; WhyDisabled *Why }
func (s State) Query(sourceType string, instanceID *string) Decision   // type off beats instance on; instance off beats type on; no rows = enabled unless DefaultDeny
type Follower struct{...}                                    // holds the last applied State
func (f *Follower) Apply(in State) (applied bool)             // strictly higher Version within the same Epoch, or any Version under a new Epoch (B-09)
func (f *Follower) State() State;  func (f *Follower) Query(...) Decision   // with no state: everything enabled
```

### 3.7 `ed269` (WP-5)

```go
type Restriction string  // PROHIBITED, REQ_AUTHORISATION, CONDITIONAL, NO_RESTRICTION (ED-269 spelling kept exactly)
func (r Restriction) ZoneType() core.ZoneType
type Reason, Uom ("M","FT"), Purpose, YesNo string;  type VerticalRef = core.VerticalRef (AGL, AMSL, WGS84 as this project's extension, Z-05)
type Position = core.LatLon
type HorizontalProjection struct{ Type string ("Polygon"|"Circle"); Rings [][]Position; Center *Position; RadiusM *float64 }  // Rings in [lon,lat] order on the wire, Position in memory
type Volume struct{ Uom Uom; LowerLimit, UpperLimit *float64 (as written, in Uom); LowerRef, UpperRef VerticalRef; Projection HorizontalProjection }
func (v Volume) LowerM() *float64;  func (v Volume) UpperM() *float64;  func (v Volume) RadiusM() *float64   // feet * 0.3048 exactly
type Authority struct{ Name, Service, ContactName, SiteURL, Email, Phone *string; Purpose *Purpose; IntervalBefore *string }
type GeoZone struct{ Identifier, Country string; Name, Message, OtherReasonInfo *string; Type string; Restriction Restriction; Reason []Reason; RestrictionConditions []string; Region *int; RegulationExemption *YesNo; USpaceClass *string; Applicability []Period; ZoneAuthority []Authority; Geometry []Volume (exactly one, Z-04); ExtendedProperties map[string]any; extra map[string]any }
type Document struct{ Title, Description *string; Zones []GeoZone; Wrapper string ("features"|"UASZoneList"); extra ... }
// Applicability (Z-07, T-09). A naive time is a programming error: At must have a location; UTC is used for the comparison.
type DailyPeriod struct{ Days []time.Weekday (ANY = all); Start, End time.Time (clock + offset); Offset *time.Location }
type Period struct{ Permanent bool; Start, End *time.Time; Schedule []DailyPeriod }
func (p Period) Contains(at time.Time) bool;  func (d DailyPeriod) Contains(at time.Time) bool
func Applies(periods []Period, at time.Time) bool             // any period applies
func ParseApplicability(raw json.RawMessage) ([]Period, Problems)
// Parsing (Z-01..Z-06). Limits are the vector header: identifier 7, name 200, message 200, reasons 9, ring vertices 5000, problems 100.
type Problem struct{ Field, Reason string };  type Problems []Problem;  func (p Problems) Error() string;  Truncated int (count beyond 100)
type Limits struct{ IdentifierMax, NameMax, MessageMax, ReasonsMax, MaxRingVertices, MaxProblems, MaxDepth, MaxBytes int };  var DefaultLimits
func Parse(data []byte, lim Limits) (*Document, Problems)     // BOM accepted; both wrappers; depth-bounded; all or nothing
func ParseZone(raw json.RawMessage, lim Limits) (*GeoZone, Problems)
func Export(doc *Document) ([]byte, error);  func Feature(z *GeoZone) (map[string]any, error)   // export(parse(f)) == f by value; null optionals absent
```

### 3.8 `timeplace`, `rid` (WP-6)

```go
// timeplace
type Fallback string  // "", FallbackClockAhead "clock_ahead", FallbackTooOld "too_old", FallbackUnknown "unknown", FallbackInvalid "invalid"
type BroadcastPolicy struct{ ToleranceS, MaxLatencyS float64 }   // defaults 1, 5
type Placement struct{ TS, CapturedAt time.Time; Source core.TimeSource; Fallback Fallback }
// seconds after the hour from the wire (tenths; 0xFFFF unknown; >= 3600 invalid); accuracyCode per MAV_ODID_TIME_ACC (k = k/10 s, 0 unknown)
func PlaceBroadcast(timestampTenths uint16, tsAccuracyCode uint8, receivedAt time.Time, pol BroadcastPolicy) Placement
type NetworkPolicy struct{ MaxAgeS, ToleranceS, MaxLatencyS float64 }   // defaults 60, 1, 5
type NetworkNote string  // "", NoteAheadOfResponse, NoteClockAhead, NoteTooOld
func PlaceNetwork(stateTS time.Time, responseTS *time.Time, receivedAt time.Time, pol NetworkPolicy) (p Placement, note NetworkNote, shown bool)   // shown=false: older than MaxAge, not shown at all
func PlaceBatch(rxTS time.Time, ts []time.Time, maxSpacing time.Duration) (capturedAt []time.Time, clamped int)   // T-02: rx - (newest - ts); negative or > maxSpacing clamped and counted
func Times(p Placement, rxTS time.Time, backlog bool) core.Times

// rid
var Namespace = uuid "6f1c7d52-4a0b-5c1e-9d3a-2b8e41f07a65"          // never changes (I-06)
func AircraftID(idType odid.IDType, uaID string) string               // uuid5(ns, "<id_type>:<ua_id>")
func UnidentifiedID(transmitter string) string                        // uuid5(ns, "transmitter:<address>")
type Settings struct{ IdentityTTLS, MaxGapS, IdentifyWithinS float64 }   // defaults 15, 3, 4
type Frame struct{ Receiver, Transmitter string; Messages []odid.Message; NowS float64; RxTS time.Time }
type Observation struct{ DroneID, Label string; Identified bool; UAID string; IDType odid.IDType; OperatorID *string; Location odid.Location; System *odid.System; Receiver, Transmitter string; RxTS time.Time }
type Tracker struct{...};  func NewTracker(s Settings) *Tracker
func (t *Tracker) Take(f Frame) *Observation                          // nil = nothing published this step (R-13); counters identity_changes, silences, unidentified, address_conflicts
func (t *Tracker) Forget(nowS float64) int;  func (t *Tracker) Counters() *core.Counters;  func (t *Tracker) Transmitters() int   // bounded (E-10)
type AltInput struct{ AltHAEM, AltPressureM *float64; VertAccuracyCode uint8; UndulationM *float64 }
type AltPolicy struct{ MinVerticalAccuracy uint8 (2); HoldPressure bool; PressureHoldS float64 (10) }
type AltResult struct{ AltAMSLM *float64; Source core.AltSource (AltNone when nil) }
func SelectAltitude(in AltInput, pol AltPolicy) AltResult             // stateless (R-07, R-08 without hold)
type AltitudeSelector struct{...};  func NewAltitudeSelector(pol AltPolicy) *AltitudeSelector;  func (s *AltitudeSelector) Select(in AltInput, nowS float64) AltResult   // with the 10 s hold per track
func VelocityNED(speedMS, trackDeg, climbMS *float64) (vn, ve, vd *float64)   // all nil unless speed and track known (R-10)
func Airborne(st odid.Status) bool
```

### 3.9 `identify` (WP-7)

```go
type OperatorFacts struct{ OperatorID, RegistrationNumber, Status string }                 // active | suspended | revoked
type UASFacts struct{ DroneID, Label, Serial, RegistrationStatus string; OperatorID *string; InRegistry bool }
type Lookup interface {
    UASBySerial(serial string) (UASFacts, Match)   // Match: MatchExact, MatchFolded, MatchAmbiguous, MatchNone (G-05)
    UASByID(droneID string) (UASFacts, bool)
    Operator(operatorID string) (OperatorFacts, bool)
}
type Snapshot struct{...};  func NewSnapshot(ops []OperatorFacts, uas []UASFacts) *Snapshot   // implements Lookup; Empty()
func ResolveBroadcast(reg Lookup, serial, operatorReg *string) core.Identification          // kind "broadcast"; Basis as_broadcast
type RemoteIDIdentity struct{ Identified bool; UAID string; IDType odid.IDType; OperatorID *string }
func ResolveRemoteID(reg Lookup, id RemoteIDIdentity) core.Identification                   // kind "remote_id_block" (I-05)
func ResolveBound(reg Lookup, droneID string) core.Identification                           // kind "bound"; Basis authenticated; reason session_binding or the inactive reason
func SerialConflict(serial string, operatorReg *string) core.Identification                 // unknown_operator, serial_conflict, mismatch true
func Unavailable(serial, operatorReg *string) core.Identification                           // registry_unavailable (04 §3.2; no vector)
// Spoofing guard (I-08, I-09)
type AuthRow struct{ HeardAtS float64; Pos *core.LatLon; Backlog bool; BehindS float64; Source string ("" = authenticated telemetry; "remote_id"/"network_remote_id" never vouch) }
type FleetInput struct{ SerialIsOurs bool; Rows []AuthRow; Broadcast core.LatLon; NowS, LiveForS, SpoofDistanceM float64 }
type Verdict string  // VerdictStranger, VerdictWithhold, VerdictAsOurs, VerdictConflict
type FleetResult struct{ Verdict Verdict; ApartM *float64; IgnoredHistoryRows int }
func JudgeFleet(in FleetInput) FleetResult                                                  // haversine (D-11)
```

### 3.10 `zones` (WP-8)

```go
type Limit struct{ ValueM float64; Ref core.VerticalRef }
type Zone struct{ Identifier, Country string; Type core.ZoneType; Restriction ed269.Restriction; Lower, Upper *Limit; Shape geodesy.Polygon | geodesy.Circle (two optional fields); BBox geodesy.BBox; Periods []ed269.Period }
func FromED269(z *ed269.GeoZone) (*Zone, error)
func (z *Zone) ContainsHorizontally(p core.LatLon) (bool, error)   // bbox prefilter then polygon/circle (Z-06, Z-11)
func (z *Zone) AppliesAt(at time.Time) bool
func (z *Zone) NeedsTerrain() bool;  func (z *Zone) NeedsGeoid() bool
type GroundKind int  // GroundNotConfigured, GroundUnknown, GroundKnown
type Env struct{ Ground GroundKind; GroundM float64; UndulationM *float64 }     // what the caller resolved for this position
type Aircraft struct{ AltAMSLM *float64; AltSource core.AltSource }
type Policy struct{ PressureUncertaintyM float64 (250); ConditionalSeverity core.Severity (warning); MaxHeightAGLM *float64 }
type Raise struct{ Kind string ("zone"|"height"); Severity core.Severity; Detail Detail }
type Detail struct{ Identifier, Restriction string; VerticalKnown *bool; WithinBand *bool; LimitNotJudged *bool; NotJudged []string; HeightAGLM, AltHAEM, MaxHeightAGLM *float64 }
type Result struct{ Raise *Raise; NotEvaluated bool; LimitNotJudged bool }   // counters zone_checks_not_evaluated, zone_limits_not_judged
func JudgeVertical(z *Zone, ac Aircraft, env Env, pol Policy) Result        // the aircraft is already inside horizontally and the zone applies
func JudgeHeightLimit(ac Aircraft, env Env, pol Policy) Result               // the 120 m rule; strictly greater; unknown ground = not evaluated (D-04)
func Severity(t core.ZoneType, pol Policy) (core.Severity, bool)            // Z-10; false for NO_RESTRICTION
type Index struct{...};  func NewIndex(zs []*Zone) *Index;  func (i *Index) Candidates(p core.LatLon) []*Zone   // bbox grid prefilter
```

### 3.11 `cpa` (WP-9)

```go
type State struct{ Pos core.LatLon; AltAMSLM float64; VerticalKnown bool; VNMS, VEMS, VDMS float64; CapturedAtS float64 }   // VD positive down
type Policy struct{ TCPAMaxS, DHorizontalMinM, DVerticalMinM, NeighbourRadiusM, NeighbourMaxAgeS float64 }   // defaults 60, 60, 20, 800, 10
type Result struct{ Judged bool; TCPAS, DCPAHorizontalM, DAltAtCPAM, DHorizontalNowM, DAltNowM float64; VerticalKnown, Conflict bool }
func Advance(s State, toS float64) State                       // along its NED velocity; longitude wrapped
func Evaluate(a, b State, pol Policy) Result                   // symmetric (order-a-b == order-b-a); Judged false when |Δcaptured| > NeighbourMaxAgeS
type Grid struct{...};  func NewGrid(cellM float64) *Grid      // cellM >= radius (C-15)
func (g *Grid) Upsert(id string, p core.LatLon);  func (g *Grid) Remove(id string)
func (g *Grid) Near(p core.LatLon, radiusM float64) []string   // candidates within the ring; tested against brute force
```

### 3.12 `alerting` (WP-10)

```go
type Config struct{ Policy cpa.Policy; ClearAfterS, StaleAfterS, LiveMaxAgeS float64 (3, 15, 10); PressureUncertaintyM float64 (250); Zones []*zones.Zone; ZonePolicy zones.Policy; MismatchSeverity, IdentificationSeverity core.Severity (warning, critical) }
type Track struct{ ID string; Pos core.LatLon; AltAMSLM *float64; AltSource core.AltSource; VNMS, VEMS, VDMS float64; Flying *bool; CapturedAtS, RxAtS float64; SourceTS *float64; Backlog bool; Source, Station string; Transmitter *string; Identified *bool; Identification *core.Identification; Env zones.Env }
type Alert struct{ Key string; Kind string ("conflict"|"zone"|"identification"|"identification_mismatch"|"height"); Severity core.Severity; Aircraft []string; Detail map[string]any; RaisedAtS, LastTrueS, LastFalseS float64 }
type ClearReason string  // "resolved", "stale", "source_disabled"
type Events struct{ Raised []Alert; Cleared []Cleared }    // Cleared = Alert + Reason
type Monitor struct{...};  func NewMonitor(c Config) *Monitor
func (m *Monitor) Observe(tr Track, wallS float64) Events
func (m *Monitor) Tick(wallS float64) Events
func (m *Monitor) SwitchSource(st sources.State, wallS float64) Events   // or (sourceType string, instanceID *string, enabled bool)
func (m *Monitor) Active() []Alert;  func (m *Monitor) Counters() *core.Counters   // rejected_backlog, rejected_late, rejected_out_of_order, rejected_source_disabled, zone_checks_not_evaluated, zone_limits_not_judged
func (m *Monitor) Drop(id string, wallS float64) Events        // the aircraft left the picture (stale/landed)
```

### 3.13 `auth` (WP-11)

```go
// Receiver HMAC (R-06). The datagram is <report bytes>\nsig=<hex>.
func SignReport(key, report []byte) string                      // hex HMAC-SHA256
func Datagram(report []byte, sigHex string) []byte
type ReceiverVerifier struct{...};  func NewReceiverVerifier(keys map[string][]byte, maxSkew time.Duration, opts ...ReceiverOption) (*ReceiverVerifier, error)   // keys >= 32 bytes; empty id or key is an error; nonce memory bounded (E-10)
type Report struct{ ReceiverID, Transmitter, PayloadHex string; SentAtMS int64; Nonce string; Raw json.RawMessage }
func (v *ReceiverVerifier) Verify(datagram []byte, now time.Time) (Report, error)   // errors with the vector phrases: not signed, unknown receiver 'x', bad signature from x, sent N s from now, nonce repeated by x, no nonce, ...
func (v *ReceiverVerifier) Counters() *core.Counters
// JWT (00 §6.2, 06 §3), on lestrrat-go/jwx/v3.
type IssuerConfig struct{ JWKSURL string; Keys jwk.Set (tests) }
type Config struct{ Issuers map[string]IssuerConfig; Audience string; MaxSkew time.Duration (30 s); JWKSCacheTTL time.Duration (24 h); HTTPClient *http.Client; Now func() time.Time }
type Claims struct{ Issuer, Subject, Audience, JTI, KeyID string; Scopes []string; ExpiresAt, IssuedAt time.Time; Raw jwt.Token }
type Verifier struct{...};  func NewVerifier(ctx context.Context, c Config) (*Verifier, error)
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error)   // RS256 only; iss allow-listed; kid required; aud == Audience; exp within skew; jti present
func (c Claims) HasScope(scope string) bool;  func RequireScope(c Claims, scope string) error
func (v *Verifier) Counters() *core.Counters                    // rejected_<reason>, jwks_refresh, jwks_refresh_failed
type Issuer struct{...};  func NewIssuer(iss string, key *rsa.PrivateKey, kid string) *Issuer
func (i *Issuer) Issue(sub, aud string, scopes []string, ttl time.Duration, now time.Time) (string, error);  func (i *Issuer) JWKS() jwk.Set
```

### 3.14 `ed318`, `f3411`, `f3548` (WP-12, G-M2)

```go
// ed318
type UASZone struct{ Identifier, Country, Name string; Type core.ZoneType; Variant string; RestrictionConditions []string; Region *int; Reason []string; OtherReasonInfo, RegulationExemption, Message *string; ExtendedProperties map[string]any; LimitedApplicability []TimePeriod; ZoneAuthority []Authority; DataSource *DataSource }
type Geometry struct{ UomDimensions string; LowerLimit, UpperLimit *float64; LowerVerticalReference, UpperVerticalReference core.VerticalRef; HorizontalProjection geojson }
type TimePeriod / DailyPeriod with StartEvent/EndEvent (BMCT, SR, SS, EECT) resolved by a Daylight interface
type Feature struct{ Type string; Geometry Geometry; Properties UASZone };  type FeatureCollection struct{ Metadata; Features []Feature }
func Parse(data []byte, lim Limits) (*FeatureCollection, ed269.Problems);  func Export(fc *FeatureCollection) ([]byte, error)
func FromED269(doc *ed269.Document) (*FeatureCollection, error);  func ToZones(fc *FeatureCollection) ([]*zones.Zone, error)
// f3411, f3548: generated structs from the uas_standards OpenAPI (oapi-codegen, types only), the constants, the scopes, special-value helpers.
```

---

## 4. Third-party dependencies

| Module | Used by | Why it is allowed |
|---|---|---|
| `github.com/lestrrat-go/jwx/v3` | `auth` | Spec `00 §6.2` names it for the one JWT verifier. JWKS fetch and cache by `kid`, RS256-only enforcement and `jwt.Validate` options cover what the spec requires; writing JWKS handling by hand is the kind of code that fails quietly. Pure Go. |
| (G-M2) `github.com/oapi-codegen/oapi-codegen/v2` as a `go run` tool, not a runtime dependency | `f3411`, `f3548` | Generates the `uas_standards` types from the published OpenAPI files; output is committed with the `uas_standards` commit recorded. |
| (G-M2, test only) `github.com/google/go-cmp` | tests | Readable diffs of generated structs round-tripping schema examples. Allowed only in `_test.go`. |

Rejected: `uber/h3-go` (cgo; the partition key is a system concern, §11
gap 3), any GeoJSON or geometry library (the vectors pin ray casting and
Vincenty exactly; `geodesy` is 400 lines), `google/uuid` (uuid5 is 20
lines in `rid`), any logging library (a library does not log), any GeoTIFF
reader (D6).

---

## 5. Vectors and lessons per package

Every file is run by exactly one package (the manifest in
`vectors/manifest.go`). "Embodies" lists the LESSONS IDs whose rule the
package implements; the package's tests name them.

| Package | Vector file(s) (cases) | Embodies |
|---|---|---|
| `core` | none (types) | E-13, T-01 (the triplet), R-05 (basis), G-01 (statuses), E-09 (counters), C-09 (finite check) |
| `geodesy` | `geodesy.json` (17): `vincenty_inverse`, `compare`, `in_circle`, `in_polygon`, `local_offset_m`, `haversine_6371008.8` | D-09, D-10, D-11, Z-06 (ring bound), Z-11 (circle by centre and radius) |
| `terrain`, `geoid`, `internal/pgm` | `terrain_geoid.json` (48): `cell_name` 4, `geoid_undulation` 38 (8 synthetic, 30 GeographicLib references skipped without the grid file), `terrain_tile_elevation` 6 | D-02, D-04, D-05, D-07 (documented), D-08, R-07, B-06 |
| `odid` | `odid_decode.json` (187): 160 reference messages, 10 reference packs, 10 refusals, 7 edge cases | E-03, R-01, R-02, R-03, R-04, R-11 (Status.Airborne), R-12 (height reference kept), R-15 (raw kept by the caller; Authentication.Raw) |
| `regnum`, `serial` | `serials_and_registration.json` (41): `cta2063` 12, `serial_for_class` 9, `registration_number` 4, `public_registration_number` 12, `serial_fold` 4 | G-04, G-05, G-06, G-07, G-12 |
| `sources` | `source_control.json` (8) | B-09, B-10 (503 semantics documented for callers), B-11 (the reason travels), U-15 |
| `ed269` | `ed269_parse.json` (54), `zones_applicability.json` (32) | Z-01, Z-02, Z-03, Z-04, Z-05, Z-06, Z-07, T-09 |
| `timeplace` | `rid_time.json` (25): 18 broadcast, 7 network | T-01, T-02, T-05 (documented), T-07, T-08, T-12 |
| `rid` | `rid_identity.json` (24), `pressure_altitude.json` (16) | I-01, I-02, I-03, I-04, I-06, I-07 (documented limit), R-07, R-08, R-10, R-11, R-13, E-09, E-10 |
| `identify` | `identification_status.json` (43), `fleet_match.json` (14) | G-01, G-02, G-03, G-04, G-05, G-12, I-05, I-06, I-08, I-09, D-11, E-15 |
| `zones` | `zones_vertical.json` (49) | Z-06, Z-08, Z-09 (with S-37), Z-10, Z-11, R-09, D-01, D-04, T-09 |
| `cpa` | `cpa.json` (37) | C-01, C-02, C-03, C-04, C-09, C-10, C-15, C-19, D-10, E-15 |
| `alerting` | `alert_lifecycle.json` (35) | C-05, C-06, C-07, C-08 (republish hook), C-09, C-14, C-18, T-03, T-04, T-05, T-06, T-10, T-13, B-11, G-02, G-03, I-02, I-04, INV-03 |
| `auth` | `rid_receiver_auth.json` (14); `jwt_verify.json` (16, G-M3; written here by WP-11, now a lab file) | R-06, B-14 (duplicate credential is a startup error), 06 §3, E-14 |
| `ed318`, `f3411`, `f3548` | schema examples of `uas_standards`; an ED-318 round-trip vector (new, G-M2) | Z-03, Z-05, 04 §3.1 special values, 09 §1.4-1.6 |

---

## 6. Vector harness design

**Choice: a vendored, pinned copy in `vectors/testdata/`, refreshed by
`scripts/sync-vectors.sh`, pinned by `vectors/testdata/VERSION`
(uspace-lab commit, path, utm commit) and `SHA256SUMS`, checked in CI
offline (checksums) and online (byte diff against the lab at the pinned
commit).** Not a git submodule, because:

1. Go module zips do not contain submodule content, so a system running
   `go test github.com/rootxkit/uspace-core/...` from the module cache
   would find no vectors; a vendored copy travels with the module.
2. `knowledge/README.md` requires that "a vector changing under a
   repository must be a reviewed change there". A sync commit is that
   review; a submodule bump hides the diff.
3. A submodule needs network at clone time and breaks `go get` for
   consumers.

Mechanics (written in WP-0):

- `vectors.Load(t, "cpa.json")` locates the file relative to the harness
  source (`runtime.Caller`), so it works from a checkout and from the
  module cache.
- `File.Run` runs every case as a subtest, logging `why` and `source`;
  `File.RunOwned(t, "ussp", ...)` is for system repositories.
- `Case.Decode(t, &in, &exp)` decodes strictly (`DisallowUnknownFields`):
  a renamed field fails instead of reading as zero.
- `Case.ExpectedIsNull()` handles the files whose whole `expected` is
  `null` (`rid_time` network cases).
- `File.Header(t, "policy", &pol)` reads the extra header keys (policy,
  limits, defaults, ellipsoid, rule) that some files add.
- `vectors.Manifest` names the owning package and case count per file;
  `TestManifest` fails on a missing file, a count change, a stray file or
  a checksum mismatch; `TestVersionFile` checks every file's `utm_commit`
  against `VERSION`.
- CI step "every manifest file has a vector test" greps each package for
  the file name; a miss is a warning until a release tag, where it fails.

**Tolerance handling.** Each file's `tolerance` header is free text with
numbers inside ("exact", `1e-09`, "0.1 (the old monitor rounds detail to
0.1)"). The harness exposes `File.FloatTolerance(key)` which parses the
leading number when there is one. Tests do not read tolerances blindly:
each vector test declares the tolerance it applies per field as a named
constant that mirrors the header (for example `const tolDistanceM =
0.001` in `geodesy`), and asserts once that the header still says the same
(`FloatTolerance("vincenty distance_m") == 0.001`), so a tolerance change
in the lab is visible in the diff and in the test. Rules:

| Header says | Compare with |
|---|---|
| a number | `vectors.Near`/`NearPtr` with that number as an absolute tolerance |
| "exact" (times, booleans, strings, integers, verdicts) | `==` for strings/bools/ints; `time.Equal` for instants; `EqualStrPtr` for optional strings |
| "0.1 (rounded)" (alert details) | compare `got` rounded to 0.1 against `want` with tolerance 0.05, both ways documented in the test |
| "path and phrase as in must_include" (ED-269) | `strings.HasSuffix(field, field_endswith)` or `field == field`, and `strings.Contains(reason, reason_contains)`; the full `problems` list is compared as a set and a difference is reported as a `t.Logf` diff, not a failure, until the owner decides (§11 gap 6) |
| GeographicLib reference cases | skipped with `t.Skip("needs <grid file>")` when the file is absent; CI logs the skip count |

Stateful files (`rid_identity`, `alert_lifecycle`, pressure-hold cases,
`rid_receiver_auth` datagram sequences) feed steps in order to one fresh
component and compare `per_step[i]` after each step, then `counters` and
`active_after`.

How systems run the vectors: `go test -run 'Vectors' github.com/rootxkit/uspace-core/...`
in their CI (the module is in their build list; test files and testdata
ship in the module zip), plus their own `RunOwned` tests for the
adapters that map wire types onto core inputs.

---

## 7. Work packages

Each WP has a brief in `docs/WORKPACKAGES/WP-<k>.md` that is complete on
its own. Branch `feat/WP-<k>-<slug>`. Commit suffix `[WP-<k> G-M<n>]`.
Done-when always includes: gofmt, vet, staticcheck and golangci-lint
clean; `go test -race -shuffle=on` green; the named vector files passing
through `vectors.File.Run`; >= 90 % statement coverage of the owned
packages; every decoder fuzzed; every hot function benchmarked; the
package `doc.go` rewritten to describe what was built; CHANGELOG entry.

| WP | Slug | Owns (exclusively) | Vectors | Depends on | Milestone |
|---|---|---|---|---|---|
| WP-0 | `plan-scaffold` | `core/`, `vectors/`, `odid/types.go`, scripts, CI, docs | manifest | - | done on `plan/initial` |
| WP-1 | `geodesy` | `geodesy/` | `geodesy.json` | WP-0 | G-M1 |
| WP-2 | `terrain-geoid` | `terrain/`, `geoid/`, `internal/pgm/` | `terrain_geoid.json` | WP-0 | G-M1 |
| WP-3 | `odid-codec` | `odid/` except `types.go` (additions to it only by plan change) | `odid_decode.json` | WP-0 | G-M1 |
| WP-4 | `regnum-serial-sources` | `regnum/`, `serial/`, `sources/` | `serials_and_registration.json`, `source_control.json` | WP-0 | G-M1 |
| WP-5 | `ed269` | `ed269/` | `ed269_parse.json`, `zones_applicability.json` | WP-0 | G-M1 |
| WP-6 | `timeplace-rid` | `timeplace/`, `rid/` | `rid_time.json`, `rid_identity.json`, `pressure_altitude.json` | WP-0 (odid types frozen) | G-M1 |
| WP-7 | `identify` | `identify/` | `identification_status.json`, `fleet_match.json` | WP-1 (haversine), WP-4 (regnum, serial) | G-M1 |
| WP-8 | `zones` | `zones/` | `zones_vertical.json` | WP-1, WP-5 | G-M1 |
| WP-9 | `cpa` | `cpa/` | `cpa.json` | WP-1 | G-M1 |
| WP-10 | `alerting` | `alerting/` | `alert_lifecycle.json` | WP-4 (sources), WP-8, WP-9 | G-M1 |
| WP-11 | `auth` | `auth/` | `rid_receiver_auth.json`; adds `jwt_verify.json` | WP-0 | G-M1 (receiver), G-M3 (JWT vector) |
| WP-12 | `standards-types` | `ed318/`, `f3411/`, `f3548/` | ED-318 round-trip vector (new); `uas_standards` examples | WP-5, WP-8 | G-M2 |
| WP-13 | `release-policy` | `CHANGELOG.md`, `scripts/semver-gate.sh`, CI gate, tags | all | WP-1..WP-12 | G-M3 |
| WP-14 | `v1.1-additive` (JWS helpers, the `v1.1.0` release PR) | `auth/` | none (no vector changes in C1) | `v1.0.0`; release after WP-15, WP-16 | C1 (`v1.1.0`) |
| WP-15 | `geodesy-cell` | `geodesy/cell/` (new) | none | `v1.0.0` | C1 |
| WP-16 | `basis-skip-conflicts` | one constant in `core/ident.go`; `alerting.Config.SkipConflicts` | none | `v1.0.0` | C1 |

Waves (what can run in parallel):

```
wave 1 (now, 7 agents):   WP-1  WP-2  WP-3  WP-4  WP-5  WP-6  WP-11
wave 2 (after WP-1/4/5):  WP-7 (needs 1, 4)   WP-8 (needs 1, 5)   WP-9 (needs 1)
wave 3 (after 8, 9, 4):   WP-10
tag v0.1.0 = G-M1 when wave 3 merges and every manifest file is covered
wave 4:                   WP-12 (G-M2)
wave 5:                   WP-13 (G-M3, v1.0.0)
wave 6 (C1, 3 agents):    WP-14  WP-15  WP-16, then the v1.1.0 release PR (WP-14)
```

WP-1 is on the critical path and small (17 vectors): it goes first and is
reviewed first. WP-9 may start its pure math against the `geodesy`
signatures of §3.3 the day WP-1 opens its PR; its branch rebases onto
`main` once WP-1 merges.

Cross-WP conflicts are avoided by exclusive directory ownership. The only
shared files are `CHANGELOG.md` (one line per WP under Unreleased, added
in the WP's last commit; merge conflicts there are trivial) and
`docs/bench-targets.txt` (benchmark names are pre-assigned per package).

---

## 8. Engineering standards

### 8.1 Formatting and vetting
`gofmt -l .` must print nothing; `go vet ./...` with the default analysers
plus `golangci-lint`'s `govet enable-all` (minus `fieldalignment`,
`shadow`). CI fails on either. `go mod tidy` must be a no-op.

### 8.2 staticcheck and golangci-lint
`staticcheck ./...` (all checks except ST1000, ST1003: package comments
live in `doc.go`; identifiers follow E-13 with the unit suffix) and
`golangci-lint run` with `.golangci.yml` (v2 config): the standard set plus
`errorlint`, `exhaustive` (every switch over `IdentReason`, `ZoneType`,
`MessageType`, ... lists every value), `forbidigo` (no `panic`, no
`fmt.Print*`, no `log.*`, no `os.Exit` outside tests), `gosec`, `gocritic`
diagnostic+performance, `revive` exported-doc rules, `misspell` (UK; the
standards' American spellings allow-listed), `nolintlint`, `unparam`,
`prealloc`. Generated files in `f3411`/`f3548` (`*.gen.go`) are exempt
from style linters, never from `vet`.

### 8.3 Race detector
`go test -race -count=1 -shuffle=on ./...` in CI on every push. Every
stateful component (`rid.Tracker`, `alerting.Monitor`, `terrain.Store`,
`auth.ReceiverVerifier`, `auth.Verifier`, `sources.Follower`) has a test
that drives it from several goroutines, or documents "not safe for
concurrent use; one per goroutine" in its type comment and the race test
proves the documented single-goroutine use is enough for the vectors.

### 8.4 Fuzz tests for every decoder
`FuzzDecode` (odid, seeded with the 187 vector frames), `FuzzParse`
(ed269, seeded with the documents of the 52 cases incl. the BOM and byte
cases), `FuzzParsePGM` (internal/pgm), `FuzzParseTile`/`FuzzParseGrid`,
`FuzzVerifyReceiver` (auth datagrams), `FuzzVerifyJWT` (auth tokens, with
a generated key), `FuzzPlaceBroadcast` (timeplace: any tenths and
accuracy code never panic and always yield a placement), and at G-M2
`FuzzParseED318`, `FuzzUnmarshalRIDFlight`, `FuzzUnmarshalOperationalIntent`.
Property under fuzz: no panic, no unbounded allocation (inputs capped by
the documented limits), and for codecs `Encode(Decode(x)) == x` where
Decode succeeds. CI runs each target 10 s (`scripts/fuzz-smoke.sh`);
crashers found are committed under `<pkg>/testdata/fuzz/`.

### 8.5 No panics on untrusted input
Any byte slice, JSON, token, number or string that crosses a package
boundary is untrusted. Decoders return `*core.FieldError` or
`ed269.Problems`; indexes are bounds-checked; nesting depth, ring vertices,
message counts, nonce sets, tile caches and tracker tables have explicit
limits with a test past each limit (E-10). `forbidigo` enforces the
absence of `panic`; a reviewer checks for implicit panics (slice indexing
on wire lengths, integer division, nil maps) using the fuzz targets.

### 8.6 Benchmarks and targets
Hot functions have `Benchmark*` functions whose names are listed in
`docs/bench-targets.txt`; CI reports ns/op beside the target in the job
summary and never gates. Targets derive from spec `05 §1` at 5000 drones
on one core per process, with a 10x margin for GC, locking and the
process's other work:

| Function | Load at 5000 drones | Budget per call | Target ns/op |
|---|---|---|---|
| `odid.Decode` (one message) | 5000 observations/s into `rid-ingest`, up to 3 frames each | 200 µs per observation at one core; 10x margin | 1 000 (message), 3 000 (pack) |
| `timeplace.PlaceBroadcast`, `rid.Tracker.Take`, `rid.SelectAltitude` | same path | 200 µs total | 500 / 3 000 / 200 |
| `zones.JudgeVertical` + containment | 5000 tracks/s x ~20 candidate zones after bbox prefilter = 100 000 judgements/s | 10 µs, margin 2x (shared with CPA in `monitor`) | 5 000 per zone-track; `InPolygon100` 2 000 |
| `cpa.Evaluate` | 1 250 000 pair checks/s worst case over ~25 workers, 50 000/s/core budget (`05 §1`) | 20 µs per pair at budget; margin 10x | 2 000 |
| `cpa.Grid.Near` | one lookup per track per tick, 5000/s | 200 µs | 20 000 |
| `alerting.Monitor.Observe` (one track, ~10 neighbours, ~5 zones) | 5000/s per worker set | 200 µs | 50 000 |
| `identify.ResolveBroadcast` | 5000 RID + 5000 DP flights/s | 100 µs | 2 000 |
| `geodesy.Inverse` | per circular zone candidate | - | 2 000 |
| `auth.Verifier.Verify` | API calls, ~1000/s | 1 ms | 200 000 (RSA verify dominates; cache JWKS) |
| `auth.ReceiverVerifier.Verify` | 50 receivers x 1 batch/s, up to 3000 msg/s worst case | 300 µs | 5 000 |
| `ed269.Parse` (Luxembourg-sized file, 1400-vertex zone) | control plane, per publication | 50 ms | 2 000 000 |

### 8.7 Names and errors
Units in every name (E-13): `AltAMSLM`, `AltHAEM`, `HeightAGLM`,
`SpeedMS`, `TimeoutS` or `time.Duration`, `DistanceM`, `LatDeg`. Datums
never mix in one expression (D-01); a function that takes AMSL says so in
its parameter name. Errors name the field: `*core.FieldError{Field,
Reason}` for one problem, `ed269.Problems` for many, with the JSON path
(`features[3].geometry[0].upperLimit`) or the wire offset (`frame[12]`).
Error strings are lower-case, no trailing punctuation, and contain the
phrase the vector's `*_contains` pins.

### 8.8 Testing rules (CLAUDE.md, from LESSONS E-01..E-04, E-10, E-11)
E-01 every negative test has its positive twin; E-02 the success and
degraded branches are exercised and their output read; E-03 wire offsets
come from the reference and are pinned by diffing frames; E-04 a PR
reports what was run and seen, skips are reported as skips; E-10 every
bound is exceeded by a test; E-11 tests restore global state and pass
shuffled and raced. Vector tests are `TestVectors<File>` in
`<pkg>/vectors_test.go`.

---

## 9. CI workflow (`.github/workflows/ci.yml`, written)

Jobs on push to `main`, tags `v*`, and pull requests, ubuntu-latest, Go
from `go.mod`:

1. `build-vet-lint`: gofmt check, `go build`, `go vet`, `go mod tidy`
   clean, staticcheck v0.8.1, golangci-lint v2.14.0
   (`golangci/golangci-lint-action@v8`; both versions pinned, matching the
   `Makefile`, so `make tools lint` reproduces the job locally).
2. `test-race`: `go test -race -count=1 -shuffle=on -coverprofile` for the
   whole module; coverage summary printed; profile uploaded.
3. `vectors`: `scripts/check-vectors.sh` (SHA256SUMS offline; byte diff
   against uspace-lab at the pinned commit when reachable, required on
   `main`), then `go test -run 'Vector|Manifest|Version' -v ./...` with
   the log uploaded, then the manifest coverage step (warning until a
   release tag).
4. `fuzz-smoke`: every `Fuzz*` target 10 s; crashers uploaded.
5. `bench`: all benchmarks, `scripts/bench-report.sh` writes the
   target table into the job summary; artifact uploaded; never gating.
6. `gitleaks` (06 §4).
7. (WP-13) `tag`, on `v*` tags only, after jobs 1-4, 6 and
   govulncheck: `scripts/release-check.sh` (tag equals the top CHANGELOG
   heading, module path, `local_files` empty from v1, manifest coverage
   strict, the lab diff required), `scripts/consumer-check.sh` on the
   published module, then the GitHub release from the CHANGELOG.

The semver gate (WP-13) is its own workflow, `semver-gate.yml`, on pull
requests only: a diff under `vectors/testdata/` requires a CHANGELOG line
with the clause and a new lab pin, and a behaviour change also a new
major heading and the PR label `behaviour-change` (`00 §6.3`). It is not
a job of this workflow because a label change must start a fresh run
with fresh labels. `docs/RELEASING.md` §3.2 has the rules.

Branch protection on `main` requires jobs 1-4 and 6 and `semver-gate`
(the exact check names are in `docs/RELEASING.md` §7).

---

## 10. Versioning

- `v0.1.0` = G-M1: every manifest file passes against its package;
  `go test ./...` is the proof; the tag workflow fails if a manifest file
  has no test. Tagged by the owner from `main`.
- `v0.x` additive releases follow as WP-12 lands (G-M2: `v0.2.0`).
- `v1.0.0` = G-M3: CHANGELOG complete, the semver gate in CI, the
  `jwt_verify` vector added, the ED-318 round-trip vector added, API of
  §3 declared stable.
- From `v1`: within a major only additive changes (new functions, new
  optional fields, new vector cases that existing behaviour passes). A
  behavioural change to a judgement is a **major** even if no Go signature
  changes, ships with the changed vector (synced from the lab in the same
  PR) and a CHANGELOG entry naming the regulation or standard clause; two
  majors are maintained in parallel for six months on branches
  `release/v<N>`. Go module path gains `/v2` at the second major.
- Before `v1`, the same rule applies to the vector set: a PR that changes
  a vector's expected value bumps the minor and says why; the semver gate
  enforces it from WP-13 onward and reviewers enforce it before.

---

## 11. Spec gaps and decisions for the owner

| # | Gap | Resolution in this plan | Needs the owner? |
|---|---|---|---|
| 1 | `00 §6.3` lists the alert lifecycle under `cpa` and `pressure_altitude` under `zones`; `fleet_match` under `regnum`/`serial`; `rid_receiver_auth` under both `odid` and `auth`; `rid_identity` under `odid`. | Packages `alerting` and `rid` added; `fleet_match` in `identify`; receiver auth in `auth` only; `rid_identity` and `pressure_altitude` in `rid`. Import paths the spec names still exist. **Resolved:** the spec table matches this split since `uspace-lab@aa5187e` (lab PR #3). | None. |
| 2 | `00 §6.3` names `terrain` readers for "Copernicus GLO-30 / SRTM tiles". The vector pins the predecessor's PGM tile format (Offset -500, Scale 0.2, 1x1 degree cells), not GeoTIFF. | Core reads the PGM tiles; GeoTIFF conversion stays a lab tool (as in utm `tools/terrain_fetch.py`). | Confirm, or fund a stdlib GeoTIFF reader as a later WP. |
| 3 | `geodesy` is to hold "H3 helpers". The maintained Go binding (`uber/h3-go/v4`) is cgo; the partition key (`05 §3`) never crosses an external interface. | H3 is not in core at `v0.x`. Each system computes `cell5`/`cell3` in its ingest with the binding it chooses, behind an interface the system owns. Revisit for `v1` if two systems end up with two implementations (T12 risk). **Resolved (C1):** two systems did (authority `internal/cell`, USSP `c3:<row>:<col>`); the reconciliation (M35) replaces H3 with one pure-Go lat-lon grid in core, `geodesy/cell` (WP-15, `v1.1.0`); spec `05 §3` is amended by lab WP-L4. | None. |
| 4 | `alert_lifecycle.json#disarming-clears-as-stale` records the old behaviour that C-14 says the new system should improve (`landed`). `04 §3.3` lists `flight_ended` as a clear reason. | **Resolved:** the owner decided `landed` (C-14). The lab vector is now `disarming-clears-as-landed` (`uspace-lab@aa5187e`), and `alerting` passes it as written, with no override. `Monitor.Drop` still takes a reason the caller chooses (`flight_ended`). | None. |
| 5 | `identification_status.json` names the authenticated-session reason `session_binding` only through the `bound` kind, and `04 §3.2` lists `registry_unavailable` with no vector. | `identify.Unavailable` implemented from the spec text and unit-tested; a vector is proposed to the lab. The vector now uses the spec's codes (`matched`, `session_binding`) since `uspace-lab@aa5187e`, so the test maps nothing. | Lab to add the `registry_unavailable` case. |
| 6 | ED-269 refusals: the vectors list every problem the old reader found but bind only `must_include`. | `ed269` must produce `must_include`; the full list is compared and logged as a diff, not failed (§6). A stricter or looser reader is visible in CI logs. | Decide whether the full list becomes binding at `v1`. |
| 7 | `jwt_verify` vector "to add" (`00 §6.3`) and an ED-318 round-trip vector (`07` G-M2) did not exist. Vectors are generated in the lab from utm, which has neither. | WP-11 and WP-12 write the vector files in this repo under `vectors/testdata/` in the same shape, with `generated` saying "hand-written in uspace-core, not from utm" and `utm_commit` left as the pinned commit; they are proposed upstream to `uspace-lab/knowledge/vectors/` so the sync script keeps working (until merged, `check-vectors.sh` must ignore files listed in `VERSION` under `local_files`). `jwt_verify.json` is in the lab since `uspace-lab@aa5187e`. **Resolved:** `ed318_roundtrip.json` is in the lab since `uspace-lab@6b5b286` (lab PR #5); `local_files` is empty. | None. |
| 8 | `rid_identity` expected `rx_ts` values assume the tracker's `now_s` is offset from `2026-09-29T12:00:00Z`; the file says ids may differ but "one serial is always one id". | The test maps `now_s` onto that epoch; `rid.AircraftID` keeps the utm uuid5 namespace so ids match exactly (stronger than required). | None. |
| 9 | Registration number format (Q5) and whether the secret part is on air. | `regnum` keeps the pattern configurable. It strips the secret part (a hyphen and three ASCII letters or digits) only when what precedes it is a registration number under the pattern, so `GEO-OP-ABC` stays whole, and folds ASCII only (G-04, G-12). The vectors pin this per case with the pattern (`uspace-lab@aa5187e`). | GCAA (open question Q5). |
| 10 | `source_control.json` has no version/epoch cases; B-09 specifies them. | `sources.Follower.Apply` implemented from B-09 with unit tests; a vector proposed. | Lab to add cases. |
| 11 | `05 §1` gives no per-call budgets; §8.6 derives them. | Targets are design budgets, reported not gated; the lab's load tests (L-M2) are the proof. | None. |
| 12 | The spec says systems' CI "runs the vectors against the packages in every image" (`05 §7`). | Documented in §6: `go test -run Vectors github.com/rootxkit/uspace-core/...` plus `RunOwned` for adapters. WP-13 verifies it from a scratch module. | None. |

---

## 12. The `v1.1.0` additive release (C1)

Source: the cross-plan reconciliation of 2026-10-02 (`cross-plan-decisions.md`
§3 row C1, M27, M35, authority Q-A8 and Q-A9, cisp Q20), accepted by the
coordinator. Core's milestone id for it is **C1**; commit suffixes are
`[WP-<k> C1]`. Briefs: `docs/WORKPACKAGES/WP-14.md` (`auth` JWS helpers
and the release), `WP-15.md` (`geodesy/cell`), `WP-16.md`
(`core.BasisProvider`, `alerting.Config.SkipConflicts`). The sibling
plans say "core WP-14" for all of it; WP-15 and WP-16 are its parallel
parts.

### 12.1 What ships

| Item | Package | Decision | Consumers |
|---|---|---|---|
| `KeyRing`, `SignDetached`, `DetachedVerifier` (`X-JWS-Signature`: RFC 7515 App. F, RFC 7797 `b64:false`, `crit:["b64"]`, `alg RS256`, `kid`, `iat` <= 5 min), `SignCompact`, `CompactVerifier` (`application/jose`; `iss`, `aud` = callback host, `sub`, `iat`, `jti`, `body`) | `auth` (on the existing `jwx/v3`; no new dependency) | M26, M27, M19 | cisp WP-2 (may land first on `internal/jws` and switch), ansp WP-8, authority WP-6, every `/v1/cis/notifications` receiver |
| `Config.Audiences []string`, `Claims.Roles`, `Claims.Realm` | `auth` | M18, M20; **proposed beside C1, flagged** in WP-14 part 4 for the coordinator to keep or strike | every verifier (`*_AUDIENCES`) |
| `geodesy/cell`: `c5` = 0.1°, `c3` = 1°, `c5:<lat_idx>:<lon_idx>`, parent, ring-1, bbox cover | `geodesy/cell` (new) | M35, §11 gap 3 resolved | authority WP-10, ussp WP-6/WP-11 |
| `core.BasisProvider = "provider"` | `core` | Q-A8 | authority WP-14; `uspace-ui` `IdentBasis` |
| `alerting.Config.SkipConflicts` (+ `conflict_checks_skipped`) | `alerting` | Q-A9 | authority WP-12 |

### 12.2 The semver rule for this release

Everything above is additive under `docs/RELEASING.md` §1 and §10 of this
plan: new identifiers and optional fields whose zero value means "as
before"; no change to what any judgement returns; no file under
`vectors/testdata/` added, edited or removed, so the semver gate reports
"nothing to gate" on every C1 pull request and `uspace-lab@6b5b286`
stays the pin. The tag is `v1.1.0`, a minor: the module path keeps no
`/vN`, no `release/v1` branch is cut, and the `[1.1.0]` CHANGELOG section
declares the new identifiers stable. A new vector *file* for the JWS
helpers or the cells would be a major under `RELEASING.md` §3.2, so none
is written; WP-14 and WP-15 propose their cases to the lab instead (as a
new file to be synced at the next major, or as additive cases of
`geodesy.json`), and their unit tests are the pin until then.

§3 of this plan is the contract: each C1 pull request extends the
relevant §3 subsection (3.1, 3.3, 3.12, 3.13) with its signatures, and
§2 gains `geodesy/cell` (deps: `core`, `geodesy`).

### 12.3 Open questions (owner-only, from reconciliation §2.1; not decided here)

Core does not decide any of these. Each is recorded with the demo
default the reconciliation proposes; the code is written so that the
answer is a caller's configuration, never a constant here.

| Q | Question | Demo default until answered | Touches |
|---|---|---|---|
| cisp Q16 | Signing-key custody (06 T4: HSM/KMS, 90-day rotation). | File-mounted PEM on the droplet, two-key JWKS overlap; the state's custody in production. | `auth.KeyRing` holds keys the caller loaded and supports a retired key; it never reads a path. |
| authority Q-A10 | Registration-number format and whether the secret part is on air (spec Q5). | `regnum` pattern from `authority_policy`; compare on the public part. | Unchanged in C1 (`regnum` stays configurable, §11 gap 9). |
| ussp Q6 | Deviation and CPA threshold defaults (Art. 10(2)(d), spec Q17). | The demo policy (CPA 60 s / 60 m / 20 m / 800 m) shown with `policy_version`. | Unchanged in C1: `cpa.DefaultPolicy` and `alerting.DefaultConfig` keep the vector-pinned values; `SkipConflicts` defaults to false. |
| (new, owner) | Whether `RELEASING.md` §3.2 should let a new vector file that pins only new API (no existing judgement) count as additive. | As written: a new file is a major; C1 ships without vector files for the new API. | WP-14, WP-15 lab proposals. |
