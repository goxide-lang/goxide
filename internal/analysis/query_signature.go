package analysis

import (
	"go/types"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type SignatureHelp struct {
	Label           string
	Parameters      []string
	ActiveParameter int
}

// SignatureHelp selects the innermost authored call and counts commas in its
// lexical argument list. Nested expressions and string contents do not advance
// the active parameter. The declaration/signature must exist in this snapshot.
func (q *Queries) SignatureHelp(file string, offset uint32) (*SignatureHelp, error) {
	if q.result == nil || q.result.Checked == nil || q.result.Resolved == nil {
		return nil, nil
	}
	source := q.result.Sources[file]
	if source == nil || source.Syntax == nil || int(offset) > len(source.Content) {
		return nil, nil
	}
	if reason := q.blocked[sourcePackage(file)]; reason != "" {
		return nil, &QueryUnavailable{Reason: reason}
	}
	var chosen *syntax.CallExpr
	active := 0
	syntax.Walk(source.Syntax, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || call.Fun == nil || call.Fun.Span().End >= offset {
			return true
		}
		argument, inside := callArgument(source.Syntax.Tokens, call.Fun.Span().End, offset)
		if inside && (chosen == nil || call.Fun.Span().End > chosen.Fun.Span().End) {
			chosen = call
			active = argument
		}
		return true
	})
	var signature *types.Signature
	var declaration *syntax.FuncDecl
	var method bool
	var label string
	if chosen != nil {
		signature, declaration, method = q.callSignature(file, chosen)
		label = printer.Source(chosen.Fun)
	} else {
		// The parser intentionally replaces an unfinished statement with BadStmt.
		// Retained lexical delimiters locate its call; only a known simple binding
		// can supply a signature, without parsing or checking the body again.
		for i := len(source.Syntax.Tokens) - 1; i > 0; i-- {
			opening := source.Syntax.Tokens[i]
			if opening.Kind != token.LPAREN || opening.End > offset {
				continue
			}
			name := source.Syntax.Tokens[i-1]
			if name.Kind != token.IDENT {
				continue
			}
			if i > 1 {
				switch source.Syntax.Tokens[i-2].Kind {
				case token.FUNC, token.PERIOD, token.DCOLON, token.NOT:
					continue
				}
			}
			argument, inside := callArgument(source.Syntax.Tokens, name.End, offset)
			if !inside {
				continue
			}
			signature, declaration = q.lexicalSignature(file, name.Text, name.Start)
			label, active = name.Text, argument
			break
		}
	}
	if signature == nil || !reliableType(signature, map[types.Type]bool{}) {
		return nil, nil
	}
	parameters := []string{}
	if declaration != nil {
		params := declaration.Params
		if method && len(params) > 0 && len(params[0].Names) == 1 && params[0].Names[0].Name == "self" {
			params = params[1:]
		}
		for _, param := range params {
			if len(param.Names) <= 1 {
				parameters = append(parameters, printer.Source(param))
				continue
			}
			for _, name := range param.Names {
				copy := *param
				copy.Names = []*syntax.Ident{name}
				parameters = append(parameters, printer.Source(&copy))
			}
		}
		label += "(" + strings.Join(parameters, ", ") + ")"
		var results []string
		for _, result := range declaration.Results {
			results = append(results, printer.Source(result))
		}
		if len(results) > 0 {
			label += " "
			if declaration.ResultParen || len(results) > 1 {
				label += "(" + strings.Join(results, ", ") + ")"
			} else {
				label += results[0]
			}
		}
	} else {
		qualifier := func(pkg *types.Package) string { return pkg.Name() }
		for i := 0; i < signature.Params().Len(); i++ {
			parameter := signature.Params().At(i)
			text := types.TypeString(parameter.Type(), qualifier)
			if signature.Variadic() && i == signature.Params().Len()-1 {
				if slice, ok := parameter.Type().(*types.Slice); ok {
					text = "..." + types.TypeString(slice.Elem(), qualifier)
				}
			}
			if parameter.Name() != "" {
				text = parameter.Name() + " " + text
			}
			parameters = append(parameters, text)
		}
		rendered := types.TypeString(signature, qualifier)
		at := strings.Index(rendered, "(")
		if at >= 0 {
			label += rendered[at:]
		}
	}
	if len(parameters) == 0 {
		active = 0
	} else if active >= len(parameters) {
		active = len(parameters) - 1
	}
	return &SignatureHelp{Label: label, Parameters: parameters, ActiveParameter: active}, nil
}

func callArgument(tokens []token.Token, funEnd, offset uint32) (int, bool) {
	opening := -1
	for i, item := range tokens {
		if item.Start >= funEnd {
			if item.Kind != token.LPAREN {
				return 0, false
			}
			opening = i
			break
		}
	}
	if opening < 0 || tokens[opening].End > offset {
		return 0, false
	}
	depth, argument := 0, 0
	for _, item := range tokens[opening+1:] {
		if item.Start >= offset {
			return argument, true
		}
		switch item.Kind {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			if depth == 0 {
				return 0, false
			}
			depth--
		case token.COMMA:
			if depth == 0 {
				argument++
			}
		case token.EOF:
			return argument, true
		}
	}
	return argument, true
}

func (q *Queries) callSignature(file string, call *syntax.CallExpr) (*types.Signature, *syntax.FuncDecl, bool) {
	span := call.Fun.Span()
	var selected *queryEntry
	for i := range q.entries[file] {
		entry := &q.entries[file][i]
		if entry.location.Generated || entry.unavailable != "" || entry.location.Span.Start < span.Start || entry.location.Span.End > span.End {
			continue
		}
		if _, ok := entry.typ.(*types.Signature); !ok {
			continue
		}
		if selected == nil || entry.location.Span.End-entry.location.Span.Start > selected.location.Span.End-selected.location.Span.Start {
			selected = entry
		}
	}
	if selected != nil {
		signature := selected.typ.(*types.Signature)
		if selected.member != nil && selected.member.Member != nil {
			return signature, selected.member.Member.Decl, !selected.member.MethodExpr && !selected.member.Member.Static
		}
		if symbol := q.actionSymbol(selected.symbol); symbol != nil {
			if declaration := q.signatureDeclaration(symbol); declaration != nil {
				return signature, declaration, false
			}
		}
		return signature, nil, false
	}
	// A recovered node can still have a known simple lexical callee.
	var name string
	switch fun := call.Fun.(type) {
	case *syntax.Ident:
		name = fun.Name
	case *syntax.PathExpr:
		if fun.Path != nil && len(fun.Path.Segments) == 1 {
			name = fun.Path.Segments[0].Name.Name
		}
	}
	if name == "" {
		return nil, nil, false
	}
	signature, declaration := q.lexicalSignature(file, name, call.Fun.Span().Start)
	return signature, declaration, false
}

func (q *Queries) lexicalSignature(file, name string, offset uint32) (*types.Signature, *syntax.FuncDecl) {
	selected := q.scopeAt(file, offset)
	if selected == nil {
		return nil, nil
	}
	for scope := selected.scope; scope != nil; scope = scope.Parent {
		symbol := scope.Bindings[resolve.Key{Name: name}]
		if symbol == nil {
			if aliases := scope.Aliases[resolve.Key{Name: name}]; len(aliases) == 1 {
				symbol = aliases[0]
			} else if len(aliases) > 1 {
				return nil, nil
			}
		}
		if symbol == nil {
			continue
		}
		if declaration := q.nodes[symbol.Decl]; declaration != nil && symbol.Decl.File == file && symbol.Kind == resolve.Variable && declaration.Span().Start > offset {
			return nil, nil
		}
		signature, _ := q.symbolType(symbol).(*types.Signature)
		symbol = q.actionSymbol(symbol)
		var declaration *syntax.FuncDecl
		if symbol != nil {
			declaration = q.signatureDeclaration(symbol)
		}
		return signature, declaration
	}
	return nil, nil
}

func (q *Queries) signatureDeclaration(symbol *resolve.Symbol) *syntax.FuncDecl {
	for ref, node := range q.nodes {
		if ref.File != symbol.Decl.File {
			continue
		}
		if declaration, ok := node.(*syntax.FuncDecl); ok && declaration.Name != nil && declaration.Name.ID() == symbol.Decl.ID {
			return declaration
		}
	}
	return nil
}
