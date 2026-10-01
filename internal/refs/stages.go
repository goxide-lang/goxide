package refs

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// Prepare retains nominal permissions while other frontend passes emit their
// code. Its result is compiler IR, not checked Go and must pass Finish.
func Prepare(filename string, source []byte) (Result, error) {
	return lower(filename, source, nil, true)
}

// Finish checks the complete, generated program before erasing permissions.
// The intermediate generic write constructor is resolved back to scalar syntax;
// Lower then performs complete type checking and validates all consuming sites.
// Provisional typing here is used only to resolve constructor element types.
func Finish(filename string, source []byte, imp types.Importer) (Result, error) {
	if !strings.Contains(string(source), "type __hgo_ref_write[") {
		return Result{Source: source}, nil
	}
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, filename, source, parser.AllErrors)
	if err != nil {
		return Result{}, err
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}}
	if imp == nil {
		imp = importer.Default()
	}
	conf := types.Config{Importer: imp, Error: func(error) {}}
	_, _ = conf.Check("hgo.local/refs", fs, []*ast.File{f}, info)
	offset := func(p token.Pos) int { return fs.Position(p).Offset }
	var es []edit
	var failure error
	for _, d := range f.Decls {
		generated := false
		switch d := d.(type) {
		case *ast.FuncDecl:
			generated = d.Name.Name == "__hgo_ref_make_write"
		case *ast.GenDecl:
			if len(d.Specs) == 1 {
				if t, ok := d.Specs[0].(*ast.TypeSpec); ok {
					generated = strings.HasPrefix(t.Name.Name, prefix)
				}
			}
		}
		if generated {
			es = append(es, edit{offset(d.Pos()), offset(d.End()), ""})
			continue
		}
		ast.Inspect(d, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "__hgo_ref_make_write" {
					if len(n.Args) != 1 {
						failure = fmt.Errorf("%s: refs: invalid internal write constructor", fs.Position(n.Pos()))
						return false
					}
					p, ok := info.TypeOf(n.Args[0]).(*types.Pointer)
					if !ok {
						failure = fmt.Errorf("%s: refs: unresolved write reference element", fs.Position(n.Pos()))
						return false
					}
					b, ok := p.Elem().(*types.Basic)
					if !ok || scalar(b.Name()) == "" {
						failure = fmt.Errorf("%s: refs: unsupported reference element; built-in scalar required", fs.Position(n.Pos()))
						return false
					}
					es = append(es, edit{offset(n.Pos()), offset(n.Args[0].Pos()), "(mut"}, edit{offset(n.Args[0].End()), offset(n.End()), ")"})
					return true
				}
			case *ast.IndexExpr:
				if id, ok := n.X.(*ast.Ident); ok && id.Name == "__hgo_ref_write" {
					b, ok := info.TypeOf(n.Index).(*types.Basic)
					if !ok || scalar(b.Name()) == "" {
						failure = fmt.Errorf("%s: refs: unsupported reference element", fs.Position(n.Pos()))
						return false
					}
					es = append(es, edit{offset(n.Pos()), offset(n.End()), "mut& " + scalar(b.Name())})
					return false
				}
			case *ast.Ident:
				if r, ok := refType(info.TypeOf(n)); ok {
					if _, isType := info.Uses[n].(*types.TypeName); isType {
						spelling := "& " + r.Element
						if r.Permission == Write {
							spelling = "mut" + spelling
						}
						es = append(es, edit{offset(n.Pos()), offset(n.End()), spelling})
					}
				}
			}
			return true
		})
	}
	if failure != nil {
		return Result{}, failure
	}
	restored := original(source).apply(es).text
	return lower(filename, restored, imp, false)
}
