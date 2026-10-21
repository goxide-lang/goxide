package expand

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

func procBridgeFixture(t *testing.T, input string) (*engine, procInvocationConfig) {
	t.Helper()
	definition := expandFile(t, "macro.gox", "pub func identity() {}")
	caller := expandFile(t, "caller.gox", "var value = identity!("+input+")")
	call := expandValue(t, caller, "value").(*ast.MacroExpr)
	e := &engine{
		r: &Result{
			Origins: map[ast.Node]Origin{}, Locations: map[ast.Node]Origin{},
			Scopes: map[*ast.Ident]Scope{}, Contexts: map[ast.SyntaxCtx]Context{},
			Expansions: map[ast.ExpnID]Expansion{},
		},
		trees: map[*ast.TokenTree][]syntaxToken{},
	}
	e.reserve(definition)
	e.reserve(caller)
	config := procInvocationConfig{
		RequestID: "bridge-1", Entry: "identity", Slot: exprKind,
		Definition: Origin{definition, definition.Items[0].Span()}, Call: Origin{caller, call.Span()},
		Ctx: 1, Caller: Scope{File: caller}, Target: pm.Target{OS: "linux", Arch: "arm64"},
		Features: []string{"second", "first"},
	}
	e.r.Contexts[1] = Context{Definition: Scope{File: definition}, Caller: config.Caller, Expn: 1}
	e.r.Expansions[1] = Expansion{Definition: config.Definition, Call: config.Call, Ctx: 1, Stack: []ast.ExpnID{1}}
	e.nextCtx = 1
	config.Input = e.treeInput(call.Args, caller, 0)
	return e, config
}

func procBridgeInvocation(t *testing.T, e *engine, config procInvocationConfig) *procInvocation {
	t.Helper()
	invocation, err := e.newProcInvocation(config)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func procBridgeResponse(t *testing.T, invocation *procInvocation, output func(pm.Request) pm.Output) []byte {
	t.Helper()
	var request bytes.Buffer
	if err := pm.WriteRequest(&request, invocation.request); err != nil {
		t.Fatal(err)
	}
	guest, err := pm.ReadRequest(&request)
	if err != nil {
		t.Fatal(err)
	}
	var response bytes.Buffer
	if err := pm.WriteResponse(&response, guest, output(guest)); err != nil {
		t.Fatal(err)
	}
	return response.Bytes()
}

func procBridgeOutput(t *testing.T, invocation *procInvocation, output func(pm.Request) pm.Output) []syntaxToken {
	t.Helper()
	response := procBridgeResponse(t, invocation, output)
	tokens, diagnostics, err := invocation.readResponse(bytes.NewReader(response))
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("response: diagnostics=%v, error=%v", diagnostics, err)
	}
	return tokens
}

func TestProcBridgeSourceTokensRoundTrip(t *testing.T) {
	e, config := procBridgeFixture(t, "r#type::Value(-42, 3.5, 2i, 'x', \"s\"); match value { _ => 7\n}")
	invocation := procBridgeInvocation(t, e, config)
	output := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output {
		if request.Kind() != pm.Function || request.Position() != pm.ExpressionPosition ||
			request.Context().Target() != config.Target ||
			!reflect.DeepEqual(request.Context().Features(), []string{"first", "second"}) {
			t.Fatal("request lost function slot or frozen build configuration")
		}
		return pm.Success(request.Input())
	})
	if !reflect.DeepEqual(output, config.Input) {
		t.Fatal("identity response changed lexical tokens, contexts or provenance")
	}
	if output[0].Token == config.Input[0].Token {
		t.Fatal("response occurrence reuses mutable source token storage")
	}

	config.RequestID = "bridge-root"
	config.Input = []syntaxToken{{Tree: &syntaxTree{Delim: ast.NoDelim, Tokens: config.Input}}}
	root := procBridgeInvocation(t, e, config)
	unwrapped := procBridgeOutput(t, root, func(request pm.Request) pm.Output { return pm.Success(request.Input()) })
	if !reflect.DeepEqual(unwrapped, output) {
		t.Fatal("transparent root acquired a public delimiter")
	}
}

func TestProcBridgeRuntimeRootRoundTripKeepsBoundContext(t *testing.T) {
	e, config := procBridgeFixture(t, "value")
	root := config.Input[0]
	root.Ctx = 2
	root.Scope, root.Lexical = Scope{File: config.Call.File, Ctx: 2}, Scope{File: config.Call.File, Ctx: 2}
	config.RuntimeRoots = map[string][]syntaxToken{"logical.example/runtime": {root}}
	invocation := procBridgeInvocation(t, e, config)
	output := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output {
		ctx := request.Context()
		stream, err := ctx.RuntimeRoot("logical.example/runtime")
		if err != nil {
			t.Fatal(err)
		}
		original := stream.Tokens()[0]
		rebuilt, err := public.NewIdent(original.Text(), false, original.Context())
		if err != nil {
			t.Fatal(err)
		}
		return pm.Success(public.New(original.WithSpan(ctx.DefSpan()), rebuilt.WithSpan(ctx.CallSpan())))
	})
	if len(output) != 2 || output[0].Origin != root.Origin || output[1].Origin != config.Definition ||
		output[0].Location != config.Definition || output[1].Location != config.Call {
		t.Fatal("runtime root round trip lost reference identity or diagnostic spans")
	}
	for _, actual := range output {
		if actual.Ctx != root.Ctx || actual.Scope != root.Scope || actual.Lexical != root.Lexical {
			t.Fatal("runtime root round trip replaced its binding with DefSite or CallSite")
		}
	}
}

func TestProcBridgeSpanChangesOnlyDiagnostics(t *testing.T) {
	e, config := procBridgeFixture(t, "outer(inner)")
	invocation := procBridgeInvocation(t, e, config)
	output := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output {
		nodes := request.Input().Tokens()
		nodes[0] = nodes[0].WithSpan(request.Context().DefSpan())
		nodes[1] = nodes[1].WithSpan(request.Context().CallSpan()).
			WithDelimiterSpans(request.Context().DefSpan(), request.Context().CallSpan())
		return pm.Success(public.New(nodes...))
	})
	first, original := output[0], config.Input[0]
	if first.Origin != original.Origin || first.Ctx != original.Ctx ||
		first.Scope != original.Scope || first.Lexical != original.Lexical ||
		*first.Token != *original.Token || first.Location != config.Definition {
		t.Fatal("WithSpan changed input identity or failed to redirect its diagnostic")
	}
	group := output[1]
	if group.Origin != config.Input[1].Origin || group.Tree.Origin != config.Input[1].Tree.Origin ||
		group.Location != config.Call || group.Tree.Location != config.Call ||
		group.Tree.Open != config.Definition || group.Tree.Close != config.Call ||
		!reflect.DeepEqual(group.Tree.Tokens, config.Input[1].Tree.Tokens) {
		t.Fatal("group span overrides changed children or trusted provenance")
	}
}

func TestProcBridgeNewSyntaxDoesNotRecoverInputProvenance(t *testing.T) {
	e, config := procBridgeFixture(t, "value")
	other, lexical := &ast.File{}, &ast.File{}
	source := config.Input[0]
	second, third, fourth := source, source, source
	second.Origin.File, second.Location.File = other, other
	third.Scope.File = other
	fourth.Lexical.File = lexical
	config.Input = []syntaxToken{source, second, third, fourth}
	invocation := procBridgeInvocation(t, e, config)
	output := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output {
		input := request.Input().Tokens()
		seen := map[uint32]bool{}
		var nodes []public.Token
		for _, original := range input {
			if seen[original.Context().Reference()] {
				t.Fatal("distinct source/lexical ownership collapsed into one context")
			}
			seen[original.Context().Reference()] = true
			node, err := public.NewIdent("rebuilt", false, original.Context())
			if err != nil {
				t.Fatal(err)
			}
			nodes = append(nodes, node.WithSpan(original.Span()))
		}
		for _, context := range []public.BindingContext{{}, request.Context().CallSite()} {
			node, err := public.NewIdent("generated", false, context)
			if err != nil {
				t.Fatal(err)
			}
			nodes = append(nodes, node)
		}
		return pm.Success(public.New(nodes...))
	})
	for i, original := range config.Input {
		if output[i].Origin != config.Definition || output[i].Location != tokenLocation(original) ||
			output[i].Ctx != original.Ctx || output[i].Scope != original.Scope || output[i].Lexical != original.Lexical {
			t.Fatalf("rebuilt token %d recovered trusted input provenance or lost binding", i)
		}
	}
	defSite, callSite := output[4], output[5]
	if defSite.Ctx != config.Ctx || defSite.Scope != e.r.Contexts[config.Ctx].Definition ||
		defSite.Lexical != (Scope{config.Definition.File, config.Ctx}) || defSite.Location != config.Call {
		t.Fatal("new zero-context syntax did not bind at DefSite or local span did not fall back to Call")
	}
	if callSite.Ctx != config.Caller.Ctx || callSite.Scope != config.Caller || callSite.Lexical != config.Caller ||
		callSite.Origin != config.Definition || callSite.Location != config.Call {
		t.Fatal("explicit CallSite changed provenance or lost lexical caller")
	}
}

func TestProcBridgeNewSyntaxParsesInEverySlot(t *testing.T) {
	for _, test := range []struct {
		name, source string
		slot         fragmentKind
	}{
		{"expression", "r#type::Value(-42, 3.5, 2i, 'x', \"s\")", exprKind},
		{"implicit-semicolon", "match value { _ => 42\n}", exprKind},
		{"type", "map[string][]*pkg::Value", typeKind},
		{"statement", "value >>= 2", stmtKind},
		{"item", "pub func generated() int { return 42 }", itemKind},
	} {
		t.Run(test.name, func(t *testing.T) {
			e, config := procBridgeFixture(t, "unused")
			config.Slot = test.slot
			invocation := procBridgeInvocation(t, e, config)
			output := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output {
				return pm.FromParse(public.ParseText(test.source, request.Context().DefSite()))
			})
			node, err := e.parse(output, test.slot, 1, config.Caller)
			if err != nil {
				t.Fatal(err)
			}
			ast.Walk(node, func(n ast.Node) bool {
				// The statement-list wrapper is supplied by the compiler and
				// is not one of the macro's generated syntax nodes.
				if n == node && test.slot == stmtKind {
					return true
				}
				if e.r.Origins[n] != config.Definition || e.location(n) != config.Call {
					t.Fatalf("new %T gained input provenance or unmapped local coordinates", n)
				}
				return true
			})
		})
	}
}

func TestProcBridgeNewIdentifierUsesGroupContextOwner(t *testing.T) {
	e, config := procBridgeFixture(t, "(value)")
	origin := expandFile(t, "forwarded.gox", `macro_rules! inner { () => { $caller::x }; }`)
	config.Input[0].Origin.File = origin
	config.Input[0].Tree.Origin.File = origin
	if config.Input[0].Scope.File != nil || config.Input[0].Lexical.File != nil {
		t.Fatal("fixture should exercise a group without identifier-specific scope")
	}
	invocation := procBridgeInvocation(t, e, config)
	output := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output {
		ident, err := public.NewIdent("from_group", false, request.Input().Tokens()[0].Context())
		if err != nil {
			t.Fatal(err)
		}
		return pm.Success(public.New(ident))
	})
	want := Scope{File: origin, Ctx: config.Input[0].Ctx}
	if output[0].Origin != config.Definition || output[0].Scope != want || output[0].Lexical != want {
		t.Fatal("group context fell back to the current call file")
	}
	node, err := e.parse(output, exprKind, 1, config.Caller)
	if err != nil || e.r.Scopes[node.(*ast.Ident)] != want {
		t.Fatalf("AST recovery discarded the group context owner: %v", err)
	}
	nested := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output {
		return pm.FromParse(public.ParseText("inner!()", request.Input().Tokens()[0].Context()))
	})
	node, err = e.parse(nested, exprKind, 1, config.Caller)
	if err != nil {
		t.Fatal(err)
	}
	macro := node.(*ast.MacroExpr)
	if e.lexical[macro] != want {
		t.Fatal("new nested invocation lost its group context's lexical owner")
	}
	e.localFiles = map[*ast.File]bool{origin: true}
	e.defs = map[Key]definition{{Name: "inner"}: {decl: origin.Items[0].(*ast.MacroRulesDecl), file: origin}}
	result, err := e.invoke(macro, macro.Path, macro.Args, exprKind, config.Call.File, 1)
	if err != nil {
		t.Fatal(err)
	}
	if e.r.Contexts[2].Caller != want || e.r.Scopes[result.(*ast.Ident)] != want {
		t.Fatal("nested $caller escaped to the procedural macro definition file")
	}
}

func TestProcBridgeOpaqueFragmentCopiesKeepPerNodeLocations(t *testing.T) {
	e, config := procBridgeFixture(t, "value + 42")
	fragment, err := e.parseFragment(exprKind, config.Input)
	if err != nil {
		t.Fatal(err)
	}
	input := syntaxToken{
		Fragment: fragment, Origin: config.Call,
		Ctx: config.Caller.Ctx, Scope: config.Caller, Lexical: config.Caller,
	}
	config.Input = []syntaxToken{input}
	invocation := procBridgeInvocation(t, e, config)
	output := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output {
		node := request.Input().Tokens()[0]
		if node.Kind() != public.FragmentKind || node.FragmentType() != public.ExprFragment || node.Reference() == 0 {
			t.Fatal("input lost opaque expression identity")
		}
		for _, view := range node.Children().Tokens() {
			if view.Reference() != 0 || view.Kind() == public.FragmentKind {
				t.Fatal("fragment view exposes trusted input identities")
			}
		}
		plus, err := public.NewPunct("+", request.Context().DefSite())
		if err != nil {
			t.Fatal(err)
		}
		return pm.Success(public.New(node, plus, node))
	})
	if output[0].Fragment != fragment || output[2].Fragment != fragment ||
		output[0].Location != (Origin{}) || output[2].Location != (Origin{}) {
		t.Fatal("identity fragment gained a whole-tree diagnostic override")
	}
	node, err := e.parse(output, exprKind, 1, config.Caller)
	if err != nil {
		t.Fatal(err)
	}
	binary := node.(*ast.BinaryExpr)
	if binary.X == binary.Y || binary.X == fragment.Node || binary.Y == fragment.Node {
		t.Fatal("duplicate fragment references share AST occurrences")
	}
	var original []ast.Node
	ast.Walk(fragment.Node, func(n ast.Node) bool { original = append(original, n); return true })
	for _, copy := range []ast.Node{binary.X, binary.Y} {
		var nodes []ast.Node
		ast.Walk(copy, func(n ast.Node) bool { nodes = append(nodes, n); return true })
		if len(nodes) != len(original) {
			t.Fatal("fragment copy changed tree shape")
		}
		for i, node := range nodes {
			if e.r.Origins[node] != e.r.Origins[original[i]] || e.location(node) != e.location(original[i]) {
				t.Fatalf("identity fragment lost per-node origin/location at index %d", i)
			}
		}
	}
	if e.r.Captures[binary.X].Kind != "expr" || e.r.Captures[binary.Y].Kind != "expr" {
		t.Fatal("copied fragments lost category proof")
	}

	redirected := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output {
		return pm.Success(public.New(request.Input().Tokens()[0].WithSpan(request.Context().DefSpan())))
	})
	copy, err := e.parse(redirected, exprKind, 1, config.Caller)
	if err != nil {
		t.Fatal(err)
	}
	ast.Walk(copy, func(n ast.Node) bool {
		if e.location(n) != config.Definition || e.r.Origins[n].File != config.Call.File {
			t.Fatal("WithSpan failed to redirect fragment diagnostics independently")
		}
		return true
	})
}

func TestProcBridgeFragmentViewsCannotRetainHiddenProofs(t *testing.T) {
	e, config := procBridgeFixture(t, "int")
	inner, err := e.parseFragment(exprKind, config.Input)
	if err != nil {
		t.Fatal(err)
	}
	opaque := syntaxToken{Fragment: inner, Origin: config.Call, Scope: config.Caller, Lexical: config.Caller}
	outer := &fragment{Kind: ttKind, Tokens: []syntaxToken{opaque}}
	config.Input = []syntaxToken{{Fragment: outer, Origin: config.Call, Scope: config.Caller, Lexical: config.Caller}}
	invocation := procBridgeInvocation(t, e, config)
	data, err := invocation.scope.Snapshot()
	if err != nil || len(data.Inputs) != 1 {
		t.Fatalf("fragment view issued hidden nested input references: %d, %v", len(data.Inputs), err)
	}
	identity := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output {
		return pm.Success(request.Input())
	})
	if _, err := e.parse(identity, typeKind, 1, config.Caller); err == nil {
		t.Fatal("opaque tt fragment entered a type slot")
	}
	view := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output {
		return pm.Success(request.Input().Tokens()[0].Children())
	})
	if len(view) != 1 || view[0].Fragment != nil || view[0].Origin != config.Definition {
		t.Fatal("observed fragment view recovered opaque origin or category")
	}
	if _, err := e.parse(view, typeKind, 1, config.Caller); err != nil {
		t.Fatalf("reconstructed identifier should be parsed as new type syntax: %v", err)
	}
}

func TestProcBridgeFragmentCategoryRejections(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		input  fragmentKind
		slot   fragmentKind
	}{
		{"expression-as-type", "int", exprKind, typeKind},
		{"tt-as-type", "int", ttKind, typeKind},
		{"item-as-expression", "type T int", itemKind, exprKind},
	} {
		t.Run(test.name, func(t *testing.T) {
			e, config := procBridgeFixture(t, test.source)
			capture, err := e.parseFragment(test.input, config.Input)
			if err != nil {
				t.Fatal(err)
			}
			config.Input = []syntaxToken{{Fragment: capture, Origin: config.Call, Scope: config.Caller, Lexical: config.Caller}}
			config.Slot = test.slot
			invocation := procBridgeInvocation(t, e, config)
			output := procBridgeOutput(t, invocation, func(request pm.Request) pm.Output { return pm.Success(request.Input()) })
			if _, err := e.parse(output, test.slot, 1, config.Caller); err == nil {
				t.Fatal("structured response bypassed a fragment category rejection")
			}
		})
	}
}

func TestProcBridgeDiagnosticsStayOrderedAndFailClosed(t *testing.T) {
	e, config := procBridgeFixture(t, "value")
	invocation := procBridgeInvocation(t, e, config)
	response := procBridgeResponse(t, invocation, func(request pm.Request) pm.Output {
		local, err := public.LocalSpan(900, 1000)
		if err != nil {
			t.Fatal(err)
		}
		return pm.Output{
			Tokens: request.Input(),
			Diagnostics: []pm.Diagnostic{
				{Severity: pm.Warning, Message: "first", Span: request.Input().Tokens()[0].Span()},
				{Severity: pm.Error, Message: "second", Span: request.Context().DefSpan()},
				{Severity: pm.Note, Message: "third", Span: local},
			},
		}
	})
	tokens, diagnostics, err := invocation.readResponse(bytes.NewReader(response))
	var failed *pm.DiagnosticError
	if !errors.As(err, &failed) || tokens != nil || len(diagnostics) != 3 {
		t.Fatalf("error response exposed tokens or lost diagnostics: %v / %v / %v", tokens, diagnostics, err)
	}
	want := []procDiagnostic{
		{pm.Warning, "first", tokenLocation(config.Input[0])},
		{pm.Error, "second", config.Definition},
		{pm.Note, "third", config.Call},
	}
	if !reflect.DeepEqual(diagnostics, want) {
		t.Fatalf("diagnostics = %#v, want %#v", diagnostics, want)
	}
}

func TestProcBridgeSharesExpansionOutputBudget(t *testing.T) {
	e, config := procBridgeFixture(t, "value")
	e.tokens = MaxOutputTokens - 1
	first := procBridgeInvocation(t, e, config)
	procBridgeOutput(t, first, func(request pm.Request) pm.Output { return pm.Success(request.Input()) })
	if e.tokens != MaxOutputTokens {
		t.Fatal("successful output was not charged to the engine")
	}
	config.RequestID = "budget-second"
	second := procBridgeInvocation(t, e, config)
	response := procBridgeResponse(t, second, func(request pm.Request) pm.Output { return pm.Success(request.Input()) })
	if tokens, _, err := second.readResponse(bytes.NewReader(response)); err == nil || tokens != nil ||
		!strings.Contains(err.Error(), "macro output budget") {
		t.Fatalf("second invocation bypassed the shared output budget: %v / %v", tokens, err)
	}

	e, config = procBridgeFixture(t, "(value)")
	e.tokens = MaxOutputTokens - 1
	group := procBridgeInvocation(t, e, config)
	response = procBridgeResponse(t, group, func(request pm.Request) pm.Output { return pm.Success(request.Input()) })
	if tokens, _, err := group.readResponse(bytes.NewReader(response)); err == nil || tokens != nil {
		t.Fatal("nested group tokens were not charged")
	}

	e, config = procBridgeFixture(t, "left + right")
	fragment, err := e.parseFragment(exprKind, config.Input)
	if err != nil {
		t.Fatal(err)
	}
	config.Input = []syntaxToken{{Fragment: fragment, Origin: config.Call, Scope: config.Caller, Lexical: config.Caller}}
	e.tokens = MaxOutputTokens - 1
	opaque := procBridgeInvocation(t, e, config)
	procBridgeOutput(t, opaque, func(request pm.Request) pm.Output { return pm.Success(request.Input()) })
	if e.tokens != MaxOutputTokens {
		t.Fatal("opaque view was charged as emitted tokens")
	}
}

func TestProcBridgeRejectsStaleUnknownAndLateReferences(t *testing.T) {
	e, config := procBridgeFixture(t, "value")
	first := procBridgeInvocation(t, e, config)
	response := procBridgeResponse(t, first, func(request pm.Request) pm.Output { return pm.Success(request.Input()) })
	config.RequestID = "bridge-2"
	second := procBridgeInvocation(t, e, config)
	if tokens, _, err := second.readResponse(bytes.NewReader(response)); err == nil || tokens != nil {
		t.Fatal("response from another invocation was admitted")
	}

	lateContext := first.scope.NewContext()
	lateSpan, err := first.scope.NewSpan(400, 410)
	if err != nil {
		t.Fatal(err)
	}
	node, err := public.NewIdent("late", false, lateContext)
	if err != nil {
		t.Fatal(err)
	}
	lateInput, err := first.scope.Capture(public.New(node), lateContext)
	if err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]public.WireToken{
		"unknown input": {Ref: 99999},
		"late input":    {Ref: lateInput.Tokens()[0].Reference()},
		"late context":  {Kind: "ident", Text: "late", Context: lateContext.Reference()},
		"late span":     {Ref: 1, Span: &public.WireSpan{Ref: lateSpan.Reference(), Start: 400, End: 410}},
		"forged kind":   {Ref: 1, Kind: "ident"},
	} {
		t.Run(name, func(t *testing.T) {
			wire := struct {
				ID     string             `json:"id"`
				Tokens []public.WireToken `json:"tokens"`
			}{ID: first.request.ID(), Tokens: []public.WireToken{token}}
			data, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if tokens, _, err := first.readResponse(bytes.NewReader(data)); err == nil || tokens != nil {
				t.Fatal("invalid response acquired compiler identity")
			}
		})
	}
	if tokens, _, err := first.readResponse(strings.NewReader(`{"id":"bridge-1","tokens":[{"ref":1,"origin":true}]}`)); err == nil || tokens != nil {
		t.Fatal("unrecognized semantic proof was admitted")
	}
}
