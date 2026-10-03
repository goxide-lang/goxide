// Package deriveextras implements explicit, type-directed prototype derives.
package deriveextras

import (
	"bytes"
	"fmt"
	"github.com/hxsf/goxide/internal/enums"
	"go/ast"
	"go/format"
	"go/token"
	"strconv"
	"strings"
)

// Resolver supplies only frontend-verified helper bindings and stable type names.
type Resolver func(kind string, t ast.Expr) (string, bool)
type generator struct {
	imports   map[string]string
	resolvers []Resolver
}

func resolve(rs []Resolver, kind string, t ast.Expr) (string, bool) {
	for _, r := range rs {
		if value, ok := r(kind, t); ok {
			return value, true
		}
	}
	return "", false
}
func defaultValue(t ast.Expr, rs []Resolver) (string, error) {
	return DefaultValue(t, func(t ast.Expr) (string, bool) { return resolve(rs, "Default", t) })
}
func (g *generator) typeText(t ast.Expr) string {
	if name, ok := resolve(g.resolvers, "Type", t); ok {
		return name
	}
	return typeText(t)
}

func (g *generator) imp(name string) string { a := "__hgo_" + name; g.imports[a] = name; return a }
func typeText(t ast.Expr) string {
	var b bytes.Buffer
	_ = format.Node(&b, token.NewFileSet(), t)
	return b.String()
}
func scalar(name string) bool {
	switch name {
	case "bool", "string", "byte", "rune", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64":
		return true
	}
	return false
}
func fields(spec *ast.TypeSpec) (*ast.StructType, error) {
	if spec.Assign.IsValid() {
		return nil, fmt.Errorf("derive requires a defined type, not an alias")
	}
	if spec.TypeParams != nil {
		return nil, fmt.Errorf("generic derive is not supported")
	}
	s, ok := spec.Type.(*ast.StructType)
	if !ok {
		return nil, fmt.Errorf("derive requires a struct")
	}
	for _, f := range s.Fields.List {
		for _, name := range f.Names {
			if name.Name == "_" {
				return nil, fmt.Errorf("blank fields have no derive protocol")
			}
		}
		if len(f.Names) == 0 {
			return nil, fmt.Errorf("embedded fields are not supported")
		}
	}
	return s, nil
}

// Generate emits helpers rather than automatically modifying Go method sets.
// Non-builtin named fields require matching local helpers, checked by frontend.
func Generate(kind string, spec *ast.TypeSpec, resolvers ...Resolver) (string, map[string]string, error) {
	s, err := fields(spec)
	if err != nil {
		return "", nil, err
	}
	g := &generator{imports: map[string]string{}, resolvers: resolvers}
	var b strings.Builder
	switch kind {
	case "Default":
		fmt.Fprintf(&b, "func Default%s() %s { var out %s\n", spec.Name.Name, spec.Name.Name, spec.Name.Name)
		for _, f := range s.Fields.List {
			for _, n := range f.Names {
				x, e := defaultValue(f.Type, resolvers)
				if e != nil {
					return "", nil, fmt.Errorf("field %s: %w", n.Name, e)
				}
				fmt.Fprintf(&b, "out.%s = %s\n", n.Name, x)
			}
		}
		b.WriteString("return out\n}\n")
	case "Clone":
		fmt.Fprintf(&b, "func Clone%s(v *%s) %s { var out %s\n", spec.Name.Name, spec.Name.Name, spec.Name.Name, spec.Name.Name)
		for _, f := range s.Fields.List {
			for _, n := range f.Names {
				x, e := g.clone(f.Type, "v."+n.Name)
				if e != nil {
					return "", nil, fmt.Errorf("field %s: %w", n.Name, e)
				}
				fmt.Fprintf(&b, "out.%s = %s\n", n.Name, x)
			}
		}
		b.WriteString("return out\n}\n")
	case "Debug":
		parts := []string{strconv.Quote(spec.Name.Name + " {")}
		first := true
		for _, f := range s.Fields.List {
			for _, n := range f.Names {
				x, e := g.debug(f.Type, "v."+n.Name)
				if e != nil {
					return "", nil, fmt.Errorf("field %s: %w", n.Name, e)
				}
				prefix := " "
				if !first {
					prefix = ", "
				}
				first = false
				parts = append(parts, strconv.Quote(prefix+n.Name+": "), x)
			}
		}
		parts = append(parts, strconv.Quote(" }"))
		fmt.Fprintf(&b, "func Debug%s(v *%s) string { return %s }\n", spec.Name.Name, spec.Name.Name, strings.Join(parts, " + "))
	default:
		return "", nil, fmt.Errorf("unsupported derive %s", kind)
	}
	return b.String(), g.imports, nil
}
func (g *generator) clone(t ast.Expr, x string) (string, error) {
	if fn, ok := resolve(g.resolvers, "Clone", t); ok {
		return fn + "(&(" + x + "))", nil
	}
	switch t := t.(type) {
	case *ast.Ident:
		if scalar(t.Name) {
			return x, nil
		}
		if t.Name == "error" || t.Name == "any" {
			break
		}
		return "Clone" + t.Name + "(&" + x + ")", nil
	case *ast.ArrayType:
		child, e := g.clone(t.Elt, "src[i]")
		if e != nil {
			return "", e
		}
		typ := g.typeText(t)
		init := "var dst " + typ
		if t.Len == nil {
			init = "if src == nil { return nil }; dst := make(" + typ + ", len(src))"
		}
		return "func(src " + typ + ") " + typ + " { " + init + "; for i := range src { dst[i] = " + child + " }; return dst }(" + x + ")", nil
	case *ast.MapType:
		key, ok := t.Key.(*ast.Ident)
		if !ok || !scalar(key.Name) {
			return "", fmt.Errorf("Clone map keys must be built-in scalars")
		}
		child, e := g.clone(t.Value, "item")
		if e != nil {
			return "", e
		}
		typ := g.typeText(t)
		return "func(src " + typ + ") " + typ + " { if src == nil { return nil }; dst := make(" + typ + ", len(src)); for key, item := range src { dst[key] = " + child + " }; return dst }(" + x + ")", nil
	}
	return "", fmt.Errorf("Clone has no explicit implementation for %s (pointer, external, interface and synchronization types are not copied implicitly)", typeText(t))
}
func (g *generator) debug(t ast.Expr, x string) (string, error) {
	if fn, ok := resolve(g.resolvers, "Debug", t); ok {
		return fn + "(&(" + x + "))", nil
	}
	switch t := t.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string":
			return g.imp("strconv") + ".Quote(" + x + ")", nil
		case "bool":
			return g.imp("strconv") + ".FormatBool(" + x + ")", nil
		case "int", "int8", "int16", "int32", "int64", "rune":
			return g.imp("strconv") + ".FormatInt(int64(" + x + "), 10)", nil
		case "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte":
			return g.imp("strconv") + ".FormatUint(uint64(" + x + "), 10)", nil
		case "float32", "float64":
			bits := "64"
			if t.Name == "float32" {
				bits = "32"
			}
			return g.imp("strconv") + ".FormatFloat(float64(" + x + "), 'g', -1, " + bits + ")", nil
		case "error", "any":
			return "", fmt.Errorf("Debug needs an explicit implementation for %s", t.Name)
		default:
			return "Debug" + t.Name + "(&" + x + ")", nil
		}
	case *ast.ArrayType:
		child, e := g.debug(t.Elt, "src[i]")
		if e != nil {
			return "", e
		}
		typ := g.typeText(t)
		nilCheck := ""
		if t.Len == nil {
			nilCheck = "if src == nil { return \"nil\" }; "
		}
		return "func(src " + typ + ") string { " + nilCheck + "parts := make([]string, len(src)); for i := range src { parts[i] = " + child + " }; return \"[\" + " + g.imp("strings") + ".Join(parts, \", \") + \"]\" }(" + x + ")", nil
	case *ast.MapType:
		key, ok := t.Key.(*ast.Ident)
		if !ok || key.Name != "string" {
			return "", fmt.Errorf("Debug maps currently require string keys")
		}
		child, e := g.debug(t.Value, "item")
		if e != nil {
			return "", e
		}
		typ := g.typeText(t)
		return "func(src " + typ + ") string { if src == nil { return \"nil\" }; keys := make([]string, 0, len(src)); for key := range src { keys = append(keys, key) }; " + g.imp("sort") + ".Strings(keys); parts := make([]string, 0, len(keys)); for _, key := range keys { item := src[key]; parts = append(parts, " + g.imp("strconv") + ".Quote(key) + \": \" + " + child + ") }; return \"{\" + " + g.imp("strings") + ".Join(parts, \", \") + \"}\" }(" + x + ")", nil
	}
	return "", fmt.Errorf("Debug has no explicit implementation for %s", typeText(t))
}

// GenerateEnum retains nominal variant identity and delegates supported payloads.
func GenerateEnum(kind string, d enums.Decl, resolvers ...Resolver) (string, map[string]string, error) {
	if kind == "Default" {
		var selected *enums.Variant
		for i := range d.Variants {
			v := &d.Variants[i]
			if !v.Default {
				continue
			}
			if selected != nil {
				return "", nil, fmt.Errorf("Default requires exactly one default variant")
			}
			selected = v
		}
		if selected == nil {
			return "", nil, fmt.Errorf("Default requires exactly one default variant")
		}
		argument := ""
		if selected.Type != nil {
			var err error
			argument, err = defaultValue(selected.Type, resolvers)
			if err != nil {
				return "", nil, fmt.Errorf("default variant %s: %w", selected.Name, err)
			}
		}
		return "func Default" + d.Name + "() " + d.Name + " { return " + selected.Constructor + "(" + argument + ") }\n", nil, nil
	}
	g := &generator{imports: map[string]string{}, resolvers: resolvers}
	var b strings.Builder
	result := "string"
	if kind == "Clone" {
		result = d.Name
	}
	fmt.Fprintf(&b, "func %s%s(v *%s) %s { %s(*v); switch x:=(*v).(type){", kind, d.Name, d.Name, result, d.Validator)
	for _, v := range d.Variants {
		fmt.Fprintf(&b, "case %s: _=x;", v.CaseName)
		if kind == "Clone" {
			if v.Type == nil {
				fmt.Fprintf(&b, "return %s();", v.Constructor)
			} else {
				x, e := g.clone(v.Type, "x.Value")
				if e != nil {
					return "", nil, e
				}
				fmt.Fprintf(&b, "return %s(%s);", v.Constructor, x)
			}
		} else {
			if v.Type == nil {
				fmt.Fprintf(&b, "return %q;", d.Name+"::"+v.Name)
			} else {
				x, e := g.debug(v.Type, "x.Value")
				if e != nil {
					return "", nil, e
				}
				fmt.Fprintf(&b, "return %q + %s + %q;", d.Name+"::"+v.Name+"(", x, ")")
			}
		}
	}
	fmt.Fprintf(&b, "default: panic(%q)}}", "hgo: invalid enum "+d.Name)
	return b.String(), g.imports, nil
}

// DefaultValue selects only explicit prototype implementations. Local named
// helper identity is validated by the frontend, never inferred from spelling.
func DefaultValue(t ast.Expr, defaults ...func(ast.Expr) (string, bool)) (string, error) {
	for _, resolve := range defaults {
		if value, ok := resolve(t); ok {
			return value, nil
		}
	}
	switch x := t.(type) {
	case *ast.Ident:
		switch x.Name {
		case "bool":
			return "(0 != 0)", nil
		case "string":
			return `""`, nil
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "byte", "rune", "float32", "float64":
			return x.Name + "(0)", nil
		}
		return "Default" + x.Name + "()", nil
	case *ast.IndexExpr:
		if id, ok := x.X.(*ast.Ident); ok && id.Name == "Option" {
			d, e := enums.Builtin("Option", []string{typeText(x.Index)})
			if e != nil {
				return "", e
			}
			return d.Variants[1].Constructor + "()", nil
		}
	}
	return "", fmt.Errorf("Default has no explicit implementation for %s", typeText(t))
}
