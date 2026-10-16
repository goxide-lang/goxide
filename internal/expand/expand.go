package expand

import (
	"fmt"
	"reflect"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
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

type Expansion struct {
	Definition Origin
	Call       Origin
	Original   ast.Node
	Parent     ast.ExpnID
	Stack      []ast.ExpnID // outermost first, including this expansion
	Ctx        ast.SyntaxCtx
}

// TokenContext preserves lexical identity for names that remain in token trees,
// such as protocol paths inside derive attributes.
type TokenContext struct {
	Ctx    ast.SyntaxCtx
	Scope  Scope
	Origin Origin
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
	LookupMacro(path *ast.Path, scope Scope) (*ast.MacroRulesDecl, *ast.File, error)
}

type Result struct {
	// UsesConfiguration includes evaluated source and macro-produced constructs,
	// even when a false predicate removes the entire generated item.
	UsesConfiguration bool
	Allocations       map[*ast.CallExpr]bool
	Captures          map[ast.Node]Capture
	GoResults         map[*ast.CallExpr]bool
	Prints            map[*ast.CallExpr]bool
	Files             []*ast.File
	Expansions        map[ast.ExpnID]Expansion
	Contexts          map[ast.SyntaxCtx]Context
	Origins           map[ast.Node]Origin
	Scopes            map[*ast.Ident]Scope
	TokenContexts     map[*token.Token]TokenContext
	// Introductions preserves the template key of a generated package binding.
	// resolve registers that key as well as the identifier's caller key.
	Introductions map[*ast.Ident]Key
}

type Diagnostic struct {
	Code, Message    string
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
	Ctx      ast.SyntaxCtx
	Scope    Scope
	Lexical  Scope
}
type syntaxTree struct {
	Delim  ast.Delim
	Tokens []syntaxToken
	Origin Origin
}
type fragment struct {
	Kind   fragmentKind
	Node   ast.Node
	Tokens []syntaxToken
}
type definition struct {
	decl *ast.MacroRulesDecl
	file *ast.File
}
type engine struct {
	r                    *Result
	resolver             Resolver
	features             map[*ast.File]Features
	defs                 map[Key]definition
	localFiles           map[*ast.File]bool
	captured             map[ast.Node]bool
	lexical              map[ast.Node]Scope
	originals            map[ast.Node]ast.Node
	trees                map[*ast.TokenTree][]syntaxToken
	nextCtx              ast.SyntaxCtx
	nodes, tokens, steps int
	expansions           int
}

// Expand collects every file's definitions before expanding any invocation.
// On failure Files is nil, while the tables retain the failing expansion stack.
// Files must belong to one package. Pass nil for resolver for local source only.
func Expand(files []*ast.File, resolver Resolver, configs ...map[*ast.File]Features) (*Result, error) {
	return ExpandPackages([][]*ast.File{files}, resolver, configs...)
}

// ExpandPackages keeps package definitions separate while assigning expansion
// identities across the complete source graph.
func ExpandPackages(packages [][]*ast.File, resolver Resolver, configs ...map[*ast.File]Features) (*Result, error) {
	e := &engine{r: &Result{Expansions: map[ast.ExpnID]Expansion{}, Contexts: map[ast.SyntaxCtx]Context{},
		Origins: map[ast.Node]Origin{}, Scopes: map[*ast.Ident]Scope{}, Introductions: map[*ast.Ident]Key{}, Captures: map[ast.Node]Capture{}, Allocations: map[*ast.CallExpr]bool{}}, resolver: resolver,
		defs: map[Key]definition{}, trees: map[*ast.TokenTree][]syntaxToken{}, localFiles: map[*ast.File]bool{}, captured: map[ast.Node]bool{}, lexical: map[ast.Node]Scope{}, originals: map[ast.Node]ast.Node{}}
	if len(configs) > 0 {
		e.features = configs[0]
	}
	for _, files := range packages {
		for _, f := range files {
			used, err := Configure(f, e.features[f])
			e.r.UsesConfiguration = e.r.UsesConfiguration || used
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
							Definition: Origin{f, d.Span()}, Call: Origin{f, d.Span()}}
					}
					if old, exists := e.defs[k]; exists {
						return e.r, &Diagnostic{Code: "definition", Message: "duplicate macro " + k.Name,
							Definition: Origin{old.file, old.decl.Span()}, Call: Origin{f, d.Span()}}
					}
					e.defs[k] = definition{d, f}
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
				v, err := e.rewrite(reflect.ValueOf(n.Items[i]), f, 0)
				if err != nil {
					return e.r, err
				}
				if v.IsValid() {
					items = append(items, v.Interface().(ast.Item))
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
		return &Diagnostic{Code: "budget", Message: fmt.Sprintf("macro expansion count budget exceeded (%d)", MaxExpansions), Definition: definition, Call: call, Expn: expn}
	}
	return nil
}

func (e *engine) lookup(path *ast.Path, file *ast.File) (definition, error) {
	id := path.Segments[0].Name
	scope, ok := e.r.Scopes[id]
	if !ok {
		scope = Scope{file, id.Ctx}
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
		d, f, err := e.resolver.LookupMacro(path, scope)
		if err != nil {
			return definition{}, err
		}
		if d != nil && f != nil {
			e.reserve(f)
			return definition{d, f}, nil
		}
	}
	return definition{}, fmt.Errorf("macro %s has no source definition in its resolution scope", id.Name)
}

func (e *engine) invoke(n ast.Node, path *ast.Path, args *ast.TokenTree, kind fragmentKind, file *ast.File, parent ast.ExpnID) (ast.Node, error) {
	if out, handled, err := e.builtin(n, path, args, kind, file, parent); handled {
		return out, err
	}
	call, ok := e.r.Origins[n]
	if !ok {
		call = Origin{file, n.Span()}
	}
	d, err := e.lookup(path, file)
	if err != nil {
		return nil, &Diagnostic{Code: "macro", Message: err.Error(), Call: call, Definition: call, Expn: parent}
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
		return nil, err
	}
	fail := func(code string, err error, span ast.Span) (ast.Node, error) {
		return nil, &Diagnostic{Code: code, Message: err.Error(), Definition: Origin{d.file, span}, Call: call, Expn: id}
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
	out, err := e.parse(ts, kind, id, caller)
	if err != nil {
		code, span := "template", rule.Template.Span()
		if pe, ok := err.(*parseError); ok {
			code = pe.code
			if pe.origin.File != nil {
				span = pe.origin.Span
			}
		}
		return fail(code, err, span)
	}
	if item, ok := out.(ast.Item); ok {
		keep, err := e.selection(file).item(item)
		if err != nil {
			return nil, err
		}
		if !keep {
			return nil, nil
		}
		if !e.captured[out] {
			destination := Scope{file, caller.Ctx}
			// Package items escape all template marks to the source invocation.
			for c, ok := e.r.Contexts[destination.Ctx]; ok; c, ok = e.r.Contexts[destination.Ctx] {
				destination.Ctx = c.Caller.Ctx
			}
			e.introduce(item, destination)
		}
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
			out = append(out, syntaxToken{Token: v.Token, Origin: Origin{file, ast.Span{Start: v.Token.Start, End: v.Token.End}}, Ctx: ctx, Scope: Scope{file, ctx}, Lexical: Scope{file, ctx}})
		} else if v.Tree != nil {
			out = append(out, syntaxToken{Tree: &syntaxTree{Delim: v.Tree.Delim, Tokens: e.treeInput(v.Tree, file, ctx), Origin: Origin{file, v.Tree.Span()}}, Origin: Origin{file, v.Tree.Span()}, Ctx: ctx})
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
