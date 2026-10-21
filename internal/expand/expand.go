package expand

import (
	"fmt"
	"reflect"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// Limits cover the complete ExpandPackages call, including nested expansions.
const (
	MaxExpansions   = 4096
	MaxOutputNodes  = 100_000
	MaxOutputTokens = 100_000
	MaxMatchSteps   = 1_000_000
	MaxDepth        = 128
)

type Origin struct {
	File *ast.File
	Span ast.Span
}

type Key struct {
	Name string
	Ctx  ast.SyntaxCtx
}

type Scope struct {
	File *ast.File
	Ctx  ast.SyntaxCtx
}

type Context struct {
	Definition Scope
	Caller     Scope
	Expn       ast.ExpnID
}

// RuntimeRoot is a host-selected namespace. Its hygiene context grants this
// one binding and never the private scope of the library that owns the root.
type RuntimeRoot struct {
	Name   string
	Target []string
}

type Expansion struct {
	Definition Origin
	Call       Origin
	Input      Origin // Complete attribute input, distinct from the diagnostic call range.
	Original   ast.Node
	Parent     ast.ExpnID
	Stack      []ast.ExpnID // outermost first, including this expansion
	Ctx        ast.SyntaxCtx
}

// TokenContext preserves lexical identity for names that remain in token trees,
// such as protocol paths inside derive attributes.
type TokenContext struct {
	Ctx      ast.SyntaxCtx
	Scope    Scope
	Origin   Origin
	Location Origin
}

// Capture preserves the fragment boundary after AST substitution. Callers
// that resolve an ambiguous type slot must not infer its category from text.
type Capture struct {
	Kind string
	Use  Origin
}

// Resolver supplies source macro definitions for qualified/imported paths.
// A missing source module must return an error; nil only enables package macros.
type Resolver interface {
	LookupMacro(path *ast.Path, scope Scope) (Macro, error)
}

// Macro is a source-selected definition. Run is supplied by the compilation
// host for a prepared procedural library; a missing host never executes code.
type Macro struct {
	Rules *ast.MacroRulesDecl
	Proc  *procmacro.Entry
	File  *ast.File
	Run   func(pm.Request) ([]byte, error)
}

type Result struct {
	RuntimeRoots    map[ast.SyntaxCtx]RuntimeRoot
	ProcDiagnostics []ProcDiagnostic
	// UsesConfiguration includes evaluated source and macro-produced constructs,
	// even when a false predicate removes the entire generated item.
	UsesConfiguration  bool
	ConfigurationFiles map[*ast.File]bool
	// TargetFiles contains original destination files, including removed items.
	TargetFiles map[*ast.File]bool
	Allocations map[*ast.CallExpr]bool
	Captures    map[ast.Node]Capture
	GoResults   map[*ast.CallExpr]bool
	Prints      map[*ast.CallExpr]bool
	Files       []*ast.File
	// PartialFiles holds completed items when expansion fails. Source loaders
	// may discover generated modules here without replacing diagnostic inputs.
	PartialFiles []*ast.File
	Expansions   map[ast.ExpnID]Expansion
	Contexts     map[ast.SyntaxCtx]Context
	Origins      map[ast.Node]Origin
	// Locations overrides diagnostic positions without changing semantic origins.
	// Missing entries fall back to Origins.
	Locations     map[ast.Node]Origin
	Scopes        map[*ast.Ident]Scope
	TokenContexts map[*token.Token]TokenContext
	// Introductions preserves the template key of a generated package binding.
	// resolve registers that key as well as the identifier's caller key.
	Introductions map[*ast.Ident]Key
}

type ProcDiagnostic struct {
	Severity pm.Severity
	Message  string
	Location Origin
	Expn     ast.ExpnID
}

type Diagnostic struct {
	Code, Message    string
	Location         Origin
	Definition, Call Origin
	Expn             ast.ExpnID
}

func (d *Diagnostic) Error() string {
	return fmt.Sprintf("%s: %s (definition %d:%d, call %d:%d, expansion %d)", d.Code, d.Message,
		d.Definition.Span.Start, d.Definition.Span.End, d.Call.Span.Start, d.Call.Span.End, d.Expn)
}

type syntaxToken struct {
	Token    *token.Token
	Tree     *syntaxTree
	Fragment *fragment
	Origin   Origin
	Location Origin
	Ctx      ast.SyntaxCtx
	Scope    Scope
	Lexical  Scope
}
type syntaxTree struct {
	Delim    ast.Delim
	Tokens   []syntaxToken
	Origin   Origin
	Location Origin
	Open     Origin
	Close    Origin
}
type fragment struct {
	Kind   fragmentKind
	Node   ast.Node
	Tokens []syntaxToken
}
type definition struct {
	decl *ast.MacroRulesDecl
	file *ast.File
	proc *procmacro.Entry
	run  func(pm.Request) ([]byte, error)
}
type engine struct {
	r                    *Result
	resolver             Resolver
	configs              map[*ast.File]config.Config
	defs                 map[Key]definition
	localFiles           map[*ast.File]bool
	captured             map[ast.Node]bool
	lexical              map[ast.Node]Scope
	originals            map[ast.Node]ast.Node
	trees                map[*ast.TokenTree][]syntaxToken
	itemBodies           map[ast.Item][]syntaxToken
	nextCtx              ast.SyntaxCtx
	nodes, tokens, steps int
	expansions           int
}

// Expand collects every file's definitions before expanding any invocation.
// On failure Files is nil, while the tables retain the failing expansion stack.
// Files must belong to one package. Pass nil for resolver for local source only.
func Expand(files []*ast.File, resolver Resolver, configs ...map[*ast.File]config.Config) (*Result, error) {
	return ExpandPackages([][]*ast.File{files}, resolver, configs...)
}

// ExpandPackages keeps package definitions separate while assigning expansion
// identities across the complete source graph.
func ExpandPackages(packages [][]*ast.File, resolver Resolver, configs ...map[*ast.File]config.Config) (*Result, error) {
	e := &engine{r: &Result{Expansions: map[ast.ExpnID]Expansion{}, Contexts: map[ast.SyntaxCtx]Context{}, RuntimeRoots: map[ast.SyntaxCtx]RuntimeRoot{},
		Origins: map[ast.Node]Origin{}, Locations: map[ast.Node]Origin{}, Scopes: map[*ast.Ident]Scope{}, Introductions: map[*ast.Ident]Key{}, Captures: map[ast.Node]Capture{}, Allocations: map[*ast.CallExpr]bool{}, TargetFiles: map[*ast.File]bool{}, ConfigurationFiles: map[*ast.File]bool{}}, resolver: resolver,
		defs: map[Key]definition{}, trees: map[*ast.TokenTree][]syntaxToken{}, localFiles: map[*ast.File]bool{}, captured: map[ast.Node]bool{}, lexical: map[ast.Node]Scope{}, originals: map[ast.Node]ast.Node{}}
	if len(configs) > 0 {
		e.configs = configs[0]
	}
	if e.configs == nil {
		e.configs = map[*ast.File]config.Config{}
	}
	for _, files := range packages {
		for _, f := range files {
			configuration := e.configs[f]
			if configuration.Reads == nil {
				configuration.Reads = &config.Reads{}
				e.configs[f] = configuration
			}
			used, err := Configure(f, configuration)
			e.r.UsesConfiguration = e.r.UsesConfiguration || used
			if used {
				e.r.ConfigurationFiles[f] = true
			}
			for _, targetFile := range configuration.Reads.TargetFiles() {
				e.r.TargetFiles[targetFile] = true
			}
			if err != nil {
				return e.r, err
			}
			e.reserve(f)
		}
	}
	var out []*ast.File
	for _, files := range packages {
		e.defs = map[Key]definition{}
		e.localFiles = map[*ast.File]bool{}
		for _, f := range files {
			e.localFiles[f] = true
			for _, item := range f.Items {
				if d, ok := item.(*ast.MacroRulesDecl); ok {
					k := Key{d.Name.Name, d.Name.Ctx}
					if k.Name == "cfg" || k.Name == "go_result" || k.Name == "go_result_keep" {
						return e.r, &Diagnostic{Code: "definition", Message: "reserved built-in macro " + k.Name,
							Location: Origin{f, d.Span()}, Definition: Origin{f, d.Span()}, Call: Origin{f, d.Span()}}
					}
					if old, exists := e.defs[k]; exists {
						return e.r, &Diagnostic{Code: "definition", Message: "duplicate macro " + k.Name,
							Location: Origin{f, d.Span()}, Definition: Origin{old.file, old.decl.Span()}, Call: Origin{f, d.Span()}}
					}
					e.defs[k] = definition{decl: d, file: f}
				}
			}
		}
		for _, f := range files {
			n := e.copyNode(f, 0).(*ast.File)
			var items []ast.Item
			for i, original := range f.Items {
				if _, ok := original.(*ast.MacroRulesDecl); ok {
					continue
				}
				rewritten, err := e.rewriteItems([]ast.Item{n.Items[i]}, f, 0, fileItems)
				if err != nil {
					// A module emitted by an earlier item may supply the
					// source macro now being requested. Whole-module callers
					// can grow their source graph and repeat expansion.
					n.Items = items
					e.r.PartialFiles = append(out, n)
					return e.r, err
				}
				if len(rewritten) != 0 {
					items = append(items, rewritten...)
				} else {
					dropItemComments(n, n.Items[i])
				}
			}
			n.Items = items
			var id ast.NodeID
			ast.Walk(n, func(n ast.Node) bool { id++; n.(interface{ SetID(ast.NodeID) }).SetID(id); return true })
			out = append(out, n)
		}
	}
	e.r.Files = out
	return e.r, nil
}

func (e *engine) reserve(f *ast.File) {
	ast.Walk(f, func(n ast.Node) bool {
		e.r.Origins[n] = Origin{f, n.Span()}
		if id, ok := n.(*ast.Ident); ok {
			if id.Ctx > e.nextCtx {
				e.nextCtx = id.Ctx
			}
			e.r.Scopes[id] = Scope{f, id.Ctx}
		}
		return true
	})
}

func (e *engine) location(n ast.Node) Origin {
	if location := e.r.Locations[n]; location != (Origin{}) {
		return location
	}
	return e.r.Origins[n]
}

func (e *engine) tick() error {
	e.steps++
	if e.steps > MaxMatchSteps {
		return fmt.Errorf("macro matching budget exceeded (%d steps)", MaxMatchSteps)
	}
	return nil
}
func (e *engine) emit() error {
	e.tokens++
	if e.tokens > MaxOutputTokens {
		return fmt.Errorf("macro output budget exceeded (%d tokens)", MaxOutputTokens)
	}
	return nil
}

func (e *engine) expansionBudget(call, definition Origin, expn ast.ExpnID) error {
	e.expansions++
	if e.expansions > MaxExpansions {
		return &Diagnostic{Code: "budget", Message: fmt.Sprintf("macro expansion count budget exceeded (%d)", MaxExpansions), Location: call, Definition: definition, Call: call, Expn: expn}
	}
	return nil
}

func (e *engine) lookup(path *ast.Path, file *ast.File) (definition, error) {
	id := path.Segments[0].Name
	scope, ok := e.r.Scopes[id]
	if !ok {
		scope = Scope{file, id.Ctx}
	}
	if root, ok := e.r.RuntimeRoots[scope.Ctx]; ok {
		if provider, available := e.resolver.(interface {
			LookupRuntimeRootMacro([]string, *ast.Path) (Macro, error)
		}); available && id.Name == root.Name {
			macro, err := provider.LookupRuntimeRootMacro(root.Target, path)
			if err != nil {
				return definition{}, err
			}
			if (macro.Rules != nil || macro.Proc != nil) && macro.File != nil {
				e.reserve(macro.File)
				return definition{decl: macro.Rules, file: macro.File, proc: macro.Proc, run: macro.Run}, nil
			}
		}
		return definition{}, fmt.Errorf("macro %s has no source definition in its bound runtime root", id.Name)
	}
	// Macro definitions are static package items, so template-local marks
	// fall back to their definition scope before asking for source modules.
	for c, ok := e.r.Contexts[scope.Ctx]; ok; c, ok = e.r.Contexts[scope.Ctx] {
		scope = c.Definition
	}
	if len(path.Segments) == 1 && e.localFiles[scope.File] {
		if d, ok := e.defs[Key{id.Name, scope.Ctx}]; ok {
			return d, nil
		}
	}
	if e.resolver != nil {
		d, err := e.resolver.LookupMacro(path, scope)
		if err != nil {
			return definition{}, err
		}
		if (d.Rules != nil || d.Proc != nil) && d.File != nil {
			e.reserve(d.File)
			return definition{decl: d.Rules, file: d.File, proc: d.Proc, run: d.Run}, nil
		}
	}
	return definition{}, fmt.Errorf("macro %s has no source definition in its resolution scope", id.Name)
}

func (e *engine) invoke(n ast.Node, path *ast.Path, args *ast.TokenTree, kind fragmentKind, file *ast.File, parent ast.ExpnID, slots ...fragmentKind) (ast.Node, error) {
	if out, handled, err := e.builtin(n, path, args, kind, file, parent); handled {
		return out, err
	}
	call, ok := e.r.Origins[n]
	if !ok {
		call = Origin{file, n.Span()}
	}
	location := e.location(n)
	if location == (Origin{}) {
		location = call
	}
	d, err := e.lookup(path, file)
	if err != nil {
		return nil, &Diagnostic{Code: "macro", Message: err.Error(), Location: location, Call: call, Definition: call, Expn: parent}
	}
	if d.proc != nil {
		var position pm.Position
		if len(slots) > 0 && slots[0] == singleStmtKind {
			position = pm.StatementPosition
		}
		return e.invokeProc(n, d, args, kind, position, file, parent)
	}
	id := ast.ExpnID(len(e.r.Expansions) + 1)
	stack := append([]ast.ExpnID(nil), e.r.Expansions[parent].Stack...)
	stack = append(stack, id)
	original := e.originals[n]
	if original == nil {
		original = n
	}
	e.r.Expansions[id] = Expansion{Definition: Origin{d.file, d.decl.Span()}, Call: call, Original: original, Parent: parent, Stack: stack}
	if err := e.expansionBudget(call, Origin{d.file, d.decl.Span()}, id); err != nil {
		err.(*Diagnostic).Location = location
		return nil, err
	}
	fail := func(code string, err error, span ast.Span) (ast.Node, error) {
		return nil, &Diagnostic{Code: code, Message: err.Error(), Location: Origin{d.file, span}, Definition: Origin{d.file, span}, Call: call, Expn: id}
	}
	if len(stack) > MaxDepth {
		return fail("budget", fmt.Errorf("macro depth budget exceeded (%d)", MaxDepth), d.decl.Span())
	}
	if e.nextCtx == ^ast.SyntaxCtx(0) {
		return fail("budget", fmt.Errorf("syntax context space exhausted"), d.decl.Span())
	}
	e.nextCtx++
	ctx := e.nextCtx
	entry := e.r.Expansions[id]
	entry.Ctx = ctx
	e.r.Expansions[id] = entry
	caller, ok := e.lexical[n]
	if !ok || caller.File == nil {
		caller = Scope{call.File, path.Segments[0].Name.Ctx}
	}
	e.r.Contexts[ctx] = Context{Definition: Scope{d.file, d.decl.Name.Ctx}, Caller: caller, Expn: id}
	input := e.treeInput(args, call.File, caller.Ctx)
	var matched captures
	var rule *ast.MacroRule
	var matchErr error
	for _, candidate := range d.decl.Rules {
		m, err := compileMatcher(candidate.Matcher)
		if err != nil {
			return fail("matcher", err, candidate.Matcher.Span())
		}
		matched, err = m.match(input, e.parseFragment, e.tick)
		if err == nil {
			rule = candidate
			break
		}
		matchErr = err
		if e.steps > MaxMatchSteps {
			return fail("budget", err, candidate.Matcher.Span())
		}
	}
	if rule == nil {
		if matchErr == nil {
			matchErr = fmt.Errorf("macro has no matching rule")
		}
		code := "match"
		if me, ok := matchErr.(*matchError); ok {
			code = me.Code
		}
		return fail(code, matchErr, d.decl.Span())
	}
	ts, err := renderTemplate(rule.Template, matched, d.file, ctx, caller.Ctx, e.emit)
	if err != nil {
		code, span := "template", rule.Template.Span()
		if te, ok := err.(*templateError); ok {
			code, span = te.Code, te.Span
		}
		if e.tokens > MaxOutputTokens {
			code = "budget"
		}
		return fail(code, err, span)
	}
	e.assignScopes(ts, ctx, caller)
	parseKind := kind
	if len(slots) > 0 && slots[0] != singleStmtKind {
		parseKind = slots[0]
	}
	out, err := e.parse(ts, parseKind, id, caller)
	if err != nil {
		code, span := "template", rule.Template.Span()
		location := Origin{d.file, span}
		if pe, ok := err.(*parseError); ok {
			code = pe.code
			if pe.origin != (Origin{}) {
				location = pe.origin
				if pe.origin.File == d.file {
					span = pe.origin.Span
				}
			}
		}
		return nil, &Diagnostic{Code: code, Message: err.Error(), Location: location, Definition: Origin{d.file, span}, Call: call, Expn: id}
	}
	if list, ok := out.(*ast.File); ok {
		if len(list.Items) != 1 {
			return fail("category", fmt.Errorf("declarative macro output requires exactly one item"), rule.Template.Span())
		}
		out = list.Items[0]
	}
	if item, ok := out.(ast.Item); ok {
		keep, err := e.selection(file).item(item)
		if err != nil {
			return nil, err
		}
		if !keep {
			return nil, nil
		}
		if err := e.rejectGeneratedRegistration(item, file, id); err != nil {
			return nil, err
		}
		if !e.captured[out] {
			destination := Scope{file, caller.Ctx}
			// Package items escape all template marks to the source invocation.
			for c, ok := e.r.Contexts[destination.Ctx]; ok; c, ok = e.r.Contexts[destination.Ctx] {
				destination.Ctx = c.Caller.Ctx
			}
			e.introduce(item, destination)
		}
		return item, nil
	} else if err := e.selection(file).children(out); err != nil {
		return nil, err
	}
	v, err := e.rewrite(reflect.ValueOf(out), file, id)
	if err != nil {
		return nil, err
	}
	if !v.IsValid() {
		return nil, nil
	}
	return v.Interface().(ast.Node), nil
}

func (e *engine) treeInput(tree *ast.TokenTree, file *ast.File, ctx ast.SyntaxCtx) []syntaxToken {
	if ts, ok := e.trees[tree]; ok {
		return ts
	}
	var out []syntaxToken
	for _, v := range tree.Tokens {
		if v.Token != nil {
			t := syntaxToken{Token: v.Token, Origin: Origin{file, ast.Span{Start: v.Token.Start, End: v.Token.End}}, Ctx: ctx, Scope: Scope{file, ctx}, Lexical: Scope{file, ctx}}
			if context, ok := e.r.TokenContexts[v.Token]; ok {
				t.Origin, t.Location, t.Ctx, t.Scope = context.Origin, context.Location, context.Ctx, context.Scope
			}
			if t.Location == (Origin{}) {
				t.Location = t.Origin
			}
			out = append(out, t)
		} else if v.Tree != nil {
			origin, ok := e.r.Origins[v.Tree]
			if !ok {
				origin = Origin{file, v.Tree.Span()}
			}
			location := e.location(v.Tree)
			if location == (Origin{}) {
				location = origin
			}
			open, close := location, location
			if v.Tree.Delim != ast.NoDelim && location.Span.End > location.Span.Start {
				open.Span.End = open.Span.Start + 1
				close.Span.Start = close.Span.End - 1
			}
			out = append(out, syntaxToken{Tree: &syntaxTree{
				Delim: v.Tree.Delim, Tokens: e.treeInput(v.Tree, file, ctx),
				Origin: origin, Location: location, Open: open, Close: close,
			}, Origin: origin, Location: location, Ctx: ctx})
		}
	}
	return out
}

func (e *engine) assignScopes(ts []syntaxToken, ctx ast.SyntaxCtx, caller Scope) {
	for i := range ts {
		t := &ts[i]
		if t.Fragment != nil {
			continue
		}
		t.Lexical = Scope{e.r.Contexts[ctx].Definition.File, ctx}
		if t.Ctx == ctx {
			t.Scope = e.r.Contexts[ctx].Definition
		} else {
			t.Scope = caller
		}
		if t.Tree != nil {
			e.assignScopes(t.Tree.Tokens, ctx, caller)
		}
	}
}

func (e *engine) introduce(n ast.Item, scope Scope) {
	set := func(id *ast.Ident) {
		if id != nil {
			if e.captured[id] {
				return
			}
			e.r.Introductions[id] = Key{id.Name, id.Ctx}
			id.Ctx = scope.Ctx
			e.r.Scopes[id] = scope
		}
	}
	switch n := n.(type) {
	case *ast.FuncDecl:
		set(n.Name)
	case *ast.TypeDecl:
		set(n.Name)
		for _, s := range n.Specs {
			set(s.Name)
		}
	case *ast.EnumDecl:
		set(n.Name)
	case *ast.TraitDecl:
		set(n.Name)
		for _, member := range n.Items {
			e.introduce(member, scope)
		}
	case *ast.ImplDecl:
		for _, member := range n.Items {
			e.introduce(member, scope)
		}
	case *ast.ModDecl:
		set(n.Name)
	case *ast.VarDecl:
		for _, s := range n.Specs {
			for _, id := range s.Names {
				set(id)
			}
		}
	case *ast.ConstDecl:
		for _, s := range n.Specs {
			for _, id := range s.Names {
				set(id)
			}
		}
	case *ast.DeclareDecl:
		e.introduce(n.Decl, scope)
	}
}
