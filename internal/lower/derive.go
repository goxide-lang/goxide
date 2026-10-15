package lower

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/check"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

func deriveExprText(x goast.Expr) string {
	var b bytes.Buffer
	_ = format.Node(&b, gotoken.NewFileSet(), x)
	return b.String()
}

func (e *emitter) deriveFor(t types.Type) *check.Derive {
	if t == nil {
		return nil
	}
	n, _ := types.Unalias(t).(*types.Named)
	return e.info.Derives[n]
}

func (e *emitter) deriveHelper(d *check.Derive, kind string) goast.Expr {
	helper := d.Helpers[kind]
	if helper == nil {
		e.fail(d.Node, "缺少已登记的 "+kind+" helper")
		return goast.NewIdent("missingDerivedHelper")
	}
	name := helper.GoName
	if mapped := e.symbolNames[helper]; mapped != "" {
		name = mapped
	}
	if e.samePackage(d.File) {
		return goast.NewIdent(name)
	}
	path := filepath.ToSlash(filepath.Dir(d.File))
	if mapped := e.info.PackagePaths[path]; mapped != "" {
		path = mapped
	}
	pkg := e.importName(strings.Split(path, "/"), d.Type.Obj().Pkg().Name(), 0)
	return &goast.SelectorExpr{X: pkg, Sel: goast.NewIdent(name)}
}

func (e *emitter) deriveImpl(n *syntax.ImplDecl) bool {
	for _, request := range e.result.Derives {
		if request.Impl == n {
			return true
		}
	}
	return false
}

func (e *emitter) deriveDeclarations() []goast.Decl {
	var plans []*check.Derive
	for _, d := range e.info.Derives {
		plans = append(plans, d)
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].File != plans[j].File {
			return plans[i].File < plans[j].File
		}
		if plans[i].Node.Span().Start != plans[j].Node.Span().Start {
			return plans[i].Node.Span().Start < plans[j].Node.Span().Start
		}
		return plans[i].Type.Obj().Name() < plans[j].Type.Obj().Name()
	})
	var out []goast.Decl
	for _, d := range plans {
		for _, kind := range []string{"Default", "Clone", "Debug", "PartialEq", "Eq"} {
			if !deriveHas(d, kind) {
				continue
			}
			file := d.File
			if manualFile := d.ManualFiles[kind]; manualFile != "" {
				file = manualFile
			}
			if file != e.name {
				continue
			}
			if err := e.deriveEligible(kind, d.Type, map[types.Type]bool{}, true); err != nil {
				e.fail(d.Node, err.Error())
				return nil
			}
			if kind == "Eq" {
				continue
			}
			if fn := d.Manual[kind]; fn != nil {
				var validation []goast.Stmt
				if kind == "PartialEq" && e.info.EnumOf(d.Type) != nil {
					validation = e.manualEnumValidation(d, fn)
				}
				decl := &goast.FuncDecl{Name: goast.NewIdent(deriveExprText(e.deriveHelper(d, kind))), Type: &goast.FuncType{Params: e.params(fn.Params), Results: e.params(fn.Results)}, Body: e.block(fn.Body)}
				decl.Body.List = append(validation, decl.Body.List...)
				out = append(out, decl)
				continue
			}
			body, err := e.deriveBody(d, kind)
			if err != nil {
				e.fail(d.Node, err.Error())
				return nil
			}
			out = append(out, e.deriveParse(d.Node, body)...)
		}
	}
	return out
}

func (e *emitter) deriveParse(node syntax.Node, text string) []goast.Decl {
	f, err := goparser.ParseFile(gotoken.NewFileSet(), "derived.go", "package generated\n"+text, goparser.SkipObjectResolution)
	if err != nil {
		e.fail(node, "派生 Go 代码无效: "+err.Error())
		return nil
	}
	clearGoPositions(reflect.ValueOf(f))
	return f.Decls
}

func deriveHas(d *check.Derive, kind string) bool {
	if d == nil {
		return false
	}
	_, manual := d.Manual[kind]
	return d.Requests[kind] || manual
}

func (e *emitter) deriveEligible(kind string, t types.Type, active map[types.Type]bool, root bool) error {
	t = types.Unalias(t)
	if active[t] {
		return fmt.Errorf("%s does not support recursive protocol dependencies for %s", kind, t)
	}
	active[t] = true
	defer delete(active, t)
	if instance := e.info.EnumOf(t); instance != nil && instance.Enum.Builtin {
		if kind == "Default" {
			if instance.Enum.Name == "Option" {
				return nil
			}
			return fmt.Errorf("Result has no Default implementation")
		}
		for _, v := range instance.Enum.Variants {
			for _, field := range instance.Fields(v) {
				if err := e.deriveEligible(kind, field.Type, active, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if n, ok := t.(*types.Named); ok {
		d := e.deriveFor(n)
		if !deriveHas(d, kind) {
			return fmt.Errorf("%s has no explicit %s implementation", t, kind)
		}
		if _, manual := d.Manual[kind]; manual {
			return nil
		}
		if kind == "Eq" {
			if !deriveHas(d, "PartialEq") {
				return fmt.Errorf("Eq requires PartialEq")
			}
			if _, manual := d.Manual["PartialEq"]; manual {
				return fmt.Errorf("derive Eq cannot prove a manual PartialEq; use an explicit Eq impl")
			}
		}
		if instance := e.info.EnumOf(t); instance != nil {
			variants := instance.Enum.Variants
			if kind == "Default" {
				selected, err := deriveDefaultVariant(instance)
				if err != nil {
					return err
				}
				variants = []*check.Variant{selected}
			}
			for _, v := range variants {
				for _, f := range instance.Fields(v) {
					if err := e.deriveEligible(kind, f.Type, active, false); err != nil {
						return fmt.Errorf("%s::%s field %s: %w", instance.Enum.Name, v.Name, f.Name, err)
					}
				}
			}
			return nil
		}
		if kind == "Default" || kind == "Clone" || kind == "Debug" {
			if _, ok := n.Underlying().(*types.Struct); !ok {
				return fmt.Errorf("%s derive requires a struct or enum", kind)
			}
		}
		return e.deriveEligible(kind, n.Underlying(), active, true)
	}
	switch t := t.(type) {
	case *types.Basic:
		if t.Info()&(types.IsBoolean|types.IsInteger|types.IsFloat|types.IsComplex|types.IsString) == 0 {
			break
		}
		if kind == "Eq" && t.Info()&(types.IsFloat|types.IsComplex) != 0 {
			break
		}
		if (kind == "Default" || kind == "Clone" || kind == "Debug") && t.Info()&types.IsComplex != 0 {
			break
		}
		return nil
	case *types.Struct:
		if !root && kind != "PartialEq" && kind != "Eq" {
			return fmt.Errorf("%s has no explicit %s implementation", t, kind)
		}
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			if f.Name() == "_" || f.Embedded() {
				return fmt.Errorf("%s has no protocol for blank or embedded fields", kind)
			}
			if err := e.deriveEligible(kind, f.Type(), active, false); err != nil {
				return fmt.Errorf("field %s: %w", f.Name(), err)
			}
		}
		return nil
	case *types.Array:
		if kind != "Default" {
			return e.deriveEligible(kind, t.Elem(), active, false)
		}
	case *types.Slice:
		if kind != "Default" {
			return e.deriveEligible(kind, t.Elem(), active, false)
		}
	case *types.Map:
		if kind == "Default" {
			break
		}
		if kind == "Debug" {
			b, ok := types.Unalias(t.Key()).(*types.Basic)
			if !ok || b.Kind() != types.String {
				return fmt.Errorf("Debug maps require string keys")
			}
		}
		if kind == "Clone" {
			b, ok := types.Unalias(t.Key()).(*types.Basic)
			if !ok || b.Info()&(types.IsBoolean|types.IsInteger|types.IsFloat|types.IsString) == 0 {
				return fmt.Errorf("Clone map keys require built-in scalars")
			}
		}
		if kind == "PartialEq" || kind == "Eq" {
			if !e.deriveStableKey(t.Key(), map[types.Type]bool{}) {
				return fmt.Errorf("map key %s has no stable native equality protocol", t.Key())
			}
		}
		return e.deriveEligible(kind, t.Elem(), active, false)
	}
	return fmt.Errorf("%s has no explicit %s implementation", t, kind)
}

func (e *emitter) deriveStableKey(t types.Type, seen map[types.Type]bool) bool {
	t = types.Unalias(t)
	if seen[t] {
		return false
	}
	seen[t] = true
	defer delete(seen, t)
	if n, ok := t.(*types.Named); ok {
		d := e.deriveFor(n)
		if !deriveHas(d, "PartialEq") || d.Manual["PartialEq"] != nil || e.info.EnumOf(n) != nil {
			return false
		}
		return e.deriveStableKey(n.Underlying(), seen)
	}
	switch t := t.(type) {
	case *types.Basic:
		return t.Info()&(types.IsBoolean|types.IsInteger|types.IsString) != 0
	case *types.Array:
		return e.deriveStableKey(t.Elem(), seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if !e.deriveStableKey(t.Field(i).Type(), seen) {
				return false
			}
		}
		return true
	}
	return false
}

func deriveDefaultVariant(instance *check.Instance) (*check.Variant, error) {
	var selected *check.Variant
	for _, v := range instance.Enum.Variants {
		for _, a := range v.Decl.Attrs {
			if len(a.Path.Segments) == 1 && a.Path.Segments[0].Name.Name == "default" {
				if selected != nil {
					return nil, fmt.Errorf("Default requires exactly one default variant")
				}
				selected = v
			}
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("Default requires exactly one default variant")
	}
	return selected, nil
}

func (e *emitter) deriveBody(d *check.Derive, kind string) (string, error) {
	typ := deriveExprText(e.goType(d.Type))
	helper := deriveExprText(e.deriveHelper(d, kind))
	value := e.fresh("Value").Name
	if kind == "PartialEq" {
		other := e.fresh("Other").Name
		expr, err := e.deriveEqual(d.Type, value, other, true)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("func %s(%s,%s %s) bool { return %s }", helper, value, other, typ, expr), nil
	}
	expr, err := e.deriveRootValue(kind, d.Type, "(*"+value+")")
	if err != nil {
		return "", err
	}
	if kind == "Default" {
		return fmt.Sprintf("func %s() %s { return %s }", helper, typ, expr), nil
	}
	result := typ
	if kind == "Debug" {
		result = "string"
	}
	return fmt.Sprintf("func %s(%s *%s) %s { return %s }", helper, value, typ, result, expr), nil
}

func (e *emitter) deriveValue(kind string, t types.Type, x string) (string, error) {
	if kind == "Default" {
		if _, alias := t.(*types.Alias); alias {
			if instance := e.info.EnumOf(t); instance != nil && instance.Enum.Builtin && instance.Enum.Name == "Option" {
				// None is Option's zero value; keep an exported alias so its
				// private payload type does not leak into the consumer's Go.
				return deriveExprText(e.goType(t)) + "{}", nil
			}
		}
	}
	t = types.Unalias(t)
	if d := e.deriveFor(t); deriveHas(d, kind) {
		helper := deriveExprText(e.deriveHelper(d, kind))
		if kind == "Default" {
			return helper + "()", nil
		}
		return helper + "(&(" + x + "))", nil
	}
	if n, ok := t.(*types.Named); ok && e.info.EnumOf(n) == nil {
		return "", fmt.Errorf("%s has no explicit %s implementation", t, kind)
	}
	return e.deriveRootValue(kind, t, x)
}

func (e *emitter) deriveRootValue(kind string, t types.Type, x string) (string, error) {
	if instance := e.info.EnumOf(t); instance != nil {
		return e.deriveEnumValue(kind, instance, x)
	}
	typ := deriveExprText(e.goType(t))
	if n, ok := types.Unalias(t).(*types.Named); ok {
		t = n.Underlying()
	}
	switch t := t.(type) {
	case *types.Basic:
		if kind == "Default" {
			if t.Info()&types.IsString != 0 {
				return `""`, nil
			}
			if t.Info()&types.IsBoolean != 0 {
				return "(0 != 0)", nil
			}
			return typ + "(0)", nil
		}
		if kind == "Clone" {
			return x, nil
		}
		pkg := e.importName([]string{"strconv"}, "strconv", 0).Name
		switch {
		case t.Info()&types.IsString != 0:
			return pkg + ".Quote(" + x + ")", nil
		case t.Info()&types.IsBoolean != 0:
			return pkg + ".FormatBool(" + x + ")", nil
		case t.Info()&types.IsInteger != 0 && t.Info()&types.IsUnsigned == 0:
			return pkg + ".FormatInt(int64(" + x + "), 10)", nil
		case t.Info()&types.IsUnsigned != 0:
			return pkg + ".FormatUint(uint64(" + x + "), 10)", nil
		case t.Info()&types.IsFloat != 0:
			bits := 64
			if t.Kind() == types.Float32 {
				bits = 32
			}
			return fmt.Sprintf("%s.FormatFloat(float64(%s), 'g', -1, %d)", pkg, x, bits), nil
		}
	case *types.Struct:
		fields := []string{}
		parts := []string{strconv.Quote(e.deriveDisplayName(typ) + " {")}
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			v, err := e.deriveValue(kind, f.Type(), x+"."+f.Name())
			if err != nil {
				return "", err
			}
			fields = append(fields, f.Name()+": "+v)
			prefix := " "
			if i > 0 {
				prefix = ", "
			}
			parts = append(parts, strconv.Quote(prefix+f.Name()+": "), v)
		}
		if kind == "Debug" {
			parts = append(parts, `" }"`)
			return strings.Join(parts, " + "), nil
		}
		return typ + "{" + strings.Join(fields, ", ") + "}", nil
	case *types.Array:
		return e.deriveSequence(kind, typ, t.Elem(), x, false)
	case *types.Slice:
		return e.deriveSequence(kind, typ, t.Elem(), x, true)
	case *types.Map:
		return e.deriveMap(kind, typ, t.Elem(), x)
	}
	return "", fmt.Errorf("%s has no explicit %s implementation", t, kind)
}

func (e *emitter) deriveDisplayName(goName string) string {
	for _, d := range e.info.Derives {
		if d.Type.Obj().Name() == goName {
			switch n := d.Node.(type) {
			case *syntax.TypeDecl:
				return n.Name.Name
			case *syntax.TypeSpec:
				return n.Name.Name
			case *syntax.EnumDecl:
				return n.Name.Name
			}
		}
	}
	return goName
}

func (e *emitter) deriveSequence(kind, typ string, elem types.Type, x string, slice bool) (string, error) {
	src, index, out := e.fresh("Src").Name, e.fresh("Index").Name, e.fresh("Out").Name
	child, err := e.deriveValue(kind, elem, src+"["+index+"]")
	if err != nil {
		return "", err
	}
	if kind == "Clone" {
		init := "var " + out + " " + typ + ";"
		if slice {
			init = "if " + src + " == nil { return nil }; " + out + " := make(" + typ + ", len(" + src + "));"
		}
		return fmt.Sprintf("func(%s %s) %s { %s for %s := range %s { %s[%s] = %s };return %s }(%s)", src, typ, typ, init, index, src, out, index, child, out, x), nil
	}
	nilcheck := ""
	if slice {
		nilcheck = "if " + src + " == nil {return \"nil\"};"
	}
	join := e.importName([]string{"strings"}, "strings", 0).Name + ".Join"
	return fmt.Sprintf("func(%s %s) string {%s %s:=make([]string,len(%s));for %s:=range %s {%s[%s]=%s};return \"[\"+%s(%s,\", \")+\"]\"}(%s)", src, typ, nilcheck, out, src, index, src, out, index, child, join, out, x), nil
}

func (e *emitter) deriveMap(kind, typ string, elem types.Type, x string) (string, error) {
	src, key, item, out := e.fresh("Src").Name, e.fresh("Key").Name, e.fresh("Item").Name, e.fresh("Out").Name
	child, err := e.deriveValue(kind, elem, item)
	if err != nil {
		return "", err
	}
	if kind == "Clone" {
		return fmt.Sprintf("func(%s %s) %s {if %s==nil{return nil};%s:=make(%s,len(%s));for %s,%s:=range %s {%s[%s]=%s};return %s}(%s)", src, typ, typ, src, out, typ, src, key, item, src, out, key, child, out, x), nil
	}
	keys := e.fresh("Keys").Name
	sorts := e.importName([]string{"sort"}, "sort", 0).Name
	quotes := e.importName([]string{"strconv"}, "strconv", 0).Name
	join := e.importName([]string{"strings"}, "strings", 0).Name
	return fmt.Sprintf("func(%s %s)string{if %s==nil{return \"nil\"};%s:=make([]string,0,len(%s));for %s:=range %s{%s=append(%s,%s)};%s.Strings(%s);%s:=make([]string,0,len(%s));for _,%s:=range %s{%s:=%s[%s];%s=append(%s,%s.Quote(%s)+\": \"+%s)};return \"{\"+%s.Join(%s,\", \")+\"}\"}(%s)", src, typ, src, keys, src, key, src, keys, keys, key, sorts, keys, out, keys, key, keys, item, src, key, out, out, quotes, key, child, join, out, x), nil
}
