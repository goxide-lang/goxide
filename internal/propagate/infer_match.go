package propagate

import (
	"go/ast"
	"go/types"

	"github.com/hxsf/goxide/internal/enums"
)

// matchedEnum resolves nominal identity through actual Go types and trusted
// source metadata. Underlying representations and short names are insufficient.
func (l *lowering) matchedEnum(t types.Type) (enums.Decl, bool) {
	if t == nil {
		return enums.Decl{}, false
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return enums.Decl{}, false
	}
	for _, d := range l.cfg.Families {
		if d.Family != "" {
			continue
		}
		if d.PackagePath == "" && named.Obj().Pkg() != l.pkg {
			continue
		}
		if d.PackagePath != "" && named.Obj().Pkg().Path() != d.PackagePath {
			continue
		}
		resolved := l.resolveType(d.Name)
		if resolved != nil && types.Identical(named, resolved) {
			return d, true
		}
	}
	return l.family(t)
}

func (l *lowering) inferMatches() error {
	var failure error
	ast.Inspect(l.file, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if !ok || failure != nil {
			return true
		}
		for _, stmt := range block.List {
			es, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := es.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "__goxide_enum_infer" {
				continue
			}
			if len(call.Args) != 1 {
				failure = l.fail(call, "invalid inferred match marker")
				return false
			}
			decl, ok := l.matchedEnum(l.info.TypeOf(call.Args[0]))
			if !ok {
				message := "wildcard-only match target must have a known nominal enum type; native types, interfaces and unregistered instantiations are unsupported"
				if l.familyIssue != "" {
					message = l.familyIssue
				}
				failure = l.fail(call, message)
				return false
			}
			call.Fun = ast.NewIdent(decl.Validator)
		}
		return true
	})
	return failure
}
