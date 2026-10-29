package expand

import (
	"fmt"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

type bindingTestResolver struct {
	*attributeTestResolver
	updates int
	ready   bool
}

func (r *bindingTestResolver) UpdateMacroBindings(result *Result) error {
	r.updates++
	for _, file := range result.Files {
		for _, item := range file.Items {
			if use, ok := item.(*ast.UseDecl); ok && use.Tree.Path.Segments[0].Name.Name == "dep" {
				r.ready = true
				// Routing the temporary snapshot must not change emitted code.
				use.Tree.Path.Segments[0].Name.Name = "routed"
			}
		}
	}
	return nil
}

func (r *bindingTestResolver) LookupMacro(path *ast.Path, scope Scope) (Macro, error) {
	if macroPathName(path) == "selected::answer" {
		if !r.ready {
			return Macro{}, fmt.Errorf("generated use is unavailable")
		}
		return r.macros["answer"], nil
	}
	return r.attributeTestResolver.LookupMacro(path, scope)
}

func TestGeneratedUsesReachNestedMacroLookupOnce(t *testing.T) {
	emitted, answered := 0, 0
	resolver := &bindingTestResolver{attributeTestResolver: newAttributeResolver(t, []pm.Entry{
		{Name: "emit", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
			emitted++
			return procText(ctx, "use dep as selected; func First() int { return selected::answer!() }; func Second() int { return selected::answer!() }")
		}},
		{Name: "answer", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
			answered++
			return procText(ctx, "42")
		}},
	}, nil)}
	file := expandFile(t, "generated.gox", "emit!()")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if emitted != 1 || answered != 2 || resolver.updates != 1 {
		t.Fatalf("expansion repeated or refreshed without new bindings: emit=%d answer=%d updates=%d", emitted, answered, resolver.updates)
	}
	use := result.Files[0].Items[0].(*ast.UseDecl)
	if use.Tree.Path.Segments[0].Name.Name != "dep" {
		t.Fatalf("temporary routing changed the expanded tree: %s", printer.Source(result.Files[0]))
	}
}
