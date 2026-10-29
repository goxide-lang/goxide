package expand

import (
	"fmt"
	"io"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

// procInvocationConfig describes an already resolved invocation.
// Ctx is the fresh expansion context already registered in the engine.
type procInvocationConfig struct {
	RequestID, Entry string
	Kind             pm.Kind
	Position         pm.Position
	Slot             fragmentKind
	Definition, Call Origin
	CallLocation     Origin
	Ctx              ast.SyntaxCtx
	Caller           Scope
	Input            []syntaxToken
	Args, Item       []syntaxToken
	Target           pm.Target
	Features         []string
	RuntimeRoots     map[string][]syntaxToken
}

// procBinding includes lexical ownership, not just a numeric hygiene mark.
// Source files may share Ctx zero while using different imports and cfg values.
type procBinding struct {
	File           *ast.File
	Ctx            ast.SyntaxCtx
	Scope, Lexical Scope
}

type procDiagnostic struct {
	Severity pm.Severity
	Message  string
	Location Origin
}

// The SDK owns and validates the frozen wire reference table. These host-only
// maps give its references meaning without serializing AST or compiler objects.
type procInvocation struct {
	engine       *engine
	request      pm.Request
	scope        *public.Scope
	definition   Origin
	callLocation Origin
	bindings     map[uint32]procBinding
	contexts     map[procBinding]public.BindingContext
	spans        map[uint32]Origin
	spanIDs      map[Origin]public.Span
	inputs       map[uint32]syntaxToken
}

func (e *engine) newProcInvocation(config procInvocationConfig) (*procInvocation, error) {
	positions := map[fragmentKind]pm.Position{
		exprKind: pm.ExpressionPosition, typeKind: pm.TypePosition,
		stmtKind: pm.StatementsPosition, itemKind: pm.ItemPosition,
		itemsKind: pm.ItemsPosition,
	}
	position, ok := positions[config.Slot]
	if !ok {
		return nil, fmt.Errorf("unsupported procedural macro output slot %q", config.Slot)
	}
	if config.Position != "" {
		position = config.Position
	}
	if config.Kind == "" {
		config.Kind = pm.Function
	}
	if config.Definition.File == nil || config.Call.File == nil || e.r == nil {
		return nil, fmt.Errorf("procedural macro invocation requires definition and call origins")
	}
	if config.CallLocation.File == nil {
		config.CallLocation = config.Call
	}
	context, ok := e.r.Contexts[config.Ctx]
	if !ok || config.Ctx == 0 {
		return nil, fmt.Errorf("procedural macro invocation requires a registered expansion context")
	}
	invocation := &procInvocation{
		engine: e,
		scope:  public.NewScope(config.RequestID), definition: config.Definition, callLocation: config.CallLocation,
		bindings: make(map[uint32]procBinding), contexts: make(map[procBinding]public.BindingContext),
		spans: make(map[uint32]Origin), spanIDs: make(map[Origin]public.Span),
		inputs: make(map[uint32]syntaxToken),
	}
	defSite := invocation.binding(procBinding{
		File: config.Definition.File, Ctx: config.Ctx,
		Scope: context.Definition, Lexical: Scope{File: config.Definition.File, Ctx: config.Ctx},
	})
	callSite := invocation.binding(procBinding{
		File: config.Call.File, Ctx: config.Caller.Ctx, Scope: config.Caller, Lexical: config.Caller,
	})
	defSpan, err := invocation.span(config.Definition)
	if err != nil {
		return nil, err
	}
	callSpan, err := invocation.span(config.CallLocation)
	if err != nil {
		return nil, err
	}
	decode := func(input []syntaxToken) (public.TokenStream, error) {
		wire, err := invocation.inputWire(input, false)
		if err != nil {
			return public.TokenStream{}, err
		}
		stream, err := invocation.scope.Decode(wire)
		if err != nil {
			return public.TokenStream{}, fmt.Errorf("procedural macro input: %w", err)
		}
		stream, err = invocation.scope.Capture(stream, callSite)
		if err != nil {
			return public.TokenStream{}, err
		}
		return stream, invocation.rememberInputs(stream.Tokens(), input)
	}
	input, err := decode(config.Input)
	if err != nil {
		return nil, err
	}
	args, err := decode(config.Args)
	if err != nil {
		return nil, err
	}
	item, err := decode(config.Item)
	if err != nil {
		return nil, err
	}
	roots := make(map[string]public.TokenStream, len(config.RuntimeRoots))
	for _, name := range sortedRuntimeRootNames(config.RuntimeRoots) {
		roots[name], err = decode(config.RuntimeRoots[name])
		if err != nil {
			return nil, err
		}
	}
	invocation.request, err = pm.NewRequest(invocation.scope, pm.RequestConfig{
		Entry: config.Entry, Kind: config.Kind, Position: position,
		DefSite: defSite, CallSite: callSite, DefSpan: defSpan, CallSpan: callSpan,
		Target: config.Target, Features: config.Features, Input: input, Args: args, Item: item, RuntimeRoots: roots,
	})
	if err != nil {
		return nil, err
	}
	return invocation, nil
}

func (p *procInvocation) binding(binding procBinding) public.BindingContext {
	if context, ok := p.contexts[binding]; ok {
		return context
	}
	context := p.scope.NewContext()
	p.contexts[binding], p.bindings[context.Reference()] = context, binding
	return context
}

func (p *procInvocation) span(origin Origin) (public.Span, error) {
	if origin.File == nil {
		origin = p.callLocation
	}
	if span, ok := p.spanIDs[origin]; ok {
		return span, nil
	}
	span, err := p.scope.NewSpan(origin.Span.Start, origin.Span.End)
	if err != nil {
		return public.Span{}, err
	}
	p.spanIDs[origin], p.spans[span.Reference()] = span, origin
	return span, nil
}

func (p *procInvocation) wireSpan(origin Origin) (*public.WireSpan, error) {
	span, err := p.span(origin)
	if err != nil {
		return nil, err
	}
	wire, err := p.scope.EncodeSpan(span)
	return &wire, err
}

var procLiteralKinds = map[token.Kind]public.LiteralType{
	token.INT: public.Integer, token.FLOAT: public.Float, token.IMAG: public.Imaginary,
	token.CHAR: public.Character, token.STRING: public.String,
}

// A fragment's observable view recursively loses fragment identities. In
// particular, do not issue otherwise hidden nested fragment handles while
// preparing that view; all SDK-issued references appear in the request table.
func (p *procInvocation) inputWire(input []syntaxToken, view bool) ([]public.WireToken, error) {
	var out []public.WireToken
	for _, source := range input {
		if source.Tree != nil && source.Tree.Delim == ast.NoDelim || view && source.Fragment != nil {
			children := source.Tree
			var nested []syntaxToken
			if source.Fragment != nil {
				nested = source.Fragment.Tokens
			} else {
				nested = children.Tokens
			}
			wire, err := p.inputWire(nested, view)
			if err != nil {
				return nil, err
			}
			out = append(out, wire...)
			continue
		}
		context := p.binding(procBinding{source.Origin.File, source.Ctx, source.Scope, source.Lexical})
		span, err := p.wireSpan(tokenLocation(source))
		if err != nil {
			return nil, err
		}
		wire := public.WireToken{Context: context.Reference(), Span: span}
		switch {
		case source.Fragment != nil:
			children, err := p.inputWire(source.Fragment.Tokens, true)
			if err != nil {
				return nil, err
			}
			stream, err := p.scope.Decode(children)
			if err != nil {
				return nil, err
			}
			fragment, err := p.scope.CaptureFragment(public.FragmentType(source.Fragment.Kind), stream, context)
			if err != nil {
				return nil, err
			}
			p.inputs[fragment.Reference()] = source
			wire = public.WireToken{Ref: fragment.Reference(), Span: span}
		case source.Tree != nil:
			wire.Kind = string(public.GroupKind)
			switch source.Tree.Delim {
			case ast.Paren:
				wire.Delimiter = string(public.Parenthesis)
			case ast.Bracket:
				wire.Delimiter = string(public.Bracket)
			case ast.Brace:
				wire.Delimiter = string(public.Brace)
			default:
				return nil, fmt.Errorf("unknown compiler token delimiter %d", source.Tree.Delim)
			}
			open, close := source.Tree.Open, source.Tree.Close
			fallback := tokenLocation(source)
			if source.Tree.Location.File != nil {
				fallback = source.Tree.Location
			}
			if open.File == nil {
				open = fallback
				if open.Span.End > open.Span.Start {
					open.Span.End = open.Span.Start + 1
				}
			}
			if close.File == nil {
				close = fallback
				if close.Span.End > close.Span.Start {
					close.Span.Start = close.Span.End - 1
				}
			}
			if wire.Open, err = p.wireSpan(open); err != nil {
				return nil, err
			}
			if wire.Close, err = p.wireSpan(close); err != nil {
				return nil, err
			}
			if wire.Children, err = p.inputWire(source.Tree.Tokens, view); err != nil {
				return nil, err
			}
		case source.Token != nil:
			tok := source.Token
			switch {
			case tok.Kind == token.IDENT:
				wire.Kind, wire.Text, wire.Raw = string(public.IdentKind), tok.Text, tok.Raw
			case tok.Kind == token.SEMICOLON:
				wire.Kind, wire.Implicit = string(public.SemicolonKind), tok.Implicit
			case procLiteralKinds[tok.Kind] != "":
				wire.Kind, wire.Literal, wire.Text = string(public.LiteralKind), string(procLiteralKinds[tok.Kind]), tok.Text
			case tok.Kind.IsKeyword():
				wire.Kind, wire.Text = string(public.KeywordKind), tok.Text
			case tok.Kind.IsOperator():
				wire.Kind, wire.Text = string(public.PunctKind), tok.Text
			default:
				return nil, fmt.Errorf("unsupported compiler token %s", tok.Kind)
			}
		default:
			return nil, fmt.Errorf("empty compiler syntax token")
		}
		out = append(out, wire)
	}
	return out, nil
}

func (p *procInvocation) rememberInputs(nodes []public.Token, source []syntaxToken) error {
	index := 0
	var remember func([]syntaxToken) error
	remember = func(input []syntaxToken) error {
		for _, original := range input {
			if original.Tree != nil && original.Tree.Delim == ast.NoDelim {
				if err := remember(original.Tree.Tokens); err != nil {
					return err
				}
				continue
			}
			if index >= len(nodes) || nodes[index].Reference() == 0 {
				return fmt.Errorf("procedural macro input reference mapping is incomplete")
			}
			node := nodes[index]
			index++
			p.inputs[node.Reference()] = original
			if original.Tree != nil {
				if err := p.rememberInputs(node.Children().Tokens(), original.Tree.Tokens); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := remember(source); err != nil {
		return err
	}
	if index != len(nodes) {
		return fmt.Errorf("procedural macro input reference mapping has extra nodes")
	}
	return nil
}

func (p *procInvocation) location(span public.Span) (Origin, error) {
	if span.Reference() == 0 {
		// Generated offsets have no associated source file in this bridge.
		return p.callLocation, nil
	}
	origin, ok := p.spans[span.Reference()]
	if !ok {
		return Origin{}, fmt.Errorf("unknown compiler diagnostic span %d", span.Reference())
	}
	origin.Span.Start, origin.Span.End = span.Range()
	return origin, nil
}

// readResponse is the only admission path. No compiler meaning is recovered
// until the SDK has validated the response against this invocation's snapshot.
func (p *procInvocation) readResponse(reader io.Reader) ([]syntaxToken, []procDiagnostic, error) {
	output, responseErr := pm.ReadResponse(reader, p.request)
	diagnostics := make([]procDiagnostic, 0, len(output.Diagnostics))
	for _, diagnostic := range output.Diagnostics {
		location, err := p.location(diagnostic.Span)
		if err != nil {
			return nil, diagnostics, err
		}
		diagnostics = append(diagnostics, procDiagnostic{diagnostic.Severity, diagnostic.Message, location})
	}
	if responseErr != nil {
		return nil, diagnostics, responseErr
	}
	tokens, err := p.restoreStream(output.Tokens)
	if err != nil {
		return nil, diagnostics, err
	}
	if err := p.countOutput(tokens); err != nil {
		return nil, diagnostics, err
	}
	return tokens, diagnostics, nil
}

func (p *procInvocation) countOutput(tokens []syntaxToken) error {
	for _, tok := range tokens {
		if err := p.engine.emit(); err != nil {
			return err
		}
		if tok.Tree != nil {
			if err := p.countOutput(tok.Tree.Tokens); err != nil {
				return err
			}
		}
		// Match declaration macros: a captured fragment costs one output
		// token. Its parsed result is charged by the parse/count node budget.
	}
	return nil
}

func (p *procInvocation) restoreStream(stream public.TokenStream) ([]syntaxToken, error) {
	var out []syntaxToken
	for _, node := range stream.Tokens() {
		location, err := p.location(node.Span())
		if err != nil {
			return nil, err
		}
		var restored syntaxToken
		if node.Reference() != 0 {
			var ok bool
			restored, ok = p.inputs[node.Reference()]
			if !ok {
				return nil, fmt.Errorf("unknown compiler input reference %d", node.Reference())
			}
			// An unchanged opaque fragment must retain its per-node locations.
			// A nonzero Location on the wrapper explicitly redirects its copy.
			if location != tokenLocation(restored) {
				restored.Location = location
			}
			if restored.Token != nil {
				copy := *restored.Token
				restored.Token = &copy
			}
			if restored.Fragment != nil {
				// splice will copy the AST for each use. Retaining this opaque
				// host object keeps its category and all semantic side tables.
				out = append(out, restored)
				continue
			}
		} else {
			binding, ok := p.bindings[node.Context().Reference()]
			if !ok {
				return nil, fmt.Errorf("unknown compiler binding context %d", node.Context().Reference())
			}
			scope, lexical := binding.Scope, binding.Lexical
			// Group tokens carry a context even when no identifier-specific
			// scope has been recorded yet. Their source file remains the
			// binding owner if a macro uses that context for a new identifier.
			if scope.File == nil {
				scope = Scope{File: binding.File, Ctx: binding.Ctx}
			}
			if lexical.File == nil {
				lexical = Scope{File: binding.File, Ctx: binding.Ctx}
			}
			restored = syntaxToken{
				Origin: p.definition, Location: location,
				Ctx: binding.Ctx, Scope: scope, Lexical: lexical,
			}
		}
		if node.Kind() == public.GroupKind {
			tree := &syntaxTree{Origin: restored.Origin, Location: location}
			if restored.Tree != nil {
				*tree = *restored.Tree
				tree.Location = location
			}
			switch node.Delimiter() {
			case public.Parenthesis:
				tree.Delim = ast.Paren
			case public.Bracket:
				tree.Delim = ast.Bracket
			case public.Brace:
				tree.Delim = ast.Brace
			default:
				return nil, fmt.Errorf("unsupported public token delimiter %q", node.Delimiter())
			}
			open, close := node.DelimiterSpans()
			if tree.Open, err = p.location(open); err != nil {
				return nil, err
			}
			if tree.Close, err = p.location(close); err != nil {
				return nil, err
			}
			if tree.Tokens, err = p.restoreStream(node.Children()); err != nil {
				return nil, err
			}
			restored.Tree = tree
		} else if restored.Token == nil {
			tok, err := procLexicalToken(node)
			if err != nil {
				return nil, err
			}
			tok.Start, tok.End = restored.Origin.Span.Start, restored.Origin.Span.End
			restored.Token = &tok
		}
		out = append(out, restored)
	}
	return out, nil
}

func procLexicalToken(node public.Token) (token.Token, error) {
	tok := token.Token{Text: node.Text(), Raw: node.Raw(), Implicit: node.Implicit()}
	switch node.Kind() {
	case public.IdentKind:
		tok.Kind = token.IDENT
	case public.KeywordKind:
		tok.Kind = token.Lookup(node.Text())
		if !tok.Kind.IsKeyword() {
			return tok, fmt.Errorf("unsupported public keyword %q", node.Text())
		}
	case public.LiteralKind:
		for kind, literal := range procLiteralKinds {
			if literal == node.LiteralType() {
				tok.Kind = kind
				return tok, nil
			}
		}
		return tok, fmt.Errorf("unsupported public literal %q", node.LiteralType())
	case public.SemicolonKind:
		tok.Kind, tok.Text = token.SEMICOLON, ";"
	case public.PunctKind:
		for kind := token.ADD; kind <= token.DOLLAR; kind++ {
			if kind.String() == node.Text() {
				tok.Kind = kind
				return tok, nil
			}
		}
		return tok, fmt.Errorf("unsupported public punctuation %q", node.Text())
	default:
		return tok, fmt.Errorf("unsupported new public token %q", node.Kind())
	}
	return tok, nil
}
