package expand

import (
	"bytes"
	"fmt"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// BridgeExpressionForTest exercises the protocol boundary before source
// registration and a restricted execution host are wired into Expand.
func BridgeExpressionForTest(files map[string]*ast.File, guest func([]byte) ([]byte, error)) (*Result, error) {
	callFile, definitionFile := files["main.gox"], files["definition.gox"]
	var original *ast.MacroExpr
	ast.Walk(callFile, func(n ast.Node) bool {
		if macro, ok := n.(*ast.MacroExpr); ok {
			original = macro
		}
		return true
	})
	if original == nil {
		return nil, fmt.Errorf("missing fixture invocation")
	}
	r, err := Expand([]*ast.File{callFile, definitionFile}, nil)
	if err != nil {
		return nil, err
	}
	e := &engine{r: r, trees: map[*ast.TokenTree][]syntaxToken{}}
	var slot *ast.ReturnStmt
	for _, item := range r.Files[0].Items {
		if function, ok := item.(*ast.FuncDecl); ok && function.Name.Name == "run" {
			slot = function.Body.List[len(function.Body.List)-1].(*ast.ReturnStmt)
		}
	}
	if slot == nil || len(slot.Results) != 1 {
		return nil, fmt.Errorf("missing fixture expression")
	}
	input := slot.Results[0]
	call := Origin{callFile, original.Span()}
	definition := Origin{definitionFile, definitionFile.Items[0].Span()}
	caller := Scope{callFile, 0}
	id := ast.ExpnID(len(r.Expansions) + 1)
	ctx := ast.SyntaxCtx(len(r.Contexts) + 1)
	r.Expansions[id] = Expansion{Definition: definition, Call: call, Original: original, Parent: 1, Stack: []ast.ExpnID{1, id}, Ctx: ctx}
	r.Contexts[ctx] = Context{Definition: Scope{definitionFile, 0}, Caller: caller, Expn: id}
	invocation, err := e.newProcInvocation(procInvocationConfig{
		RequestID: "bridge-e2e", Entry: "duplicate", Slot: exprKind,
		Definition: definition, Call: call, Ctx: ctx, Caller: caller,
		Target: pm.Target{OS: "linux", Arch: "amd64"},
		Input: []syntaxToken{{
			Fragment: &fragment{Kind: exprKind, Node: input, Tokens: e.treeInput(original.Args, callFile, 0)},
			Origin:   r.Origins[input], Scope: caller, Lexical: caller,
		}},
	})
	if err != nil {
		return nil, err
	}
	var request bytes.Buffer
	if err := pm.WriteRequest(&request, invocation.request); err != nil {
		return nil, err
	}
	response, err := guest(request.Bytes())
	if err != nil {
		return nil, err
	}
	tokens, diagnostics, err := invocation.readResponse(bytes.NewReader(response))
	if err != nil {
		return nil, err
	}
	if len(diagnostics) != 0 {
		return nil, fmt.Errorf("unexpected guest diagnostics: %v", diagnostics)
	}
	output, err := e.parse(tokens, exprKind, id, caller)
	if err != nil {
		return nil, err
	}
	slot.Results[0] = output.(ast.Expr)
	for _, file := range r.Files {
		var nodeID ast.NodeID
		ast.Walk(file, func(n ast.Node) bool {
			nodeID++
			n.(interface{ SetID(ast.NodeID) }).SetID(nodeID)
			return true
		})
	}
	return r, nil
}
