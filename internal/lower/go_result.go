package lower

import (
	"crypto/sha256"
	"fmt"
	goast "go/ast"
	gotoken "go/token"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (e *emitter) goResult(call *syntax.CallExpr, adapter *check.GoResult) goast.Expr {
	// The native call stays at its original call site. In particular, recover()
	// and question propagation must not move into the adapter's function body.
	native := e.expr(adapter.Call)
	hash := sha256.Sum256([]byte(e.name))
	helper := e.fresh(fmt.Sprintf("Result%x_", hash[:4]))
	value, failure := goast.NewIdent("value"), goast.NewIdent("failure")
	parameters := []*goast.Field{}
	if !adapter.Unit {
		parameters = append(parameters, &goast.Field{Names: []*goast.Ident{value}, Type: e.goType(adapter.Value)})
	}
	parameters = append(parameters, &goast.Field{Names: []*goast.Ident{failure}, Type: e.goType(adapter.Error)})
	var success goast.Expr = value
	if adapter.Unit {
		success = &goast.CompositeLit{Type: e.goType(adapter.Value)}
	}
	var rejected goast.Expr = failure
	if adapter.Keep {
		rejected = &goast.CompositeLit{Type: e.goType(adapter.Payload), Elts: []goast.Expr{
			&goast.KeyValueExpr{Key: goast.NewIdent("Value"), Value: success},
			&goast.KeyValueExpr{Key: goast.NewIdent("Err"), Value: failure},
		}}
	}
	construct := func(name string, payload goast.Expr) goast.Stmt {
		return &goast.ReturnStmt{Results: []goast.Expr{&goast.CallExpr{
			Fun: &goast.IndexListExpr{X: e.sumMember(name), Indices: []goast.Expr{e.goType(adapter.Value), e.goType(adapter.Payload)}}, Args: []goast.Expr{payload},
		}}}
	}
	e.resultAdapters = append(e.resultAdapters, &goast.FuncDecl{Name: helper,
		Type: &goast.FuncType{Params: &goast.FieldList{List: parameters}, Results: &goast.FieldList{List: []*goast.Field{{Type: e.goType(adapter.Result)}}}},
		Body: &goast.BlockStmt{List: []goast.Stmt{
			&goast.IfStmt{Cond: &goast.BinaryExpr{X: failure, Op: gotoken.NEQ, Y: goast.NewIdent("nil")}, Body: &goast.BlockStmt{List: []goast.Stmt{construct("NewErr", rejected)}}},
			construct("NewOk", success),
		}},
	})
	return &goast.CallExpr{Fun: helper, Args: []goast.Expr{native}}
}
