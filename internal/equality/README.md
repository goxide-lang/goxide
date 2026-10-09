# Explicit equality lowering

`Prepare` checks declarations, derives concrete `EqualsT(a,b T) bool` helpers,
and resolves known protocol calls. `Lower` runs after propagation, resolves deferred
receivers using actual types, and removes temporary typing intrinsics. Ordinary
native methods are preserved and never imply a registered protocol. Native source
bytes remain unchanged. Imported package bindings protect imported helper selection. Local helpers are
called directly, with colliding variables/constants renamed by Go object identity.
Local defined type names are preserved (including reflection): a function-local
helper binding before their declarations avoids capture. No package function-value
binding or extra call frame is introduced for recursive manual bodies.

Both AST passes capture per-file source evidence before mutation. Changed files
print through that snapshot: unchanged supported AST-owned tokens keep their
point origins and macro stacks even when a derived helper is appended. Files
without AST changes retain whole-token composition, including punctuation not
covered by AST token slots. Rewritten call identifiers carry only the
stack proven by the original `equals` identifier (or deferred intrinsic), with
no invented point origin. Shared helpers, helper bindings and manual enum
validation prefixes have no single macro caller and remain generated/unmapped.

Derived Eq requires explicit PartialEq plus recursively qualified fields. Bool, integers
and strings support ordinary scalar Eq; floats/complex retain PartialEq only.
Structs and arrays recurse, named types require registration, and enums validate
both inputs left-to-right after argument evaluation before comparing active cases.
Scalar calls keep a typed bool result. No general trait, deep-pointer protocol or HashMap is implemented.

The frontend separately rejects direct enum equality and known dangerous enum
comparisons/map keys in goxide source; native Go semantics and the existing nil
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

Concrete manual implementations register `ManualPartialEq` / `ManualEq` alongside
merged `PartialEq` / `Eq` capabilities. A manual PartialEq implementation is the
existing local `func EqualsT(T,T) bool`, validated by actual Go types; no second
implementation or forwarding function is generated. For enums, validators are
inserted at the beginning of this public function, left then right, preserving
its function frame (including direct `recover` in deferred calls). Both caller
arguments are still evaluated before entering the function.

Manual Eq is an explicit author promise. Combined with derived PartialEq, helper
generation checks only PartialEq requirements, even for float-containing values.
Derived Eq over manual PartialEq is rejected because the compiler cannot prove
the custom relation; explicit manual Eq is required instead. Derived aggregate
fields invoke registered manual helpers. Manual PartialEq cannot prove native
map-key consistency, including when nested inside a structural key; Manual Eq
alone does not weaken the existing structural proof. This stage supports concrete
scalar/struct/array/slice/map/enum targets, not generic or alias implementations.
