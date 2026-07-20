package parse

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
)

// directiveTag marks a struct to be excluded from generation.
const directiveTag = "standin:ignore"

// Package is a parsed source package holding the fixture targets.
type Package struct {
	// Name is the package name (e.g., "model").
	Name string
	// Path is the import path.
	Path string
	// Dir is the absolute directory of the package; empty when unknown.
	Dir string
	// Structs holds the fixture targets, sorted by name.
	Structs []Struct
}

// Struct is a single fixture target.
type Struct struct {
	Name string
	// Fields holds the exported fields in definition order.
	Fields []Field
}

// Field is a settable field of a fixture target. Embedded fields appear as
// regular fields named after their type.
type Field struct {
	Name string
	Type types.Type
	Tag  reflect.StructTag
}

// Load loads the package identified by pattern (a relative path or an import
// path) and extracts the structs eligible for fixture generation.
// The second return value holds warnings for skipped types.
func Load(pattern string) (Package, []string, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedTypes | packages.NeedSyntax,
	}

	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return Package{}, nil, fmt.Errorf("load package %s: %w", pattern, err)
	}

	if len(pkgs) != 1 {
		return Package{}, nil, fmt.Errorf("expected exactly one package for %s, got %d", pattern, len(pkgs))
	}

	pkg := pkgs[0]

	if len(pkg.Errors) > 0 {
		msgs := make([]string, 0, len(pkg.Errors))
		for _, e := range pkg.Errors {
			msgs = append(msgs, e.Error())
		}

		return Package{}, nil, fmt.Errorf("load package %s: %s", pattern, strings.Join(msgs, "; "))
	}

	// Guard against loader results that carry no type information even
	// though no errors were reported.
	if pkg.Types == nil {
		return Package{}, nil, fmt.Errorf("load package %s: no type information", pattern)
	}

	structs, warnings := extract(pkg)

	return Package{
		Name:    pkg.Name,
		Path:    pkg.PkgPath,
		Dir:     pkg.Dir,
		Structs: structs,
	}, warnings, nil
}

func extract(pkg *packages.Package) ([]Struct, []string) {
	ignored := ignoredTypes(pkg.Syntax)
	scope := pkg.Types.Scope()

	var structs []Struct

	var warnings []string

	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() || obj.IsAlias() {
			continue
		}

		named, ok := obj.Type().(*types.Named)
		if !ok {
			continue
		}

		st, ok := named.Underlying().(*types.Struct)
		if !ok {
			continue
		}

		if ignored[name] {
			continue
		}

		if named.TypeParams().Len() > 0 {
			warnings = append(warnings, "skipping generic struct "+name)

			continue
		}

		structs = append(structs, structOf(name, st))
	}

	return structs, warnings
}

func structOf(name string, st *types.Struct) Struct {
	s := Struct{Name: name, Fields: make([]Field, 0, st.NumFields())}

	for i := range st.NumFields() {
		f := st.Field(i)
		if !f.Exported() {
			continue
		}

		s.Fields = append(s.Fields, Field{
			Name: f.Name(),
			Type: f.Type(),
			Tag:  reflect.StructTag(st.Tag(i)),
		})
	}

	return s
}

// ignoredTypes collects the names of types whose doc comment carries the
// standin:ignore directive.
func ignoredTypes(files []*ast.File) map[string]bool {
	ignored := make(map[string]bool)

	for _, file := range files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}

			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				if hasIgnoreDirective(gd.Doc) || hasIgnoreDirective(ts.Doc) {
					ignored[ts.Name.Name] = true
				}
			}
		}
	}

	return ignored
}

// hasIgnoreDirective reports whether the comment group contains a
// //standin:ignore line. Only the exact directive form is accepted — no
// space after the comment marker, following the Go directive convention —
// so ordinary prose mentioning "standin:ignore" never matches. A
// non-whitespace character right after the tag (e.g. standin:ignoreXYZ) is
// rejected too.
func hasIgnoreDirective(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}

	const directive = "//" + directiveTag

	for _, c := range doc.List {
		rest, ok := strings.CutPrefix(c.Text, directive)
		if !ok {
			continue
		}

		if rest == "" {
			return true
		}

		if r, _ := utf8.DecodeRuneInString(rest); unicode.IsSpace(r) {
			return true
		}
	}

	return false
}
