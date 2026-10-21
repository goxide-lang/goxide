package lower

import (
	"fmt"
	goast "go/ast"
	gotoken "go/token"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type importUse struct {
	path, alias string
	pos         gotoken.Pos
	source      *syntax.UseDecl
}

func (e *emitter) reserveNames() {
	for _, pkg := range e.result.Packages {
		contains := false
		for _, f := range pkg.Files {
			if f == e.file {
				contains = true
			}
		}
		if !contains {
			continue
		}
		for _, symbol := range pkg.Scope.Parent.Bindings {
			e.reserved[symbol.GoName] = true
		}
		for _, f := range pkg.Files {
			for _, symbol := range f.Definitions {
				if symbol.Kind != resolve.Import && symbol.Kind != resolve.Module {
					e.reserved[symbol.GoName] = true
					if mapped := e.symbolNames[symbol]; mapped != "" {
						e.reserved[mapped] = true
					}
				}
			}
		}
	}
}

func (e *emitter) importName(target []string, preferred string, pos gotoken.Pos) *goast.Ident {
	p := strings.Join(target, "/")
	if use := e.imports[p]; use != nil {
		return &goast.Ident{NamePos: pos, Name: use.alias}
	}
	if preferred == "" || preferred == "_" || !gotoken.IsIdentifier(preferred) {
		preferred = path.Base(p)
	}
	if !gotoken.IsIdentifier(preferred) {
		preferred = "pkg"
	}
	alias := preferred
	for i := 2; e.reserved[alias]; i++ {
		alias = fmt.Sprintf("%s%d", preferred, i)
	}
	e.reserved[alias] = true
	e.imports[p] = &importUse{path: p, alias: alias, pos: pos}
	return &goast.Ident{NamePos: pos, Name: alias}
}

func (e *emitter) external(n syntax.Node, target []string, name, preferred string) goast.Expr {
	if len(target) < 2 {
		e.fail(n, "外部成员缺少包路径")
		return nil
	}
	if name == "" {
		name = target[len(target)-1]
	}
	if strings.Join(target[:len(target)-1], "/") == filepath.ToSlash(filepath.Dir(e.name)) && !e.externalTest {
		if symbol := e.file.References[n.ID()].Symbol; symbol != nil && symbol.Kind == resolve.Function {
			return e.unshadowedFunction(name)
		}
		return &goast.Ident{NamePos: e.pos(n), Name: name}
	}
	pkg := e.importName(target[:len(target)-1], preferred, e.pos(n))
	return &goast.SelectorExpr{X: pkg, Sel: &goast.Ident{NamePos: e.end(n) - gotoken.Pos(len(name)), Name: name}}
}

func (e *emitter) reference(id *syntax.Ident) goast.Expr {
	if id == nil {
		return nil
	}
	r := e.file.References[id.ID()]
	if e.instanceBody != nil && r.Symbol != nil && r.Symbol.Kind == resolve.TypeParam {
		if typ := e.info.Symbols[r.Symbol]; typ != nil {
			return e.goType(typ)
		}
	}
	prelude := r.Symbol
	for prelude != nil && prelude.AliasOf != nil {
		prelude = prelude.AliasOf
	}
	if prelude != nil && prelude.Decl.File == "" {
		if e.symbolNames[prelude] != "" && prelude.Kind == resolve.Type {
			return e.primitiveAlias(prelude.Name.Name)
		}
		if id.Expn != 0 && (prelude.Name.Name == "true" || prelude.Name.Name == "false") && e.booleanShadowed(prelude.Name.Name) {
			op := gotoken.EQL
			if prelude.Name.Name == "false" {
				op = gotoken.NEQ
			}
			return &goast.BinaryExpr{X: &goast.BasicLit{Kind: gotoken.INT, Value: "0"}, Op: op, Y: &goast.BasicLit{Kind: gotoken.INT, Value: "0"}}
		}
	}
	if out, ok := e.macroReference(id, r.Symbol); ok {
		return out
	}
	if id.Expn != 0 && prelude != nil && prelude.Kind == resolve.Function && e.samePackage(prelude.Decl.File) {
		return e.unshadowedFunction(prelude.GoName)
	}
	if r.Symbol != nil && r.Symbol.AliasOf != nil {
		symbol := r.Symbol.AliasOf
		name := symbol.GoName
		if mapped := e.symbolNames[symbol]; mapped != "" {
			name = mapped
		}
		return &goast.Ident{NamePos: e.pos(id), Name: name}
	}
	if r.Symbol != nil && len(r.Symbol.Target) > 0 {
		name := r.Symbol.GoName
		if r.Symbol.Kind == resolve.Import {
			name = ""
		}
		return e.external(id, r.Symbol.Target, name, "")
	}
	if len(r.Path) > 0 {
		return e.external(id, r.Path, "", "")
	}
	return e.ident(id)
}

func (e *emitter) path(p *syntax.Path) goast.Expr {
	if p == nil || len(p.Segments) == 0 {
		return nil
	}
	root := p.Segments[0]
	r := e.file.References[p.ID()]
	rootRef := e.file.References[root.Name.ID()]
	if rootRef.Symbol != nil && rootRef.Symbol.AliasOf == nil && len(rootRef.Symbol.Target) > 0 || rootRef.Deferred && len(r.Path) > 0 {
		target := r.Path
		name := ""
		if r.Symbol != nil {
			if len(r.Symbol.Target) > 0 {
				target = r.Symbol.Target
			}
			if r.Symbol.Kind != resolve.Import && r.Symbol.Kind != resolve.Module {
				name = r.Symbol.GoName
			}
		}
		if len(target) >= 2 {
			return e.typeArgs(e.external(p, target, name, root.Name.Name), p.Segments[len(p.Segments)-1].Args)
		}
	}
	var out goast.Expr = e.reference(root.Name)
	out = e.typeArgs(out, root.Args)
	for _, seg := range p.Segments[1:] {
		out = e.typeArgs(&goast.SelectorExpr{X: out, Sel: e.ident(seg.Name)}, seg.Args)
	}
	return out
}

func (e *emitter) addImports(f *goast.File) {
	e.addPrimitiveAliases(f)
	e.addPackageTypeAliases(f)
	e.addFunctionAliases(f)
	// blank use 的目的就是包初始化，不能按普通未使用别名删除。
	for _, u := range e.file.Uses {
		if u.Blank {
			p := strings.Join(u.Target, "/")
			if e.imports[p] == nil {
				e.imports[p] = &importUse{path: p, alias: "_"}
			}
		}
	}
	if len(e.imports) == 0 {
		return
	}
	sources := make(map[syntax.NodeID]*syntax.UseDecl)
	for _, item := range e.tree.Items {
		if use, ok := item.(*syntax.UseDecl); ok {
			sources[use.ID()] = use
		}
	}
	for _, use := range e.file.Uses {
		source := sources[use.Decl.ID]
		if source == nil {
			continue
		}
		target := strings.Join(use.Target, "/")
		for name, imported := range e.imports {
			if target != name && !strings.HasPrefix(target, name+"/") {
				continue
			}
			if imported.source == nil || source.Span().Start < imported.source.Span().Start {
				imported.source = source
			}
		}
	}
	var imports []*importUse
	for _, imported := range e.imports {
		imported.pos = e.end(e.tree.Package) + 1
		if imported.source != nil {
			imported.pos = e.pos(imported.source)
		}
		imports = append(imports, imported)
	}
	sort.Slice(imports, func(i, j int) bool {
		if imports[i].pos != imports[j].pos {
			return imports[i].pos < imports[j].pos
		}
		return imports[i].path < imports[j].path
	})
	var declarations []goast.Decl
	for _, imported := range imports {
		namePos := imported.pos
		if imported.source != nil {
			namePos = e.pos(imported.source.Tree.Path)
		}
		spec := &goast.ImportSpec{
			Name: &goast.Ident{NamePos: namePos, Name: imported.alias},
			Path: &goast.BasicLit{ValuePos: namePos, Kind: gotoken.STRING, Value: strconv.Quote(imported.path)},
		}
		// Separate declarations retain each use's documentation at its source position.
		declarations = append(declarations, &goast.GenDecl{TokPos: imported.pos, Tok: gotoken.IMPORT, Specs: []goast.Spec{spec}})
		f.Imports = append(f.Imports, spec)
	}
	f.Decls = append(declarations, f.Decls...)
}
