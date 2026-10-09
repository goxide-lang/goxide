package propagate

import (
	"fmt"
	"github.com/hxsf/goxide/internal/refs"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/sourcemap"
)

func sumType(t types.Type) (string, []types.Type) {
	if t == nil {
		return "", nil
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != enums.SumPackage {
		return "", nil
	}
	family := n.Obj().Name()
	if family != "Option" && family != "Result" {
		return "", nil
	}
	args := []types.Type{}
	for i := 0; i < n.TypeArgs().Len(); i++ {
		args = append(args, n.TypeArgs().At(i))
	}
	return family, args
}

// Expected types come only from the direct expression context and the nearest
// function, never from unrelated locals or an enclosing function's return type.
func (l *lowering) sumExpected(call ast.Expr, parents []ast.Node) types.Type {
	child := ast.Node(call)
	for i := len(parents) - 1; i >= 0; i-- {
		switch p := parents[i].(type) {
		case *ast.ParenExpr:
			child = p
			continue
		case *ast.ValueSpec:
			if p.Type != nil {
				return l.info.TypeOf(p.Type)
			}
		case *ast.AssignStmt:
			if len(p.Lhs) == len(p.Rhs) {
				for n, r := range p.Rhs {
					if r == child {
						return l.info.TypeOf(p.Lhs[n])
					}
				}
			}
		case *ast.CallExpr:
			funType := l.info.TypeOf(p.Fun)
			if funType == nil {
				return nil
			}
			sig, ok := funType.Underlying().(*types.Signature)
			if !ok {
				return nil
			}
			for n, a := range p.Args {
				if a != child {
					continue
				}
				if sig.Variadic() && n >= sig.Params().Len()-1 {
					t := sig.Params().At(sig.Params().Len() - 1).Type()
					if !p.Ellipsis.IsValid() {
						return t.(*types.Slice).Elem()
					}
					return t
				}
				if n < sig.Params().Len() {
					return sig.Params().At(n).Type()
				}
			}
		case *ast.ReturnStmt:
			index := -1
			for j, r := range p.Results {
				if r == child {
					index = j
				}
			}
			if index < 0 {
				return nil
			}
			for j := i - 1; j >= 0; j-- {
				var ft *ast.FuncType
				switch f := parents[j].(type) {
				case *ast.FuncDecl:
					ft = f.Type
				case *ast.FuncLit:
					ft = f.Type
				}
				if ft != nil {
					var results []types.Type
					if ft.Results != nil {
						for _, field := range ft.Results.List {
							count := len(field.Names)
							if count == 0 {
								count = 1
							}
							for n := 0; n < count; n++ {
								results = append(results, l.info.TypeOf(field.Type))
							}
						}
					}
					if len(results) == len(p.Results) {
						return results[index]
					}
					return nil
				}
			}
		}
		return nil
	}
	return nil
}

func (l *lowering) registerSum(family string, args []types.Type) (enums.Decl, error) {
	spellings := []string{}
	for _, t := range args {
		if containsTypeParam(t) || l.pendingMatchType(t) {
			return enums.Decl{}, fmt.Errorf("sum type remains unresolved")
		}
		s, ok := l.goResultType(t)
		if !ok {
			return enums.Decl{}, fmt.Errorf("sum payload must be nameable at package scope")
		}
		spellings = append(spellings, s)
	}
	d, err := enums.Builtin(family, spellings)
	if err != nil {
		return d, err
	}
	for _, old := range l.cfg.Families {
		if old.Name == d.Name && old.PackagePath == "" {
			return old, nil
		}
	}
	named := map[string]ast.Expr{}
	decls := append([]enums.Decl(nil), l.cfg.Families...)
	owners := make([]string, 0, len(l.cfg.FileFamilies))
	for owner := range l.cfg.FileFamilies {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		ds := l.cfg.FileFamilies[owner]
		for _, v := range ds {
			v.LookupName = ""
			decls = append(decls, v)
		}
	}
	for _, f := range append([]*ast.File{l.file}, l.companions...) {
		for _, g := range f.Decls {
			if gen, ok := g.(*ast.GenDecl); ok && gen.Tok == token.TYPE {
				for _, sp := range gen.Specs {
					t := sp.(*ast.TypeSpec)
					if t.TypeParams == nil {
						named[t.Name.Name] = enums.BindPayloadType(t.Type, l.cfg.FileFamilies[l.fs.Position(f.Pos()).Filename])
					}
				}
			}
		}
	}
	text, err := enums.InstantiateBuiltin(d, decls, named)
	if err != nil {
		return d, err
	}
	f, err := parser.ParseFile(l.fs, "<inferred sum instance>", "package "+l.file.Name.Name+";"+text, 0)
	if err != nil {
		return d, err
	}
	if l.cfg.GoResultNodes != nil {
		count := 0
		ast.Inspect(f, func(n ast.Node) bool {
			if n != nil {
				count++
			}
			return true
		})
		*l.cfg.GoResultNodes += count
		if *l.cfg.GoResultNodes > 200000 {
			return d, fmt.Errorf("typed sum generated-node budget exceeded")
		}
	}
	l.file.Decls = append(l.file.Decls, f.Decls...)
	l.cfg.Families = append(l.cfg.Families, d)
	if l.cfg.InferredFamilies != nil {
		*l.cfg.InferredFamilies = append(*l.cfg.InferredFamilies, d)
	}
	return d, nil
}

func (l *lowering) resolveSumConstructors() (bool, error, error) {
	var changed bool
	var fatal, pending error
	var stack []ast.Node
	ast.Inspect(l.file, func(n ast.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return false
		}
		if changed || fatal != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if ok {
			if id, ok := call.Fun.(*ast.Ident); ok && strings.HasPrefix(id.Name, "__goxide_enum_pending_") {
				bits := strings.Split(strings.TrimPrefix(id.Name, "__goxide_enum_pending_"), "_")
				if len(bits) != 2 {
					fatal = l.fail(call, "invalid sum constructor marker")
					return false
				}
				family, variant := bits[0], bits[1]
				arity := 1
				if variant == "None" {
					arity = 0
				}
				if len(call.Args) != arity || call.Ellipsis.IsValid() {
					fatal = l.fail(call, "sum constructor arity mismatch")
					return false
				}
				expected := l.sumExpected(call, stack)
				if l.contextualMatchAssignment(stack) && (expected == nil || l.pendingMatchType(expected) || expected == types.Typ[types.Invalid]) {
					pending = l.fail(call, "sum constructor awaits match result context")
					stack = append(stack, n)
					return true
				}
				ef, args := sumType(expected)
				if ef != "" && ef != family {
					fatal = l.fail(call, "sum constructor family conflicts with expected type")
					return false
				}
				if ef == "" {
					if family == "Option" && variant == "Some" {
						t := l.info.TypeOf(call.Args[0])
						if t != nil {
							t = types.Default(t)
							if b, ok := t.(*types.Basic); !ok || b.Kind() != types.UntypedNil {
								args = []types.Type{t}
							}
						}
					}
					if len(args) == 0 {
						pending = l.fail(call, "cannot infer all sum type arguments; provide a typed context or explicit arguments")
						stack = append(stack, n)
						return true
					}
				}
				d, err := l.registerSum(family, args)
				if err != nil {
					fatal = l.fail(call, err.Error())
					return false
				}
				for _, v := range d.Variants {
					if v.Name == variant {
						call.Fun = parseExpr(v.Constructor)
						changed = true
						return false
					}
				}
			}
		}
		stack = append(stack, n)
		return true
	})
	return changed, fatal, pending
}

func (l *lowering) resolveSumPatterns() (bool, error, error) {
	var changed bool
	var fatal, pending error
	ast.Inspect(l.file, func(n ast.Node) bool {
		if changed || fatal != nil {
			return false
		}
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i, st := range block.List {
			es, ok := st.(*ast.ExprStmt)
			if !ok {
				continue
			}
			c, ok := es.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			id, ok := c.Fun.(*ast.Ident)
			if !ok || id.Name != "__goxide_enum_context_match" {
				continue
			}
			if len(c.Args) != 2 {
				fatal = l.fail(c, "invalid contextual match")
				return false
			}
			d, ok := l.matchedEnum(l.info.TypeOf(c.Args[0]))
			if !ok {
				pending = l.fail(c, "cannot infer match family from scrutinee")
				continue
			}
			literal, ok := c.Args[1].(*ast.BasicLit)
			if !ok {
				fatal = l.fail(c, "invalid contextual match body")
				return false
			}
			encoded, err := strconv.Unquote(literal.Value)
			if err != nil {
				fatal = err
				return false
			}
			bodySource, mapping, err := enums.DecodeContextBody(encoded)
			if err != nil {
				fatal = err
				return false
			}
			// Token-aware rewriting avoids touching comments, strings or arm expressions.
			body, err := enums.BindContextPatternsMapped(string(bodySource), d, mapping)
			if err != nil {
				fatal = l.fail(c, err.Error())
				return false
			}
			fragments := sourcemap.Fragments{Input: mapping}
			source, mapping := fragments.Finish("package "+l.file.Name.Name+";func goxideContextStatement(){match goxideContextValue "+fragments.Copy(0, len(body))+"}", "contextual match wrapper")
			lowered, err := enums.LowerPackage("<contextual match>", source, mapping, append(l.cfg.Families, d), nil)
			if err != nil {
				fatal = err
				return false
			}
			// Enum constructors have now been lowered. Remaining static type paths
			// get a same-width Go view while copied member tokens keep their source
			// operator evidence for the final typed selector check.
			lowered.Source = paths.GoView(lowered.Source)
			mapping.Source = lowered.Source
			prepared, err := refs.PrepareMapped("<contextual match>", lowered.Source, mapping)
			if err != nil {
				fatal = err
				return false
			}
			f, err := parser.ParseFile(l.fs, "<contextual match>", prepared.Source, 0)
			if err != nil {
				fatal = err
				return false
			}
			if l.evidence != nil && !l.evidence.AddInput(l.fs, f, mapping) {
				fatal = l.fail(c, "cannot register contextual match source evidence")
				return false
			}
			present := map[string]bool{}
			declName := func(n ast.Decl) string {
				switch d := n.(type) {
				case *ast.FuncDecl:
					return d.Name.Name
				case *ast.GenDecl:
					if len(d.Specs) == 1 {
						switch sp := d.Specs[0].(type) {
						case *ast.TypeSpec:
							return sp.Name.Name
						case *ast.ValueSpec:
							if len(sp.Names) == 1 {
								return sp.Names[0].Name
							}
						}
					}
				}
				return ""
			}
			for _, file := range append([]*ast.File{l.file}, l.companions...) {
				for _, d := range file.Decls {
					present[declName(d)] = true
				}
			}
			for _, decl := range f.Decls {
				key := declName(decl)
				if strings.HasPrefix(key, "__goxide_ref_") && !present[key] {
					l.file.Decls = append(l.file.Decls, decl)
					present[key] = true
				}
			}
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "goxideContextStatement" {
					// The enum lowering saves its operand exactly once as the first assignment.
					for _, s := range fn.Body.List {
						if b, ok := s.(*ast.BlockStmt); ok && len(b.List) > 0 {
							if a, ok := b.List[0].(*ast.AssignStmt); ok {
								a.Rhs[0] = c.Args[0]
							}
							if decl, ok := b.List[0].(*ast.DeclStmt); ok {
								if g, ok := decl.Decl.(*ast.GenDecl); ok {
									if v, ok := g.Specs[0].(*ast.ValueSpec); ok && len(v.Values) == 1 {
										v.Values[0] = c.Args[0]
									}
								}
							}
						}
					}
					block.List = append(append(append([]ast.Stmt{}, block.List[:i]...), fn.Body.List...), block.List[i+1:]...)
					changed = true
					return false
				}
			}
		}
		return true
	})
	return changed, fatal, pending
}

func pendingSumConstructor(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.HasPrefix(id.Name, "__goxide_enum_pending_") {
			found = true
		}
		return !found
	})
	return found
}

// A context-carrying match slot must resolve before untyped payload defaults.
// Plain inferred match slots may still infer their result from concrete arms.
func (l *lowering) contextualMatchAssignment(parents []ast.Node) bool {
	var object types.Object
	for i := len(parents) - 1; i >= 0; i-- {
		if a, ok := parents[i].(*ast.AssignStmt); ok && len(a.Lhs) == 1 {
			if id, ok := a.Lhs[0].(*ast.Ident); ok {
				object = l.info.ObjectOf(id)
			}
			break
		}
	}
	if object == nil {
		return false
	}
	found := false
	ast.Inspect(l.file, func(n ast.Node) bool {
		v, ok := n.(*ast.ValueSpec)
		if !ok || len(v.Names) != 1 || len(v.Values) != 1 || l.info.Defs[v.Names[0]] != object {
			return true
		}
		if c, ok := v.Values[0].(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok {
				found = id.Name == matchOperand || id.Name == matchContext
			}
		}
		return false
	})
	return found
}
