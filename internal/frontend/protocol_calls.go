package frontend

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"

	"github.com/hxsf/goxide/internal/protocols"
	"github.com/hxsf/goxide/internal/sourcemap"
)

// Source instantiation has the declared source arity and explicit protocol
// membership. The generated Go suffix remains a normal Go API for native
// consumers; it does not become an additional Goxide source type parameter.
func (u *usePackage) validateProtocolSourceCalls(owner string, sources map[string][]byte, maps map[string]*sourcemap.Map, local *protocols.Package, imported map[string]protocols.Metadata, imp types.Importer) error {
	if u == nil {
		return nil
	}
	metadata := map[string]protocols.Metadata{}
	for path, m := range imported {
		metadata[path] = m
	}
	if local != nil {
		metadata[owner] = local.SemanticMetadata()
	}
	load := func(path string) (protocols.Metadata, error) {
		if m, ok := metadata[path]; ok {
			return m, nil
		}
		m, err := loadProtocolArtifact(path, imp)
		if err == nil {
			metadata[path] = m
		}
		return m, err
	}
	fs := token.NewFileSet()
	var files []*ast.File
	parents := map[ast.Node]ast.Node{}
	for _, name := range sortedSources(sources) {
		f, err := parser.ParseFile(fs, name, sources[name], 0)
		if err != nil {
			return err
		}
		files = append(files, f)
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			if len(stack) > 0 {
				parents[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
	}
	info := &types.Info{Instances: map[*ast.Ident]types.Instance{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	// Other lowering stages may still contain reference IR. Unrelated checking
	// errors belong to the final checker; use only resolved instances here.
	_, _ = (&types.Config{Importer: imp, Error: func(error) {}}).Check(owner, fs, files, info)
	fail := func(node ast.Node, message string) error {
		pos := fs.Position(node.Pos())
		e := &sourcemap.Error{Message: message, Generated: pos}
		if mapping := maps[pos.Filename]; mapping != nil {
			if seg := mapping.Lookup(pos.Offset); seg != nil {
				e.Origin, e.Stack = seg.Origin, seg.Stack
			}
		}
		return e
	}
	function := func(m protocols.Metadata, name string) (protocols.GenericFunction, bool) {
		for _, f := range m.Functions {
			if f.Name == name {
				return f, true
			}
		}
		return protocols.GenericFunction{}, false
	}
	for _, f := range files {
		var problem error
		ast.Inspect(f, func(n ast.Node) bool {
			if problem != nil {
				return false
			}
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			instance, ok := info.Instances[id]
			if !ok {
				return true
			}
			pos := fs.Position(id.Pos())
			mapping := maps[pos.Filename]
			if mapping == nil {
				return true
			}
			seg := mapping.Lookup(pos.Offset)
			if seg == nil || seg.Origin == nil || (seg.Kind != "source" && seg.Kind != "macro" && seg.Kind != "use-binding") || !isGoxideInput(seg.Origin.File, u.single) {
				return true
			}
			callee, ok := info.Uses[id].(*types.Func)
			if !ok || callee.Pkg() == nil || callee.Type().(*types.Signature).Recv() != nil {
				return true
			}
			m, err := load(callee.Pkg().Path())
			if err != nil {
				problem = fail(id, err.Error())
				return false
			}
			declaration, registered := function(m, callee.Name())
			if !registered {
				return true
			}
			var expression ast.Node = id
			if selector, ok := parents[id].(*ast.SelectorExpr); ok && selector.Sel == id {
				expression = selector
			}
			for {
				paren, ok := parents[expression].(*ast.ParenExpr)
				if !ok {
					break
				}
				expression = paren
			}
			var explicit []ast.Expr
			switch index := parents[expression].(type) {
			case *ast.IndexExpr:
				if index.X == expression {
					explicit = []ast.Expr{index.Index}
				}
			case *ast.IndexListExpr:
				if index.X == expression {
					explicit = index.Indices
				}
			}
			if len(explicit) > len(declaration.SourceParams) {
				problem = fail(explicit[len(declaration.SourceParams)], fmt.Sprintf("%s accepts %d source type parameters; generated carrier parameters are not source arguments", callee.Name(), len(declaration.SourceParams)))
				return false
			}
			for _, bound := range declaration.Bounds {
				index := -1
				for i, name := range declaration.SourceParams {
					if name == bound.TypeParam {
						index = i
						break
					}
				}
				if index < 0 || index >= instance.TypeArgs.Len() {
					problem = fail(id, "invalid source protocol type parameter metadata")
					return false
				}
				arg := types.Unalias(instance.TypeArgs.At(index))
				implemented := false
				if parameter, ok := arg.(*types.TypeParam); ok {
					for scope := parents[id]; scope != nil; scope = parents[scope] {
						fn, ok := scope.(*ast.FuncDecl)
						if !ok {
							continue
						}
						caller, ok := info.Defs[fn.Name].(*types.Func)
						if !ok {
							break
						}
						actual := caller.Type().(*types.Signature).TypeParams()
						callerDecl, ok := function(metadata[owner], caller.Name())
						if ok {
							for _, candidate := range callerDecl.Bounds {
								for i := 0; i < actual.Len(); i++ {
									if actual.At(i).Obj().Name() == candidate.TypeParam && types.Identical(actual.At(i), parameter) && candidate.Protocol.ID == bound.Protocol.ID {
										implemented = true
									}
								}
							}
						}
						break
					}
				} else if named, ok := arg.(*types.Named); ok && named.Obj().Pkg() != nil {
					target := protocols.ID{Owner: named.Obj().Pkg().Path(), Name: named.Obj().Name()}
					implementations, err := load(target.Owner)
					if err != nil {
						problem = fail(id, err.Error())
						return false
					}
					for _, candidate := range implementations.Implementations {
						if candidate.Target == target && candidate.Protocol == bound.Protocol.ID {
							implemented = true
						}
					}
				}
				if !implemented {
					problem = fail(id, fmt.Sprintf("type argument %s has no explicit implementation of protocol %s", types.TypeString(arg, nil), bound.Protocol.ID.String()))
					return false
				}
			}
			return true
		})
		if problem != nil {
			return problem
		}
	}
	return nil
}
