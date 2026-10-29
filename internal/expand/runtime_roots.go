package expand

import (
	"fmt"
	"sort"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func sortedRuntimeRootNames[T any](roots map[string]T) []string {
	names := make([]string, 0, len(roots))
	for name := range roots {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (e *engine) procRuntimeRoots(output *ast.File, location Origin, expn ast.ExpnID) (map[string][]syntaxToken, error) {
	provider, ok := e.resolver.(interface {
		LookupRuntimeRoots(*ast.File, Origin, ast.ExpnID) (map[string][]string, error)
	})
	if !ok {
		return nil, nil
	}
	roots, err := provider.LookupRuntimeRoots(output, location, expn)
	if err != nil {
		return nil, err
	}
	streams := make(map[string][]syntaxToken, len(roots))
	for _, name := range sortedRuntimeRootNames(roots) {
		if e.nextCtx == ^ast.SyntaxCtx(0) {
			return nil, fmt.Errorf("syntax context space exhausted")
		}
		e.nextCtx++
		ctx := e.nextCtx
		scope := Scope{File: output, Ctx: ctx}
		e.r.Contexts[ctx] = Context{Definition: Scope{File: output}, Caller: Scope{File: output}}
		if e.r.RuntimeRoots == nil {
			e.r.RuntimeRoots = map[ast.SyntaxCtx]RuntimeRoot{}
		}
		root := RuntimeRoot{Name: "runtime_root", Target: append([]string(nil), roots[name]...)}
		e.r.RuntimeRoots[ctx] = root
		streams[name] = []syntaxToken{{
			Token:  &token.Token{Kind: token.IDENT, Text: root.Name, Start: location.Span.Start, End: location.Span.End},
			Origin: Origin{File: output, Span: location.Span}, Location: location, Ctx: ctx, Scope: scope, Lexical: scope,
		}}
	}
	return streams, nil
}
