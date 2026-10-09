package macros

import (
	"fmt"
	"go/ast"
	"go/parser"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
)

// prepareSums preserves the original argument position (including question
// markers). Only the definition-site constructor/type names are substituted.
// The added functions contain no caller expressions or propagation markers.
func (e *engine) prepareSums(ts []tok) ([]tok, error) {
	var out []tok
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		if t.text != "Option" && t.text != "Result" {
			out = append(out, t)
			continue
		}
		if i+1 >= len(ts) || ts[i+1].text != "[" {
			return nil, at(t, "template sum types require fixed explicit type arguments")
		}
		end, err := closeGroup(ts, i+1)
		if err != nil {
			return nil, err
		}
		var args []string
		start := i + 2
		for j := start; j <= end; j++ {
			if j == end || ts[j].text == "," {
				arg := strings.TrimSpace(render(ts[start:j]))
				if !fixedType(arg) {
					return nil, at(t, "macro sum type arguments must be fixed builtin Go types; captured or unknown types are unsupported")
				}
				for _, typeToken := range ts[start:j] {
					if primitiveName(typeToken.text) {
						if err := e.checkPrimitiveBinding(typeToken); err != nil {
							return nil, err
						}
					}
				}
				args = append(args, arg)
				start = j + 1
				continue
			}
			if ts[j].text == "[" || ts[j].text == "(" || ts[j].text == "{" {
				j, err = closeGroup(ts, j)
				if err != nil {
					return nil, err
				}
			}
		}
		d, err := enums.Builtin(t.text, args)
		if err != nil {
			return nil, at(t, err.Error())
		}
		if end+2 < len(ts) && ts[end+1].text == ":" && ts[end+2].text == ":" {
			if end+3 >= len(ts) {
				return nil, at(t, "missing sum variant")
			}
			name := ts[end+3].text
			var variant *enums.Variant
			for n := range d.Variants {
				if d.Variants[n].Name == name {
					variant = &d.Variants[n]
					break
				}
			}
			if variant == nil {
				return nil, at(t, "unknown sum variant "+name)
			}
			key := "func " + d.Name + "::" + name
			alias := e.alias(key)
			if e.sumHelpers == nil {
				e.sumHelpers = map[string]string{}
			}
			if variant.Payload == "" {
				if end+4 < len(ts) && ts[end+4].text == "(" {
					return nil, at(t, "unit variant takes no arguments")
				}
				e.sumHelpers[key] = fmt.Sprintf("func %s() %s { return %s::%s }", alias, d.Name, d.Name, name)
				t.text = alias
				out = append(out, t)
				p := t
				p.before = ""
				p.text = "("
				out = append(out, p)
				p.text = ")"
				out = append(out, p)
			} else {
				if end+4 >= len(ts) || ts[end+4].text != "(" {
					return nil, at(t, "sum payload constructor requires argument")
				}
				e.sumHelpers[key] = fmt.Sprintf("func %s(value %s) %s { return %s::%s(value) }", alias, variant.Payload, d.Name, d.Name, name)
				t.text = alias
				out = append(out, t)
			}
			i = end + 3
		} else {
			t.text = e.alias("type " + d.Name)
			out = append(out, t)
			i = end
		}
	}
	return out, nil
}
func fixedType(source string) bool {
	x, err := parser.ParseExpr(source)
	if err != nil {
		return false
	}
	var valid func(ast.Expr) bool
	valid = func(x ast.Expr) bool {
		switch x := x.(type) {
		case *ast.Ident:
			switch x.Name {
			case "bool", "string", "byte", "rune", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128", "any", "error":
				return true
			}
			return false
		case *ast.StarExpr:
			return valid(x.X)
		case *ast.ArrayType:
			if x.Len != nil {
				if _, ok := x.Len.(*ast.BasicLit); !ok {
					return false
				}
			}
			return valid(x.Elt)
		case *ast.MapType:
			return valid(x.Key) && valid(x.Value)
		case *ast.ChanType:
			return valid(x.Value)
		case *ast.ParenExpr:
			return valid(x.X)
		}
		return false
	}
	return valid(x)
}
