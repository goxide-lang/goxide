// Package deriveextras implements explicit, type-directed prototype derives.
package deriveextras

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"strconv"
	"strings"
)

type generator struct{ imports map[string]string }

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
func Generate(kind string, spec *ast.TypeSpec) (string, map[string]string, error) {
	s, err := fields(spec)
	if err != nil {
		return "", nil, err
	}
	g := &generator{imports: map[string]string{}}
	var b strings.Builder
	switch kind {
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
		typ := typeText(t)
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
		typ := typeText(t)
		return "func(src " + typ + ") " + typ + " { if src == nil { return nil }; dst := make(" + typ + ", len(src)); for key, item := range src { dst[key] = " + child + " }; return dst }(" + x + ")", nil
	}
	return "", fmt.Errorf("Clone has no explicit implementation for %s (pointer, external, interface and synchronization types are not copied implicitly)", typeText(t))
}
func (g *generator) debug(t ast.Expr, x string) (string, error) {
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
		typ := typeText(t)
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
		typ := typeText(t)
		return "func(src " + typ + ") string { if src == nil { return \"nil\" }; keys := make([]string, 0, len(src)); for key := range src { keys = append(keys, key) }; " + g.imp("sort") + ".Strings(keys); parts := make([]string, 0, len(keys)); for _, key := range keys { item := src[key]; parts = append(parts, " + g.imp("strconv") + ".Quote(key) + \": \" + " + child + ") }; return \"{\" + " + g.imp("strings") + ".Join(parts, \", \") + \"}\" }(" + x + ")", nil
	}
	return "", fmt.Errorf("Debug has no explicit implementation for %s", typeText(t))
}
