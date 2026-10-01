package macros

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// hygiene resolves template bindings before inserting caller fragments. It does
// not infer bindings by identifier spelling. Function/import references use a
// package-level alias to bypass caller-local shadowing. Unknown free identifiers
// are rejected rather than silently resolved at the call site.
func (e *engine) hygiene(ts []tok, ph map[string][]tok) ([]tok, error) {
	if len(ts) == 0 {
		return ts, nil
	}
	ts = append([]tok(nil), ts...)
	var text strings.Builder
	offsets := map[int]int{}
	for i, t := range ts {
		text.WriteString(t.before)
		offsets[text.Len()] = i
		text.WriteString(t.text)
	}
	fs := token.NewFileSet()
	prefix := "package p\nvar template = "
	var root ast.Node
	f, err := parser.ParseFile(fs, "template", prefix+text.String()+"\n", 0)
	if err == nil {
		root = f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Values[0]
	} else {
		fs = token.NewFileSet()
		prefix = "package p\nfunc template(){\n"
		f, x := parser.ParseFile(fs, "template", prefix+text.String()+"\n}", 0)
		if x != nil {
			return nil, at(ts[0], "unsupported template syntax: "+x.Error())
		}
		root = f.Decls[0].(*ast.FuncDecl).Body
	}
	index := func(p token.Pos) (int, bool) {
		off := fs.Position(p).Offset - len(prefix)
		i, ok := offsets[off]
		return i, ok
	}
	ignored := map[*ast.Ident]bool{}
	var problem error
	ast.Inspect(root, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			ignored[n.Sel] = true
			if x, ok := n.X.(*ast.Ident); ok && x.Obj == nil && e.imports[x.Name] {
				// Only named package function calls are supported: no imported global
				// snapshots, constants, types, generic indexing or arbitrary selector chains.
				callable := false
				ast.Inspect(root, func(k ast.Node) bool {
					if c, ok := k.(*ast.CallExpr); ok && c.Fun == n {
						callable = true
					}
					return true
				})
				if !callable {
					problem = at(ts[0], "import selectors in templates must be direct function calls")
					return false
				}
				path := e.importPaths[x.Name]
				pkg := e.importCache[path]
				if pkg == nil {
					var err error
					pkg, err = e.importer.Import(path)
					if err != nil {
						problem = at(ts[0], "cannot resolve definition-site import "+path+"; pass function as explicit capture: "+err.Error())
						return false
					}
					e.importCache[path] = pkg
				}
				fn, ok := pkg.Scope().Lookup(n.Sel.Name).(*types.Func)
				if !ok {
					problem = at(ts[0], "imported template selector must name a function, not a mutable function variable")
					return false
				}
				if sig, ok := fn.Type().(*types.Signature); !ok || sig.TypeParams().Len() != 0 {
					problem = at(ts[0], "generic imported helper requires an explicit capture")
					return false
				}
				key := x.Name + "." + n.Sel.Name
				alias := e.alias(key)
				a, ok1 := index(x.Pos())
				b, ok2 := index(n.Sel.Pos())
				if ok1 && ok2 {
					ts[a].text = alias
					for i := a + 1; i <= b; i++ {
						ts[i].text = ""
					}
				}
				ignored[x] = true
			}
		case *ast.KeyValueExpr:
			if _, ok := n.Key.(*ast.Ident); ok {
				problem = at(ts[0], "identifier keyed literals in templates are unsupported; capture the complete expression")
			}
		case *ast.StructType, *ast.InterfaceType, *ast.TypeSpec, *ast.LabeledStmt:
			problem = at(ts[0], "template local type/struct/interface/label declarations are unsupported")
		case *ast.BranchStmt:
			if n.Label != nil {
				problem = at(ts[0], "template goto and labeled control flow are unsupported")
			}
		}
		return true
	})
	if problem != nil {
		return nil, problem
	}
	bindings := map[*ast.Object]string{}
	ast.Inspect(root, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || ignored[id] || id.Name == "_" {
			return true
		}
		i, ok := index(id.Pos())
		if !ok {
			return true
		}
		if _, ok := ph[id.Name]; ok {
			return true
		}
		if strings.HasPrefix(id.Name, e.prefix) {
			return true
		}
		if id.Obj != nil {
			if bindings[id.Obj] == "" {
				bindings[id.Obj] = e.fresh()
			}
			ts[i].text = bindings[id.Obj]
			return true
		}
		if e.funcs[id.Name] {
			ts[i].text = e.alias(id.Name)
			return true
		}
		switch id.Name {
		case "bool", "string", "byte", "rune", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128", "any", "error":
			ts[i].text = e.alias("type " + id.Name)
			return true
		case "true", "false":
			ts[i].text = e.alias("const " + id.Name)
			return true
		}
		problem = at(ts[i], fmt.Sprintf("unsupported free template name %q; pass it as an explicit capture (builtins included)", id.Name))
		return true
	})
	return ts, problem
}
func (e *engine) alias(key string) string {
	if e.aliases[key] == "" {
		e.aliases[key] = e.fresh()
	}
	return e.aliases[key]
}
