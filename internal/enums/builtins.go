package enums

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

// Builtin returns the nominal case ABI for an explicitly instantiated built-in.
// Constructors and markers carry every type parameter, including phantom ones.
func Builtin(family string, args []string) (Decl, error) {
	count := 1
	if family == "Result" {
		count = 2
	} else if family != "Option" {
		return Decl{}, fmt.Errorf("unknown builtin sum family %s", family)
	}
	if len(args) != count {
		return Decl{}, fmt.Errorf("%s requires %d explicit type arguments", family, count)
	}
	norm := make([]string, len(args))
	for i, arg := range args {
		e, err := parser.ParseExpr(arg)
		if err != nil {
			return Decl{}, err
		}
		var b bytes.Buffer
		_ = format.Node(&b, token.NewFileSet(), e)
		norm[i] = b.String()
	}
	suffix := "[" + strings.Join(norm, ",") + "]"
	d := Decl{Name: family + suffix, Family: family, TypeArgs: norm, Marker: "__hgo_enum_marker_" + family, Validator: fmt.Sprintf("__hgo_enum_inst_%x", sha256.Sum256([]byte(family+suffix)))}
	names := []string{"Some", "None"}
	payloads := []string{norm[0], ""}
	if family == "Result" {
		names = []string{"Ok", "Err"}
		payloads = []string{norm[0], norm[1]}
	}
	for i, n := range names {
		var typ ast.Expr
		if payloads[i] != "" {
			typ, _ = parser.ParseExpr(payloads[i])
		}
		d.Variants = append(d.Variants, Variant{Name: n, Payload: payloads[i], Type: typ, CaseName: "__hgo_enum_case_" + family + "_" + n + suffix, Constructor: "__hgo_enum_ctor_" + family + "_" + n + suffix})
	}
	return d, nil
}
func (l *lowerer) builtinAt(i int) (Decl, int, error) {
	end, err := l.close(i + 1)
	if err != nil {
		return Decl{}, 0, err
	}
	var args []string
	start := i + 2
	for j := start; j <= end; j++ {
		if j == end || l.ts[j].text == "," {
			if j == start {
				return Decl{}, 0, l.err(l.ts[i].start, "empty builtin type argument")
			}
			args = append(args, string(l.src[l.ts[start].start:l.ts[j-1].end]))
			start = j + 1
			continue
		}
		if l.ts[j].text == "[" || l.ts[j].text == "(" || l.ts[j].text == "{" {
			j, err = l.close(j)
			if err != nil {
				return Decl{}, 0, err
			}
		}
	}
	d, err := Builtin(l.ts[i].text, args)
	if err != nil {
		return d, end, l.err(l.ts[i].start, err.Error())
	}
	d.Position = l.position(l.ts[i].start)
	return d, end, nil
}
func (l *lowerer) collectBuiltins() error {
	for i, t := range l.ts {
		if t.text == "func" && i+2 < len(l.ts) && l.ts[i+1].kind == token.IDENT && l.ts[i+2].text == "[" {
			return l.err(t.start, "user generic functions unsupported; builtin sums require concrete type arguments")
		}
		if t.text != "Option" && t.text != "Result" {
			continue
		}
		if i > 0 && l.ts[i-1].text == "." {
			return l.err(t.start, "cross-package builtin sums unsupported")
		}
		if i+1 >= len(l.ts) || l.ts[i+1].text != "[" {
			if i+1 < len(l.ts) && l.ts[i+1].text == ":" {
				return l.err(t.start, "builtin constructor requires explicit type arguments")
			}
			continue
		}
		d, _, e := l.builtinAt(i)
		if e != nil {
			return e
		}
		if l.byName[d.Name] == nil {
			l.decls = append(l.decls, d)
			l.byName[d.Name] = &l.decls[len(l.decls)-1]
		}
	}
	return nil
}
func (l *lowerer) qualified(i int) (*Decl, int, bool, error) {
	j := i + 1
	name := l.ts[i].text
	if (name == "Option" || name == "Result") && j < len(l.ts) && l.ts[j].text == "[" {
		d, end, e := l.builtinAt(i)
		if e != nil {
			return nil, 0, false, e
		}
		name = d.Name
		j = end + 1
	}
	if j+2 < len(l.ts) && l.ts[j].text == ":" && l.ts[j+1].text == ":" {
		return l.byName[name], j, true, nil
	}
	return nil, 0, false, nil
}
func (l *lowerer) builtinDeclarations() string {
	var b strings.Builder
	seen := map[string]bool{}
	for _, d := range l.decls {
		if d.Family == "" || seen[d.Family] {
			continue
		}
		seen[d.Family] = true
		params, args := "[T any]", "[T]"
		mark := "(T)"
		if d.Family == "Result" {
			params, args, mark = "[T any,E any]", "[T,E]", "(T,E)"
		}
		fmt.Fprintf(&b, "\ntype %s%s interface{%s%s};\n", d.Family, params, d.Marker, mark)
		genericArgs := []string{"T"}
		if d.Family == "Result" {
			genericArgs = append(genericArgs, "E")
		}
		g, _ := Builtin(d.Family, genericArgs)
		for _, v := range g.Variants {
			baseCase := strings.Split(v.CaseName, "[")[0]
			ctor := strings.Split(v.Constructor, "[")[0]
			if v.Payload == "" {
				fmt.Fprintf(&b, "func %s%s() %s%s{return nil};\n", ctor, params, d.Family, args)
				continue
			}
			fmt.Fprintf(&b, "type %s%s struct{Value %s};func(%s) %s%s{};func %s%s(v %s)%s%s{return %s{Value:v}};\n", baseCase, params, v.Payload, v.CaseName, d.Marker, mark, ctor, params, v.Payload, d.Family, args, v.CaseName)
		}
		fmt.Fprintf(&b, "func __hgo_enum_validate_%s%s(v %s%s){switch v.(type){", d.Family, params, d.Family, args)
		if d.Family == "Option" {
			b.WriteString("case nil:return;")
		}
		for _, v := range g.Variants {
			if v.Payload != "" {
				fmt.Fprintf(&b, "case %s:return;", v.CaseName)
			}
		}
		fmt.Fprintf(&b, "default:panic(%q)}};\n", "hgo: invalid enum "+d.Family)
	}
	for _, d := range l.decls {
		if d.Family == "" {
			continue
		}
		fmt.Fprintf(&b, "func %s(v %s){ __hgo_enum_validate_%s[%s](v); switch x:=v.(type){", d.Validator, d.Name, d.Family, strings.Join(d.TypeArgs, ","))
		for _, v := range d.Variants {
			if v.Payload == "" {
				continue
			}
			fmt.Fprintf(&b, "case %s: _=x;", v.CaseName)
			if nested := l.byName[v.Payload]; nested != nil {
				fmt.Fprintf(&b, "%s(x.Value);", nested.Validator)
			} else if e, err := parser.ParseExpr(v.Payload); err == nil {
				var f string
				var args []string
				switch n := e.(type) {
				case *ast.IndexExpr:
					if id, ok := n.X.(*ast.Ident); ok {
						f = id.Name
					}
					var z bytes.Buffer
					format.Node(&z, token.NewFileSet(), n.Index)
					args = []string{z.String()}
				case *ast.IndexListExpr:
					if id, ok := n.X.(*ast.Ident); ok {
						f = id.Name
					}
					for _, a := range n.Indices {
						var z bytes.Buffer
						format.Node(&z, token.NewFileSet(), a)
						args = append(args, z.String())
					}
				}
				if nested, err := Builtin(f, args); err == nil {
					fmt.Fprintf(&b, "%s(x.Value);", nested.Validator)
				}
			}
		}
		b.WriteString("}};\n")
	}
	return b.String()
}

// Composite payloads that conceal a statically known enum need projection
// validation before exposing their payload. Until that lowering exists, reject
// those instances explicitly rather than silently checking only the outer case.
func (l *lowerer) checkCompositePayloads() error {
	named := map[string]ast.Expr{}
	for i, t := range l.ts {
		if t.text == "struct" && i+2 < len(l.ts) && l.ts[i+1].kind == token.IDENT && l.ts[i+2].text == "{" {
			end, err := l.close(i + 2)
			if err != nil {
				return err
			}
			if e, err := parser.ParseExpr("struct " + string(l.src[l.ts[i+2].start:l.ts[end].end])); err == nil {
				named[l.ts[i+1].text] = e
			}
		}
		if t.kind != token.TYPE || i+2 >= len(l.ts) || l.ts[i+1].kind != token.IDENT {
			continue
		}
		start := i + 2
		if l.ts[start].text == "[" {
			return l.err(t.start, "user generic type declarations unsupported")
		}
		if l.ts[start].text == "=" {
			start++
		}
		end := start
		for end < len(l.ts) && l.ts[end].kind != token.SEMICOLON {
			if l.ts[end].text == "{" || l.ts[end].text == "[" || l.ts[end].text == "(" {
				k, e := l.close(end)
				if e != nil {
					return e
				}
				end = k
			}
			end++
		}
		if end > start {
			if e, err := parser.ParseExpr(string(l.src[l.ts[start].start:l.ts[end-1].end])); err == nil {
				named[l.ts[i+1].text] = e
			}
		}
	}
	var hasEnum func(ast.Expr, map[string]bool) bool
	hasEnum = func(e ast.Expr, seen map[string]bool) bool {
		switch n := e.(type) {
		case *ast.Ident:
			if d := l.byName[n.Name]; d != nil {
				return true
			}
			if seen[n.Name] {
				return false
			}
			seen[n.Name] = true
			return named[n.Name] != nil && hasEnum(named[n.Name], seen)
		case *ast.IndexExpr:
			if id, ok := n.X.(*ast.Ident); ok && (id.Name == "Option" || id.Name == "Result") {
				return true
			}
			return hasEnum(n.X, seen) || hasEnum(n.Index, seen)
		case *ast.IndexListExpr:
			if id, ok := n.X.(*ast.Ident); ok && (id.Name == "Option" || id.Name == "Result") {
				return true
			}
			for _, a := range n.Indices {
				if hasEnum(a, seen) {
					return true
				}
			}
		case *ast.ArrayType:
			return hasEnum(n.Elt, seen)
		case *ast.MapType:
			return hasEnum(n.Key, seen) || hasEnum(n.Value, seen)
		case *ast.StarExpr:
			return hasEnum(n.X, seen)
		case *ast.ChanType:
			return hasEnum(n.Value, seen)
		case *ast.ParenExpr:
			return hasEnum(n.X, seen)
		case *ast.StructType:
			for _, f := range n.Fields.List {
				if hasEnum(f.Type, seen) {
					return true
				}
			}
		}
		return false
	}
	for _, d := range l.decls {
		if d.Family == "" {
			continue
		}
		for _, v := range d.Variants {
			if v.Type == nil {
				continue
			}
			direct := false
			switch n := v.Type.(type) {
			case *ast.Ident:
				direct = l.byName[n.Name] != nil
			case *ast.IndexExpr:
				if id, ok := n.X.(*ast.Ident); ok {
					direct = id.Name == "Option" || id.Name == "Result"
				}
			case *ast.IndexListExpr:
				if id, ok := n.X.(*ast.Ident); ok {
					direct = id.Name == "Option" || id.Name == "Result"
				}
			}
			if !direct && hasEnum(v.Type, map[string]bool{}) {
				return l.err(d.Position.Offset, "builtin composite payload containing enum requires projection validation; unsupported: "+v.Payload)
			}
		}
	}
	return nil
}
