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

// inferrer carries the source package identity needed to resolve
// same-package references and named-type conversions.
type inferrer struct {
	pkgPath string
	pkgName string
	targets map[string]bool
	graph   map[string][]string
}

// Fixtures computes the field expressions for each struct. pkgPath and
// pkgName identify the source package, whose name qualifies the emitted
// types. structs must already be filtered down to the set that gets a
// fixture function; references to types outside that set fall back to zero
// values.
func Fixtures(structs []parse.Struct, pkgPath, pkgName string) []Fixture {
	targets := make(map[string]bool, len(structs))
	for _, s := range structs {
		targets[s.Name] = true
	}

	inf := inferrer{pkgPath: pkgPath, pkgName: pkgName, targets: targets}
	inf.graph = inf.referenceGraph(structs)

	fixtures := make([]Fixture, 0, len(structs))

	for _, s := range structs {
		fx := Fixture{Name: s.Name}

		for _, f := range s.Fields {
			expr := inf.fieldExpr(f, s.Name)
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
func (inf inferrer) fieldExpr(f parse.Field, owner string) string {
	typ := types.Unalias(f.Type)

	if tag, ok := f.Tag.Lookup("fake"); ok {
		return inf.tagExpr(tag, typ)
	}

	if expr, ok := nameExpr(f.Name, typ); ok {
		return expr
	}

	if name, ok := inf.fixtureRef(typ); ok {
		if reaches(inf.graph, name, owner) {
			// Calling the fixture would recurse forever; fall back to the
			// zero value.
			return ""
		}

		return name + "()"
	}

	return typeExpr(typ)
}

// basicOf resolves typ to its basic type. For a named basic type declared in
// the source package (e.g., type Status string) it also returns the
// qualified name used to convert the emitted expression; named basic types
// from other packages are not resolvable.
func (inf inferrer) basicOf(typ types.Type) (*types.Basic, string, bool) {
	if b, ok := typ.(*types.Basic); ok {
		return b, "", true
	}

	named, ok := typ.(*types.Named)
	if !ok {
		return nil, "", false
	}

	b, ok := named.Underlying().(*types.Basic)
	if !ok {
		return nil, "", false
	}

	obj := named.Obj()
	if obj.Pkg() == nil || obj.Pkg().Path() != inf.pkgPath {
		return nil, "", false
	}

	return b, inf.pkgName + "." + obj.Name(), true
}

// qualify wraps expr in a conversion to the named type when qualifier is
// non-empty.
func qualify(expr, qualifier string) string {
	if qualifier == "" {
		return expr
	}

	return qualifier + "(" + expr + ")"
}

// stringTagCalls maps a known gofakeit tag template to a string-typed call.
var stringTagCalls = map[string]string{
	"{email}":     "gofakeit.Email()",
	"{firstname}": "gofakeit.FirstName()",
	"{lastname}":  "gofakeit.LastName()",
	"{name}":      "gofakeit.Name()",
	"{phone}":     "gofakeit.Phone()",
	"{url}":       "gofakeit.URL()",
	"{uuid}":      "gofakeit.UUID()",
	"{word}":      "gofakeit.Word()",
	"{city}":      "gofakeit.City()",
	"{country}":   "gofakeit.Country()",
}

// tagExpr resolves a fake struct tag. Unknown templates fall back to the
// mustGenerate helper for string fields and to the zero value otherwise.
// Named basic types from the source package are supported through a
// conversion, e.g. model.Status(gofakeit.Word()).
func (inf inferrer) tagExpr(tag string, typ types.Type) string {
	if tag == "" || tag == "skip" {
		return ""
	}

	if tag == "{date}" && isTime(typ) {
		return "gofakeit.Date()"
	}

	b, qualifier, ok := inf.basicOf(typ)
	if !ok {
		return ""
	}

	if expr, ok := stringTagCalls[tag]; ok && b.Kind() == types.String {
		return qualify(expr, qualifier)
	}

	if expr, ok := inf.paramTagExpr(tag, b, qualifier); ok {
		return expr
	}

	if b.Kind() == types.String {
		// mustGenerate is a helper emitted into the generated file; the
		// two-value gofakeit.Generate cannot be called in a composite literal.
		return qualify("mustGenerate("+strconv.Quote(tag)+")", qualifier)
	}

	return ""
}

type argClass int

const (
	classInt argClass = iota
	classUint
	classFloat
)

// argSpec constrains one template argument: it must be a literal of the
// class representable in the given bit width.
type argSpec struct {
	class argClass
	bits  int
}

// int and uint arguments are validated at 32 bits so the emitted literals
// compile on 32-bit platforms too.
var (
	intArg     = argSpec{class: classInt, bits: 32}
	uintArg    = argSpec{class: classUint, bits: 32}
	float32Arg = argSpec{class: classFloat, bits: 32}
	float64Arg = argSpec{class: classFloat, bits: 64}
)

type paramCall struct {
	fn     string
	args   []argSpec
	result types.BasicKind
}

// paramCalls maps a parameterized template name to a typed call.
var paramCalls = map[string]paramCall{
	"number":       {fn: "gofakeit.Number", args: []argSpec{intArg, intArg}, result: types.Int},
	"intrange":     {fn: "gofakeit.IntRange", args: []argSpec{intArg, intArg}, result: types.Int},
	"uintrange":    {fn: "gofakeit.UintRange", args: []argSpec{uintArg, uintArg}, result: types.Uint},
	"float32range": {fn: "gofakeit.Float32Range", args: []argSpec{float32Arg, float32Arg}, result: types.Float32},
	"float64range": {fn: "gofakeit.Float64Range", args: []argSpec{float64Arg, float64Arg}, result: types.Float64},
	"price":        {fn: "gofakeit.Price", args: []argSpec{float64Arg, float64Arg}, result: types.Float64},
	"sentence":     {fn: "gofakeit.Sentence", args: []argSpec{intArg}, result: types.String},
}

// paramTagExpr resolves a parameterized template like {number:1,10}. It only
// matches the calls in paramCalls, and only when every argument validates as
// a literal of the expected kind, so the emitted code always compiles.
func (inf inferrer) paramTagExpr(tag string, b *types.Basic, qualifier string) (string, bool) {
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

	// A numeric result also has to fit the field's kind, so that the
	// conversion never wraps the declared range into garbage (e.g.,
	// {intrange:-5,5} on a uint8 field is rejected, not wrapped).
	fieldSpec, checkField := argSpec{}, false

	if call.result != types.String {
		fs, ok := numericSpec(b.Kind())
		if !ok {
			return "", false
		}

		fieldSpec, checkField = fs, true
	}

	for i, arg := range args {
		arg = strings.TrimSpace(arg)
		if !validArg(arg, call.args[i]) {
			return "", false
		}

		if checkField && !validArg(arg, fieldSpec) {
			return "", false
		}

		args[i] = arg
	}

	return convert(call.fn+"("+strings.Join(args, ", ")+")", call.result, b, qualifier)
}

// validArg reports whether s is a literal of the class representable in the
// spec's bit width.
func validArg(s string, spec argSpec) bool {
	switch spec.class {
	case classInt:
		_, err := strconv.ParseInt(s, 10, spec.bits)

		return err == nil
	case classUint:
		_, err := strconv.ParseUint(s, 10, spec.bits)

		return err == nil
	case classFloat:
		f, err := strconv.ParseFloat(s, spec.bits)

		// Inf and NaN parse fine but are not valid Go literals.
		return err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
	default:
		return false
	}
}

// numericSpec returns the argument constraint matching a numeric field kind.
// Platform-sized kinds are constrained to 32 bits so the emitted literals
// compile everywhere.
func numericSpec(kind types.BasicKind) (argSpec, bool) {
	switch kind {
	case types.Int8:
		return argSpec{class: classInt, bits: 8}, true
	case types.Int16:
		return argSpec{class: classInt, bits: 16}, true
	case types.Int32:
		return argSpec{class: classInt, bits: 32}, true
	case types.Int64:
		return argSpec{class: classInt, bits: 64}, true
	case types.Int:
		return intArg, true
	case types.Uint8:
		return argSpec{class: classUint, bits: 8}, true
	case types.Uint16:
		return argSpec{class: classUint, bits: 16}, true
	case types.Uint32:
		return argSpec{class: classUint, bits: 32}, true
	case types.Uint64:
		return argSpec{class: classUint, bits: 64}, true
	case types.Uint, types.Uintptr:
		return uintArg, true
	case types.Float32:
		return float32Arg, true
	case types.Float64:
		return float64Arg, true
	case types.Invalid, types.Bool, types.String, types.Complex64, types.Complex128, types.UnsafePointer,
		types.UntypedBool, types.UntypedInt, types.UntypedRune, types.UntypedFloat,
		types.UntypedComplex, types.UntypedString, types.UntypedNil:
		return argSpec{}, false
	default:
		return argSpec{}, false
	}
}

// convert adapts a call returning `result` to the field's basic type.
// Numeric results are wrapped in a conversion when the field is a different
// numeric kind; a named-type qualifier subsumes the numeric conversion.
// String results never convert across kinds (string(int) is a rune
// conversion).
func convert(expr string, result types.BasicKind, b *types.Basic, qualifier string) (string, bool) {
	if result == types.String {
		if b.Kind() != types.String {
			return "", false
		}

		return qualify(expr, qualifier), true
	}

	if b.Info()&(types.IsInteger|types.IsFloat) == 0 {
		return "", false
	}

	if qualifier != "" {
		return qualify(expr, qualifier), true
	}

	if b.Kind() == result {
		return expr, true
	}

	return b.Name() + "(" + expr + ")", true
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
func (inf inferrer) referenceGraph(structs []parse.Struct) map[string][]string {
	g := make(map[string][]string)

	for _, s := range structs {
		for _, f := range s.Fields {
			if name, ok := inf.fixtureRef(types.Unalias(f.Type)); ok {
				g[s.Name] = append(g[s.Name], name)
			}
		}
	}

	return g
}

// fixtureRef reports the fixture target referenced by t as a value field of a
// same-package named struct.
func (inf inferrer) fixtureRef(t types.Type) (string, bool) {
	named, ok := t.(*types.Named)
	if !ok {
		return "", false
	}

	obj := named.Obj()
	if obj.Pkg() == nil || obj.Pkg().Path() != inf.pkgPath || !inf.targets[obj.Name()] {
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
