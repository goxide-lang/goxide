// Package procmacro collects source-owned procedural macro registrations.
package procmacro

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// Entry retains the source function separately from its consumer-facing name.
type Entry struct {
	Name      string
	Kind      pm.Kind
	Helpers   []string
	Function  *ast.FuncDecl
	File      *ast.File
	Attribute *ast.Attr
}

type Diagnostic struct {
	File    string
	Span    ast.Span
	Message string
}

func (d *Diagnostic) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", d.File, d.Span.Start, d.Span.End, d.Message)
}

// Collect reads original source after cfg selection. It does not change symbols
// or consume attributes; callers strip validated registrations before checking.
func Collect(files map[string]*ast.File, rootPackage string, enabled bool) ([]*Entry, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var entries []*Entry
	registered := map[string]*Entry{}
	for _, name := range names {
		file := files[name]
		functions := map[*ast.Attr]*ast.FuncDecl{}
		counts := map[*ast.FuncDecl]int{}
		for _, item := range file.Items {
			if function, ok := item.(*ast.FuncDecl); ok {
				for _, attr := range function.Attrs {
					if registrationKind(attr) != "" {
						functions[attr] = function
						counts[function]++
					}
				}
			}
		}
		var failure error
		ast.Walk(file, func(node ast.Node) bool {
			if failure != nil {
				return false
			}
			attr, ok := node.(*ast.Attr)
			if !ok || registrationKind(attr) == "" {
				return true
			}
			fail := func(message string) bool {
				failure = &Diagnostic{File: name, Span: attr.Span(), Message: message}
				return false
			}
			if !enabled {
				return fail("procedural macro registration requires a proc_macro lib")
			}
			function := functions[attr]
			if file.NativeGo || path.Dir(filepath.ToSlash(name)) != rootPackage || function == nil || attr.Inner {
				return fail("procedural macros must be explicit functions in the lib root package")
			}
			if file.Expn != 0 || function.Expn != 0 || attr.Expn != 0 {
				return fail("macro output cannot register procedural macros")
			}
			if counts[function] != 1 {
				return fail("procedural macro function requires exactly one registration attribute")
			}
			if function.Vis.Kind != ast.VisPub {
				return fail("procedural macro function requires explicit pub visibility")
			}
			if function.Generics != nil || function.Where != nil || len(function.Recv) != 0 || function.Path != nil {
				return fail("procedural macro function must be non-generic and have no receiver")
			}
			if function.Body == nil {
				return fail("procedural macro function requires a body")
			}
			entry := &Entry{Name: function.Name.Name, Kind: registrationKind(attr), Function: function, File: file, Attribute: attr}
			if entry.Kind == pm.Derive {
				var err error
				entry.Name, entry.Helpers, err = deriveArguments(attr)
				if err != nil {
					return fail(err.Error())
				}
			} else if attr.Args != nil {
				return fail("procedural macro registration does not accept arguments")
			}
			if entry.Name == "_" {
				return fail("procedural macro name cannot be _")
			}
			if reserved(entry.Name) {
				return fail("reserved built-in macro name " + entry.Name)
			}
			if registered[entry.Name] != nil {
				return fail("duplicate procedural macro name " + entry.Name)
			}
			registered[entry.Name] = entry
			entries = append(entries, entry)
			return false
		})
		if failure != nil {
			return nil, failure
		}
	}
	return entries, nil
}

func registrationKind(attr *ast.Attr) pm.Kind {
	if attr.Path == nil || len(attr.Path.Segments) != 1 || attr.Path.Root != ast.NoRoot {
		return ""
	}
	switch attr.Path.Segments[0].Name.Name {
	case "proc_macro":
		return pm.Function
	case "proc_macro_attribute":
		return pm.Attribute
	case "proc_macro_derive":
		return pm.Derive
	}
	return ""
}

func reserved(name string) bool {
	switch name {
	case "cfg", "go_result", "go_result_keep":
		return true
	}
	return false
}

func deriveArguments(attr *ast.Attr) (string, []string, error) {
	invalid := func() (string, []string, error) {
		return "", nil, fmt.Errorf("proc_macro_derive requires Name followed by optional attributes(helper, ...)")
	}
	if attr.Args == nil || attr.Args.Delim != ast.Paren || len(attr.Args.Tokens) == 0 {
		return invalid()
	}
	tokens := attr.Args.Tokens
	name, ok := identifier(tokens[0])
	if !ok {
		return invalid()
	}
	tokens = tokens[1:]
	if len(tokens) == 0 {
		return name, nil, nil
	}
	if !punct(tokens[0], token.COMMA) {
		return invalid()
	}
	tokens = tokens[1:]
	if len(tokens) == 0 {
		return name, nil, nil
	}
	if len(tokens) == 3 && punct(tokens[2], token.COMMA) {
		tokens = tokens[:2]
	}
	if len(tokens) != 2 {
		return invalid()
	}
	label, ok := identifier(tokens[0])
	group := tokens[1].Tree
	if !ok || label != "attributes" || group == nil || group.Delim != ast.Paren {
		return invalid()
	}
	var helpers []string
	seen := map[string]bool{}
	for tokens := group.Tokens; len(tokens) != 0; {
		helper, ok := identifier(tokens[0])
		if t := tokens[0].Token; t != nil && t.Kind == token.DEFAULT {
			helper, ok = t.Text, true
		}
		if !ok {
			return invalid()
		}
		if seen[helper] {
			return "", nil, fmt.Errorf("duplicate procedural macro helper %s", helper)
		}
		seen[helper] = true
		helpers = append(helpers, helper)
		tokens = tokens[1:]
		if len(tokens) != 0 {
			if !punct(tokens[0], token.COMMA) {
				return invalid()
			}
			tokens = tokens[1:]
		}
	}
	return name, helpers, nil
}

func identifier(value ast.TokenOrTree) (string, bool) {
	t := value.Token
	if t == nil || t.Kind != token.IDENT || t.Text == "_" {
		return "", false
	}
	return t.Text, true
}

func punct(value ast.TokenOrTree, kind token.Kind) bool {
	return value.Token != nil && value.Token.Kind == kind
}

// Strip removes only the registration attributes accepted by Collect.
func Strip(entries []*Entry) {
	for _, entry := range entries {
		attrs := entry.Function.Attrs
		for i, attr := range attrs {
			if attr == entry.Attribute {
				entry.Function.Attrs = append(attrs[:i:i], attrs[i+1:]...)
				break
			}
		}
	}
}
