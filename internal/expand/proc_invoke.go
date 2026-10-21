package expand

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

func (e *engine) invokeProc(n ast.Node, d definition, args *ast.TokenTree, kind fragmentKind, position pm.Position, file *ast.File, parent ast.ExpnID) (ast.Node, error) {
	if d.proc.Kind != pm.Function {
		return nil, e.attributeError(n, file, parent, "macro invocation requires a function-like procedural macro")
	}
	caller := e.procCaller(n, file)
	var input []syntaxToken
	if args != nil {
		input = e.treeInput(args, caller.File, caller.Ctx)
	}
	ts, id, caller, err := e.runProc(n, d, input, nil, kind, position, file, parent)
	if err != nil {
		return nil, err
	}
	out, err := e.parse(ts, kind, id, caller)
	if err != nil {
		return nil, e.procParseError(err, n, file, id)
	}
	if item, ok := out.(ast.Item); ok {
		items, err := e.prepareItems([]ast.Item{item}, file, id, singleItem)
		if err != nil {
			return nil, err
		}
		return items[0], nil
	}
	if err := e.selection(file).children(out); err != nil {
		return nil, err
	}
	v, err := e.rewrite(reflect.ValueOf(out), file, id)
	if err != nil {
		return nil, err
	}
	return v.Interface().(ast.Node), nil
}

func (e *engine) invokeProcItems(n ast.Node, d definition, args, item []syntaxToken, file *ast.File, parent ast.ExpnID, slots ...itemSlot) ([]ast.Item, ast.ExpnID, error) {
	position := pm.ItemsPosition
	parseKind := itemsKind
	if len(slots) > 0 {
		position = slotPosition(slots[0])
		switch slots[0] {
		case traitItems:
			parseKind = traitMembersKind
		case implItems, traitImplItems:
			parseKind = implMembersKind
		case defaultImplItems:
			parseKind = defaultMembersKind
		}
	}
	ts, id, caller, err := e.runProc(n, d, args, item, itemsKind, position, file, parent)
	if err != nil {
		return nil, id, err
	}
	out, err := e.parse(ts, parseKind, id, caller)
	if err != nil {
		return nil, id, e.procParseError(err, n, file, id)
	}
	return out.(*ast.File).Items, id, nil
}

func slotPosition(slot itemSlot) pm.Position {
	switch slot {
	case singleItem:
		return pm.ItemPosition
	case traitItems:
		return pm.TraitMembersPosition
	case implItems, traitImplItems, defaultImplItems:
		return pm.ImplMembersPosition
	}
	return pm.ItemsPosition
}

func slotParseKind(slot itemSlot) fragmentKind {
	switch slot {
	case traitItems:
		return traitMembersKind
	case implItems, traitImplItems:
		return implMembersKind
	case defaultImplItems:
		return defaultMembersKind
	}
	return itemKind
}

func (e *engine) procCaller(n ast.Node, file *ast.File) Scope {
	if caller := e.lexical[n]; caller.File != nil {
		return caller
	}
	origin := e.r.Origins[n]
	if origin.File == nil {
		origin = Origin{file, n.Span()}
	}
	var ctx ast.SyntaxCtx
	switch n := n.(type) {
	case *ast.Attr:
		if n.Path != nil && len(n.Path.Segments) > 0 {
			ctx = n.Path.Segments[0].Name.Ctx
		}
	case *ast.MacroExpr:
		ctx = n.Path.Segments[0].Name.Ctx
	case *ast.MacroType:
		ctx = n.Path.Segments[0].Name.Ctx
	case *ast.MacroItem:
		ctx = n.Path.Segments[0].Name.Ctx
	}
	return Scope{File: origin.File, Ctx: ctx}
}

func (e *engine) runProc(n ast.Node, d definition, args, item []syntaxToken, kind fragmentKind, position pm.Position, file *ast.File, parent ast.ExpnID) ([]syntaxToken, ast.ExpnID, Scope, error) {
	call := e.r.Origins[n]
	if call.File == nil {
		call = Origin{file, n.Span()}
	}
	location := e.location(n)
	if location.File == nil {
		location = call
	}
	definition := Origin{d.file, d.proc.Function.Span()}
	if d.proc.Attribute != nil {
		definition.Span = d.proc.Attribute.Span()
	}
	id := ast.ExpnID(len(e.r.Expansions) + 1)
	stack := append(append([]ast.ExpnID(nil), e.r.Expansions[parent].Stack...), id)
	original := e.originals[n]
	if original == nil {
		original = n
	}
	e.r.Expansions[id] = Expansion{Definition: definition, Call: call, Original: original, Parent: parent, Stack: stack}
	fail := func(code string, err error) ([]syntaxToken, ast.ExpnID, Scope, error) {
		return nil, id, Scope{}, &Diagnostic{Code: code, Message: err.Error(), Location: location, Definition: definition, Call: call, Expn: id}
	}
	if err := e.expansionBudget(call, definition, id); err != nil {
		return fail("budget", err)
	}
	if len(stack) > MaxDepth {
		return fail("budget", fmt.Errorf("macro depth budget exceeded (%d)", MaxDepth))
	}
	if e.nextCtx == ^ast.SyntaxCtx(0) {
		return fail("budget", fmt.Errorf("syntax context space exhausted"))
	}
	e.nextCtx++
	ctx := e.nextCtx
	entry := e.r.Expansions[id]
	entry.Ctx = ctx
	e.r.Expansions[id] = entry
	caller := e.procCaller(n, file)
	e.r.Contexts[ctx] = Context{Definition: Scope{File: d.file, Ctx: d.proc.Function.Name.Ctx}, Caller: caller, Expn: id}
	configuration := e.configs[file]
	var target pm.Target
	if configuration.Environment != nil {
		target = pm.Target{OS: configuration.Environment.Target.OS, Arch: configuration.Environment.Target.Arch}
	}
	var features []string
	for name, enabled := range configuration.Features.Enabled {
		if enabled {
			features = append(features, name)
		}
	}
	sort.Strings(features)
	cfg := procInvocationConfig{RequestID: fmt.Sprintf("expansion-%d", id), Entry: d.proc.Name, Kind: d.proc.Kind, Slot: kind, Position: position, Definition: definition, Call: call, CallLocation: location, Ctx: ctx, Caller: caller, Target: target, Features: features}
	if d.proc.Kind == pm.Function {
		cfg.Input = args
	} else {
		cfg.Args, cfg.Item = args, item
	}
	invocation, err := e.newProcInvocation(cfg)
	if err != nil {
		return fail("macro", err)
	}
	if d.run == nil {
		return fail("macro", fmt.Errorf("restricted procedural macro host is not configured for %s", d.proc.Name))
	}
	response, err := d.run(invocation.request)
	if err != nil {
		return fail("macro", err)
	}
	tokens, diagnostics, err := invocation.readResponse(bytes.NewReader(response))
	errorLocation := false
	for _, diagnostic := range diagnostics {
		e.r.ProcDiagnostics = append(e.r.ProcDiagnostics, ProcDiagnostic{diagnostic.Severity, diagnostic.Message, diagnostic.Location, id})
		if diagnostic.Severity == pm.Error && !errorLocation {
			location = diagnostic.Location
			errorLocation = true
		}
	}
	if err != nil {
		return fail("macro", err)
	}
	return tokens, id, caller, nil
}

func (e *engine) procParseError(err error, n ast.Node, file *ast.File, id ast.ExpnID) error {
	location := e.location(n)
	if location.File == nil {
		location = Origin{file, n.Span()}
	}
	code := "category"
	if pe, ok := err.(*parseError); ok {
		code = pe.code
		if pe.origin.File != nil {
			location = pe.origin
		}
	}
	frame := e.r.Expansions[id]
	return &Diagnostic{Code: code, Message: err.Error(), Location: location, Definition: frame.Definition, Call: frame.Call, Expn: id}
}
