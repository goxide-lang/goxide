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
deep-pointer protocol or HashMap is implemented.

The frontend separately rejects direct enum equality and known dangerous enum
comparisons/map keys in hgo source; native Go semantics and the existing nil
exception remain. Metadata v3 carries exact target/helper identity and Eq status.
A helper's name alone never authorizes a protocol. See
[real module tests](../../tests/acceptance/equality_test.go).


Slice and map fields now compare container contents through element/value
protocols. Two nil containers compare equal; nil and nonnil empty containers do
not. Nil/length checks and element comparisons short-circuit. Equal backing
storage does not bypass element comparison (including float NaN). These rules
also apply recursively to named containers with explicit derives.

Map keys must have a proven match between protocol equality and native Go lookup:
bool, integer and string keys, arrays/structs composed of these, and locally
derived named forms. Floats, complex numbers, pointers, channels, interfaces and
enums are rejected as protocol map keys. Imported named protocol helpers carry no
native-key consistency proof yet, so they cannot silently authorize map keys.
Ordinary native Go map operations are not changed by this derive restriction.

Registered concrete Option/Result fields receive conditional comparison through
actual type identity and the matching producer validator. Both operands validate
left-to-right before variant comparison; Option None, Some(nil), and Some(empty)
remain distinct. Payloads recurse using their own protocols. Across files,
import aliases are resolved in the helper's source scope and filtered by canonical
family package identity. Unnameable imported payloads still require a supported
public type alias. This does not add direct anonymous container/sum `.equals`
syntax or infer protocols from type names.

Container checks preserve field/element short-circuiting; unvisited elements do
not acquire whole-object prevalidation. Go map iteration order is unspecified.
Recursive container protocol dependencies are diagnosed: legal Go slice/map
values can contain cycles, and this stage does not define a visited-object or
cyclic graph equality policy. Package-level `len`/`nil` shadowing is diagnosed
where generated container operations need those predeclared names, rather than
silently changing comparison semantics. These are implementation boundaries,
not a claim that all remaining forms have been approved for permanent exclusion.
