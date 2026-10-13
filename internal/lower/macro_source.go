package lower

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

type moduleMacros struct {
	files    map[string]*syntax.File
	resolved *resolve.Result
}

func (m *moduleMacros) LookupMacro(path *syntax.Path, scope expand.Scope) (*syntax.MacroRulesDecl, *syntax.File, error) {
	var source *resolve.File
	for name, file := range m.files {
		if file == scope.File {
			source = m.resolved.Files[name]
			break
		}
	}
	if source == nil {
		return nil, nil, fmt.Errorf("macro definition source is unavailable")
	}
	parts := make([]string, len(path.Segments))
	for i, segment := range path.Segments {
		parts[i] = segment.Name.Name
	}
	directory := filepath.ToSlash(filepath.Dir(source.Name))
	target := directory
	name := parts[len(parts)-1]
	if len(parts) > 1 {
		target = strings.Join(parts[:len(parts)-1], "/")
		for _, use := range source.Uses {
			if !use.Glob && use.Name.Name == parts[0] {
				target = strings.Join(append(append([]string(nil), use.Target...), parts[1:len(parts)-1]...), "/")
				break
			}
		}
	} else {
		for _, use := range source.Uses {
			if !use.Glob && use.Name.Name == name && len(use.Target) > 1 {
				target = strings.Join(use.Target[:len(use.Target)-1], "/")
				name = use.Target[len(use.Target)-1]
				break
			}
		}
	}
	for _, pkg := range m.resolved.Packages {
		if filepath.ToSlash(pkg.Dir) != target {
			continue
		}
		for _, file := range pkg.Files {
			for _, item := range file.AST.Items {
				decl, ok := item.(*syntax.MacroRulesDecl)
				if !ok || decl.Name.Name != name {
					continue
				}
				if target != directory && decl.Vis.Kind != syntax.VisPub {
					return nil, nil, fmt.Errorf("macro %s is private in %s", name, target)
				}
				return decl, file.AST, nil
			}
		}
		return nil, nil, fmt.Errorf("macro %s has no source definition in %s", name, target)
	}
	return nil, nil, fmt.Errorf("macro %s requires source module %s", name, target)
}
