# Checked reference permissions

This finite prototype implements source access permissions from D08/D21. It adds
no ownership, uniqueness, non-escape, lifetime, race-freedom, Arc/Box protocol, or
native-pointer bridge. `&T` protects a selected access path; `mut& T` does not
exclude other aliases. Existing writable aliases can still modify the object.

`Lower(filename, source)` returns ordinary Go plus reference position, element,
permission, and resolved symbol metadata. Frontend integration uses
`Prepare → derive/propagation → Finish`; the staged API is unchanged. Prepare
retains nominal reference types and write-construction markers. Finish resolves
concrete element types, restores source reference syntax, and performs complete
Go typing and permission checks before erasure. Provisional marker typing is
never final validation. Compiler-reserved identifiers cannot be supplied by
source programs.

## Concrete elements and value copies

Supported concrete elements include built-in scalars, local named structs,
fixed arrays, slices, maps, and ordinary Go pointer fields. Anonymous struct,
reference-bearing fields/containers, and generic reference element syntax remain
outside this slice. New references may appear in direct function parameters,
one unnamed result, and initialized locals. Read/write references preserve
ordinary pointer identity and can be copied, rebound, returned, or captured by a
closure whose signature does not itself carry references.

```hgo
type State struct { N int; Values [2]int; Items []int; Counts map[string]int; P *int }
func inspect(r &State) int {
    v := *r
    v.N++                       // independent value slot
    v.Values[0]++               // independent fixed-array slot
    items := r.Items            // inferred readonly shared-storage view
    // items[0]++               // error
    // v.Items[0]++             // error: copying the struct preserves shared fields
    // w := mut& v              // error: would upgrade those shared fields
    copied := make([]int, len(items))
    copy(copied, items)           // independent scalar element storage
    copied[0]++
    return v.N + items[0] + r.Counts["x"] + *r.P
}
```

Read permission propagates through selectors (including promoted fields), array
and slice indexes, map values and range keys, dereferences, and slice views.
Copying scalars or aggregates of pure values permits writes to the independent
copy. Copying an aggregate with shared fields preserves restrictions only on
those fields. Addressing a restricted field cannot restore write access.
Direct checked read-reference parameters can receive explicit read addresses;
ordinary writable parameters cannot consume a restricted shared projection.

## Qualification representation and finite boundaries

Nominal pointer types encode source reference permissions during analysis. A
structural access-path qualification supplements Go expression types: it records
slot permission, transitive projection permission, and field/element permissions
retained after value copies. Inferred local qualifications are keyed by
`go/types.Object` identity; selector paths use `go/types.Selection`. There is no
source-variable-name taint table or heap-alias inference.

Local binding constraints first converge to a conservative summary; a transitive
`deep` qualifier absorbs nested qualifiers so recursive assignments terminate.
Point facts then permit ordinary local fresh rebinding, simultaneous RHS
assignments, and joins of reachable branch exits. A returning branch does not
contaminate its sibling continuation. For/range loops join entry and backedge
facts to convergence; continue passes through post and break reaches the correct
labelled exit. Conditional loops retain the possible zero-iteration path. Range
source qualifications are frozen at source evaluation, and iteration destinations
and local declarations are updated on each execution, including zero-value vars.
Local variables declared inside a closure use the same rules. Existing aliases
retain their previous qualification. This is a finite intraprocedural refinement,
not whole-program borrow analysis or an inferred effect system.

Captured slots, actual address-taken local slots, and slots mentioned by
switch/select or goto retain the conservative summary. Loops containing those
unsupported switch/select constructs also retain it. Taking a slice element or
pointee address does not expose the local slice header/pointer slot; rebinding
that local is allowed while the old derived address keeps its own qualification. Replacing individual fields with fresh storage
also remains conservative. Rejections involving these inferred slots explicitly
say `prototype tracking limitation` and suggest a separate fresh local. This
message describes an analysis limit, not proof that the particular execution
writes shared storage. Definite readonly writes and permission-erasing boundaries
remain errors; nothing is demoted to a warning. Independent/shadowed local
bindings have distinct identities. Named results and global/interface range
targets cannot erase qualifications.

A qualification is never silently dropped at unsupported boundaries. Restricted
shared projections cannot be put into container/struct literals, ordinary parameters or results, interfaces/any, generic
calls, channels, or function values. Qualified interface, channel, and function
projections themselves are unsupported. Methods on restricted paths are rejected;
no readonly method-effect protocol is inferred. Map comma-ok/tuple flow carrying
restricted projections is rejected rather than losing the value's permission.
Range destinations which cannot preserve qualification are also rejected.

`len` and `cap` can inspect restricted containers. `copy` permits a restricted
source only when its copied elements contain no restricted shared capability;
`append(writable, readonly...)` supports the same value-element rule;
its destination must be writable. `append`, `delete`, `clear`, and other consuming
builtins cannot mutate restricted shared storage. Copying readonly pointer
contents into a new slice remains unsupported; the checker does not claim that a
new outer slice grants write access to the original pointer targets.

Aliases/defined reference types, nested new references, references in generic
arguments (including phantom arguments), multiple reference results, named
reference result slots, reference callbacks/function values, globals, zero/nil
reference initialization, raw-pointer adoption, and explicit permission-erasing
conversions remain rejected. Native imported concrete types with unusual import
aliases have not been separately qualified as supported. Pure value copies can
cross ordinary Go boundaries; methods may return fresh objects without having
all their results mechanically marked readonly.

## Mixed-host compatibility and verification

Only an explicit reference expectation or address of an already typed reference
makes ordinary `&value` a new read-reference constructor. Bare host-Go addresses
remain raw pointers, but addresses derived from a restricted projection retain
that path's qualification. Lexically bound `mut` identifiers preserve normal
bitwise-AND behavior. Files with no new reference syntax are unchanged.

There is no runtime permission or nil guard on generated Go entry points;
unverified Go callers can write through aliases. Physical allocation is decided
by Go. All final generated declarations are typechecked, including derive and
propagation output; unavailable imports are not ignored.

Tests in `composite_test.go` cover concrete elements, value/shared copying,
fixed-point restrictions and supported scalar copying. Frontend tests cover
propagation/derive composition. Independent acceptance tests exercise generated
modules with `go test -race`, writable-alias visibility, and permission-erasure
counterexamples. These executions are not a whole-program race-freedom proof.


Directory compilation uses `FinishPackage`: each file is checked against shared
nominal reference declarations before any permissions are erased. File-local
imports remain separate. Every body (including native Go consumers) must pass its
own permission check; companions provide signatures, not permission-erased Go
stubs. Exported concrete reference signatures now use the build-local metadata protocol
below. Other public element/signature forms remain engineering gaps. Ordinary Go pointer APIs remain ordinary
pointer boundaries and cannot silently consume checked references.


## Public concrete reference metadata

Directory builds publish internal v2 metadata only after complete provider checks.
`Metadata.Path` is the actual import path, and each package function records every
parameter/result Go type and each reference permission. Ordinary slots and reference elements have
transparent generated aliases, so private and transitive types retain identity.
Consumers resolve package function objects (not selector spellings or methods),
validate the marker digest, version, path, complete signature and alias identity,
and temporarily use local nominal analysis signatures. Generated analysis
functions are removed by AST spans after checking. Imports and source identifiers
retain scope; generated names avoid identifiers in every selected file.

Checks include native `.go` consumers without new reference syntax. Returned read
references retain permissions through local inference and checked forwarding;
ordinary pointer/interface/closure returns cannot erase them. Existing writable
aliases remain usable. Native unmarked pointer APIs stay native APIs; D11 bridge
work is not implemented by pretending a checked reference is a raw pointer.

Generated packages contain `HgoReferenceMetadata` and `.hgo.refs.json`. The JSON
is inspectable output, **not an external metadata input format**. Only the current
module build's registry is consumed. A marked dependency without matching trusted
build metadata is diagnosed, including in single-file compilation with no new
reference syntax. Single-file exported-reference output also carries a marker,
with internal identity `hgo/generated`; it does not enable cross-build recovery.
This is not a stable public ABI or cryptographic provenance mechanism.

Supported elements include builtin scalars, non-generic named concrete types,
structs, slices, maps, arrays and recursively composed pointers. Generic nominal
read/write origins parameterized by actual element types preserve Go identity:
alias spellings unify, defined types and different package paths do not. Concrete
analysis aliases have per-file namespaces; generated element aliases preserve
private/re-exported types and are removed only by recorded declaration identity.
All analysis types erase to ordinary pointers, without runtime wrappers.

The public signature subset remains ordinary exported functions and one unnamed
result. Interface/channel/function/generic elements, reference methods, generics,
callbacks/function values, reference-containing containers, dot imports,
named/multiple results and external-source/precompiled metadata recovery remain
incomplete. A reference to a supported container is distinct from a container of
references. Existing projection restrictions remain; this metadata extension does
not claim unrestricted composite operations or method-effect inference.

These checks apply to files processed by hgo. Native `go build` of generated
pointers does not enforce hgo permissions. Other writable aliases and native calls
on ordinary pointers remain valid. New import/function-value diagnostics preserve
proven source spans; downstream reference reconstruction can remain
`generated/unmapped`. See RFC 0005 §6.15–6.16 and the separate generated-module
race acceptance tests.

The real CLI cross-package acceptance in `refs_control_flow_test.go` exercises
local loop/branch rebinding, value copies across ordinary calls, element-address
identity, labelled exits, and per-iteration zero initialization. Its generated
module runs race/vet separately. Loop-carried readonly values, zero-trip exits,
old derived pointers, captured/header aliases, and interface erasure remain
errors. This is local permission typing, not heap-alias risk analysis.
