package expand

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

type itemSlot uint8

const (
	fileItems itemSlot = iota
	implItems
	traitItems
	singleItem
	traitImplItems
)

func itemAttributes(item ast.Item) *[]*ast.Attr {
	return reflect.ValueOf(item).Elem().FieldByName("Attrs").Addr().Interface().(*[]*ast.Attr)
}

func attributeName(attr *ast.Attr) string {
	if attr.Path != nil && len(attr.Path.Segments) == 1 {
		return attr.Path.Segments[0].Name.Name
	}
	return ""
}

// rewriteItems is the declaration-list counterpart of statement-list splicing.
// The caller retains the containing declaration's extra constraints (for
// example, inherited visibility in a trait impl).
func (e *engine) rewriteItems(items []ast.Item, file *ast.File, parent ast.ExpnID, slot itemSlot) ([]ast.Item, error) {
	var result []ast.Item
	for _, item := range items {
		keep := true
		var err error
		if slot == fileItems {
			keep, err = e.selection(file).item(item)
		} else {
			err = e.selection(file).children(item)
		}
		if err != nil {
			return nil, err
		}
		if !keep {
			continue
		}
		output, err := e.rewriteItem(item, file, parent, slot)
		if err != nil {
			return nil, err
		}
		result = append(result, output...)
	}
	if slot == singleItem && len(result) != 1 {
		return nil, e.attributeError(firstItem(items), file, parent, "this declaration slot requires exactly one item")
	}
	for _, item := range result {
		if err := e.validateItemSlot(item, file, parent, slot); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// prepareItems admits raw procedural output before recursively expanding it.
// Source declarations use rewriteItems directly, so their bindings are never
// introduced a second time merely because they have attributes.
func (e *engine) prepareItems(items []ast.Item, file *ast.File, expn ast.ExpnID, slot itemSlot) ([]ast.Item, error) {
	var selected []ast.Item
	for _, item := range items {
		keep := true
		var err error
		if slot == fileItems {
			keep, err = e.selection(file).item(item)
		} else {
			err = e.selection(file).children(item)
		}
		if err != nil {
			return nil, err
		}
		if !keep {
			continue
		}
		if err := e.rejectGeneratedRegistration(item, file, expn); err != nil {
			return nil, err
		}
		if !e.captured[item] {
			destination := e.r.Contexts[e.r.Expansions[expn].Ctx].Caller
			if destination.File == nil {
				destination.File = file
			}
			for context, ok := e.r.Contexts[destination.Ctx]; ok; context, ok = e.r.Contexts[destination.Ctx] {
				destination = context.Caller
			}
			e.introduce(item, destination)
		}
		selected = append(selected, item)
	}
	return e.rewriteItems(selected, file, expn, slot)
}

// rewriteItem transforms the whole declaration before visiting its body. Each
// replacement is classified anew; every derive later observes the same input.
func (e *engine) rewriteItem(item ast.Item, file *ast.File, parent ast.ExpnID, slot itemSlot) ([]ast.Item, error) {
	requests, requestsErr := e.itemDerives(item, file)
	helpers := deriveHelpers(requests)
	if err := e.checkHelperAmbiguity(item, helpers, file, parent); err != nil {
		return nil, err
	}
	attrs := itemAttributes(item)
	for index, attr := range *attrs {
		if attr.Inner || attributeName(attr) == "derive" || helpers[attributeName(attr)] != nil {
			continue
		}
		definition, err := e.lookup(attr.Path, file)
		if err != nil || definition.proc == nil || definition.proc.Kind != pm.Attribute {
			continue
		}
		*attrs = append(append([]*ast.Attr(nil), (*attrs)[:index]...), (*attrs)[index+1:]...)
		input, err := e.itemInput(item, file)
		if err != nil {
			return nil, e.attributeError(attr, file, parent, err.Error())
		}
		var args []syntaxToken
		if attr.Args != nil {
			args = e.treeInput(attr.Args, file, attr.Path.Segments[0].Name.Ctx)
		}
		output, expn, err := e.invokeProcItems(attr, definition, args, input, file, parent, slot)
		if err != nil {
			return nil, err
		}
		entry := e.r.Expansions[expn]
		entry.Input = e.r.Origins[item]
		if entry.Input.File == nil {
			entry.Input = Origin{file, item.Span()}
		}
		e.r.Expansions[expn] = entry
		return e.prepareItems(output, file, expn, slot)
	}
	// An attribute may delete a declaration with an unresolved derive, so this
	// check deliberately occurs only after all executable attributes are gone.
	if requestsErr != nil {
		return nil, e.attributeError(item, file, parent, requestsErr.Error())
	}
	for _, request := range requests {
		if request.err != nil {
			return nil, e.attributeError(request.attr, file, parent, request.err.Error())
		}
	}
	if len(requests) > 0 {
		switch declaration := item.(type) {
		case *ast.TypeDecl:
			if declaration.Grouped || declaration.Name == nil {
				return nil, e.attributeError(item, file, parent, "derive requires a single named type declaration")
			}
		case *ast.EnumDecl:
			if declaration.Name == nil {
				return nil, e.attributeError(item, file, parent, "derive requires a single named type declaration")
			}
		default:
			return nil, e.attributeError(item, file, parent, "derive requires a single named type declaration")
		}
	}
	if declaration, ok := item.(*ast.TypeDecl); ok && declaration.Grouped {
		for _, spec := range declaration.Specs {
			if len(spec.Attrs) != 0 {
				return e.rewriteTypeSpecs(declaration, file, parent, slot)
			}
		}
	}
	var input []syntaxToken
	if len(requests) != 0 {
		snapshot := e.copyNode(item, 0).(ast.Item)
		removeDerives(snapshot)
		var err error
		input, err = e.itemInput(snapshot, file)
		if err != nil {
			return nil, e.attributeError(item, file, parent, err.Error())
		}
		removeDerives(item)
	}
	occurrences := helperOccurrences(item, helpers)
	// Begin at fields rather than the item pointer: rewrite's item dispatch must
	// not restart this item's attribute state machine.
	value, err := e.rewrite(reflect.ValueOf(item).Elem(), file, parent)
	if err != nil {
		return nil, err
	}
	reflect.ValueOf(item).Elem().Set(value)
	output := []ast.Item{item}
	if macro, ok := item.(*ast.MacroItem); ok {
		if len(*attrs) != 0 {
			return nil, e.attributeError((*attrs)[0], file, parent, "unsupported attribute or invalid attribute position on macro item")
		}
		definition, lookupErr := e.lookup(macro.Path, file)
		if lookupErr == nil && definition.proc != nil {
			if definition.proc.Kind != pm.Function {
				return nil, e.attributeError(macro, file, parent, "macro invocation requires a function-like procedural macro")
			}
			args := e.treeInput(macro.Args, file, macro.Path.Segments[0].Name.Ctx)
			items, expn, err := e.invokeProcItems(macro, definition, args, nil, file, parent, slot)
			if err != nil {
				return nil, err
			}
			return e.prepareItems(items, file, expn, slot)
		}
		result, err := e.invoke(macro, macro.Path, macro.Args, itemKind, file, parent, slotParseKind(slot))
		if err != nil {
			return nil, err
		}
		output = nil
		if result != nil {
			return e.rewriteItems([]ast.Item{result.(ast.Item)}, file, nodeExpn(result), slot)
		}
	}
	for _, request := range requests {
		items, expn, err := e.invokeProcItems(request.attr, request.definition, nil, input, file, parent, slot)
		if err != nil {
			return nil, err
		}
		if len(items) != 0 {
			items, err = e.prepareItems(items, file, expn, slot)
			if err != nil {
				return nil, err
			}
		}
		output = append(output, items...)
		for _, helper := range helpers {
			delete(helper.pending, request.consumer)
		}
	}
	// A shared helper disappears only after every procedural consumer ran.
	consumeHelpers(item, occurrences)
	return output, nil
}

// A grouped type member is a complete named declaration for derives. Lift the
// group into ordinary items so generated impls remain siblings of their types.
func (e *engine) rewriteTypeSpecs(group *ast.TypeDecl, file *ast.File, parent ast.ExpnID, slot itemSlot) ([]ast.Item, error) {
	if len(group.Attrs) != 0 {
		return nil, e.attributeError(group.Attrs[0], file, parent, "unsupported attribute or invalid attribute position on type group")
	}
	var output []ast.Item
	for _, spec := range group.Specs {
		declaration := &ast.TypeDecl{ItemBase: spec.ItemBase, Name: spec.Name, Type: spec.Type, Alias: spec.Alias, Form: spec.Form, Generics: spec.Generics, Where: spec.Where}
		if declaration.Vis.Kind == ast.VisNone {
			declaration.Vis = group.Vis
		}
		if _, err := e.itemInput(spec, file); err != nil {
			return nil, e.attributeError(spec, file, parent, err.Error())
		}
		body := append([]syntaxToken(nil), e.itemBodies[spec]...)
		prefix := 0
		if len(body) != 0 && body[0].Token != nil && body[0].Token.Kind == token.PUB {
			prefix = 1
			if len(body) > 1 && body[1].Tree != nil && body[1].Tree.Delim == ast.Paren {
				prefix++
			}
		}
		typeToken := body[prefix]
		typeToken.Tree, typeToken.Fragment = nil, nil
		typeToken.Token = &token.Token{Kind: token.TYPE, Text: "type"}
		retained := append([]syntaxToken(nil), body[:prefix]...)
		if prefix == 0 && group.Vis.Kind != ast.VisNone {
			pub := typeToken
			pub.Token = &token.Token{Kind: token.PUB, Text: "pub"}
			retained = append(retained, pub)
		}
		retained = append(retained, typeToken)
		retained = append(retained, body[prefix:]...)
		e.itemBodies[declaration] = retained
		e.r.Origins[declaration], e.r.Locations[declaration] = e.r.Origins[spec], e.r.Locations[spec]
		e.lexical[declaration] = e.lexical[spec]
		items, err := e.rewriteItem(declaration, file, parent, slot)
		if err != nil {
			return nil, err
		}
		output = append(output, items...)
	}
	return output, nil
}

type deriveConsumer struct {
	file     *ast.File
	function *ast.FuncDecl
}

type deriveRequest struct {
	attr       *ast.Attr
	path       *ast.Path
	definition definition
	consumer   deriveConsumer
	helpers    []string
	err        error
}

type helperConsumers struct {
	pending map[deriveConsumer]bool
}

func deriveHelpers(requests []deriveRequest) map[string]*helperConsumers {
	result := map[string]*helperConsumers{}
	for _, request := range requests {
		if request.err != nil {
			continue
		}
		for _, name := range request.helpers {
			consumers := result[name]
			if consumers == nil {
				consumers = &helperConsumers{pending: map[deriveConsumer]bool{}}
				result[name] = consumers
			}
			consumers.pending[request.consumer] = true
		}
	}
	return result
}

func (e *engine) itemDerives(item ast.Item, file *ast.File) ([]deriveRequest, error) {
	// Without a source resolver there are no procedural derives or executable
	// attributes. Keep the complete request and its helpers for the caller's
	// remaining processing.
	if e.resolver == nil {
		return nil, nil
	}
	var result []deriveRequest
	var firstError error
	for _, attr := range *itemAttributes(item) {
		if attributeName(attr) != "derive" || attr.Inner {
			continue
		}
		paths, err := e.derivePaths(attr, file)
		if err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		for _, request := range paths {
			request.definition, request.err = e.lookup(request.path, file)
			if request.err == nil && request.definition.proc != nil && request.definition.proc.Kind == pm.Derive {
				entry := request.definition.proc
				request.consumer = deriveConsumer{file: entry.File, function: entry.Function}
				request.helpers = entry.Helpers
			} else if request.err == nil {
				request.err = fmt.Errorf("derive requires a derive procedural macro: %s", macroPathName(request.path))
			} else {
				request.err = fmt.Errorf("unknown derive: %s: %w", macroPathName(request.path), request.err)
			}
			result = append(result, request)
		}
	}
	return result, firstError
}

func macroPathName(path *ast.Path) string {
	var names []string
	for _, segment := range path.Segments {
		names = append(names, segment.Name.Name)
	}
	return strings.Join(names, "::")
}

func (e *engine) derivePaths(attr *ast.Attr, file *ast.File) ([]deriveRequest, error) {
	if attr.Args == nil || attr.Args.Delim != ast.Paren || len(attr.Args.Tokens) == 0 {
		return nil, fmt.Errorf("derive requires parenthesized protocol paths")
	}
	var result []deriveRequest
	current := deriveRequest{attr: attr}
	wantName := true
	for _, value := range attr.Args.Tokens {
		tok := value.Token
		if tok == nil {
			return nil, fmt.Errorf("derive arguments must be protocol paths")
		}
		if wantName {
			if tok.Kind != token.IDENT {
				return nil, fmt.Errorf("derive requires a protocol name")
			}
			base := ast.Base{Loc: ast.Span{Start: tok.Start, End: tok.End}, Expn: attr.Expn}
			id := &ast.Ident{Base: base, Name: tok.Text, Raw: tok.Raw}
			scope := e.r.Scopes[attr.Path.Segments[0].Name]
			if scope.File == nil {
				scope = Scope{File: file, Ctx: attr.Path.Segments[0].Name.Ctx}
			}
			origin, location := Origin{scope.File, base.Loc}, Origin{}
			if context, ok := e.r.TokenContexts[tok]; ok {
				scope, origin, location, id.Ctx = context.Scope, context.Origin, context.Location, context.Ctx
			} else {
				id.Ctx = scope.Ctx
			}
			e.r.Scopes[id], e.r.Origins[id] = scope, origin
			if location.File != nil {
				e.r.Locations[id] = location
			}
			if current.path == nil {
				current.path = &ast.Path{Base: base}
			}
			current.path.Segments = append(current.path.Segments, &ast.PathSegment{Base: base, Name: id})
			current.path.Loc.End = tok.End
			wantName = false
			continue
		}
		switch tok.Kind {
		case token.DCOLON:
			wantName = true
		case token.COMMA:
			result = append(result, current)
			current = deriveRequest{attr: attr}
			wantName = true
		default:
			return nil, fmt.Errorf("derive arguments must be comma-separated protocol paths")
		}
	}
	if current.path != nil {
		if wantName {
			return nil, fmt.Errorf("derive path requires a member after ::")
		}
		result = append(result, current)
	}
	return result, nil
}

func removeDerives(item ast.Item) {
	var attrs []*ast.Attr
	for _, attr := range *itemAttributes(item) {
		if attr.Inner || attributeName(attr) != "derive" {
			attrs = append(attrs, attr)
		}
	}
	*itemAttributes(item) = attrs
}

// Helpers are scoped to this declaration and its fields/variants. Nested
// declarations, function bodies and token trees start no inherited helper scope.
func helperAttributeLists(item ast.Item, visit func(*[]*ast.Attr)) {
	visit(itemAttributes(item))
	ast.Walk(item, func(node ast.Node) bool {
		if node != item {
			if _, nested := node.(ast.Item); nested {
				return false
			}
		}
		switch node := node.(type) {
		case *ast.Block, *ast.TokenTree, *ast.Attr:
			return false
		case *ast.Field:
			visit(&node.Attrs)
		case *ast.Variant:
			visit(&node.Attrs)
		}
		return true
	})
}

func (e *engine) checkHelperAmbiguity(item ast.Item, helpers map[string]*helperConsumers, file *ast.File, parent ast.ExpnID) error {
	var err error
	helperAttributeLists(item, func(attrs *[]*ast.Attr) {
		for _, attr := range *attrs {
			if err != nil || attr.Inner || helpers[attributeName(attr)] == nil {
				continue
			}
			definition, lookupErr := e.lookup(attr.Path, file)
			if lookupErr == nil && definition.proc != nil && definition.proc.Kind == pm.Attribute {
				err = e.attributeError(attr, file, parent, "attribute "+attributeName(attr)+" is both a derive helper and an executable attribute; qualify the executable attribute path")
			}
		}
	})
	return err
}

func helperOccurrences(item ast.Item, helpers map[string]*helperConsumers) map[*ast.Attr]*helperConsumers {
	result := map[*ast.Attr]*helperConsumers{}
	helperAttributeLists(item, func(attrs *[]*ast.Attr) {
		for _, attr := range *attrs {
			if helper := helpers[attributeName(attr)]; helper != nil && !attr.Inner {
				result[attr] = helper
			}
		}
	})
	return result
}

func consumeHelpers(item ast.Item, occurrences map[*ast.Attr]*helperConsumers) {
	helperAttributeLists(item, func(attrs *[]*ast.Attr) {
		var retained []*ast.Attr
		for _, attr := range *attrs {
			consumers := occurrences[attr]
			if consumers == nil || len(consumers.pending) != 0 {
				retained = append(retained, attr)
			}
		}
		*attrs = retained
	})
}

func (e *engine) rejectGeneratedRegistration(item ast.Item, file *ast.File, expn ast.ExpnID) error {
	var err error
	ast.Walk(item, func(node ast.Node) bool {
		if err != nil {
			return false
		}
		switch node := node.(type) {
		case *ast.TokenTree:
			return false
		case *ast.MacroRulesDecl:
			err = e.attributeError(node, file, expn, "macros cannot generate macro definitions")
		case *ast.Attr:
			switch attributeName(node) {
			case "proc_macro", "proc_macro_attribute", "proc_macro_derive":
				err = e.attributeError(node, file, expn, "macros cannot generate procedural macro registrations")
			}
		}
		return err == nil
	})
	return err
}

func (e *engine) validateItemSlot(item ast.Item, file *ast.File, parent ast.ExpnID, slot itemSlot) error {
	impl := slot == implItems || slot == traitImplItems
	if !impl && slot != traitItems {
		return nil
	}
	switch declaration := item.(type) {
	case *ast.FuncDecl:
		if impl && declaration.Body == nil {
			return e.attributeError(item, file, parent, "impl member requires a body")
		}
	case *ast.TypeDecl:
		if declaration.Grouped || impl && (declaration.Type == nil || !declaration.Alias) {
			return e.attributeError(item, file, parent, "impl/trait associated type requires a single type binding; impl bindings require =")
		}
	default:
		return e.attributeError(item, file, parent, "trait/impl member output requires func or type")
	}
	if (slot == traitItems || slot == traitImplItems) && reflect.ValueOf(item).Elem().FieldByName("Vis").Interface().(ast.Visibility).Kind != ast.VisNone {
		return e.attributeError(item, file, parent, "trait members inherit visibility")
	}
	return nil
}

func firstItem(items []ast.Item) ast.Node {
	if len(items) > 0 {
		return items[0]
	}
	return nil
}

func (e *engine) attributeError(node ast.Node, file *ast.File, parent ast.ExpnID, message string) error {
	origin, location := e.r.Expansions[parent].Call, Origin{}
	if node != nil {
		origin, location = e.r.Origins[node], e.location(node)
		if origin.File == nil {
			origin = Origin{file, node.Span()}
		}
		if parent == 0 {
			parent = nodeExpn(node)
		}
	}
	if location.File == nil {
		location = origin
	}
	call := origin
	if parent != 0 {
		call = e.r.Expansions[parent].Call
	}
	return &Diagnostic{Code: "attribute", Message: message, Location: location, Definition: origin, Call: call, Expn: parent}
}
