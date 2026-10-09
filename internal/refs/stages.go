package refs

import (
	"bytes"
	"fmt"
	"github.com/hxsf/goxide/internal/sourcemap"
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

// PrepareMapped carries only bytes actually copied by the reference lowerer.
// Replacement IR receives no fabricated token origin. The coarse diagnostic
// origin index used by the permission checker is intentionally not sufficient.
func PrepareMapped(filename string, source []byte, provenance *sourcemap.Map) (Result, error) {
	result, err := Prepare(filename, source)
	if err != nil || provenance == nil {
		return result, err
	}
	if bytes.Equal(source, result.Source) {
		provenance.Source = result.Source
		provenance.Stages = append(provenance.Stages, "refs.Prepare")
		return result, nil
	}
	next := &sourcemap.Map{Source: result.Source, Stages: append(append([]string{}, provenance.Stages...), "refs.Prepare")}
	for start := 0; start < len(result.copied); {
		input := result.copied[start]
		if input < 0 || input >= len(source) || start >= len(result.Source) || result.Source[start] != source[input] {
			start++
			continue
		}
		end := start + 1
		for end < len(result.copied) && end < len(result.Source) && result.copied[end] == input+end-start && result.copied[end] < len(source) && result.Source[end] == source[result.copied[end]] {
			end++
		}
		next.Segments = append(next.Segments, provenance.Copy(input, input+end-start, start)...)
		start = end
	}
	*provenance = *next
	return result, nil
}

// Finish checks the complete, generated program before erasing permissions.
// The intermediate generic write constructor is resolved back to concrete reference syntax;
// Lower then performs complete type checking and validates all consuming sites.
// Provisional typing here is used only to resolve constructor element types.
func Finish(filename string, source []byte, imp types.Importer, trusted ...map[string]bool) (Result, error) {
	return FinishWithPackagePath(defaultPackagePath, filename, source, imp, trusted...)
}

// FinishWithPackagePath checks and erases references under the consumer's actual
// package path. An empty path retains Finish's legacy synthetic package path.
func FinishWithPackagePath(path, filename string, source []byte, imp types.Importer, trusted ...map[string]bool) (Result, error) {
	restored, err := restore(path, filename, source, imp, nil)
	if err != nil {
		return Result{}, err
	}
	if !strings.Contains(string(source), "type __goxide_ref_write[") {
		return Result{Source: source}, nil
	}
	return lowerWithFiles(path, filename, restored, imp, false, nil, trusted...)
}
func restore(path, filename string, source []byte, imp types.Importer, files map[string][]byte) ([]byte, error) {
	if len(files) == 0 && !strings.Contains(string(source), "type __goxide_ref_write[") {
		return source, nil
	}
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, filename, source, parser.AllErrors)
	if err != nil {
		return nil, err
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}}
	if imp == nil {
		imp = importer.Default()
	}
	conf := types.Config{Importer: imp, Error: func(error) {}}
	all := []*ast.File{f}
	for _, name := range sourceNames(files) {
		if name == filename {
			continue
		}
		peer, err := parser.ParseFile(fs, name, files[name], parser.AllErrors)
		if err != nil {
			return nil, err
		}
		all = append(all, peer)
	}
	deduplicateIR(all)
	pkg, _ := conf.Check(packagePath(path), fs, all, info)
	offset := func(p token.Pos) int { return fs.Position(p).Offset }
	var es []edit
	var failure error
	for _, d := range f.Decls {
		generated := false
		switch d := d.(type) {
		case *ast.FuncDecl:
			generated = d.Name.Name == "__goxide_ref_make_write"
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
				if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "__goxide_ref_make_write" {
					if len(n.Args) != 1 {
						failure = fmt.Errorf("%s: refs: invalid internal write constructor", fs.Position(n.Pos()))
						return false
					}
					p, ok := info.TypeOf(n.Args[0]).(*types.Pointer)
					if !ok {
						failure = fmt.Errorf("%s: refs: unresolved write reference element", fs.Position(n.Pos()))
						return false
					}
					if element(p.Elem(), pkg) == "" {
						failure = fmt.Errorf("%s: refs: unsupported reference element", fs.Position(n.Pos()))
						return false
					}
					es = append(es, edit{offset(n.Pos()), offset(n.Args[0].Pos()), "(mut"}, edit{offset(n.Args[0].End()), offset(n.End()), ")"})
					return true
				}
			case *ast.IndexExpr:
				if id, ok := n.X.(*ast.Ident); ok && id.Name == "__goxide_ref_write" {
					if info.TypeOf(n.Index) == nil {
						failure = fmt.Errorf("%s: refs: unsupported reference element", fs.Position(n.Pos()))
						return false
					}
					es = append(es, edit{offset(n.Pos()), offset(n.End()), "mut& " + elementInFile(info.TypeOf(n.Index), f, info, pkg)})
					return false
				}
			case *ast.Ident:
				if r, ok := refType(info.TypeOf(n), pkg); ok {
					if _, isType := info.Uses[n].(*types.TypeName); isType {
						spelling := "& " + elementInFile(referenceTarget(info.TypeOf(n)), f, info, pkg)
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
		return nil, failure
	}
	restored := original(source).apply(es).text
	return restored, nil
}
