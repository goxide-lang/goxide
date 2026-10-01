package sourcemap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
)

// Nodes snapshots AST object identities before mutation. Generated AST nodes
// can reuse token.Pos numbers from another FileSet, so positions alone are not
// evidence that they came from the input.
func Nodes(root ast.Node) map[ast.Node]bool {
	out := map[ast.Node]bool{}
	ast.Inspect(root, func(n ast.Node) bool {
		if n != nil {
			out[n] = true
		}
		return true
	})
	return out
}
func leafText(n ast.Node) string {
	switch n := n.(type) {
	case *ast.Ident:
		return n.Name
	case *ast.BasicLit:
		return n.Value
	}
	return ""
}

// AST transfers surviving identifier/literal identities through a formatter by
// the complete ordered AST leaf stream. A structural mismatch fails closed.
func (m *Map) AST(fs *token.FileSet, root ast.Node, original map[ast.Node]bool, after []byte, stage string) *Map {
	next := &Map{Source: after, Stages: append(append([]string{}, m.Stages...), stage)}
	newFS := token.NewFileSet()
	f, err := parser.ParseFile(newFS, "", after, 0)
	if err != nil {
		return next
	}
	collect := func(n ast.Node) []ast.Node {
		var out []ast.Node
		ast.Inspect(n, func(n ast.Node) bool {
			if leafText(n) != "" {
				out = append(out, n)
			}
			return true
		})
		return out
	}
	a, b := collect(root), collect(f)
	if len(a) != len(b) {
		return next
	}
	for i := range a {
		if leafText(a[i]) != leafText(b[i]) {
			return next
		}
	}
	for i, n := range a {
		if !original[n] {
			continue
		}
		p := fs.Position(n.Pos()).Offset
		if old := m.Lookup(p); old != nil {
			next.Attach(newFS.Position(b[i].Pos()).Offset, newFS.Position(b[i].End()).Offset, old.Origin, old.Stack, old.Kind)
		}
	}
	// Also transport punctuation positions only if the complete AST structure
	// survives formatting. This covers a real type error located on a retained
	// parenthesized expression without assigning origins to generated nodes.
	all := func(root ast.Node) []ast.Node {
		var ns []ast.Node
		ast.Inspect(root, func(n ast.Node) bool {
			if n == nil {
				return false
			}
			switch n.(type) {
			case *ast.Comment, *ast.CommentGroup:
				return false
			}
			ns = append(ns, n)
			return true
		})
		return ns
	}
	aa, bb := all(root), all(f)
	exact := len(aa) == len(bb)
	if exact {
		for i := range aa {
			if reflect.TypeOf(aa[i]) != reflect.TypeOf(bb[i]) || leafText(aa[i]) != leafText(bb[i]) {
				exact = false
				break
			}
		}
	}
	if exact {
		for i, n := range aa {
			if !original[n] {
				continue
			}
			old := m.Lookup(fs.Position(n.Pos()).Offset)
			if old == nil {
				continue
			}
			p := newFS.Position(bb[i].Pos()).Offset
			if old.Start != fs.Position(n.Pos()).Offset {
				continue
			}
			length := old.End - old.Start
			if p+length <= len(after) && old.End <= len(m.Source) && string(after[p:p+length]) == string(m.Source[old.Start:old.End]) {
				next.Attach(p, p+length, old.Origin, old.Stack, old.Kind)
			}
		}
	}
	return next
}
