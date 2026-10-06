package macros

import (
	"bytes"
	"fmt"
	"github.com/hxsf/goxide/internal/sourcemap"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"sort"
	"strconv"
	"strings"
)

type TypeContext struct {
	Types   map[string]*ast.TypeSpec
	Imports []*ast.ImportSpec
}

type BuiltinBindingError struct {
	Position token.Position
	Message  string
}

func (e *BuiltinBindingError) Error() string { return fmt.Sprintf("%s: %s", e.Position, e.Message) }
func bindingError(fs *token.FileSet, p token.Pos, message string) error {
	return &BuiltinBindingError{fs.Position(p), message}
}

func (e *engine) definitionNames() map[string]bool {
	if e.active != nil {
		return e.active.pkg.topNames
	}
	return e.topNames
}
func (e *engine) callerNames() map[string]bool {
	if e.pkg != nil {
		return e.pkg.topNames
	}
	return e.topNames
}

// Only declaration heads are inspected: a builtin in an initializer is not a
// declaration. Balanced bodies and types are skipped, including goxide syntax.
func declaredNames(ts []tok) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < len(ts); i++ {
		switch ts[i].text {
		case "func", "enum", "struct":
			if i+1 < len(ts) && ident(ts[i+1].text) {
				out[ts[i+1].text] = true
			}
		case "var", "const", "type":
			kind := ts[i].text
			j := i + 1
			grouped := j < len(ts) && ts[j].text == "("
			if grouped {
				j++
			}
			for j < len(ts) {
				if ts[j].text == ";" {
					j++
					continue
				}
				if ts[j].text == ")" {
					break
				}
				if ident(ts[j].text) {
					out[ts[j].text] = true
					j++
				}
				if kind != "type" {
					for j+1 < len(ts) && ts[j].text == "," && ident(ts[j+1].text) {
						out[ts[j+1].text] = true
						j += 2
					}
				}
				for j < len(ts) && ts[j].text != ";" && ts[j].text != ")" {
					if ts[j].text == "(" || ts[j].text == "[" || ts[j].text == "{" {
						if end, err := closeGroup(ts, j); err == nil {
							j = end
						}
					}
					j++
				}
				if !grouped {
					break
				}
			}
			i = j
		}
		if i < len(ts) && (ts[i].text == "(" || ts[i].text == "[" || ts[i].text == "{") {
			if end, err := closeGroup(ts, i); err == nil {
				i = end
			}
		}
	}
	return out
}

// ResolveBuiltins restores native Go operations, without a wrapper or extra
// evaluation boundary. Only lexical locals/imports are renamed. Package-level
// shadows are rejected earlier; renaming user named types would change identity.
// Markers are allocated by the expander, never inferred from user spelling.
func ResolveBuiltins(fs *token.FileSet, f *ast.File, markers map[string]string, imp types.Importer, contexts map[string]TypeContext) error {
	return ResolveBuiltinsWithPath("", fs, f, markers, imp, contexts)
}

// ResolveBuiltinsWithPath retains the actual consumer identity during binding.
// An empty path preserves the standalone binding owner used by ResolveBuiltins.
func ResolveBuiltinsWithPath(packagePath string, fs *token.FileSet, f *ast.File, markers map[string]string, imp types.Importer, contexts map[string]TypeContext) error {
	if packagePath == "" {
		packagePath = "macro/builtin/bindings"
	}
	needed := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && markers[id.Name] != "" {
			needed[markers[id.Name]] = true
		}
		return true
	})
	if len(needed) == 0 {
		return nil
	}
	for name := range needed {
		if f.Scope.Lookup(name) != nil {
			return bindingError(fs, f.Pos(), fmt.Sprintf("macro builtin %s is shadowed by a caller package declaration; this builtin lowering is unsupported", name))
		}
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	markerNames := make([]string, 0, len(markers))
	for name := range markers {
		markerNames = append(markerNames, name)
	}
	sort.Strings(markerNames)
	localPrefix := markerNames[0] + "Local"
	next := 0
	fresh := func() string {
		for {
			next++
			n := fmt.Sprintf("%s%d", localPrefix, next)
			if !used[n] {
				used[n] = true
				return n
			}
		}
	}
	// parser.Object binds struct literal keys as if they were variables. Use
	// Go field identity before renaming any ambiguous keyed identifier.
	keyInfo := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Scopes: map[ast.Node]*types.Scope{}, Implicits: map[ast.Node]types.Object{}}
	if imp == nil {
		imp = importer.Default()
	}
	conf := types.Config{Importer: imp, Error: func(error) {}}
	files := []*ast.File{f}
	// Same-package type declarations are needed to disambiguate keyed literals in
	// another source file. Field identity does not require valid function bodies.
	contextNames := make([]string, 0, len(contexts))
	for name := range contexts {
		contextNames = append(contextNames, name)
	}
	sort.Strings(contextNames)
	for _, filename := range contextNames {
		context := contexts[filename]
		names := make([]string, 0, len(context.Types))
		for name := range context.Types {
			if f.Scope.Lookup(name) == nil {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			continue
		}
		var declarations bytes.Buffer
		fmt.Fprintf(&declarations, "package %s\n", f.Name.Name)
		for _, im := range context.Imports {
			declarations.WriteString("import ")
			_ = format.Node(&declarations, fs, im)
			declarations.WriteString("\n")
		}
		for _, name := range names {
			declarations.WriteString("type ")
			_ = format.Node(&declarations, fs, context.Types[name])
			declarations.WriteString("\n")
		}
		if extra, err := parser.ParseFile(fs, filename+".macrotypes", declarations.Bytes(), 0); err == nil {
			files = append(files, extra)
		}
	}
	_, _ = conf.Check(packagePath, fs, files, keyInfo)
	active := map[types.Object]bool{}
	var scopeError error
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || markers[id.Name] == "" {
			return true
		}
		scope := keyInfo.Scopes[f]
		if scope == nil {
			scopeError = bindingError(fs, id.Pos(), "cannot resolve macro builtin lexical scope")
			return true
		}
		scope = scope.Innermost(id.Pos())
		for scope != nil {
			found, obj := scope.LookupParent(markers[id.Name], id.Pos())
			if obj == nil || found == types.Universe {
				break
			}
			if _, ok := obj.(*types.TypeName); ok {
				scopeError = bindingError(fs, id.Pos(), fmt.Sprintf("macro builtin %s is shadowed by a local named type; renaming its identity is unsupported", markers[id.Name]))
				break
			}
			active[obj] = true
			scope = found.Parent()
		}
		return true
	})
	if scopeError != nil {
		return scopeError
	}
	activeBindings := map[*ast.Object]bool{}
	for id, obj := range keyInfo.Defs {
		if active[obj] && id.Obj != nil {
			activeBindings[id.Obj] = true
		}
	}
	// Type switches define a distinct implicit variable in each case, while
	// parser.Object associates all uses with the single source lhs binding.
	ast.Inspect(f, func(n ast.Node) bool {
		sw, ok := n.(*ast.TypeSwitchStmt)
		if !ok {
			return true
		}
		a, ok := sw.Assign.(*ast.AssignStmt)
		if !ok || len(a.Lhs) != 1 {
			return true
		}
		id, ok := a.Lhs[0].(*ast.Ident)
		if !ok || id.Obj == nil {
			return true
		}
		for _, c := range sw.Body.List {
			if active[keyInfo.Implicits[c]] {
				activeBindings[id.Obj] = true
			}
		}
		return true
	})
	ignored := map[*ast.Ident]bool{}
	var keyError error
	fields := map[*ast.Object]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			ignored[n.Sel] = true
		case *ast.KeyValueExpr:
			if id, ok := n.Key.(*ast.Ident); ok && needed[id.Name] {
				if obj := keyInfo.Uses[id]; obj != nil {
					if field, ok := obj.(*types.Var); ok && field.IsField() {
						ignored[id] = true
					}
				} else if id.Obj == nil {
					ignored[id] = true
				} else if activeBindings[id.Obj] {
					keyError = bindingError(fs, id.Pos(), "macro builtin hygiene cannot resolve keyed identifier "+id.Name)
				}
			}
		case *ast.StructType:
			for _, field := range n.Fields.List {
				for _, id := range field.Names {
					fields[id.Obj] = true
				}
			}
		case *ast.InterfaceType:
			for _, field := range n.Methods.List {
				for _, id := range field.Names {
					fields[id.Obj] = true
				}
			}
		}
		return true
	})
	if keyError != nil {
		return keyError
	}
	imports := map[string]string{}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := path.Base(p)
		if im.Name == nil {
			if pkg, err := imp.Import(p); err == nil {
				name = pkg.Name()
			}
		}
		if im.Name != nil {
			name = im.Name.Name
		}
		if needed[name] {
			alias := fresh()
			imports[name] = alias
			if im.Name == nil {
				im.Name = ast.NewIdent(alias)
			} else {
				im.Name.Name = alias
			}
			ignored[im.Name] = true
		}
	}
	bindings := map[*ast.Object]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || ignored[id] {
			return true
		}
		if name := markers[id.Name]; name != "" {
			id.Name = name
			return true
		}
		if !needed[id.Name] {
			return true
		}
		if id.Obj != nil && !fields[id.Obj] && activeBindings[id.Obj] {
			if bindings[id.Obj] == "" {
				bindings[id.Obj] = fresh()
			}
			id.Name = bindings[id.Obj]
		} else if id.Obj == nil && imports[id.Name] != "" {
			id.Name = imports[id.Name]
		}
		return true
	})
	return nil
}

func (e *engine) finishBuiltins(ts []tok) ([]tok, error) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, e.file, render(ts), 0)
	if err != nil {
		return ts, nil
	} // The frontend resolves after lowering the DSL.
	edits := map[int]string{}
	before := map[*ast.Ident]string{}
	importNames := map[*ast.ImportSpec]string{}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		n := path.Base(p)
		if im.Name == nil {
			if pkg, err := e.importer.Import(p); err == nil && pkg != nil {
				n = pkg.Name()
			}
		}
		if im.Name != nil {
			n = im.Name.Name
		}
		importNames[im] = n
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			before[id] = id.Name
		}
		return true
	})
	packagePath := ""
	if e.pkg != nil {
		packagePath = e.pkg.path
	}
	if err = ResolveBuiltinsWithPath(packagePath, fs, f, e.builtins, e.importer, nil); err != nil {
		if e.pkg != nil && strings.Contains(err.Error(), "cannot resolve keyed identifier") {
			return ts, nil
		}
		if located, ok := err.(*BuiltinBindingError); ok {
			offset := 0
			for _, t := range ts {
				offset += len(t.before)
				if offset == located.Position.Offset {
					mapped := &sourcemap.Error{Message: located.Message, Generated: located.Position, Stack: t.stack}
					if t.sourceEnd > t.pos.Offset {
						mapped.Origin = &sourcemap.Span{File: t.pos.Filename, Start: t.pos.Offset, End: t.sourceEnd, Line: t.pos.Line, Column: t.pos.Column}
					}
					return nil, mapped
				}
				offset += len(t.text)
			}
		}
		return nil, err
	}
	for im, old := range importNames {
		if im.Name != nil && im.Name.Name != old {
			for key, alias := range e.aliases {
				if strings.HasPrefix(key, old+".") {
					delete(e.aliases, key)
					e.aliases[im.Name.Name+strings.TrimPrefix(key, old)] = alias
				}
			}
		}
	}
	for id, old := range before {
		if id.Name != old {
			edits[fs.Position(id.Pos()).Offset] = id.Name
		}
	}
	// An implicit import needs insertion rather than replacement of its path.
	for _, im := range f.Imports {
		if im.Name != nil && !im.Name.Pos().IsValid() {
			edits[fs.Position(im.Path.Pos()).Offset] = im.Name.Name + " " + im.Path.Value
		}
	}
	offset := 0
	for i := range ts {
		offset += len(ts[i].before)
		old := ts[i].text
		if v, ok := edits[offset]; ok {
			ts[i].text = v
		}
		offset += len(old)
	}
	return ts, nil
}

// Import package names need not match path basenames. Explicit aliases already
// live in the definition scope; resolve implicit imports through the same importer.
func (e *engine) builtinImportShadow(name string) bool {
	paths := e.implicitImports
	imports := e.importPaths
	if e.active != nil {
		paths = e.active.implicitImports
		imports = e.active.imports
	}
	bound := e.isImport(name)
	for _, p := range paths {
		pkg := e.importCache[p]
		if pkg == nil {
			pkg, _ = e.importer.Import(p)
			if pkg != nil {
				e.importCache[p] = pkg
			}
		}
		if pkg != nil {
			if pkg.Name() == name {
				return true
			}
			if imports[name] == p {
				bound = false
			}
		}
	}
	return bound
}
