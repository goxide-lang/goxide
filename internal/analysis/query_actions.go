package analysis

import (
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type CodeAction struct {
	Title        string
	Kind         string
	Edits        []SourceEdit
	placeholders []actionPlaceholder
}

type actionPlaceholder struct {
	file, name string
	insertion  uint32
	offset     int
}

// CodeActions reads the checked snapshot. It does not repair syntax or infer a
// missing impl, and it never edits an expansion's mapped call/definition span.
func (q *Queries) CodeActions(file string, selection syntax.Span) ([]CodeAction, error) {
	actions := []CodeAction{}
	if q.result == nil || q.result.Checked == nil || q.result.Resolved == nil {
		return actions, nil
	}
	source := q.result.Sources[file]
	if source == nil || source.Syntax == nil || int(selection.End) > len(source.Content) {
		return actions, nil
	}
	for declaration, implementation := range q.result.Checked.Impls {
		if implementation.File != file || implementation.Trait == nil || !actionIntersects(declaration.Span(), selection) || q.location(file, declaration).Generated {
			continue
		}
		actions = append(actions, q.implActions(file, implementation)...)
	}
	for expression, match := range q.result.Checked.Matches {
		location := q.location(file, expression)
		// Node IDs and spans are file-local; confirm actual ownership as well.
		if q.nodes[resolve.Ref{File: file, ID: expression.ID()}] != expression || location.Generated || !actionIntersects(expression.Span(), selection) || len(match.Missing) == 0 {
			continue
		}
		if action := q.matchAction(file, expression, match); action != nil {
			actions = append(actions, *action)
		}
	}
	actions = append(actions, q.importActions(file, selection)...)
	sort.SliceStable(actions, func(i, j int) bool { return actions[i].Title < actions[j].Title })
	return actions, nil
}

func actionIntersects(node, selection syntax.Span) bool {
	if selection.Start == selection.End {
		return node.Start <= selection.Start && selection.Start < node.End
	}
	return node.Start < selection.End && selection.Start < node.End
}

func (q *Queries) implActions(file string, implementation *check.Impl) []CodeAction {
	existing := map[string]bool{}
	for _, item := range implementation.Decl.Items {
		switch item := item.(type) {
		case *syntax.FuncDecl:
			if item.Name != nil {
				existing[item.Name.Name] = true
			}
		case *syntax.TypeDecl:
			if item.Name != nil {
				existing["type "+item.Name.Name] = true
			}
		}
	}
	var missing []*check.Member
	for _, method := range implementation.Trait.Methods {
		if method.Decl != nil && method.Decl.Body == nil && !existing[method.Name] {
			missing = append(missing, method)
		}
	}
	var associated []string
	var placeholders []actionPlaceholder
	used := map[string]bool{}
	for name := range newActionRenderer(q, file, implementation.Decl.Span().Start).bindings() {
		used[name] = true
	}
	for _, item := range implementation.Trait.Associated {
		if !existing["type "+item.Name] {
			base := "__GoxideTODO_" + item.Name
			name := base
			for suffix := 2; used[name]; suffix++ {
				name = base + "_" + strconv.Itoa(suffix)
			}
			used[name] = true
			associated = append(associated, "type "+item.Name+" = "+name+" // TODO: choose the associated type")
			placeholders = append(placeholders, actionPlaceholder{file: file, name: name})
		}
	}
	if len(missing) == 0 && len(associated) == 0 {
		return nil
	}
	makeAction := func(methods []*check.Member, title string) *CodeAction {
		renderer := newActionRenderer(q, file, implementation.Decl.Span().Start)
		replacements := map[types.Type]string{}
		for i, parameter := range implementation.Trait.TypeParams {
			if i < len(implementation.InputNodes) {
				replacements[parameter] = printer.Source(implementation.InputNodes[i])
			}
		}
		parts := append([]string(nil), associated...)
		for _, method := range methods {
			signature, ok := renderer.fragment(method.File, method.Decl, replacements)
			if !ok {
				return nil
			}
			signature = strings.TrimSuffix(strings.TrimSpace(signature), ";")
			parts = append(parts, signature+" {\n\tpanic("+strconv.Quote("TODO: implement "+method.Name)+")\n}")
		}
		edit, ok := q.insertMembers(file, implementation.Decl, strings.Join(parts, "\n\n"))
		if !ok {
			return nil
		}
		planned := append([]actionPlaceholder(nil), placeholders...)
		for i := range planned {
			planned[i].insertion = edit.Span.Start
			assignment := strings.Index(edit.NewText, "= "+planned[i].name+" // TODO:")
			if assignment < 0 {
				return nil
			}
			planned[i].offset = assignment + len("= ")
		}
		return &CodeAction{Title: title, Kind: "quickfix", Edits: append(renderer.importEdits(), edit), placeholders: planned}
	}
	actions := []CodeAction{}
	for _, method := range missing {
		if action := makeAction([]*check.Member{method}, "Implement "+method.Name); action != nil {
			actions = append(actions, *action)
		}
	}
	if len(missing) > 1 || len(associated) > 0 {
		if action := makeAction(missing, "Implement all missing members"); action != nil {
			actions = append(actions, *action)
		}
	}
	return actions
}

func (q *Queries) insertMembers(file string, node syntax.Node, text string) (SourceEdit, bool) {
	content := q.result.Sources[file].Content
	end := int(node.Span().End)
	if end <= 0 || end > len(content) || content[end-1] != '}' {
		return SourceEdit{}, false
	}
	closing := end - 1
	line := strings.LastIndexByte(string(content[:closing]), '\n') + 1
	indent := string(content[line:closing])
	if strings.TrimSpace(indent) != "" {
		indent = ""
	}
	newline := "\n"
	if strings.Contains(string(content), "\r\n") {
		newline = "\r\n"
	}
	start := closing
	if indent != "" {
		start = line
	}
	prefix := ""
	if start > 0 && content[start-1] != '\n' {
		prefix += "\n"
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	body := indent + "\t" + strings.ReplaceAll(text, "\n", "\n"+indent+"\t") + "\n" + indent
	return SourceEdit{File: file, Span: syntax.Span{Start: uint32(start), End: uint32(closing)}, NewText: strings.ReplaceAll(prefix+body, "\n", newline)}, true
}

func (q *Queries) matchAction(file string, expression *syntax.MatchExpr, match *check.Match) *CodeAction {
	if match.Enum == nil || match.Enum.Enum == nil {
		return nil
	}
	renderer := newActionRenderer(q, file, expression.Span().Start)
	symbol := renderer.namedSymbol(match.Enum.Enum.Type)
	name, ok := renderer.spelling(symbol)
	if !ok && match.Enum.Enum.Builtin {
		binding := renderer.bindings()[match.Enum.Enum.Name]
		if binding != nil && binding.Kind == resolve.Type && binding.Decl.File == "" && len(binding.Target) == 0 {
			name, ok = match.Enum.Enum.Name, true
		}
	}
	if !ok {
		return nil
	}
	var resultType string
	if !match.Statement {
		ok := false
		if annotation := q.matchReturnType(file, expression); annotation != nil {
			resultType, ok = renderer.fragment(file, annotation, nil)
		}
		if !ok {
			resultType, ok = renderer.goType(match.Type)
		}
		if !ok {
			return nil
		}
	}
	var arms []string
	for _, variant := range match.Missing {
		pattern := name + "::" + variant.Name
		kind := syntax.Unit
		if variant.Decl != nil {
			kind = variant.Decl.Kind
		} else if len(variant.Fields) > 0 {
			kind = syntax.Tuple
		}
		switch kind {
		case syntax.Tuple:
			fields := make([]string, len(variant.Fields))
			for i := range fields {
				fields[i] = "_"
			}
			pattern += "(" + strings.Join(fields, ", ") + ")"
		case syntax.Struct:
			var fields []string
			for _, field := range variant.Fields {
				fields = append(fields, field.Name+": _")
			}
			pattern += " { " + strings.Join(fields, ", ") + " }"
		}
		body := "{ panic(" + strconv.Quote("TODO: handle "+variant.Name) + ") }"
		if !match.Statement {
			// A panic-only closure is an explicit implementation placeholder and keeps
			// an expression arm well typed without inventing a result value.
			body = "func() " + resultType + " { panic(" + strconv.Quote("TODO: handle "+variant.Name) + ") }()"
		}
		arms = append(arms, pattern+" => "+body+",")
	}
	edit, ok := q.insertMembers(file, expression, strings.Join(arms, "\n"))
	if !ok {
		return nil
	}
	edits := renderer.importEdits()
	if len(expression.Arms) > 0 {
		at := expression.Arms[len(expression.Arms)-1].Span().End
		comma := false
		for _, item := range q.result.Sources[file].Syntax.Tokens {
			if item.Start >= at && item.Start < expression.Span().End-1 && item.Kind == token.COMMA {
				comma = true
				break
			}
		}
		if !comma {
			if at == edit.Span.Start {
				edit.NewText = "," + edit.NewText
			} else {
				edits = append(edits, SourceEdit{File: file, Span: syntax.Span{Start: at, End: at}, NewText: ","})
			}
		}
	}
	return &CodeAction{Title: "Add missing match arms", Kind: "quickfix", Edits: append(edits, edit)}
}

// A direct return supplies an authored result annotation even when every arm
// is still missing. It also preserves source &T permissions erased by Go types.
func (q *Queries) matchReturnType(file string, expression *syntax.MatchExpr) syntax.Type {
	var result syntax.Type
	returned := false
	span := expression.Span()
	syntax.Walk(q.result.Resolved.Files[file].AST, func(node syntax.Node) bool {
		location := node.Span()
		if location.Start > span.Start || location.End < span.End {
			return false
		}
		switch node := node.(type) {
		case *syntax.FuncDecl:
			result = nil
			if len(node.Results) == 1 {
				result = node.Results[0].Type
			}
		case *syntax.FuncLit:
			result = node.Result
			if result == nil && len(node.Results) == 1 {
				result = node.Results[0].Type
			}
		case *syntax.ReturnStmt:
			returned = len(node.Results) == 1 && node.Results[0] == expression
		}
		return true
	})
	if !returned || result == nil || q.result.Checked.SourceType(result) == nil {
		return nil
	}
	return result
}

// fragment preserves source reference permissions, receiver syntax, generic
// constraints and associated projections. Only checked path identities change.
func (r *actionRenderer) fragment(file string, node syntax.Node, sub map[types.Type]string) (string, bool) {
	source := r.q.result.Sources[file]
	span := node.Span()
	if source == nil || int(span.End) > len(source.Content) || span.End <= span.Start || r.q.location(file, node).Generated {
		return "", false
	}
	var edits []SourceEdit
	valid := true
	syntax.Walk(node, func(child syntax.Node) bool {
		if !valid {
			return false
		}
		typ, ok := child.(*syntax.PathType)
		if !ok {
			return true
		}
		if replacement := sub[r.q.result.Checked.SourceType(typ)]; replacement != "" {
			edits = append(edits, SourceEdit{Span: typ.Span(), NewText: replacement})
			return false
		}
		path := typ.Path
		if path == nil || len(path.Segments) == 0 {
			valid = false
			return false
		}
		reference := r.q.result.Resolved.Files[file].References[path.ID()]
		symbol := reference.Symbol
		if symbol == nil {
			symbol = r.q.result.Resolved.Files[file].References[path.Segments[0].Name.ID()].Symbol
		}
		if symbol == nil {
			valid = false
			return false
		}
		if symbol.Kind == resolve.Builtin || symbol.Kind == resolve.TypeParam || symbol.Kind == resolve.ConstParam || symbol.Decl.File == "" && len(symbol.Target) == 0 {
			return true
		}
		spelling, ok := r.spelling(symbol)
		if !ok {
			valid = false
			return false
		}
		// Deferred paths retain the checked root's projection suffix.
		last := len(path.Segments) - 1
		if reference.Deferred {
			last = 0
		}
		edits = append(edits, SourceEdit{Span: syntax.Span{Start: path.Segments[0].Name.Span().Start, End: path.Segments[last].Name.Span().End}, NewText: spelling})
		return true
	})
	if !valid {
		return "", false
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].Span.Start > edits[j].Span.Start })
	text := string(source.Content[span.Start:span.End])
	last := span.End
	for _, edit := range edits {
		if edit.Span.Start < span.Start || edit.Span.End > last {
			return "", false
		}
		a, b := edit.Span.Start-span.Start, edit.Span.End-span.Start
		text = text[:a] + edit.NewText + text[b:]
		last = edit.Span.Start
	}
	return text, true
}

// goType renders only source representations certified by the checked type.
// Erased pointers may represent either *T or &T, so those require an authored
// annotation rather than a guessed edit.
func (r *actionRenderer) goType(typ types.Type) (string, bool) {
	switch typ := types.Unalias(typ).(type) {
	case *types.Basic:
		return typ.Name(), typ.Kind() != types.Invalid && typ.Info()&types.IsUntyped == 0
	case *types.TypeParam:
		for name, symbol := range r.bindings() {
			if r.q.symbolType(symbol) == typ {
				return name, true
			}
		}
	case *types.Named:
		symbol := r.namedSymbol(typ.Origin())
		if symbol == nil {
			return "", false
		}
		name, ok := r.spelling(symbol)
		if !ok {
			return "", false
		}
		if typ.TypeArgs().Len() > 0 {
			var arguments []string
			for i := 0; i < typ.TypeArgs().Len(); i++ {
				argument, ok := r.goType(typ.TypeArgs().At(i))
				if !ok {
					return "", false
				}
				arguments = append(arguments, argument)
			}
			name += "[" + strings.Join(arguments, ", ") + "]"
		}
		return name, true
	case *types.Slice:
		element, ok := r.goType(typ.Elem())
		return "[]" + element, ok
	case *types.Array:
		element, ok := r.goType(typ.Elem())
		return "[" + strconv.FormatInt(typ.Len(), 10) + "]" + element, ok
	case *types.Map:
		key, ok := r.goType(typ.Key())
		if !ok {
			return "", false
		}
		value, ok := r.goType(typ.Elem())
		return "map[" + key + "]" + value, ok
	case *types.Chan:
		element, ok := r.goType(typ.Elem())
		prefix := "chan "
		if typ.Dir() == types.SendOnly {
			prefix = "chan<- "
		} else if typ.Dir() == types.RecvOnly {
			prefix = "<-chan "
		}
		return prefix + element, ok
	}
	return "", false
}

func (r *actionRenderer) namedSymbol(named *types.Named) *resolve.Symbol {
	var candidates []*resolve.Symbol
	for symbol, known := range r.q.result.Checked.Symbols {
		if symbol.Kind != resolve.Type && symbol.Kind != resolve.Enum {
			continue
		}
		other, ok := types.Unalias(known).(*types.Named)
		if ok && other.Origin() == named.Origin() {
			candidates = append(candidates, symbol)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Decl.File != b.Decl.File {
			return a.Decl.File < b.Decl.File
		}
		return a.Decl.ID < b.Decl.ID
	})
	if len(candidates) > 0 {
		return candidates[0]
	}
	object := named.Obj()
	if object.Pkg() == nil {
		return nil
	}
	// Native Go types have no authored Goxide declaration to rename.
	for _, pkg := range r.q.result.Resolved.Packages {
		canonical := r.q.result.Checked.PackagePaths[pkg.Dir]
		if canonical == "" {
			canonical = pkg.Dir
		}
		if canonical == object.Pkg().Path() {
			return nil
		}
	}
	return &resolve.Symbol{Name: resolve.Key{Name: object.Name()}, Kind: resolve.Type, Visibility: resolve.Public, Target: strings.Split(object.Pkg().Path()+"/"+object.Name(), "/")}
}
