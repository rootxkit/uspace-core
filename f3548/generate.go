package f3548

// The types in types.gen.go are generated from the ASTM F3548-21 OpenAPI
// file that InterUSS uas_standards pins (see SOURCE), types only
// (oapi-codegen.yaml), then rewritten by oapialias
// (../f3411/internal/oapialias) so that they depend on the standard
// library alone. `go generate ./f3548` reproduces them; it needs network
// access to fetch the OpenAPI file at its pinned commit.
// CI never runs it (owner decision on PR #16): TestGeneratedTypesUnchanged
// checks offline that types.gen.go is the file whose hash the last run
// recorded in SOURCE.

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config oapi-codegen.yaml https://raw.githubusercontent.com/interuss/astm-utm-protocol/1d3d8fbe75414e23d7e19ce35955770bea5e413f/utm.yaml
//go:generate go run ../f3411/internal/oapialias -file types.gen.go -source SOURCE
