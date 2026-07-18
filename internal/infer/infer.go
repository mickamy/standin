package infer

import (
	"go/types"
	"math"
	"strconv"
	"strings"

	"github.com/mickamy/standin/internal/parse"
)

// FieldValue is one field assignment in a generated fixture literal.
type FieldValue struct {
	Name string
	Expr string
}

// Fixture is the inferred body of one fixture function. Fields holds only
// the fields that get a non-zero value, in definition order.
type Fixture struct {
	Name   string
	Fields []FieldValue
}

// Fixtures computes the field expressions for each struct. structs must
// already be filtered down to the set that gets a fixture function;
// references to types outside that set fall back to zero values.
func Fixtures(structs []parse.Struct, pkgPath string) []Fixture {
	targets := make(map[string]bool, len(structs))
	for _, s := range structs {
		targets[s.Name] = true
	}

	graph := referenceGraph(structs, pkgPath, targets)

	fixtures := make([]Fixture, 0, len(structs))

	for _, s := range structs {
		fx := Fixture{Name: s.Name}

		for _, f := range s.Fields {
			expr := fieldExpr(f, pkgPath, targets, graph, s.Name)
			if expr == "" {
				continue
			}

			fx.Fields = append(fx.Fields, FieldValue{Name: f.Name, Expr: expr})
		}

		fixtures = append(fixtures, fx)
	}

	return fixtures
}

// fieldExpr returns the Go expression assigned to the field, applying the
// inference rules in priority order. An empty string means the field is
// omitted from the literal (zero value).
func fieldExpr(f parse.Field, pkgPath string, targets map[string]bool, graph map[string][]string, owner string) string {
	typ := types.Unalias(f.Type)

	if tag, ok := f.Tag.Lookup("fake"); ok {
		return tagExpr(tag, typ)
	}

	if expr, ok := nameExpr(f.Name, typ); ok {
		return expr
	}

	if name, ok := fixtureRef(typ, pkgPath, targets); ok {
		if reaches(graph, name, owner) {
			// Calling the fixture would recurse forever; fall back to the
			// zero value.
			return ""
		}

		return name + "()"
	}

	return typeExpr(typ)
}

type tagCall struct {
	expr    string
	matches func(types.Type) bool
}

// tagCalls maps a known gofakeit tag template to a typed call.
var tagCalls = map[string]tagCall{
	"{email}":     {expr: "gofakeit.Email()", matches: isString},
	"{firstname}": {expr: "gofakeit.FirstName()", matches: isString},
	"{lastname}":  {expr: "gofakeit.LastName()", matches: isString},
	"{name}":      {expr: "gofakeit.Name()", matches: isString},
	"{phone}":     {expr: "gofakeit.Phone()", matches: isString},
	"{url}":       {expr: "gofakeit.URL()", matches: isString},
	"{uuid}":      {expr: "gofakeit.UUID()", matches: isString},
	"{word}":      {expr: "gofakeit.Word()", matches: isString},
	"{city}":      {expr: "gofakeit.City()", matches: isString},
	"{country}":   {expr: "gofakeit.Country()", matches: isString},
	"{date}":      {expr: "gofakeit.Date()", matches: isTime},
}

// tagExpr resolves a fake struct tag. Unknown templates fall back to
// gofakeit.Generate for string fields and to the zero value otherwise.
func tagExpr(tag string, typ types.Type) string {
	if tag == "" || tag == "skip" {
		return ""
	}

	if call, ok := tagCalls[tag]; ok && call.matches(typ) {
		return call.expr
	}

	if expr, ok := paramTagExpr(tag, typ); ok {
		return expr
	}

	if isString(typ) {
		return "gofakeit.Generate(" + strconv.Quote(tag) + ")"
	}

	return ""
}

type argKind int

const (
	argInt argKind = iota
	argUint
	argFloat
)

type paramCall struct {
	fn     string
	args   []argKind
	result types.BasicKind
}

// paramCalls maps a parameterized template name to a typed call.
var paramCalls = map[string]paramCall{
	"number":       {fn: "gofakeit.Number", args: []argKind{argInt, argInt}, result: types.Int},
	"intrange":     {fn: "gofakeit.IntRange", args: []argKind{argInt, argInt}, result: types.Int},
	"uintrange":    {fn: "gofakeit.UintRange", args: []argKind{argUint, argUint}, result: types.Uint},
	"float32range": {fn: "gofakeit.Float32Range", args: []argKind{argFloat, argFloat}, result: types.Float32},
	"float64range": {fn: "gofakeit.Float64Range", args: []argKind{argFloat, argFloat}, result: types.Float64},
	"price":        {fn: "gofakeit.Price", args: []argKind{argFloat, argFloat}, result: types.Float64},
	"sentence":     {fn: "gofakeit.Sentence", args: []argKind{argInt}, result: types.String},
}

// paramTagExpr resolves a parameterized template like {number:1,10}. It only
// matches the calls in paramCalls, and only when every argument validates as
// a literal of the expected kind, so the emitted code always compiles.
func paramTagExpr(tag string, typ types.Type) (string, bool) {
	body, ok := strings.CutPrefix(tag, "{")
	if !ok {
		return "", false
	}

	body, ok = strings.CutSuffix(body, "}")
	if !ok {
		return "", false
	}

	name, rawArgs, ok := strings.Cut(body, ":")
	if !ok {
		return "", false
	}

	call, ok := paramCalls[name]
	if !ok {
		return "", false
	}

	args := strings.Split(rawArgs, ",")
	if len(args) != len(call.args) {
		return "", false
	}

	for i, arg := range args {
		arg = strings.TrimSpace(arg)
		if !validArg(arg, call.args[i]) {
			return "", false
		}

		args[i] = arg
	}

	return convert(call.fn+"("+strings.Join(args, ", ")+")", call.result, typ)
}

// validArg reports whether s is a Go literal of the expected kind.
func validArg(s string, kind argKind) bool {
	switch kind {
	case argInt:
		_, err := strconv.ParseInt(s, 10, 64)

		return err == nil
	case argUint:
		_, err := strconv.ParseUint(s, 10, 64)

		return err == nil
	case argFloat:
		f, err := strconv.ParseFloat(s, 64)

		// Inf and NaN parse fine but are not valid Go literals.
		return err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
	default:
		return false
	}
}

// convert adapts a call returning `result` to the field type. Numeric results
// are wrapped in a conversion when the field is a different numeric kind;
// string results never convert (string(int) is a rune conversion).
func convert(expr string, result types.BasicKind, typ types.Type) (string, bool) {
	b, ok := typ.(*types.Basic)
	if !ok {
		return "", false
	}

	if b.Kind() == result {
		return expr, true
	}

	if result != types.String && b.Info()&(types.IsInteger|types.IsFloat) != 0 {
		return b.Name() + "(" + expr + ")", true
	}

	return "", false
}

// nameCalls maps a field name to a gofakeit call for string fields.
var nameCalls = map[string]string{
	"Email":     "gofakeit.Email()",
	"Name":      "gofakeit.Name()",
	"FirstName": "gofakeit.FirstName()",
	"LastName":  "gofakeit.LastName()",
	"Phone":     "gofakeit.Phone()",
	"URL":       "gofakeit.URL()",
	"UUID":      "gofakeit.UUID()",
	"Address":   "gofakeit.Address().Address",
	"City":      "gofakeit.City()",
	"Country":   "gofakeit.Country()",
}

// nameExpr applies the field-name heuristics. It only matches when the field
// type agrees with the call's result type.
func nameExpr(name string, typ types.Type) (string, bool) {
	if expr, ok := nameCalls[name]; ok && isString(typ) {
		return expr, true
	}

	if strings.HasSuffix(name, "At") && isTime(typ) {
		return "gofakeit.Date()", true
	}

	return "", false
}

// typeExpr applies the type-based rules. Pointers, slices, maps, interfaces,
// channels, funcs, and named non-struct types all fall back to zero values.
func typeExpr(typ types.Type) string {
	if isTime(typ) {
		return "gofakeit.Date()"
	}

	b, ok := typ.(*types.Basic)
	if !ok {
		return ""
	}

	switch b.Kind() {
	case types.String:
		return "gofakeit.Word()"
	case types.Bool:
		return "gofakeit.Bool()"
	case types.Int:
		return "gofakeit.Int()"
	case types.Int8:
		return "gofakeit.Int8()"
	case types.Int16:
		return "gofakeit.Int16()"
	case types.Int32:
		return "gofakeit.Int32()"
	case types.Int64:
		return "gofakeit.Int64()"
	case types.Uint:
		return "gofakeit.Uint()"
	case types.Uint8:
		return "gofakeit.Uint8()"
	case types.Uint16:
		return "gofakeit.Uint16()"
	case types.Uint32:
		return "gofakeit.Uint32()"
	case types.Uint64:
		return "gofakeit.Uint64()"
	case types.Float32:
		return "gofakeit.Float32()"
	case types.Float64:
		return "gofakeit.Float64()"
	case types.Invalid, types.Uintptr, types.Complex64, types.Complex128, types.UnsafePointer,
		types.UntypedBool, types.UntypedInt, types.UntypedRune, types.UntypedFloat,
		types.UntypedComplex, types.UntypedString, types.UntypedNil:
		return ""
	default:
		return ""
	}
}

// referenceGraph collects, per struct, the same-package fixture targets it
// references as value fields.
func referenceGraph(structs []parse.Struct, pkgPath string, targets map[string]bool) map[string][]string {
	g := make(map[string][]string)

	for _, s := range structs {
		for _, f := range s.Fields {
			if name, ok := fixtureRef(types.Unalias(f.Type), pkgPath, targets); ok {
				g[s.Name] = append(g[s.Name], name)
			}
		}
	}

	return g
}

// fixtureRef reports the fixture target referenced by t as a value field of a
// same-package named struct.
func fixtureRef(t types.Type, pkgPath string, targets map[string]bool) (string, bool) {
	named, ok := t.(*types.Named)
	if !ok {
		return "", false
	}

	obj := named.Obj()
	if obj.Pkg() == nil || obj.Pkg().Path() != pkgPath || !targets[obj.Name()] {
		return "", false
	}

	if _, ok := named.Underlying().(*types.Struct); !ok {
		return "", false
	}

	return obj.Name(), true
}

// reaches reports whether `to` is reachable from `from` in the reference
// graph. A field edge A -> B is downgraded to a zero value when B reaches A,
// which breaks every cycle (including self-references).
func reaches(g map[string][]string, from, to string) bool {
	if from == to {
		return true
	}

	seen := map[string]bool{from: true}
	queue := []string{from}

	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]

		for _, m := range g[n] {
			if m == to {
				return true
			}

			if !seen[m] {
				seen[m] = true
				queue = append(queue, m)
			}
		}
	}

	return false
}

func isString(typ types.Type) bool {
	b, ok := typ.(*types.Basic)

	return ok && b.Kind() == types.String
}

func isTime(typ types.Type) bool {
	named, ok := typ.(*types.Named)
	if !ok {
		return false
	}

	obj := named.Obj()

	return obj.Pkg() != nil && obj.Pkg().Path() == "time" && obj.Name() == "Time"
}
