# Explicit equality lowering

`Prepare` checks declarations, derives concrete `EqualsT(a,b T) bool` helpers,
and resolves known protocol calls. `Lower` runs after propagation, resolves deferred
receivers using actual types, and removes temporary typing intrinsics. Ordinary
native methods are preserved and never imply a registered protocol. Native source
bytes remain unchanged. Package bindings protect generated helper selection from
caller locals.

Eq requires explicit PartialEq plus recursively qualified fields. Bool, integers
and strings support ordinary scalar Eq; floats/complex retain PartialEq only.
Structs and arrays recurse, named types require registration, and enums validate
both inputs left-to-right after argument evaluation before comparing active cases.
Scalar calls keep a typed bool result. No general trait, manual implementation,
container/deep-pointer protocol or HashMap is implemented.

The frontend separately rejects direct enum equality and known dangerous enum
comparisons/map keys in hgo source; native Go semantics and the existing nil
exception remain. Metadata v3 carries exact target/helper identity and Eq status.
A helper's name alone never authorizes a protocol. See
[real module tests](../../tests/acceptance/equality_test.go).
