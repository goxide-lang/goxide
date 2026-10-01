# Scalar reference prototype

This pass implements a deliberately finite part of D21, not the complete readonly
projection model of RFC 0003. It adds no ownership, uniqueness, non-escape,
thread-safety, or lifetime proof, and no warning analyzer. The syntax is prototype
syntax, not approval of any pending Arc/Box/AsPtr protocol.

`Lower(filename, source) (Result, error)` returns lowered `Source` plus source
`References` records containing an element type, permission, position and, for
identifier uses, a symbol identity with declaration position. Integration order:
macro expansion, enum lowering, `Prepare`, derive generation, propagation,
`Finish`, final Go checking. `Prepare` emits compiler-only nominal permission
types and a write-construction marker. `Finish` resolves marker scalar elements,
restores reference syntax, and runs the full type and permission checker before
erasing permissions. Its provisional type information is never validation;
unresolved marker elements and all final type errors are rejected. Both staged
APIs are compiler internals; only the frontend may supply their generated names.
The semantic checking itself uses `go/types` objects and expression types, not a
variable-name taint map. Distinct named pointer types represent read/write access
only in the analysis IR; every supported consuming context is separately checked
because Go permits assignments between named pointers and unnamed `*T`.

```hgo
func create() mut& int { n := 4; return mut& n }
func read(r &int) int { return *r }
func increment(w mut& int) { (*w)++ }
func example() int {
    w := create()
    copy := w                 // ordinary shared pointer identity, not unique ownership
    increment(copy)
    r := &(*w)                // explicit narrowing; also valid on an existing read ref
    value := *r               // independent scalar copy can be modified
    value++
    var other &int = &value    // contextual read-reference creation
    increment(w)              // allowed: r does not freeze the whole object
    return read(r) + read(other)
}
```

After checking, signatures become `*int`, `mut& n` becomes `&n`, and `&(*w)`
becomes the corresponding ordinary Go address expression. Returned local storage
is kept alive by Go's normal semantics; physical stack/heap placement is not a
language promise. Copies and rebinding preserve the nominal source permission.
Read access does not imply global immutability. Write access does not imply
exclusive access. A scalar-only closure may capture references and its body is
checked normally; closures with reference parameters or reference results are
outside this slice. `go` and `defer` direct checked calls retain ordinary Go
semantics; no race-free or join/escape claim is made.

Supported elements are predeclared bool, string, integer, floating-point and
complex scalars, including byte/rune aliases. Explicit parameter types, one
unnamed result, initialized local `var`, inferred locals, assignments, direct
local calls, dereference reads/writes, increment/decrement, compound assignment,
and range assignment destinations are checked. Independent same-name locals
have independent identities. Discarding a reference with `_ = r` is allowed.

Only an explicit read-reference expectation or dereferencing an already typed
reference makes ordinary `&value` a read-reference constructor. A bare
`raw := &value` stays a host Go raw pointer. A lexically bound Go variable named
`mut`, or a selector field named `mut`, keeps its host meaning in `mut & n`.
This compatibility rule is for the mixed prototype, not the final grammar.
Host files without new reference syntax are returned byte-for-byte unchanged.

Errors include writing through a read reference, read-to-write promotion, and
implicit conversion to a raw pointer or `any`. Examples:

```hgo
func bad(r &int) {
    *r += 1                  // error: read permission
    w := mut& *r             // error: cannot create write access from read access
    var raw *int = r         // error: raw boundary would erase permission
    print(r)                 // unsupported: builtin consumes reference itself
    // print(*r) uses a copied scalar and is fine.
}
```

The following forms explicitly remain unsupported when they carry a new
reference: compound elements/projections, nested references, structs and
containers of references, aliases/defined reference types, methods, reference
function values/callback signatures, generic calls, interfaces, comparisons,
multiple-result reference signatures/tuple flow, package globals, uninitialized
references, nil/adoption/conversions, and named reference result slots. These are
implementation coverage limits, not borrow-risk errors. In particular the
prototype has not decided the language's full nil invariant. Lowered Go entry
points have no added nil/permission guard: unchecked Go callers can pass nil or
write through their own aliases. Source checking is not a guarantee about those
callers. It rejects an
unsupported zero-value reference rather than silently selecting one.
No ordinary Go call is restricted just for lacking an effect summary. A call
with a *new reference value* requires an actual compatible checked signature;
D11's pending native bridge is not automatically enabled. Native calls on
independent raw pointers and copied scalars remain ordinary Go. Macro definition-site
bindings that turn a reference-signature function into a package-level function
value remain unsupported; direct checked calls work, and macros may consume
dereferenced scalar values. This boundary reports the existing package/global
reference or reference-function-value diagnostic.

The pass typechecks the whole reference-bearing host file with the Go importer
(the frontend supplies its module-aware importer). Derive helpers are available
before checking, and scalar reference parameters/locals compose with ordinary
Option/Result payloads and `?`, including direct deferred calls and scalar closure
captures. Option/Result reference payloads remain explicitly unsupported; no
container permission protocol or borrow analysis is added. Generic type arguments
are checked even for phantom parameters. Unavailable package export data and
unresolved types still produce errors; the pass never ignores final type errors. Derive attribute lines are preserved, and raw strings/comments
that resemble attributes are not stripped. Reference-bearing files currently
reject rebinding/shadowing predeclared scalar names. Diagnostic positions map to
original input offsets; this is not a precise macro-origin source map.

Run `go test ./internal/refs` with the actual Go compiler on PATH. Tests include a
real lowered module executed with `go run -race .`; independent acceptance tests
also run generated modules with `go test -race`. They exercise the executed
paths, not all possible concurrent executions or a full source permission proof.

Staged diagnostics currently point into the generated input after derive and
propagation, rather than precise original source columns. Frontend combination
tests cover permission rejection after propagation and derive availability.
