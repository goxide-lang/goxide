package analysis

import (
	"errors"
	"path"
	"sort"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func (r *Result) finishEditor(execution error) error {
	var diagnostics []error
	projectionSafe := execution == nil && r.Checked != nil && len(r.Diagnostics) == 0
	var names []string
	for name := range r.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		source := r.Sources[name]
		if source.GoSyntax != nil && source.err != nil {
			projectionSafe = false
		}
		for _, diagnostic := range source.Diagnostics {
			if diagnostic.Severity == parser.Error {
				diagnostics = append(diagnostics, &Diagnostic{File: name, Source: source.Syntax, Span: diagnostic.Span, Message: diagnostic.Message, Origin: resolve.Origin{File: name, Span: diagnostic.Span}})
			}
		}
		if r.state != nil {
			if partial := r.state.sources.partials[name]; partial != nil {
				r.Regions = append(r.Regions, partial.regions...)
				for _, region := range partial.regions {
					projectionSafe = projectionSafe && region.Kind == IncompleteBody
				}
			}
		}
	}
	for _, diagnostic := range r.Diagnostics {
		if diagnostic, ok := diagnostic.(check.Diagnostic); ok && diagnostic.Code == check.TraitInvalid {
			r.Regions = append(r.Regions, Region{File: diagnostic.File, Span: diagnostic.Span, Kind: UnknownProtocol, Reason: diagnostic.Message})
		}
	}
	r.Diagnostics = append(diagnostics, r.Diagnostics...)
	if len(r.Diagnostics) != 0 {
		r.Err = r.Diagnostics[0]
		if projectionSafe && len(r.Regions) != 0 {
			projection := *r
			projection.Err, projection.Projection, projection.ProjectionOnly = nil, nil, true
			r.Projection = &projection
		}
	}
	var sourceError *Diagnostic
	var expansionError *expand.Diagnostic
	var bindingError resolve.Diagnostic
	var checkingError check.Diagnostic
	if errors.As(execution, &sourceError) || errors.As(execution, &expansionError) || errors.As(execution, &bindingError) || errors.As(execution, &checkingError) {
		return nil
	}
	return execution
}

// Region identifies current source that cannot supply complete semantic facts.
// Its byte range always belongs to the original input, never an older snapshot.
type Region struct {
	File   string
	Span   syntax.Span
	Kind   string
	Reason string
}

const (
	IncompleteBody      = "incomplete-body"
	UnknownDeclaration  = "unknown-declaration"
	UnknownDeclarations = "unknown-declarations"
	UnknownProtocol     = "unknown-protocol"
)

type partialSource struct {
	file                string
	diagnostics         []parser.Diagnostic
	regions             []Region
	unknownDeclarations bool
	unknownProtocol     bool
}

func projectSource(name string, file *syntax.File, diagnostics []parser.Diagnostic) (*syntax.File, *partialSource) {
	partial := &partialSource{file: name, diagnostics: diagnostics}
	if file == nil || file.Package == nil || file.Package.Missing {
		partial.unknownDeclarations = true
		if file != nil {
			partial.region(file.Span(), UnknownDeclarations, "package declaration is incomplete")
		}
		return nil, partial
	}
	file.Items = partial.items(file.Items)
	return file, partial
}

func (p *partialSource) region(span syntax.Span, kind, reason string) {
	p.regions = append(p.regions, Region{File: p.file, Span: span, Kind: kind, Reason: reason})
}

func (p *partialSource) items(items []syntax.Item) []syntax.Item {
	kept := make([]syntax.Item, 0, len(items))
	for _, item := range items {
		switch node := item.(type) {
		case *syntax.FuncDecl:
			signature := *node
			signature.Body = nil
			if hasIncompleteSyntax(&signature) {
				p.region(node.Span(), UnknownDeclaration, "function signature is incomplete")
				continue
			}
			p.function(node)
		case *syntax.ImplDecl:
			signature := *node
			signature.Items = nil
			unknown := hasIncompleteSyntax(&signature)
			for _, member := range node.Items {
				if function, ok := member.(*syntax.FuncDecl); ok {
					copy := *function
					copy.Body = nil
					unknown = unknown || hasIncompleteSyntax(&copy)
				} else {
					unknown = unknown || hasIncompleteSyntax(member)
				}
			}
			if unknown {
				p.unknownProtocol = true
				p.region(node.Span(), UnknownProtocol, "implementation declaration is incomplete")
				continue
			}
			var members []syntax.Item
			for _, member := range node.Items {
				if function, ok := member.(*syntax.FuncDecl); ok {
					p.function(function)
				}
				members = append(members, member)
			}
			node.Items = members
		case *syntax.TraitDecl:
			if hasIncompleteSyntax(node) {
				p.unknownProtocol = true
				p.region(node.Span(), UnknownProtocol, "protocol declaration is incomplete")
				continue
			}
		case *syntax.ModDecl:
			if node.Name == nil || node.Name.Missing {
				p.unknownDeclarations = true
				p.region(node.Span(), UnknownDeclarations, "module name is incomplete")
				continue
			}
			node.Items = p.items(node.Items)
		case *syntax.DeclareDecl:
			children := p.items([]syntax.Item{node.Decl})
			if len(children) == 0 {
				continue
			}
			node.Decl = children[0]
		case *syntax.BadItem:
			// The parser puts a file-header marker on lexical errors anywhere
			// in the file. Errors confined to known bodies do not hide names.
			if node.Text == "invalid package or token" && p.errorsInBodies(items) {
				continue
			}
			p.unknownDeclarations = true
			p.region(node.Span(), UnknownDeclarations, "declaration set is incomplete")
			continue
		default:
			if hasIncompleteSyntax(node) {
				kind := UnknownDeclaration
				if _, macro := node.(*syntax.MacroItem); macro {
					kind = UnknownDeclarations
					p.unknownDeclarations = true
				}
				p.region(node.Span(), kind, "declaration contains incomplete syntax")
				continue
			}
		}
		kept = append(kept, item)
	}
	return kept
}

func (p *partialSource) errorsInBodies(items []syntax.Item) bool {
	var bodies []syntax.Span
	for _, item := range items {
		syntax.Walk(item, func(node syntax.Node) bool {
			if function, ok := node.(*syntax.FuncDecl); ok && function.Body != nil {
				bodies = append(bodies, function.Body.Span())
				return false
			}
			return true
		})
	}
	for _, diagnostic := range p.diagnostics {
		covered := false
		for _, body := range bodies {
			covered = covered || spanContains(body, diagnostic.Span)
		}
		if !covered {
			return false
		}
	}
	return len(p.diagnostics) != 0
}

func (p *partialSource) function(function *syntax.FuncDecl) {
	if function.Body == nil {
		return
	}
	incomplete := hasIncompleteSyntax(function.Body)
	for _, diagnostic := range p.diagnostics {
		incomplete = incomplete || diagnostic.Severity == parser.Error && spanContains(function.Body.Span(), diagnostic.Span)
	}
	if !incomplete {
		return
	}
	p.region(function.Body.Span(), IncompleteBody, "function body is incomplete")
	p.block(function.Body)
	// The projection can express the current signature without inventing a return
	// value. This synthetic terminator has no editable source location.
	end := function.Body.Span().End
	base := syntax.StmtBase{Base: syntax.Base{Loc: syntax.Span{Start: end, End: end}}}
	function.Body.List = append(function.Body.List, &syntax.ForStmt{StmtBase: base, Body: &syntax.Block{StmtBase: base}})
}

func (p *partialSource) block(block *syntax.Block) {
	if block != nil {
		block.List = p.statements(block.List)
	}
}

func (p *partialSource) statements(statements []syntax.Stmt) []syntax.Stmt {
	kept := make([]syntax.Stmt, 0, len(statements))
	for _, statement := range statements {
		if expression, ok := statement.(*syntax.ExprStmt); ok {
			if selector, ok := expression.X.(*syntax.SelectorExpr); ok && selector.Sel != nil && selector.Sel.Missing && !hasIncompleteSyntax(selector.X) {
				base := syntax.Base{Loc: syntax.Span{Start: selector.Span().End, End: selector.Span().End}}
				kept = append(kept, &syntax.AssignStmt{StmtBase: expression.StmtBase, Lhs: []syntax.Expr{&syntax.Ident{Base: base, Name: "_"}}, Tok: token.ASSIGN, Rhs: []syntax.Expr{selector.X}})
				continue
			}
		}
		if hasIncompleteHeader(statement) || p.headerError(statement) {
			continue
		}
		switch node := statement.(type) {
		case *syntax.Block:
			p.block(node)
		case *syntax.IfStmt:
			p.block(node.Body)
			if node.Else != nil {
				child := p.statements([]syntax.Stmt{node.Else})
				if len(child) == 0 {
					node.Else = nil
				} else {
					node.Else = child[0]
				}
			}
		case *syntax.ForStmt:
			p.block(node.Body)
		case *syntax.RangeStmt:
			p.block(node.Body)
		case *syntax.LoopStmt:
			p.block(node.Body)
		case *syntax.WhileStmt:
			p.block(node.Body)
		case *syntax.SwitchStmt:
			for _, clause := range node.Cases {
				clause.Body = p.statements(clause.Body)
			}
		case *syntax.TypeSwitchStmt:
			for _, clause := range node.Cases {
				clause.Body = p.statements(clause.Body)
			}
		case *syntax.SelectStmt:
			for _, clause := range node.Cases {
				clause.Body = p.statements(clause.Body)
			}
		}
		kept = append(kept, statement)
	}
	return kept
}

func (p *partialSource) headerError(statement syntax.Stmt) bool {
	var nested []syntax.Span
	syntax.Walk(statement, func(node syntax.Node) bool {
		if block, ok := node.(*syntax.Block); ok {
			nested = append(nested, block.Span())
			return false
		}
		return true
	})
	for _, diagnostic := range p.diagnostics {
		if !spanContains(statement.Span(), diagnostic.Span) {
			continue
		}
		covered := false
		for _, block := range nested {
			covered = covered || spanContains(block, diagnostic.Span)
		}
		if !covered {
			return true
		}
	}
	return false
}

func hasIncompleteSyntax(node syntax.Node) bool { return incompleteSyntax(node, false) }
func hasIncompleteHeader(node syntax.Node) bool { return incompleteSyntax(node, true) }
func incompleteSyntax(node syntax.Node, skipBlocks bool) bool {
	bad := false
	syntax.Walk(node, func(current syntax.Node) bool {
		if skipBlocks {
			if _, block := current.(*syntax.Block); block {
				return false
			}
		}
		switch current := current.(type) {
		case *syntax.BadItem, *syntax.BadStmt, *syntax.BadExpr, *syntax.BadType, *syntax.BadPat:
			bad = true
		case *syntax.Ident:
			bad = bad || current.Missing
		case *syntax.TokenTree:
			bad = bad || current.Missing
		}
		return !bad
	})
	return bad
}
func spanContains(outer, inner syntax.Span) bool {
	return outer.Start <= inner.Start && inner.End <= outer.End
}
func sourcePackage(name string) string { return path.Dir(name) }

// An expansion failure leaves the generated declaration set unknown. Reuse
// untouched source declarations from this same attempt; never invoke a second
// expander or fill the gap with declarations from an older analysis.
func (g *graph) recoverExpansion(expanded *expand.Result) {
	expanded.Files = nil
	var names []string
	for name := range g.files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		original := g.files[name]
		copy := *original
		copy.Items = nil
		partial := g.sources.partials[name]
		if partial == nil {
			partial = &partialSource{file: name}
			g.sources.partials[name] = partial
		}
		for _, item := range original.Items {
			if _, definition := item.(*syntax.MacroRulesDecl); definition {
				continue
			}
			if function, ok := item.(*syntax.FuncDecl); ok {
				signature := *function
				signature.Body = nil
				if hasUnexpandedMacro(&signature) {
					partial.region(function.Span(), UnknownDeclaration, "function signature requires unavailable macro expansion")
					continue
				}
				if hasUnexpandedMacro(function.Body) {
					partial.region(function.Body.Span(), IncompleteBody, "function body requires unavailable macro expansion")
					end := function.Body.Span().End
					base := syntax.StmtBase{Base: syntax.Base{Loc: syntax.Span{Start: end, End: end}}}
					body := *function.Body
					body.List = []syntax.Stmt{&syntax.ForStmt{StmtBase: base, Body: &syntax.Block{StmtBase: base}}}
					signature.Body = &body
					item = &signature
				}
			} else if hasUnexpandedMacro(item) {
				partial.region(item.Span(), UnknownDeclarations, "declaration set requires unavailable macro expansion")
				continue
			}
			copy.Items = append(copy.Items, item)
		}
		var id syntax.NodeID
		syntax.Walk(&copy, func(node syntax.Node) bool { id++; node.(interface{ SetID(syntax.NodeID) }).SetID(id); return true })
		expanded.Origins[&copy] = expand.Origin{File: original, Span: original.Span()}
		expanded.Files = append(expanded.Files, &copy)
	}
}

func hasUnexpandedMacro(node syntax.Node) bool {
	found := false
	syntax.Walk(node, func(node syntax.Node) bool {
		switch node.(type) {
		case *syntax.MacroExpr, *syntax.MacroType, *syntax.MacroItem, *syntax.Attr:
			found = true
		}
		return !found
	})
	return found
}
