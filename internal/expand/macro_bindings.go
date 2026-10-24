package expand

import "github.com/goxide-lang/goxide/internal/syntax/ast"

// A retained generated use can supply a macro needed by the next item. Keep
// its binding visible without repeating expansion or eagerly resolving every
// item. The resolver receives a snapshot only before the next external lookup.
type macroBindings struct {
	files        []*ast.File
	replacements map[ast.Item][]ast.Item
	generated    map[*ast.File][]ast.Item
	seen         map[ast.Item]bool
	dirty        bool
}

func newMacroBindings(packages [][]*ast.File) *macroBindings {
	b := &macroBindings{replacements: map[ast.Item][]ast.Item{}, generated: map[*ast.File][]ast.Item{}, seen: map[ast.Item]bool{}}
	for _, files := range packages {
		b.files = append(b.files, files...)
	}
	return b
}

func staticMacroBinding(item ast.Item) bool {
	switch item := item.(type) {
	case *ast.UseDecl, *ast.TypeDecl, *ast.EnumDecl, *ast.TraitDecl, *ast.ModDecl, *ast.MacroRulesDecl:
		return true
	case *ast.DeclareDecl:
		return staticMacroBinding(item.Decl)
	}
	return false
}

func (b *macroBindings) retain(file *ast.File, items []ast.Item) {
	for _, item := range items {
		if nodeExpn(item) == 0 || !staticMacroBinding(item) || b.seen[item] {
			continue
		}
		b.seen[item] = true
		b.generated[file] = append(b.generated[file], item)
		b.dirty = true
	}
}

func (b *macroBindings) replace(original ast.Item, output []ast.Item) {
	b.replacements[original] = output
	if staticMacroBinding(original) && (len(output) != 1 || nodeExpn(output[0]) != 0) {
		b.dirty = true
	}
}

func (e *engine) updateMacroBindings() error {
	b := e.bindings
	if b == nil || !b.dirty {
		return nil
	}
	b.dirty = false
	snapshot := *e.r
	snapshot.Files = nil
	for _, original := range b.files {
		tree := *original
		tree.Items = nil
		seen := map[ast.Item]bool{}
		appendItems := func(items []ast.Item) {
			for _, item := range items {
				if !seen[item] {
					seen[item] = true
					tree.Items = append(tree.Items, item)
				}
			}
		}
		for _, item := range original.Items {
			if output, ok := b.replacements[item]; ok {
				appendItems(output)
			} else {
				appendItems([]ast.Item{item})
			}
		}
		appendItems(b.generated[original])
		e.r.Origins[&tree] = Origin{original, original.Span()}
		// Root routing rewrites paths and node IDs. Its temporary view must not
		// mutate either source definitions or the in-progress expansion.
		snapshot.Files = append(snapshot.Files, e.copyNode(&tree, 0).(*ast.File))
	}
	return e.resolver.(interface{ UpdateMacroBindings(*Result) error }).UpdateMacroBindings(&snapshot)
}
