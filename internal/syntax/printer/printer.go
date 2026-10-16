// Package printer renders syntax trees as Goxide source.
package printer

import (
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

// Source formats n without consulting its original source text.
func Source(n ast.Node) string { p := &printer{}; p.node(n); return p.b.String() }

// Fprint writes the formatted syntax tree to w.
func Fprint(w io.Writer, n ast.Node) error { _, err := io.WriteString(w, Source(n)); return err }

type printer struct {
	b        strings.Builder
	comments []*ast.CommentGroup
	comment  int
	docs     map[*ast.CommentGroup]bool
}

func (p *printer) s(s string) { p.b.WriteString(s) }
func (p *printer) before(n ast.Node) {
	if n == nil {
		return
	}
	leading := false
	switch n.(type) {
	case ast.Item, *ast.Field, *ast.ValueSpec, *ast.TypeSpec:
		leading = true
	}
	p.beforePosition(n.Span().Start, leading)
}
func (p *printer) beforeOffset(offset uint32) { p.beforePosition(offset, false) }
func (p *printer) beforePosition(offset uint32, leading bool) {
	for p.comment < len(p.comments) && p.comments[p.comment].Span().Start < offset {
		p.commentGroup(p.comments[p.comment], leading)
		p.comment++
	}
}
func (p *printer) commentGroup(g *ast.CommentGroup, leading bool) {
	inline := !leading && !p.docs[g]
	for _, c := range g.List {
		if !strings.HasPrefix(c.Text, "/*") {
			inline = false
		}
	}
	if inline {
		p.s(" ")
		for _, c := range g.List {
			p.s(c.Text)
			p.s(" ")
		}
		return
	}
	p.s("\n\n")
	for _, c := range g.List {
		p.s(c.Text)
		p.s("\n")
	}
	if !p.docs[g] {
		p.s("\n")
	}
}
func (p *printer) attrs(attrs []*ast.Attr) {
	for _, a := range attrs {
		p.node(a)
		p.s("\n")
	}
}
func (p *printer) vis(v ast.Visibility) {
	if v.Kind == ast.VisPub {
		p.s("pub ")
	} else if v.Kind == ast.VisScoped {
		p.s("pub(")
		p.node(v.Scope)
		p.s(") ")
	}
}
func (p *printer) itemBase(b ast.ItemBase) { p.attrs(b.Attrs); p.vis(b.Vis) }
func (p *printer) mods(m ast.FuncMods) {
	if m.Unsafe {
		p.s("unsafe ")
	}
	if m.Async {
		p.s("async ")
	}
	if m.Const {
		p.s("const ")
	}
}
func (p *printer) identList(ns []*ast.Ident) {
	for i, n := range ns {
		if i > 0 {
			p.s(", ")
		}
		p.node(n)
	}
}
func (p *printer) exprList(es []ast.Expr) {
	for i, e := range es {
		if i > 0 {
			p.s(", ")
		}
		p.node(e)
	}
}
func (p *printer) params(ps []*ast.Param) {
	for i, v := range ps {
		if i > 0 {
			p.s(", ")
		}
		p.node(v)
	}
}
func (p *printer) signature(ps, rs []*ast.Param, paren bool) {
	p.s("(")
	p.params(ps)
	p.s(")")
	p.results(rs, paren)
}
func (p *printer) results(rs []*ast.Param, paren bool) {
	if paren {
		p.s(" (")
		p.params(rs)
		p.s(")")
	} else if len(rs) > 0 {
		p.s(" ")
		p.params(rs)
	}
}
func (p *printer) valueSpecs(kw string, grouped bool, specs []*ast.ValueSpec) {
	p.s(kw)
	p.s(" ")
	if grouped {
		p.s("(\n")
	}
	for i, v := range specs {
		if i > 0 {
			p.s(";\n")
		}
		p.node(v)
	}
	if grouped {
		p.s("\n)")
	}
}
func (p *printer) stmts(ss []ast.Stmt) {
	for _, s := range ss {
		p.node(s)
		if !endsWithEmpty(s) {
			p.s(";")
		}
		p.s("\n")
	}
}
func (p *printer) items(is []ast.Item) {
	for _, i := range is {
		p.node(i)
		p.s(";\n")
	}
}
func (p *printer) node(n ast.Node) {
	if n == nil || (reflect.ValueOf(n).Kind() == reflect.Pointer && reflect.ValueOf(n).IsNil()) {
		return
	}
	p.before(n)
	switch n := n.(type) {
	case *ast.File:
		p.comments = n.Comments
		p.docs = make(map[*ast.CommentGroup]bool)
		ast.Walk(n, func(child ast.Node) bool {
			v := reflect.ValueOf(child).Elem().FieldByName("Doc")
			if v.IsValid() && !v.IsNil() {
				p.docs[v.Interface().(*ast.CommentGroup)] = true
			}
			return true
		})
		if n.Package != nil {
			p.before(n.Package)
			p.s("package ")
			p.node(n.Package)
			p.s(";\n\n")
		}
		p.attrs(n.Attrs)
		p.items(n.Items)
		for p.comment < len(p.comments) {
			p.commentGroup(p.comments[p.comment], true)
			p.comment++
		}
	case *ast.Ident:
		if n.Raw {
			p.s("r#")
		}
		p.s(n.Name)
	case *ast.Comment:
		p.s(n.Text)
	case *ast.CommentGroup:
		p.commentGroup(n, true)
	case *ast.Directive:
		p.s(n.Text)
	case *ast.Attr:
		p.s("#")
		if n.Inner {
			p.s("!")
		}
		p.s("[")
		p.node(n.Path)
		p.node(n.Args)
		p.s("]")
	case *ast.TokenTree:
		open, close := "", ""
		switch n.Delim {
		case ast.Paren:
			open, close = "(", ")"
		case ast.Bracket:
			open, close = "[", "]"
		case ast.Brace:
			open, close = "{", "}"
		}
		p.s(open)

		var previous uint32
		for i, v := range n.Tokens {
			var start, end uint32
			if v.Tree != nil {
				start, end = v.Tree.Span().Start, v.Tree.Span().End
			} else if v.Token != nil {
				start, end = v.Token.Start, v.Token.End
			}
			if i > 0 && (previous != start || (start == 0 && end == 0)) {
				p.s(" ")
			}
			previous = end
			if v.Tree != nil {
				p.node(v.Tree)
			} else if v.Token != nil {
				t := v.Token
				p.beforeOffset(t.Start)
				if t.Implicit && t.Kind == token.SEMICOLON {
					p.s("\n")
				} else {
					if t.Raw {
						p.s("r#")
					}
					p.s(t.Text)
				}
			}
		}
		p.beforeOffset(n.Span().End)
		p.s(close)
	case *ast.Path:
		if n.Root == ast.SuperRoot {
			p.s("super::")
		}
		for i, s := range n.Segments {
			if i > 0 {
				p.s("::")
			}
			p.node(s)
		}
	case *ast.PathSegment:
		p.node(n.Name)
		p.node(n.Args)
	case *ast.GenericArgs:
		p.s("[")
		for i, a := range n.Args {
			if i > 0 {
				p.s(", ")
			}
			p.node(a)
		}
		p.s("]")
	case *ast.GenericArg:
		if n.Name != nil {
			p.node(n.Name)
			p.s(" = ")
		}
		p.node(n.Type)
	case *ast.GenericParams:
		p.s("[")
		for i, a := range n.Params {
			if i > 0 {
				p.s(", ")
			}
			p.node(a)
		}
		if len(n.Params) == 1 && len(n.Params[0].Names) == 1 {
			p.s(",")
		}
		p.s("]")
	case *ast.GenericParam:
		p.identList(n.Names)
		if n.Constraint != nil {
			p.s(" ")
			p.node(n.Constraint)
		}
	case *ast.WhereClause:
		p.s(" where ")
		for i, a := range n.Predicates {
			if i > 0 {
				p.s(", ")
			}
			p.node(a)
		}
	case *ast.WherePredicate:
		p.node(n.Type)
		p.s(": ")
		for i, b := range n.Bounds {
			if i > 0 {
				p.s(" + ")
			}
			p.node(b)
		}
	case *ast.Field:
		p.attrs(n.Attrs)
		p.vis(n.Vis)
		p.identList(n.Names)
		if len(n.Names) > 0 {
			p.s(" ")
		}
		p.node(n.Type)
		if n.Tag != nil {
			p.s(" ")
			p.node(n.Tag)
		}
	case *ast.Param:
		p.attrs(n.Attrs)
		p.identList(n.Names)
		if len(n.Names) > 0 && n.Type != nil {
			p.s(" ")
		}
		if n.Variadic {
			p.s("...")
		}
		p.node(n.Type)
	case *ast.Variant:
		p.attrs(n.Attrs)
		p.node(n.Name)
		if n.Kind == ast.Tuple {
			p.s("(")
			for i, f := range n.Fields {
				if i > 0 {
					p.s(", ")
				}
				p.node(f)
			}
			p.s(")")
		} else if n.Kind == ast.Struct {
			p.s(" { ")
			for _, f := range n.Fields {
				p.node(f)
				p.s("; ")
			}
			p.s("}")
		}
		if n.Disc != nil {
			p.s(" = ")
			p.node(n.Disc)
		}
	case *ast.UseTree:
		p.node(n.Path)
		if n.Glob {
			p.s("::*")
		}
		if n.Children != nil {
			p.s("::{")
			for i, c := range n.Children {
				if i > 0 {
					p.s(", ")
				}
				p.node(c)
			}
			p.s("}")
		}
		if n.Alias != nil {
			p.s(" as ")
			p.node(n.Alias)
		}
	case *ast.UseDecl:
		p.itemBase(n.ItemBase)
		p.s("use ")
		p.node(n.Tree)
	case *ast.ModDecl:
		p.itemBase(n.ItemBase)
		p.s("mod ")
		p.node(n.Name)
		if n.Inline {
			p.s(" {\n")
			p.items(n.Items)
			p.s("}")
		}
	case *ast.FuncDecl:
		p.itemBase(n.ItemBase)
		p.mods(n.Mods)
		p.s("func ")
		if len(n.Recv) > 0 {
			p.s("(")
			p.params(n.Recv)
			p.s(") ")
		}
		if n.Path != nil {
			p.node(n.Path)
		} else {
			p.node(n.Name)
		}
		p.node(n.Generics)
		p.signature(n.Params, n.Results, n.ResultParen)
		p.node(n.Where)
		if n.Body != nil {
			p.s(" ")
			p.node(n.Body)
		}
	case *ast.TypeDecl:
		p.itemBase(n.ItemBase)
		if n.Grouped {
			p.s("type (\n")
			for _, s := range n.Specs {
				p.node(s)
				p.s(";\n")
			}
			p.s(")")
		} else {
			p.typeDecl(n.Name, n.Type, n.Alias, n.Form, n.Generics, n.Where, true)
		}
	case *ast.TypeSpec:
		p.itemBase(n.ItemBase)
		p.typeDecl(n.Name, n.Type, n.Alias, n.Form, n.Generics, n.Where, false)
	case *ast.EnumDecl:
		p.itemBase(n.ItemBase)
		p.s("enum ")
		p.node(n.Name)
		p.node(n.Generics)
		p.node(n.Where)
		p.s(" {\n")
		for _, v := range n.Variants {
			p.node(v)
			p.s(",\n")
		}
		p.s("}")
	case *ast.TraitDecl:
		p.itemBase(n.ItemBase)
		p.s("trait ")
		p.node(n.Name)
		p.node(n.Generics)
		p.node(n.Where)
		p.s(" {\n")
		for _, i := range n.Items {
			p.node(i)
			p.s(";\n")
		}
		p.s("}")
	case *ast.ImplDecl:
		p.itemBase(n.ItemBase)
		p.s("impl")
		p.node(n.Generics)
		p.s(" ")
		if n.Trait != nil {
			p.node(n.Trait)
			p.s(" for ")
		}
		p.node(n.Type)
		p.node(n.Where)
		if n.Items != nil {
			p.s(" {\n")
			p.items(n.Items)
			p.s("}")
		}
	case *ast.DeclareDecl:
		p.itemBase(n.ItemBase)
		p.s("declare ")
		p.node(n.Decl)
	case *ast.MacroRulesDecl:
		p.itemBase(n.ItemBase)
		p.s("macro_rules! ")
		p.node(n.Name)
		p.s(" {\n")
		for _, r := range n.Rules {
			p.node(r)
			p.s(";\n")
		}
		p.s("}")
	case *ast.MacroRule:
		p.node(n.Matcher)
		p.s(" => ")
		p.node(n.Template)
	case *ast.MacroItem:
		p.itemBase(n.ItemBase)
		p.node(n.Path)
		p.s("!")
		p.node(n.Args)
	case *ast.ConstDecl:
		p.itemBase(n.ItemBase)
		p.valueSpecs("const", n.Grouped, n.Specs)
	case *ast.VarDecl:
		p.itemBase(n.ItemBase)
		p.valueSpecs("var", n.Grouped, n.Specs)
	case *ast.ValueSpec:
		p.itemBase(n.ItemBase)
		p.identList(n.Names)
		if n.Type != nil {
			p.s(" ")
			p.node(n.Type)
		}
		if len(n.Values) > 0 {
			p.s(" = ")
			p.exprList(n.Values)
		}
	case *ast.BadItem:
		p.s("/* bad declaration */")
	case *ast.PathType:
		p.node(n.Path)
	case *ast.RefType:
		p.s("&")
		if n.Mut {
			p.s("mut ")
		} else if _, nested := n.Elem.(*ast.RefType); nested {
			p.s(" ")
		}
		p.node(n.Elem)
	case *ast.PointerType:
		p.s("*")
		p.node(n.Elem)
	case *ast.SliceType:
		p.s("[]")
		p.node(n.Elem)
	case *ast.ArrayType:
		p.s("[")
		if n.Ellipsis {
			p.s("...")
		} else {
			p.node(n.Len)
		}
		p.s("]")
		p.node(n.Elem)
	case *ast.MapType:
		p.s("map[")
		p.node(n.Key)
		p.s("]")
		p.node(n.Value)
	case *ast.ChanType:
		switch n.Dir {
		case ast.SendOnly:
			p.s("chan<- ")
		case ast.RecvOnly:
			p.s("<-chan ")
		default:
			p.s("chan ")
		}
		p.node(n.Elem)
	case *ast.FuncType:
		p.s("func")
		p.signature(n.Params, n.Results, n.ResultParen)
	case *ast.StructType:
		p.s("struct ")
		p.fields(n.Fields)
	case *ast.InterfaceType:
		p.s("interface {\n")
		for _, m := range n.Methods {
			p.node(m)
			p.s(";\n")
		}
		p.s("}")
	case *ast.InterfaceMethod:
		p.attrs(n.Attrs)
		if n.Name != nil {
			p.node(n.Name)
			p.signature(n.Params, n.Results, n.ResultParen)
		} else {
			p.node(n.Type)
		}
	case *ast.UnionType:
		for i, t := range n.Terms {
			if i > 0 {
				p.s(" | ")
			}
			p.node(t)
		}
	case *ast.ApproxType:
		p.s("~")
		p.node(n.Type)
	case *ast.SelfType:
		p.s("Self")
	case *ast.ProjectionType:
		p.node(n.X)
		if n.Dot {
			p.s(".")
		} else {
			p.s("::")
		}
		p.node(n.Item)
		p.node(n.Args)
	case *ast.ParenType:
		p.s("(")
		p.node(n.Type)
		p.s(")")
	case *ast.MacroType:
		p.node(n.Path)
		p.s("!")
		p.node(n.Args)
	case *ast.BadType:
		p.s("/* bad type */ _")
	case *ast.BasicLit:
		p.s(n.Value)
	case *ast.BoolLit:
		if n.Value {
			p.s("true")
		} else {
			p.s("false")
		}
	case *ast.CompositeLit:
		p.node(n.Type)
		p.s("{")
		p.exprList(n.Elts)
		if len(n.Elts) > 0 {
			p.s(",")
		}
		p.s("}")
	case *ast.KeyValue:
		p.node(n.Key)
		p.s(": ")
		p.node(n.Value)
	case *ast.ParenExpr:
		p.s("(")
		p.node(n.X)
		p.s(")")
	case *ast.SelectorExpr:
		p.expression(n.X, 7)
		p.s(".")
		p.node(n.Sel)
	case *ast.IndexExpr:
		p.expression(n.X, 7)
		p.s("[")
		p.exprList(n.Indices)
		p.s("]")
	case *ast.SliceExpr:
		p.expression(n.X, 7)
		p.s("[")
		p.node(n.Low)
		p.s(":")
		p.node(n.High)
		if n.Slice3 {
			p.s(":")
			p.node(n.Max)
		}
		p.s("]")
	case *ast.TypeAssertExpr:
		p.expression(n.X, 7)
		p.s(".(")
		if n.Type == nil {
			p.s("type")
		} else {
			p.node(n.Type)
		}
		p.s(")")
	case *ast.CallExpr:
		p.expression(n.Fun, 7)
		p.s("(")
		p.exprList(n.Args)
		if n.Ellipsis {
			p.s("...")
		}
		p.s(")")
	case *ast.StarExpr:
		p.s("*")
		p.expression(n.X, 6)
	case *ast.UnaryExpr:
		p.s(n.Op.String())
		p.s(" ")
		p.expression(n.X, 6)
	case *ast.BinaryExpr:
		p.expression(n.X, n.Op.Precedence())
		p.s(" " + n.Op.String() + " ")
		p.expression(n.Y, n.Op.Precedence()+1)
	case *ast.FuncLit:
		p.mods(n.Mods)
		if n.Form == ast.Bar {
			p.s("|")
			p.params(n.Params)
			p.s("|")
			if n.Result != nil {
				p.s(" ")
				p.node(n.Result)
			}
			p.s(" ")
		} else {
			p.s("func")
			p.signature(n.Params, n.Results, n.ResultParen)
			p.s(" ")
		}
		if n.Body != nil {
			p.node(n.Body)
		} else {
			p.node(n.Expr)
		}
	case *ast.PathExpr:
		p.node(n.Path)
	case *ast.MatchExpr:
		p.s("match ")
		p.node(n.X)
		p.s(" {\n")
		for _, a := range n.Arms {
			p.node(a)
			p.s(",\n")
		}
		p.s("}")
	case *ast.MatchArm:
		p.attrs(n.Attrs)
		p.node(n.Pat)
		if n.Guard != nil {
			p.s(" if ")
			p.node(n.Guard)
		}
		p.s(" => ")
		if n.Block != nil {
			p.node(n.Block)
		} else {
			p.node(n.Body)
		}
	case *ast.TryExpr:
		p.expression(n.X, 7)
		p.s("?")
	case *ast.RefExpr:
		p.s("&")
		if n.Mut {
			p.s("mut ")
		} else if _, nested := n.X.(*ast.RefExpr); nested {
			p.s(" ")
		}
		p.expression(n.X, 6)
	case *ast.UnsafeExpr:
		p.s("unsafe ")
		p.expression(n.X, 6)
	case *ast.MacroExpr:
		p.node(n.Path)
		p.s("!")
		p.node(n.Args)
	case *ast.QualifiedSelectorExpr:
		p.expression(n.X, 7)
		p.s(".")
		p.node(n.Sel)
		p.s("::<")
		p.node(n.Trait)
		p.s(">")
	case *ast.AwaitExpr:
		p.s("await ")
		p.node(n.X)
	case *ast.TypeExpr:
		p.node(n.Type)
	case *ast.BadExpr:
		p.s("/* bad expression */ _")
	case *ast.AssignStmt:
		p.attrs(n.Attrs)
		p.exprList(n.Lhs)
		p.s(" " + n.Tok.String() + " ")
		p.exprList(n.Rhs)
	case *ast.IncDecStmt:
		p.attrs(n.Attrs)
		p.node(n.X)
		p.s(n.Tok.String())
	case *ast.ExprStmt:
		p.attrs(n.Attrs)
		p.node(n.X)
	case *ast.SendStmt:
		p.attrs(n.Attrs)
		p.node(n.Chan)
		p.s(" <- ")
		p.node(n.Value)
	case *ast.GoStmt:
		p.attrs(n.Attrs)
		p.s("go ")
		p.node(n.Call)
	case *ast.DeferStmt:
		p.attrs(n.Attrs)
		p.s("defer ")
		p.node(n.Call)
	case *ast.ReturnStmt:
		p.attrs(n.Attrs)
		p.s("return")
		if len(n.Results) > 0 {
			p.s(" ")
			p.exprList(n.Results)
		}
	case *ast.BranchStmt:
		p.attrs(n.Attrs)
		p.s(n.Tok.String())
		if n.Label != nil {
			p.s(" ")
			p.node(n.Label)
		}
	case *ast.Block:
		p.attrs(n.Attrs)
		p.s("{\n")
		p.stmts(n.List)
		p.beforeOffset(n.Span().End)
		p.s("}")
	case *ast.IfStmt:
		p.attrs(n.Attrs)
		p.s("if ")
		if n.Init != nil {
			p.node(n.Init)
			p.s("; ")
		}
		p.node(n.Cond)
		p.s(" ")
		p.node(n.Body)
		if n.Else != nil {
			p.s(" else ")
			p.node(n.Else)
		}
	case *ast.SwitchStmt:
		p.attrs(n.Attrs)
		p.s("switch ")
		if n.Init != nil {
			p.node(n.Init)
			p.s("; ")
		}
		p.node(n.Tag)
		p.s(" {\n")
		for _, c := range n.Cases {
			p.node(c)
		}
		p.s("}")
	case *ast.TypeSwitchStmt:
		p.attrs(n.Attrs)
		p.s("switch ")
		if n.Init != nil {
			p.node(n.Init)
			p.s("; ")
		}
		p.node(n.Assign)
		p.s(" {\n")
		for _, c := range n.Cases {
			p.node(c)
		}
		p.s("}")
	case *ast.CaseClause:
		p.attrs(n.Attrs)
		if n.Default {
			p.s("default")
		} else {
			p.s("case ")
			if n.Types != nil {
				for i, t := range n.Types {
					if i > 0 {
						p.s(", ")
					}
					p.node(t)
				}
			} else {
				p.exprList(n.List)
			}
		}
		p.s(":\n")
		p.stmts(n.Body)
	case *ast.SelectStmt:
		p.attrs(n.Attrs)
		p.s("select {\n")
		for _, c := range n.Cases {
			p.node(c)
		}
		p.s("}")
	case *ast.CommClause:
		p.attrs(n.Attrs)
		if n.Default {
			p.s("default")
		} else {
			p.s("case ")
			p.node(n.Comm)
		}
		p.s(":\n")
		p.stmts(n.Body)
	case *ast.ForStmt:
		p.attrs(n.Attrs)
		p.s("for ")
		if n.Init != nil || n.Post != nil {
			p.node(n.Init)
			p.s("; ")
			p.node(n.Cond)
			p.s("; ")
			p.node(n.Post)
		} else {
			p.node(n.Cond)
		}
		p.s(" ")
		p.node(n.Body)
	case *ast.RangeStmt:
		p.attrs(n.Attrs)
		p.s("for ")
		if n.Key != nil {
			p.node(n.Key)
			if n.Value != nil {
				p.s(", ")
				p.node(n.Value)
			}
			p.s(" " + n.Tok.String() + " ")
		}
		p.s("range ")
		p.node(n.X)
		p.s(" ")
		p.node(n.Body)
	case *ast.LabeledStmt:
		p.attrs(n.Attrs)
		p.node(n.Label)
		p.s(": ")
		p.node(n.Stmt)
	case *ast.DeclStmt:
		p.attrs(n.Attrs)
		p.node(n.Decl)
	case *ast.EmptyStmt:
		p.attrs(n.Attrs)
		if !n.Implicit {
			p.s(";")
		}
	case *ast.LetStmt:
		p.attrs(n.Attrs)
		p.s("let ")
		p.node(n.Pat)
		if n.Type != nil {
			p.s(" ")
			p.node(n.Type)
		}
		if n.Value != nil {
			p.s(" = ")
			p.node(n.Value)
		}
	case *ast.LoopStmt:
		p.attrs(n.Attrs)
		p.s("loop ")
		p.node(n.Body)
	case *ast.WhileStmt:
		p.attrs(n.Attrs)
		p.s("while ")
		p.node(n.Cond)
		p.s(" ")
		p.node(n.Body)
	case *ast.BadStmt:
		p.attrs(n.Attrs)
		p.s("/* bad statement */")
	case *ast.WildPat:
		p.s("_")
	case *ast.BindPat:
		if n.Ref {
			p.s("ref ")
		}
		if n.Mut {
			p.s("mut ")
		}
		p.node(n.Name)
		if n.Sub != nil {
			p.s(" @ ")
			p.node(n.Sub)
		}
	case *ast.LitPat:
		p.node(n.Value)
	case *ast.PathPat:
		p.node(n.Path)
	case *ast.TuplePat:
		p.node(n.Path)
		p.s("(")
		for i, e := range n.Elts {
			if i > 0 {
				p.s(", ")
			}
			p.node(e)
		}
		p.s(")")
	case *ast.StructPat:
		p.node(n.Path)
		p.s(" { ")
		for _, f := range n.Fields {
			p.node(f)
			p.s(", ")
		}
		if n.Rest {
			p.s("..")
		}
		p.s(" }")
	case *ast.PatField:
		p.node(n.Name)
		if n.Pat != nil {
			p.s(": ")
			p.node(n.Pat)
		}
	case *ast.OrPat:
		for i, a := range n.Alts {
			if i > 0 {
				p.s(" | ")
			}
			p.node(a)
		}
	case *ast.RangePat:
		p.node(n.Low)
		p.s("..")
		if n.Inclusive {
			p.s("=")
		}
		p.node(n.High)
	case *ast.BadPat:
		p.s("/* bad pattern */ _")
	default:
		panic(fmt.Sprintf("printer: unhandled node %T", n))
	}
}
func (p *printer) fields(fs []*ast.Field) {
	p.s("{\n")
	for _, f := range fs {
		p.node(f)
		p.s(";\n")
	}
	p.s("}")
}
func (p *printer) typeDecl(name *ast.Ident, t ast.Type, alias bool, form ast.TypeForm, g *ast.GenericParams, w *ast.WhereClause, keyword bool) {
	if form == ast.StructSugar {
		p.s("struct ")
		p.node(name)
		p.node(g)
		p.node(w)
		p.s(" ")
		if s, ok := t.(*ast.StructType); ok {
			p.fields(s.Fields)
		} else {
			p.node(t)
		}
		return
	}
	if keyword {
		p.s("type ")
	}
	p.node(name)
	p.node(g)
	if alias {
		p.s(" =")
	}
	if t != nil {
		p.s(" ")
		p.node(t)
	}
	p.node(w)
}

func (p *printer) expression(e ast.Expr, minimum int) {
	precedence := 7
	switch n := e.(type) {
	case *ast.BinaryExpr:
		precedence = n.Op.Precedence()
	case *ast.UnaryExpr, *ast.StarExpr, *ast.RefExpr, *ast.UnsafeExpr:
		precedence = 6
	}
	if precedence < minimum {
		p.s("(")
	}
	p.node(e)
	if precedence < minimum {
		p.s(")")
	}
}

func endsWithEmpty(s ast.Stmt) bool {
	switch n := s.(type) {
	case *ast.EmptyStmt:
		return true
	case *ast.LabeledStmt:
		return endsWithEmpty(n.Stmt)
	}
	return false
}
