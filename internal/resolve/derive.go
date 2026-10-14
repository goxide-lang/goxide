package resolve

import (
	"fmt"
	"strings"

	"github.com/hxsf/goxide/internal/derives"
	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

// Derive records explicit protocol identity before helper references are walked.
type Derive struct {
	Name, Owner, File string
	Node              ast.Node
	Attr              *ast.Attr
	Target, Helper    *Symbol
	Impl              *ast.ImplDecl
}

func AttributeName(a *ast.Attr) string {
	if a == nil || a.Path == nil || len(a.Path.Segments) != 1 {
		return ""
	}
	return a.Path.Segments[0].Name.Name
}

func (r *resolver) collectDerives() {
	for _, f := range r.pkg.Files {
		r.file = f
		for _, item := range f.AST.Items {
			switch n := item.(type) {
			case *ast.TypeDecl:
				if n.Grouped {
					for _, spec := range n.Specs {
						r.deriveAttrs(spec, spec.Name, spec.Attrs)
					}
				} else {
					r.deriveAttrs(n, n.Name, n.Attrs)
				}
			case *ast.EnumDecl:
				r.deriveAttrs(n, n.Name, n.Attrs)
			case *ast.ImplDecl:
				r.deriveManual(n)
			}
		}
	}
}

type derivePath struct {
	parts []string
	root  *token.Token
}

func derivePaths(a *ast.Attr) ([]derivePath, error) {
	if a.Args == nil || len(a.Args.Tokens) == 0 {
		return nil, fmt.Errorf("derive requires a protocol name")
	}
	var paths []derivePath
	var path derivePath
	wantName := true
	for _, value := range a.Args.Tokens {
		if value.Token == nil {
			return nil, fmt.Errorf("derive arguments must be protocol paths")
		}
		t := value.Token
		if wantName {
			if t.Kind != token.IDENT {
				return nil, fmt.Errorf("derive requires a protocol name")
			}
			if path.root == nil {
				path.root = t
			}
			path.parts = append(path.parts, t.Text)
			wantName = false
			continue
		}
		switch t.Kind {
		case token.DCOLON:
			wantName = true
		case token.COMMA:
			paths = append(paths, path)
			path = derivePath{}
			wantName = true
		default:
			return nil, fmt.Errorf("derive arguments must be comma-separated protocol paths")
		}
	}
	if len(path.parts) > 0 {
		if wantName {
			return nil, fmt.Errorf("derive path requires a member after ::")
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (r *resolver) deriveAttrs(node ast.Node, name *ast.Ident, attrs []*ast.Attr) {
	if name == nil {
		return
	}
	for _, a := range attrs {
		if AttributeName(a) != "derive" {
			continue
		}
		paths, err := derivePaths(a)
		if err != nil {
			r.diag(a, "derive", err.Error())
			continue
		}
		for _, path := range paths {
			definition, ok := r.deriveDefinition(a, path)
			if !ok {
				r.diag(a, "derive-owner", "unknown derive or non-canonical owner: "+strings.Join(path.parts, "::"))
				continue
			}
			d := Derive{Name: definition.Name, Owner: definition.Owner, File: r.file.Name, Node: node, Attr: a, Target: r.file.Definitions[name.ID()]}
			r.declareDerive(&d, name)
			r.result.Derives = append(r.result.Derives, d)
		}
	}
}

func (r *resolver) deriveDefinition(a *ast.Attr, path derivePath) (derives.Definition, bool) {
	parts := path.parts
	scope, ctx := r.tokenScope(r.file.Scope, path.root, a.Expn)
	root := r.lookup(scope, Key{Name: parts[0], Ctx: ctx}, true)
	if len(parts) == 1 {
		if def, ok := derives.Builtin(parts[0]); ok && (root.Symbol == nil || root.Symbol.Decl.File == "" && root.Symbol.Kind == Trait) {
			return def, true
		}
	}
	if root.Symbol == nil || root.Symbol.AliasOf != nil || len(root.Symbol.Target) == 0 {
		return derives.Definition{}, false
	}
	canonical := append(append([]string{}, root.Symbol.Target...), parts[1:]...)
	if imports, ok := r.importer.(interface {
		CanonicalPath([]string) ([]string, bool)
	}); ok {
		var found bool
		canonical, found = imports.CanonicalPath(canonical)
		if !found {
			return derives.Definition{}, false
		}
	}
	if len(canonical) < 2 {
		return derives.Definition{}, false
	}
	owner := strings.Join(canonical[:len(canonical)-1], "/")
	return derives.Lookup(owner, canonical[len(canonical)-1])
}

func (r *resolver) deriveManual(n *ast.ImplDecl) {
	trait, ok := n.Trait.(*ast.PathType)
	if !ok || trait.Path == nil || len(trait.Path.Segments) != 1 {
		return
	}
	name := trait.Path.Segments[0].Name
	if name.Name != "Default" && name.Name != "PartialEq" && name.Name != "Eq" {
		return
	}
	if trait.Path.Segments[0].Args != nil {
		r.diag(n, "derive-manual", "manual "+name.Name+" does not accept protocol type arguments")
		return
	}
	target, ok := n.Type.(*ast.PathType)
	if !ok || target.Path == nil || len(target.Path.Segments) != 1 || target.Path.Segments[0].Args != nil {
		r.diag(n, "derive-manual", "manual "+name.Name+" requires a local concrete type")
		return
	}
	id := target.Path.Segments[0].Name
	symbol := r.lookup(r.identScope(r.file.Scope, id), key(id), true).Symbol
	if symbol == nil || symbol.Decl.File == "" || symbol.Kind != Type && symbol.Kind != Enum {
		r.diag(n, "derive-manual", "manual "+name.Name+" requires a local concrete type")
		return
	}
	d := Derive{Name: name.Name, File: r.file.Name, Node: n, Target: symbol, Impl: n}
	r.declareDerive(&d, id)
	r.result.Derives = append(r.result.Derives, d)
}

func (r *resolver) declareDerive(d *Derive, name *ast.Ident) {
	if d.Target == nil {
		return
	}
	prefix := d.Name
	switch d.Name {
	case "PartialEq":
		prefix = "Equals"
	case "Eq":
		return
	case "Error":
		found := false
		ast.Walk(d.Node, func(n ast.Node) bool {
			if a, ok := n.(*ast.Attr); ok && AttributeName(a) == "from" {
				found = true
			}
			return true
		})
		if !found {
			return
		}
		prefix = "From"
	}
	helper := prefix + name.Name
	k := Key{Name: helper, Ctx: d.Target.Name.Ctx}
	for _, prev := range r.result.Derives {
		if prev.Target == d.Target && prev.Name == d.Name {
			d.Helper = prev.Helper
			return // check reports the duplicate protocol, not a synthetic name collision.
		}
	}
	if old := r.pkg.Scope.Bindings[k]; old != nil {
		r.diag(d.Node, "derive-helper-conflict", fmt.Sprintf("generated helper %s conflicts with a package declaration", helper))
		return
	}
	symbol := &Symbol{Name: k, Kind: Function, Decl: r.ref(d.Node), Visibility: d.Target.Visibility, GoName: helper}
	for _, old := range r.pkg.Scope.Bindings {
		if old.GoName == helper {
			r.diag(d.Node, "derive-helper-conflict", fmt.Sprintf("generated helper %s conflicts with Go name of %s", helper, old.Name.Name))
			return
		}
	}
	r.pkg.Scope.Bindings[k] = symbol
	d.Helper = symbol
}
