package lower

import (
	goast "go/ast"

	"github.com/goxide-lang/goxide/internal/check"
)

// The checker owns configuration and member selection. A Go forwarding method
// passes its receiver directly to the selected implementation, preserving a
// borrowed receiver's storage identity.
func (e *emitter) sugarDeclarations() []goast.Decl {
	var declarations []goast.Decl
	for _, member := range e.info.Sugar {
		if member.Impl.File != e.name {
			continue
		}
		signature, arguments := e.memberSignature(member.Signature)
		receiver := e.fresh("Receiver")
		arguments = append([]goast.Expr{receiver}, arguments...)
		selection := &check.MemberSelection{Member: member.Member, Impl: member.Impl, Signature: member.Signature}
		declarations = append(declarations, &goast.FuncDecl{
			Name: goast.NewIdent(member.Name),
			Recv: receiverField(receiver.Name, e.goType(member.Receiver)),
			Type: signature,
			Body: forwardBody(e.operationFunction(selection), arguments, member.Signature),
		})
	}
	return declarations
}
