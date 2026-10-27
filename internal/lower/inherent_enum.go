package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"strconv"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func nativeEnumMember(member *check.Member) bool {
	return member != nil && member.Trait == nil && !member.Static && member.Receiver != check.WriteRef && check.CanUseEnumReceiver(implTemplate(member.Impl))
}

func enumReadOperation(member *check.Member) string {
	name := "GoxideRead" + member.GoName
	if member.Symbol != nil && member.Symbol.Visibility != resolve.Public {
		name = privateName(name)
	}
	return name
}

func (e *emitter) enumInherentSnapshot(member *check.Member, receiver goast.Expr) ([]goast.Stmt, goast.Expr) {
	if member.Trait != nil || member.Static {
		return nil, receiver
	}
	instance := e.info.EnumOf(member.Impl.Target)
	if instance == nil {
		return nil, receiver
	}
	var prefix []goast.Stmt
	value := receiver
	if member.Receiver == check.ReadRef {
		snapshot := e.fresh("Snapshot")
		prefix = append(prefix, varStmt(snapshot, e.goType(member.Impl.Target), &goast.StarExpr{X: receiver}))
		value = snapshot
		receiver = &goast.UnaryExpr{Op: gotoken.AND, X: snapshot}
	}
	valid := &goast.CallExpr{Fun: e.enumValidFunction(instance), Args: []goast.Expr{value}}
	prefix = append(prefix, &goast.IfStmt{Cond: &goast.UnaryExpr{Op: gotoken.NOT, X: valid}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ExprStmt{X: &goast.CallExpr{Fun: goast.NewIdent("panic"), Args: []goast.Expr{&goast.BasicLit{Kind: gotoken.STRING, Value: strconv.Quote("goxide: invalid enum " + instance.Enum.Name)}}}}}}})
	return prefix, receiver
}

func (e *emitter) guardEnumInherentBody(member *check.Member, body *goast.BlockStmt) {
	if member.Static || member.Trait != nil {
		return
	}
	self := e.ident(member.Decl.Params[0].Names[0])
	prefix, snapshot := e.enumInherentSnapshot(member, self)
	if snapshot != self {
		prefix = append(prefix, &goast.AssignStmt{Lhs: []goast.Expr{self}, Tok: gotoken.ASSIGN, Rhs: []goast.Expr{snapshot}})
	}
	body.List = append(prefix, body.List...)
}

func (e *emitter) enumInherentMethods(target *types.Named) []*goast.Field {
	var fields []*goast.Field
	for _, impl := range e.traits.impls {
		if impl.Base != target || impl.Trait != nil {
			continue
		}
		for _, member := range impl.Methods {
			if !nativeEnumMember(member) {
				continue
			}
			e.generatedMember(target, member.GoName, impl)
			sub := map[*types.TypeParam]types.Type{}
			for index, parameter := range impl.TypeParams {
				sub[parameter] = target.TypeParams().At(index)
			}
			signature := e.typeState.Substitute(e.info, member.Signature, sub, nil).(*types.Signature)
			typ, _ := e.memberSignature(signature)
			fields = append(fields, &goast.Field{Names: []*goast.Ident{goast.NewIdent(member.GoName)}, Type: typ})
		}
	}
	return fields
}

func (e *emitter) enumInherentDeclarations(member *check.Member) []goast.Decl {
	restore := e.memberBodyInfo(member)
	defer restore()
	instance := e.info.EnumOf(member.Impl.Target)
	var declarations []goast.Decl
	for _, variant := range instance.Enum.Variants {
		receiver := e.fresh("Receiver")
		decl := &goast.FuncDecl{Name: goast.NewIdent(member.GoName), Recv: receiverField(receiver.Name, &goast.StarExpr{X: e.variantType(instance, variant)}), Type: &goast.FuncType{Params: e.params(member.Decl.Params[1:]), Results: e.params(member.Decl.Results)}}
		decl.Body = e.functionBody(decl.Type, member.Decl.Body)
		source := member.Decl.Params[0].Names[0]
		symbol := e.file.Definitions[source.ID()]
		used := false
		syntax.Walk(member.Decl.Body, func(node syntax.Node) bool {
			if id, ok := node.(*syntax.Ident); ok && e.file.References[id.ID()].Symbol == symbol && symbol != nil {
				used = true
			}
			return !used
		})
		if source.Name != "_" && used {
			self := e.ident(source)
			if member.Receiver == check.ReadRef {
				snapshot := e.fresh("Snapshot")
				decl.Body.List = append([]goast.Stmt{varStmt(snapshot, e.goType(member.Impl.Target), receiver), varStmt(self, &goast.StarExpr{X: e.goType(member.Impl.Target)}, &goast.UnaryExpr{Op: gotoken.AND, X: snapshot})}, decl.Body.List...)
			} else {
				decl.Body.List = append([]goast.Stmt{varStmt(self, e.goType(member.Impl.Target), receiver)}, decl.Body.List...)
			}
		}
		// A typed-nil variant must never enter the user's method body, even
		// when that body does not inspect self.
		invalid := &goast.IfStmt{Cond: &goast.BinaryExpr{X: receiver, Op: gotoken.EQL, Y: goast.NewIdent("nil")}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ExprStmt{X: &goast.CallExpr{Fun: goast.NewIdent("panic"), Args: []goast.Expr{&goast.BasicLit{Kind: gotoken.STRING, Value: strconv.Quote("goxide: invalid enum " + instance.Enum.Name)}}}}}}}
		decl.Body.List = append([]goast.Stmt{invalid}, decl.Body.List...)
		declarations = append(declarations, e.traitSourceBody(decl, member.Decl))
	}
	if e.traits.enumReadExpressions[member.Decl] {
		declarations = append(declarations, e.enumReadExpressionDeclaration(member))
	}
	return declarations
}

// A source method expression accepts &E, whereas an interface method expression
// accepts E. This demanded carrier entry contains the original body, rather than
// forwarding to a method and adding a frame around recover.
func (e *emitter) enumReadExpressionDeclaration(member *check.Member) goast.Decl {
	name := enumReadOperation(member)
	for _, impl := range e.traits.impls {
		if impl.Trait != nil || e.inherentCarrier(impl) != e.inherentCarrier(member.Impl) {
			continue
		}
		for _, other := range impl.Methods {
			if other.Static && other.GoName == name {
				e.fail(member.Decl, "生成的 Go carrier 方法 "+name+" 与固有方法冲突")
			}
		}
	}
	decl := &goast.FuncDecl{Name: goast.NewIdent(name), Recv: receiverField("", e.inherentCarrierType(member.Impl)), Type: &goast.FuncType{Params: e.params(member.Decl.Params), Results: e.params(member.Decl.Results)}}
	decl.Body = e.functionBody(decl.Type, member.Decl.Body)
	e.guardEnumInherentBody(member, decl.Body)
	return e.traitSourceBody(decl, member.Decl)
}
