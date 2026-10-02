package refs

import (
	"go/ast"
	"go/types"
)

// nativeWriteArgument permits a writable scalar view at an ordinary Go pointer
// parameter. It is deliberately a call-argument rule, not general permission
// erasure into assignments, interfaces, or composite representations. The Go
// callee may retain the pointer; no lifetime or exclusive-access claim is made.
func (c *checker) nativeWriteArgument(dst types.Type, src ast.Expr) bool {
	source := c.typ(src)
	ref, ok := c.refType(source)
	if !ok || ref.Permission != Write {
		return false
	}
	pointer, ok := types.Unalias(dst).(*types.Pointer)
	if !ok {
		return false
	}
	element := referenceTarget(source)
	basic, ok := element.Underlying().(*types.Basic)
	if !ok || scalar(basic.Name()) == "" {
		return false
	}
	return types.Identical(element, pointer.Elem()) && types.AssignableTo(source, dst)
}
