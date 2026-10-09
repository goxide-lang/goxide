// Package paths represents source static paths and explicit dependency namespaces.
// Namespace membership comes from a manifest/export table, never directory search.
package paths

import (
	"fmt"
	"go/scanner"
	"go/token"
)

type Roots map[string]Namespace

type UnboundError struct{ Name string }

func (e *UnboundError) Error() string { return "unknown static path binding " + e.Name }

type Namespace struct {
	PackagePath string               `json:"package,omitempty"`
	ModulePath  string               `json:"module,omitempty"`
	Source      string               `json:"source,omitempty"`
	Children    map[string]Namespace `json:"children,omitempty"`
}

type Binding struct {
	Kind, OwnerPath, Name, RoutePath, RouteName string
	Namespace                                   *Namespace
}

type Segment struct {
	Name       string
	Start, End int
}

type Path struct {
	Segments []Segment
}

func (p Path) Names() []string {
	names := make([]string, len(p.Segments))
	for i, s := range p.Segments {
		names[i] = s.Name
	}
	return names
}

func ValidIdentifier(name string) bool {
	if !token.IsIdentifier(name) || name == "_" {
		return false
	}
	switch name {
	case "use", "mod", "pub", "struct", "enum", "match", "impl", "trait", "macro_rules", "as":
		return false
	}
	return true
}

// Parse accepts one source static path. Whitespace/comments do not change its
// segments; package/member dots and expression syntax are not path syntax.
func Parse(text string) (Path, error) {
	fs := token.NewFileSet()
	f := fs.AddFile("path", -1, len(text))
	var scan scanner.Scanner
	bad := false
	scan.Init(f, []byte(text), func(token.Position, string) { bad = true }, 0)
	var path Path
	needName := true
	for {
		pos, kind, name := scan.Scan()
		if kind == token.SEMICOLON && name == "\n" {
			_, kind, _ = scan.Scan()
			if kind != token.EOF {
				return Path{}, fmt.Errorf("invalid static path %q", text)
			}
		}
		if kind == token.EOF {
			break
		}
		if needName {
			if kind != token.IDENT || !ValidIdentifier(name) {
				return Path{}, fmt.Errorf("invalid static path %q", text)
			}
			start := f.Offset(pos)
			path.Segments = append(path.Segments, Segment{name, start, start + len(name)})
			needName = false
			continue
		}
		_, next, _ := scan.Scan()
		if kind != token.COLON || next != token.COLON {
			return Path{}, fmt.Errorf("static paths use '::': %s", text)
		}
		needName = true
	}
	if bad || needName || len(path.Segments) == 0 {
		return Path{}, fmt.Errorf("invalid static path %q", text)
	}
	return path, nil
}

// GoView provides a same-length Go parser view; it is not name resolution or
// authorization. Strings and comments are left untouched. Callers retain the
// original tokens and must validate each selector's source intent separately.
func GoView(source []byte) []byte {
	out := append([]byte(nil), source...)
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(source))
	var scan scanner.Scanner
	scan.Init(f, source, func(token.Position, string) {}, 0)
	prior := -1
	for {
		pos, kind, _ := scan.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.COLON {
			off := f.Offset(pos)
			if prior >= 0 {
				out[prior], out[off] = '.', ' '
				prior = -1
			} else {
				prior = off
			}
		} else {
			prior = -1
		}
	}
	return out
}
