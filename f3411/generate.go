package f3411

// The types in types.gen.go are generated from the ASTM F3411-22a OpenAPI
// file that InterUSS uas_standards pins (see SOURCE), types only
// (oapi-codegen.yaml), then rewritten by oapialias so that they depend on
// the standard library alone. `go generate ./f3411` reproduces them; it
// needs network access to fetch the OpenAPI file at its pinned commit.
// CI never runs it (owner decision on PR #16): TestGeneratedTypesUnchanged
// checks offline that types.gen.go is the file whose hash the last run
// recorded in SOURCE.

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config oapi-codegen.yaml https://raw.githubusercontent.com/uastech/standards/dd4016b09fc8cb98f30c2a17b5a088fb2995ab54/remoteid/updated.yaml
//go:generate go run ./internal/oapialias -file types.gen.go -source SOURCE
