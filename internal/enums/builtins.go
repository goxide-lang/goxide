package enums

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/hxsf/goxide/internal/paths"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

const SumPackage = "github.com/goxide-lang/std/sum"
const SumABI = 3

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
		e, err := parser.ParseExpr(string(paths.GoView([]byte(arg))))
		if err != nil {
			return Decl{}, err
		}
		containsRef := false
		ast.Inspect(e, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.UnaryExpr:
				containsRef = containsRef || x.Op == token.AND
			case *ast.BinaryExpr:
				containsRef = containsRef || x.Op == token.AND
			}
			return true
		})
		if containsRef {
			return Decl{}, fmt.Errorf("reference payload in Option/Result is unsupported; scalar references may accompany ordinary sum payloads")
		}
		var b bytes.Buffer
		_ = format.Node(&b, token.NewFileSet(), e)
		norm[i] = b.String()
	}
	suffix := "[" + strings.Join(norm, ",") + "]"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(family+suffix)))
	d := Decl{Name: family + suffix, Family: family, TypeArgs: norm, Identity: family,
		Validator: "__goxide_enum_check" + family + "_" + digest[:16], ExportValidator: "GoxideInst" + digest}
	d.Tag = "__goxide_enum_tag_" + family + suffix
	d.TagField = "tag"
	names := []string{"Some", "None"}
	payloads := []string{norm[0], ""}
	if family == "Result" {
		names = []string{"Ok", "Err"}
		payloads = []string{norm[0], norm[1]}
	}
	for i, n := range names {
		var typ ast.Expr
		if payloads[i] != "" {
			typ, _ = parser.ParseExpr(string(paths.GoView([]byte(payloads[i]))))
		}
		tag := i + 1
		if n == "None" {
			tag = 0
		}
		field := strings.ToLower(n)
		d.Variants = append(d.Variants, Variant{Tag: tag, Field: field, Value: "__goxide_enum_value_" + family + "_" + n + suffix, Project: "__goxide_enum_project_" + family + "_" + n + suffix, Name: n, Payload: payloads[i], Type: typ, Constructor: "__goxide_enum_ctor_" + family + "_" + n + suffix})
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
			continue
		}
		if i+1 >= len(l.ts) || l.ts[i+1].text != "[" {
			if i+1 < len(l.ts) && l.ts[i+1].text == ":" {
				args := []string{"struct{}"}
				if t.text == "Result" {
					args = append(args, "struct{}")
				}
				seed, _ := Builtin(t.text, args)
				if l.byName[seed.Name] == nil {
					l.decls = append(l.decls, seed)
					l.byName[seed.Name] = &l.decls[len(l.decls)-1]
				}
				continue
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
	if j+1 < len(l.ts) && l.ts[j].text == "." {
		name += "." + l.ts[j+1].text
		j += 2
	}
	if (name == "Option" || name == "Result") && j < len(l.ts) && l.ts[j].text == "[" {
		d, end, e := l.builtinAt(i)
		if e != nil {
			return nil, 0, false, e
		}
		name = d.Name
		j = end + 1
	}
	if family := l.byName[name]; family != nil && family.PackagePath != "" && family.Family != "" && family.LookupName != "" {
		args := []string{}
		if j < len(l.ts) && l.ts[j].text == "[" {
			end, err := l.close(j)
			if err != nil {
				return nil, 0, false, err
			}
			begin := j + 1
			for k := begin; k <= end; k++ {
				if k == end || l.ts[k].text == "," {
					if k == begin {
						return nil, 0, false, l.err(l.ts[j].start, "empty builtin type argument")
					}
					args = append(args, string(l.src[l.ts[begin].start:l.ts[k-1].end]))
					begin = k + 1
				} else if l.ts[k].text == "[" || l.ts[k].text == "(" || l.ts[k].text == "{" {
					k, err = l.close(k)
					if err != nil {
						return nil, 0, false, err
					}
				}
			}
			j = end + 1
		}
		if j+2 < len(l.ts) && l.ts[j].text == ":" && l.ts[j+1].text == ":" {
			if len(args) != len(family.TypeParams) {
				return nil, 0, false, l.err(l.ts[i].start, "imported sum requires its explicit alias type arguments")
			}
			replacements := map[string]string{}
			for k, p := range family.TypeParams {
				replacements[p] = args[k]
			}
			canonical := make([]string, len(family.TypeArgs))
			for k, arg := range family.TypeArgs {
				var err error
				canonical[k], err = substituteSumType(arg, replacements)
				if err != nil {
					return nil, 0, false, l.err(l.ts[i].start, err.Error())
				}
			}
			d := *family
			d.TypeArgs = canonical
			suffix := "[" + strings.Join(canonical, ",") + "]"
			d.Name = name
			if len(args) > 0 {
				d.Name += "[" + strings.Join(args, ",") + "]"
			}
			d.ConstructorType = d.Name
			d.Validator = "__goxide_enum_infer"
			d.Tag += suffix
			d.Variants = append([]Variant(nil), family.Variants...)
			for k := range d.Variants {
				d.Variants[k].Value += suffix
				d.Variants[k].Project += suffix
				d.Variants[k].Constructor += suffix
				if d.Variants[k].Payload != "" {
					index := 0
					if d.Variants[k].Name == "Err" {
						index = 1
					}
					d.Variants[k].Payload = canonical[index]
					d.Variants[k].PayloadAlias = ""
				}
			}
			return &d, j, true, nil
		}
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
		if d.Family == "Result" {
			params, args = "[T any,E any]", "[T,E]"
		}
		fmt.Fprintf(&b, "\nvar _ [%d-__goxide_enum_std.GoxideSumABI]struct{};var _ [__goxide_enum_std.GoxideSumABI-%d]struct{};\n", SumABI, SumABI)
		fmt.Fprintf(&b, "\ntype %s%s = __goxide_enum_std.%s%s;\n", d.Family, params, d.Family, args)
		genericArgs := []string{"T"}
		if d.Family == "Result" {
			genericArgs = append(genericArgs, "E")
		}
		g, _ := Builtin(d.Family, genericArgs)
		for _, v := range g.Variants {
			value := strings.Split(v.Value, "[")[0]
			project := strings.Split(v.Project, "[")[0]
			ctor := strings.Split(v.Constructor, "[")[0]
			if v.Payload == "" {
				fmt.Fprintf(&b, "func %s%s() %s%s{return __goxide_enum_std.None%s()};\n", ctor, params, d.Family, args, args)
				continue
			}
			fmt.Fprintf(&b, "func %s%s(v %s)%s%s{return __goxide_enum_std.New%s%s(v)};\n", ctor, params, v.Payload, d.Family, args, v.Name, args)
			fmt.Fprintf(&b, "func %s%s(v %s%s) %s { return __goxide_enum_std.Value%s%s(v) };\n", value, params, d.Family, args, v.Payload, v.Name, args)
			fmt.Fprintf(&b, "func %s%s(v *%s%s) *%s { return __goxide_enum_std.Project%s%s(v) };\n", project, params, d.Family, args, v.Payload, v.Name, args)
		}
		fmt.Fprintf(&b, "func __goxide_enum_tag_%s%s(v %s%s) __goxide_enum_std.Tag { return __goxide_enum_std.Tag%s%s(v) };\n", d.Family, params, d.Family, args, d.Family, args)
		fmt.Fprintf(&b, "func __goxide_enum_validate_%s%s(v %s%s){__goxide_enum_std.Validate%s%s(v)};\n", d.Family, params, d.Family, args, d.Family, args)

		for _, v := range g.Variants {
			arg, call := "", ""
			if v.Payload != "" {
				arg, call = "v "+v.Payload, "v"
			}
			fmt.Fprintf(&b, "func GoxideNew%s%s%s(%s) %s%s { return %s(%s) };\n", d.Family, v.Name, params, arg, d.Family, args, v.Constructor, call)
		}
	}
	b.WriteString(l.builtinInstances())
	return b.String()
}

func (l *lowerer) builtinInstances(shapeValidator ...string) string {
	var b strings.Builder
	for _, d := range l.decls {
		if d.Family == "" {
			continue
		}
		shape := fmt.Sprintf("__goxide_enum_validate_%s[%s]", d.Family, strings.Join(d.TypeArgs, ","))
		if len(shapeValidator) > 0 {
			shape = shapeValidator[0]
		}
		fmt.Fprintf(&b, "func %s(__goxide_enum_value %s){ %s(__goxide_enum_value);", d.Validator, d.Name, shape)
		hasChecks := false
		for _, v := range d.Variants {
			hasChecks = hasChecks || l.payloadChecks[d.Name+"/"+v.Name] != ""
		}
		if hasChecks {
			fmt.Fprintf(&b, "switch %s(__goxide_enum_value) {", d.Tag)
			for _, v := range d.Variants {
				check := l.payloadChecks[d.Name+"/"+v.Name]
				if check == "" {
					continue
				}
				fmt.Fprintf(&b, "case %d:", v.Tag)
				b.WriteString(check)
			}
			b.WriteString("}")
		}
		b.WriteString("};\n")
		fmt.Fprintf(&b, "func %s(v %s) { %s(v) };\n", d.ExportValidator, d.Name, d.Validator)
	}
	return b.String()
}

// Validate value-struct projections before exposing sum payloads. Reference
// containers and recursive object graphs remain outside this finite boundary.
func (l *lowerer) collectNamedTypes() (map[string]ast.Expr, error) {
	named := map[string]ast.Expr{}
	hasBuiltin := false
	for _, d := range l.decls {
		hasBuiltin = hasBuiltin || d.Family != ""
	}
	depth := 0
	for i, t := range l.ts {
		if t.kind == token.RBRACE {
			depth--
		}
		atPackage := depth == 0
		if t.kind == token.LBRACE {
			depth++
		}
		if !atPackage {
			continue
		}
		if (hasBuiltin || len(l.decls) > 0 || l.packageTypes != nil) && t.kind == token.TYPE && i+1 < len(l.ts) && l.ts[i+1].kind == token.LPAREN {
			end, err := l.close(i + 1)
			if err != nil {
				return nil, err
			}
			declaration := withoutPayloadAttrs(string(l.src[t.start:l.ts[end].end]))
			f, err := parser.ParseFile(token.NewFileSet(), "", "package p;"+string(paths.GoView([]byte(declaration))), 0)
			if err != nil {
				return nil, l.err(t.start, "unsupported grouped type declaration in sum payload analysis")
			}
			for _, spec := range f.Decls[0].(*ast.GenDecl).Specs {
				ts := spec.(*ast.TypeSpec)
				if ts.TypeParams != nil {
					return nil, l.err(t.start, "user generic type declarations unsupported")
				}
				named[ts.Name.Name] = ts.Type
			}
		}
		if t.text == "struct" && i+2 < len(l.ts) && l.ts[i+1].kind == token.IDENT && l.ts[i+2].text == "{" {
			end, err := l.close(i + 2)
			if err != nil {
				return nil, err
			}
			if e, err := parser.ParseExpr(string(paths.GoView([]byte(withoutPayloadAttrs("struct " + string(l.src[l.ts[i+2].start:l.ts[end].end])))))); err == nil {
				named[l.ts[i+1].text] = e
			}
		}
		if t.kind != token.TYPE || i+2 >= len(l.ts) || l.ts[i+1].kind != token.IDENT {
			continue
		}
		start := i + 2
		if l.ts[start].text == "=" {
			start++
		}
		end := start
		for end < len(l.ts) && l.ts[end].kind != token.SEMICOLON {
			if l.ts[end].text == "{" || l.ts[end].text == "[" || l.ts[end].text == "(" {
				k, e := l.close(end)
				if e != nil {
					return nil, e
				}
				end = k
			}
			end++
		}
		if end > start {
			declaration := "package p;" + string(paths.GoView(l.src[t.start:l.ts[end-1].end]))
			if f, e := parser.ParseFile(token.NewFileSet(), "", declaration, 0); e == nil {
				if spec, ok := f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec); ok && spec.TypeParams != nil {
					return nil, l.err(t.start, "user generic type declarations unsupported")
				}
			}
			if e, err := parser.ParseExpr(string(paths.GoView([]byte(withoutPayloadAttrs(string(l.src[l.ts[start].start:l.ts[end-1].end])))))); err == nil {
				named[l.ts[i+1].text] = e
			}
		}
	}
	return named, nil
}

func (l *lowerer) checkCompositePayloads() error {
	named, err := l.collectNamedTypes()
	if err != nil {
		return err
	}
	for name, typ := range l.packageTypes {
		if named[name] == nil {
			named[name] = typ
		}
	}
	selectorDecl := func(n *ast.SelectorExpr) *Decl {
		var b bytes.Buffer
		_ = format.Node(&b, token.NewFileSet(), n)
		return l.byName[b.String()]
	}
	var hasEnum func(ast.Expr, map[string]bool) bool
	hasEnum = func(e ast.Expr, seen map[string]bool) bool {
		switch n := e.(type) {
		case *ast.SelectorExpr:
			return selectorDecl(n) != nil
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
	l.payloadChecks = map[string]string{}
	dependencies := map[string][]string{}
	owner := ""
	projectionID := 0
	var project func(ast.Expr, string, map[string]bool) (string, error)
	project = func(e ast.Expr, path string, active map[string]bool) (string, error) {
		if !hasEnum(e, map[string]bool{}) {
			return "", nil
		}
		switch n := e.(type) {
		case *ast.SelectorExpr:
			if d := selectorDecl(n); d != nil {
				dependencies[owner] = append(dependencies[owner], d.Name)
				return d.Validator + "(" + path + ");", nil
			}
		case *ast.Ident:
			if d := l.byName[n.Name]; d != nil {
				dependencies[owner] = append(dependencies[owner], d.Name)
				return d.Validator + "(" + path + ");", nil
			}
			if active[n.Name] {
				return "", fmt.Errorf("recursive value projection %s", n.Name)
			}
			active[n.Name] = true
			code, err := project(named[n.Name], path, active)
			delete(active, n.Name)
			return code, err
		case *ast.ParenExpr:
			return project(n.X, path, active)
		case *ast.ArrayType:
			projectionID++
			value := fmt.Sprintf("__goxide_enum_projection_%d", projectionID)
			part, err := project(n.Elt, value, active)
			if err != nil {
				return "", err
			}
			return "for _," + value + ":=range " + path + "{" + part + "};", nil
		case *ast.MapType:
			projectionID++
			key := fmt.Sprintf("__goxide_enum_projection_key_%d", projectionID)
			value := fmt.Sprintf("__goxide_enum_projection_value_%d", projectionID)
			keyPart, err := project(n.Key, key, active)
			if err != nil {
				return "", err
			}
			valuePart, err := project(n.Value, value, active)
			if err != nil {
				return "", err
			}
			return "for " + key + "," + value + ":=range " + path + "{_=" + key + ";_=" + value + ";" + keyPart + valuePart + "};", nil
		case *ast.StructType:
			var code strings.Builder
			for _, f := range n.Fields.List {
				if !hasEnum(f.Type, map[string]bool{}) {
					continue
				}
				if len(f.Names) == 0 {
					return "", fmt.Errorf("embedded enum-bearing field")
				}
				for _, id := range f.Names {
					if id.Name == "_" {
						return "", fmt.Errorf("blank enum-bearing field")
					}
					part, err := project(f.Type, path+"."+id.Name, active)
					if err != nil {
						return "", err
					}
					code.WriteString(part)
				}
			}
			return code.String(), nil
		case *ast.IndexExpr, *ast.IndexListExpr:
			var b bytes.Buffer
			_ = format.Node(&b, token.NewFileSet(), e)
			// collectBuiltins already registered each concrete source instantiation.
			compact := strings.ReplaceAll(b.String(), " ", "")
			for _, d := range l.decls {
				if d.Family != "" && strings.ReplaceAll(d.Name, " ", "") == compact {
					dependencies[owner] = append(dependencies[owner], d.Name)
					return d.Validator + "(" + path + ");", nil
				}
			}
			if d := l.byName[compact]; d != nil && d.Family != "" && len(d.TypeArgs) > 0 {
				dependencies[owner] = append(dependencies[owner], d.Name)
				return d.Validator + "(" + path + ");", nil
			}
		}
		return "", fmt.Errorf("enum-bearing pointer/container projection")
	}
	for _, d := range l.decls {
		owner = d.Name
		for _, v := range d.Variants {
			if v.Type == nil {
				continue
			}
			if d.Family == "" {
				var allowed func(ast.Expr, map[string]bool) bool
				allowed = func(e ast.Expr, seen map[string]bool) bool {
					switch t := e.(type) {
					case *ast.Ident:
						if scalar(t.Name) || (t.Name == "error" && v.HasMessage && (v.Source || v.From)) || l.byName[t.Name] != nil {
							return true
						}
						if seen[t.Name] || named[t.Name] == nil {
							return false
						}
						seen[t.Name] = true
						ok := allowed(named[t.Name], seen)
						delete(seen, t.Name)
						return ok
					case *ast.StructType:
						for _, f := range t.Fields.List {
							if len(f.Names) == 0 || !allowed(f.Type, seen) {
								return false
							}
						}
						return true
					case *ast.ArrayType:
						return allowed(t.Elt, seen)
					case *ast.MapType:
						return allowed(t.Key, seen) && allowed(t.Value, seen)
					}
					return false
				}
				if !allowed(v.Type, map[string]bool{}) {
					return l.err(v.Position.Offset, "unsupported enum payload: use scalar or finite local value struct/container")
				}
			}
			payload := v.Value + "(v)"
			if d.Family != "" {
				payload = v.Value + "(__goxide_enum_value)"
			}
			code, err := project(v.Type, payload, map[string]bool{})
			if err != nil {
				return l.err(d.Position.Offset, "builtin payload projection unsupported: "+v.Payload+": "+err.Error())
			}
			l.payloadChecks[d.Name+"/"+v.Name] = code
		}
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("recursive sum value projection: %s", name)
		}
		if done[name] {
			return nil
		}
		visiting[name] = true
		for _, child := range dependencies[name] {
			if err := visit(child); err != nil {
				return err
			}
		}
		delete(visiting, name)
		done[name] = true
		return nil
	}
	for _, d := range l.decls {
		if err := visit(d.Name); err != nil {
			return l.err(d.Position.Offset, err.Error())
		}
	}
	return nil
}

// Type and field attributes belong to derive processing; blank only whole
// attribute lines while recovering the host type shape for projection checks.
// Preserve byte offsets in this parser-only view; the source is not rewritten.
func withoutPayloadAttrs(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#[") && strings.HasSuffix(t, "]") {
			lines[i] = strings.Repeat(" ", len(line))
		}
	}
	return strings.Join(lines, "\n")
}

// Substitute alias parameters as syntax nodes, never substring replacement.
func substituteSumType(source string, args map[string]string) (string, error) {
	fs := token.NewFileSet()
	x, err := parser.ParseExprFrom(fs, "", source, 0)
	if err != nil {
		return "", err
	}
	var changes []edit
	var walk func(ast.Node) bool
	walk = func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.SelectorExpr:
			// Neither the package qualifier nor selected member is a type parameter.
			return false
		case *ast.Field:
			// Field and parameter names are declarations, not type references.
			ast.Inspect(t.Type, walk)
			return false
		case *ast.Ident:
			if arg, ok := args[t.Name]; ok {
				changes = append(changes, edit{int(t.Pos()) - 1, int(t.End()) - 1, arg})
			}
		}
		return true
	}
	ast.Inspect(x, walk)
	for i := len(changes) - 1; i >= 0; i-- {
		e := changes[i]
		source = source[:e.start] + e.text + source[e.end:]
	}
	return source, nil
}

// InstantiateBuiltin emits only the concrete validators, using the same payload
// projection checks as source-written instances. It never reparses lowered Go
// or regenerates a nominal family. Callers must retain returned Decl metadata.
// An optional shapeValidator selects an already-authorized shared std helper;
// the complete concrete payload checks are still generated unconditionally.
func InstantiateBuiltin(d Decl, declarations []Decl, named map[string]ast.Expr, shapeValidator ...string) (string, error) {
	if err := CheckBuiltinValidatorNames(append(append([]Decl(nil), declarations...), d)); err != nil {
		return "", err
	}
	l := &lowerer{filename: "<Go result instance>", decls: []Decl{d}, byName: map[string]*Decl{}, packageTypes: named}
	for i := range declarations {
		prior := &declarations[i]
		l.byName[prior.Name] = prior
		if prior.LookupName != "" {
			l.byName[prior.LookupName] = prior
		}
		l.byName[strings.ReplaceAll(prior.Name, " ", "")] = prior
	}
	l.byName[d.Name] = &l.decls[0]
	if err := l.checkCompositePayloads(); err != nil {
		return "", err
	}
	return l.builtinInstances(shapeValidator...), nil
}

// BindPayloadType preserves the declaring file's trusted enum bindings when a
// type shape is shared with other files. Clone first: source AST is not changed.
func BindPayloadType(expr ast.Expr, declarations []Decl) ast.Expr {
	var b bytes.Buffer
	_ = format.Node(&b, token.NewFileSet(), expr)
	fs := token.NewFileSet()
	copy, err := parser.ParseExprFrom(fs, "", b.String(), 0)
	if err != nil {
		return expr
	}
	bindings := map[string]string{}
	for _, d := range declarations {
		if d.Family == "" && d.LookupName != "" {
			bindings[d.LookupName] = d.Name
		}
	}
	var edits []edit
	ast.Inspect(copy, func(n ast.Node) bool {
		if selector, ok := n.(*ast.SelectorExpr); ok {
			var text bytes.Buffer
			_ = format.Node(&text, fs, selector)
			if name := bindings[text.String()]; name != "" {
				edits = append(edits, edit{int(selector.Pos()) - 1, int(selector.End()) - 1, name})
				return false
			}
		}
		return true
	})
	source := b.String()
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		source = source[:e.start] + e.text + source[e.end:]
	}
	bound, err := parser.ParseExpr(source)
	if err != nil {
		return expr
	}
	return bound
}

// CheckBuiltinValidatorNames rejects shortened-name collisions before emitting or
// deduplicating declarations. Short digests are spellings, not nominal identities.
// Imported bindings carry their own qualified validators and are not local names.
func CheckBuiltinValidatorNames(declarations []Decl) error {
	seen := map[string]string{}
	for _, d := range declarations {
		if d.Family == "" || d.PackagePath != "" || d.ExportValidator == "" {
			continue
		}
		if prior, ok := seen[d.Validator]; ok && prior != d.ExportValidator {
			return fmt.Errorf("generated builtin validator name collision %s (%s and %s)", d.Validator, prior, d.ExportValidator)
		}
		seen[d.Validator] = d.ExportValidator
	}
	return nil
}
