package lower

import (
	"sort"

	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func addStandardPrelude(file *syntax.File, config moduleRootConfig, packages map[string]map[string]bool) {
	const standard = "github.com/goxide-lang/std"
	if file.NativeGo || config.module == standard || config.roots["crate"].ModulePath == standard {
		return
	}
	for _, item := range file.Items {
		if use, ok := item.(*syntax.UseDecl); ok && use.Implicit {
			return
		}
	}
	var aliases []string
	for alias, root := range config.roots {
		if root.ModulePath == standard && packages[root.PackagePath+"/prelude"] != nil {
			aliases = append(aliases, alias)
		}
	}
	if len(aliases) == 0 {
		return
	}
	sort.Strings(aliases)
	file.Items = append(file.Items, &syntax.UseDecl{Implicit: true, Tree: &syntax.UseTree{
		Glob: true,
		Path: &syntax.Path{Segments: []*syntax.PathSegment{
			{Name: &syntax.Ident{Name: aliases[0]}},
			{Name: &syntax.Ident{Name: "prelude"}},
		}},
	}})
}
