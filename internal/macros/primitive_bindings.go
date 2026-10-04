package macros

import "fmt"

func primitiveName(name string) bool {
	switch name {
	case "bool", "string", "byte", "rune", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128", "any", "error", "true", "false":
		return true
	}
	return false
}

// Primitive aliases are emitted at package scope in the caller's file. They
// bypass caller locals, but cannot bypass package declarations or file imports.
// Reject those collisions rather than silently rebinding the definition token.
// Likewise, a primitive spelling in the definition is not proof that it denotes
// a predeclared object. Other definition-site objects require explicit captures.
func (e *engine) checkPrimitiveBinding(t tok) error {
	if e.builtinImportShadow(t.text) {
		return at(t, fmt.Sprintf("template primitive %s names a definition-site import, not a predeclared object; pass it as an explicit capture", t.text))
	}
	if e.definitionNames()[t.text] {
		return at(t, fmt.Sprintf("template primitive %s names a definition-site declaration, not a predeclared object; pass it as an explicit capture", t.text))
	}
	if e.callerNames()[t.text] {
		return at(t, fmt.Sprintf("template primitive %s is shadowed by a caller package declaration; definition-site binding is unsupported for this collision", t.text))
	}
	caller := *e
	caller.active = nil
	if e.pkg != nil {
		caller.active = e.pkg.scopes[e.file]
	}
	if caller.builtinImportShadow(t.text) {
		return at(t, fmt.Sprintf("template primitive %s is shadowed by a caller file import; definition-site binding is unsupported for this collision", t.text))
	}
	return nil
}
