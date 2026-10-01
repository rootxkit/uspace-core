// Command oapialias post-processes a types file that oapi-codegen wrote for
// a uas_standards OpenAPI file, so that the generated types need nothing
// beyond the standard library.
//
// The ASTM OpenAPI files that uas_standards pins attach a description to a
// reference by wrapping it in a one-element anyOf:
//
//	current_state:
//	  anyOf:
//	  - $ref: '#/components/schemas/RIDAircraftState'
//	  description: The most up-to-date state of the aircraft.
//
// oapi-codegen turns every anyOf into a union (a struct holding a
// json.RawMessage with As/From/Merge methods that import
// github.com/oapi-codegen/runtime), which would make the runtime module a
// dependency of uspace-core (docs/PLAN.md section 4 allows oapi-codegen
// only as a `go run` tool) and would hide the referenced type behind
// RawMessage. A one-element anyOf is exactly its element, so this command
// rewrites each such union into an alias of its one variant, removes the
// union's methods and the imports nothing uses any more, and replaces the
// package comment oapi-codegen writes with its "Code generated" line (the
// package is documented in doc.go). A union with more than one variant is
// an error: it would need a real union type and a decision.
//
//	go run ../f3411/internal/oapialias -file types.gen.go -source SOURCE
//
// With -check-spec SOURCE it instead fetches the OpenAPI file named by
// `spec_url` in SOURCE and fails unless its SHA-256 is `spec_sha256`, so
// that go generate never builds from a file that changed upstream under
// the same URL. It is the first go:generate step.
//
// With -source it also records the rewritten file's SHA-256 (of its LF
// form) as `generated_sha256` in the SOURCE file, which the package tests
// compare offline: a generated file edited by hand, or regenerated without
// committing the new hash, fails the build without network access.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	file := flag.String("file", "", "generated Go file to rewrite in place")
	source := flag.String("source", "", "SOURCE file in which to record the generated file's SHA-256")
	checkSpec := flag.String("check-spec", "", "SOURCE file whose spec_url must hash to its spec_sha256")
	flag.Parse()
	if *checkSpec != "" {
		if err := checkSpecHash(*checkSpec, http.DefaultClient); err != nil {
			fmt.Fprintln(os.Stderr, "oapialias:", err)
			os.Exit(1) //nolint:forbidigo // a command reports failure by its exit status
		}
		return
	}
	if *file == "" {
		fmt.Fprintln(os.Stderr, "oapialias: -file is required")
		os.Exit(2) //nolint:forbidigo // a command reports failure by its exit status
	}
	err := rewriteFile(*file)
	if err == nil && *source != "" {
		err = recordHash(*file, *source)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "oapialias:", err)
		os.Exit(1) //nolint:forbidigo // a command reports failure by its exit status
	}
}

// GeneratedHash is the SHA-256 of a generated file in its LF form, so
// that a checkout with CRLF line endings hashes the same.
func GeneratedHash(src []byte) string {
	sum := sha256.Sum256(bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n")))
	return hex.EncodeToString(sum[:])
}

// recordHash writes `generated_sha256 = <hash>` into the SOURCE file,
// replacing the line when there is one.
func recordHash(file, source string) error {
	src, err := os.ReadFile(file) //nolint:gosec // G304: the path is this command's own flag
	if err != nil {
		return err
	}
	return setKey(source, "generated_sha256", GeneratedHash(src))
}

// sourceKey reads `key = value` from a SOURCE file.
func sourceKey(source, key string) (string, error) {
	raw, err := os.ReadFile(source) //nolint:gosec // G304: the path is this command's own flag
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v), nil
		}
	}
	return "", fmt.Errorf("%s has no %s", source, key)
}

// maxSpecBytes bounds the OpenAPI file fetched (both are under 200 kB).
const maxSpecBytes = 16 << 20

// checkSpecHash fetches spec_url and compares its SHA-256 with
// spec_sha256.
func checkSpecHash(source string, client *http.Client) error {
	url, err := sourceKey(source, "spec_url")
	if err != nil {
		return err
	}
	want, err := sourceKey(source, "spec_sha256")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSpecBytes))
	if err != nil {
		return fmt.Errorf("fetching %s: %w", url, err)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("%s hashes %s, %s records %s: the pinned OpenAPI file changed", url, got, source, want)
	}
	fmt.Fprintf(os.Stderr, "oapialias: %s matches spec_sha256\n", url)
	return nil
}

// setKey sets `key = value` in a SOURCE file.
func setKey(source, key, value string) error {
	raw, err := os.ReadFile(source) //nolint:gosec // G304: the path is this command's own flag
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	line := key + " = " + value
	done := false
	for i, l := range lines {
		if k, _, ok := strings.Cut(l, "="); ok && strings.TrimSpace(k) == key {
			lines[i], done = line, true
		}
	}
	if !done {
		lines = append(lines, line)
	}
	return os.WriteFile(source, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

// rewriteFile rewrites path in place.
func rewriteFile(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, n, err := rewrite(src)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "oapialias: %s: %d one-variant unions made aliases\n", path, n)
	return nil
}

// rewrite returns src with every one-variant union made an alias, and the
// number of unions rewritten.
func rewrite(src []byte) ([]byte, int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "types.gen.go", src, parser.ParseComments)
	if err != nil {
		return nil, 0, err
	}
	unions := unionTypes(f)
	variants := map[string]string{}
	var multi []string
	for name := range unions {
		vs := variantsOf(f, name)
		if len(vs) != 1 {
			multi = append(multi, fmt.Sprintf("%s (%d variants)", name, len(vs)))
			continue
		}
		variants[name] = vs[0]
	}
	if len(multi) > 0 {
		sort.Strings(multi)
		return nil, 0, errors.New("unions that are not one reference: " + strings.Join(multi, ", "))
	}

	// Drop every method of a union, and the comments inside it.
	var removed []ast.Node
	decls := f.Decls[:0]
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && unions[receiverName(fd)] {
			removed = append(removed, fd)
			continue
		}
		decls = append(decls, d)
	}
	f.Decls = decls

	// Make each union an alias of its variant.
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			ts, ok := s.(*ast.TypeSpec)
			if !ok {
				continue
			}
			v, ok := variants[ts.Name.Name]
			if !ok {
				continue
			}
			removed = append(removed, ts.Type)
			ts.Assign = ts.Name.End()
			ts.Type = ast.NewIdent(v)
		}
	}

	f.Comments = keepComments(f, removed)
	pruneImports(f)

	var buf bytes.Buffer
	if err := (&printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}).Fprint(&buf, fset, f); err != nil {
		return nil, 0, err
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, 0, err
	}
	return out, len(variants), nil
}

// unionTypes finds the types oapi-codegen generates for anyOf and oneOf: a
// struct whose only field is `union json.RawMessage`.
func unionTypes(f *ast.File) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || st.Fields == nil || len(st.Fields.List) != 1 {
			return true
		}
		fld := st.Fields.List[0]
		if len(fld.Names) != 1 || fld.Names[0].Name != "union" {
			return true
		}
		if sel, ok := fld.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "RawMessage" {
			out[ts.Name.Name] = true
		}
		return true
	})
	return out
}

// variantsOf lists the result types of the As<Variant> methods of union
// name: one per variant.
func variantsOf(f *ast.File, name string) []string {
	var out []string
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || receiverName(fd) != name || !strings.HasPrefix(fd.Name.Name, "As") {
			continue
		}
		if fd.Type.Results == nil || len(fd.Type.Results.List) == 0 {
			continue
		}
		if id, ok := fd.Type.Results.List[0].Type.(*ast.Ident); ok {
			out = append(out, id.Name)
		}
	}
	return out
}

// receiverName is the type name of a method's receiver, "" for a function.
func receiverName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	t := fd.Recv.List[0].Type
	if st, ok := t.(*ast.StarExpr); ok {
		t = st.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// keepComments drops the comments inside removed nodes and replaces the
// package comment with its "Code generated" line.
func keepComments(f *ast.File, removed []ast.Node) []*ast.CommentGroup {
	var out []*ast.CommentGroup
	for _, cg := range f.Comments {
		if cg == f.Doc {
			var gen []*ast.Comment
			for _, c := range cg.List {
				if strings.HasPrefix(c.Text, "// Code generated ") {
					gen = append(gen, c)
				}
			}
			if len(gen) == 0 {
				continue
			}
			cg.List = gen
			out = append(out, cg)
			continue
		}
		inside := false
		for _, n := range removed {
			if cg.Pos() >= n.Pos() && cg.End() <= n.End() {
				inside = true
				break
			}
		}
		for _, d := range removedDocs(removed) {
			if d == cg {
				inside = true
			}
		}
		if !inside {
			out = append(out, cg)
		}
	}
	return out
}

// removedDocs are the doc comments of the removed methods.
func removedDocs(removed []ast.Node) []*ast.CommentGroup {
	var out []*ast.CommentGroup
	for _, n := range removed {
		if fd, ok := n.(*ast.FuncDecl); ok && fd.Doc != nil {
			out = append(out, fd.Doc)
		}
	}
	return out
}

// pruneImports removes the imports the rewritten file no longer uses.
func pruneImports(f *ast.File) {
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		specs := gd.Specs[:0]
		for _, s := range gd.Specs {
			if is, ok := s.(*ast.ImportSpec); ok && used[importName(is)] {
				specs = append(specs, s)
			}
		}
		gd.Specs = specs
	}
	decls := f.Decls[:0]
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT && len(gd.Specs) == 0 {
			continue
		}
		decls = append(decls, d)
	}
	f.Decls = decls
	imports := f.Imports[:0]
	for _, is := range f.Imports {
		if used[importName(is)] {
			imports = append(imports, is)
		}
	}
	f.Imports = imports
}

// importName is the name an import is used by.
func importName(is *ast.ImportSpec) string {
	if is.Name != nil {
		return is.Name.Name
	}
	p, err := strconv.Unquote(is.Path.Value)
	if err != nil {
		return ""
	}
	return p[strings.LastIndex(p, "/")+1:]
}
