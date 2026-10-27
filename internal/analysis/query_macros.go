package analysis

import syntax "github.com/goxide-lang/goxide/internal/syntax/ast"

func (q *Queries) indexMacroCalls() {
	if q.result.Expanded == nil {
		return
	}
	for _, frame := range q.result.Expanded.Expansions {
		var path *syntax.Path
		switch original := frame.Original.(type) {
		case *syntax.MacroExpr:
			path = original.Path
		case *syntax.MacroType:
			path = original.Path
		case *syntax.MacroItem:
			path = original.Path
		}
		if path == nil || len(path.Segments) == 0 {
			continue
		}
		name := path.Segments[len(path.Segments)-1].Name
		if name == nil || name.Missing {
			continue
		}
		callName, definitionName := q.names[frame.Call.File], q.names[frame.Definition.File]
		if callName == "" || definitionName == "" {
			continue
		}
		definition := Location{File: definitionName, Span: frame.Definition.Span}
		if source := q.result.Sources[definitionName]; source != nil && source.Syntax != nil {
			for _, item := range source.Syntax.Items {
				if !spanContains(item.Span(), frame.Definition.Span) {
					continue
				}
				switch declaration := item.(type) {
				case *syntax.MacroRulesDecl:
					definition.Span = declaration.Name.Span()
				case *syntax.FuncDecl:
					definition.Span = declaration.Name.Span()
				}
			}
		}
		location := q.location(callName, name)
		q.entries[location.File] = append(q.entries[location.File], queryEntry{location: location, definition: &definition, text: "macro " + name.Name})
	}
}
