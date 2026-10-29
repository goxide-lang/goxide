package resolve

import "github.com/goxide-lang/goxide/internal/syntax/ast"

// DeclarationName exposes the resolver's written-form and inherited visibility
// rules to source edits without resolving a second copy of the program.
func DeclarationName(name string, written ast.Visibility, goForm bool, inherited *Visibility) (string, Visibility, bool) {
	visibility := declarationVisibility(&ast.Ident{Name: name}, written, goForm, inherited)
	goName, ok := generatedName(name, visibility)
	return goName, visibility, ok
}
