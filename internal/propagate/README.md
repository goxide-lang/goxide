# Dedicated propagation prototype

`Prepare` masks postfix `?` with a compiler-only parsing marker after the frontend
rejects user-written reserved identifiers. The macro pass must explicitly trust
that generated marker. `Lower` runs after enum lowering and derive helper emission.
It accepts only nominal built-in sum metadata supplied by `enums.Lower`, then
removes all temporary typing functions. A caller must perform final complete Go
type checking with the same importer; provisional type checking inside this pass
only resolves operand/return types and is not validation of the whole program.

Success payloads and errors are typed through Go's actual instantiated named
types, resolving metadata with semantic type identity so aliases remain valid. Propagation reconstructs the target sum even when its success type differs.
Error conversion checks Go assignability first, otherwise requires exactly one
registered explicit `Conversion{Source, Target, Helper}`. It never searches chains
or inspects dynamic error variants; ordinary Go interface assignment preserves
typed nil. Selected conversion helpers are bound at package scope under reserved
compiler names, preventing local names from capturing the registered conversion.
Raw Go multi-return calls require explicit user wrapping.

The lowering inserts temporaries, validation, branches and returns in the original
lexical function, including function literals. It supports identifier assignments with one or multiple RHS expressions,
one-spec variable declarations (including multiple values), returns, calls (including `defer` and `go` argument
evaluation), ordinary unary/binary/selector expressions, indexes, slices, composite literals, pointer dereferences, type assertions
(including a single comma-ok RHS), sends, short-circuit booleans,
and `if` initializers and conditions. An initializer and its condition prelude
remain in one lexical block enclosing both branches, including else-if chains. Calls freeze preceding evaluated runtime
arguments before later propagation. Declared function names have no mutable
runtime value and remain direct calls; function variables are still frozen.
Constants retain contextual types. A failing
`defer f(load()?)` does not register that defer, and earlier defers execute normally.

Explicitly unsupported: user generic functions; propagation inside loops, switches,
select candidates, non-identifier assignment destinations, and unevaluated
unsafe.Sizeof/Alignof/Offsetof operands. Use a preceding binding or explicit
match; these contexts are rejected rather than silently reordered. No public
Try/Residual, cross-family coercion, or risk/borrow analysis is introduced.

Diagnostics carry parser positions in the lowered input; preserving exact original
columns through macro, enum and marker rewrites remains the frontend source-map
limitation. No runtime closure is inserted by propagation. Tests execute generated
Go with `go run -race`, including ordering, short circuit, closure and defer cases;
API negatives cover ambiguous/missing/two-hop From. These are compiler tests, not
claims about future general ownership analysis.

## Imported sums in directory compilation

An imported call can be used directly as `dep.Load()?`. Only families and concrete
instances registered by a successfully compiled producer in the same module are
eligible. The nominal Go package path and actual instantiated type must match;
a native Go type named `Result` does not authorize propagation. Concrete exported
validators preserve producer-side nested payload validation, including typed-nil
and forged case rejection. Success/error argument types must be nameable in the
consumer's file import scope; private or otherwise inaccessible argument types
receive an explicit limitation diagnostic. Generated package bindings preserve
protocol identity when a local variable shadows an import or payload type.

From registration remains scoped to the current package, with assignability first
and exactly one explicit, single-step conversion otherwise. Importing a package
does not automatically register its From helpers. No cross-package macro export,
conversion graph search, or generic Try/Residual ABI is introduced.


## Staged values and sends

RHS operands are evaluated in source order before the original simultaneous
assignment; failed propagation performs no destination writes. Earlier runtime
values are captured before a later operand. Constants/nil retain contextual
types. Runtime shifts use typed temporaries, with semantic scalar aliases bound
before local name shadowing and before nested closures are lowered. Generated
package aliases carry the file namespace for directory compilation. This does
not implement user generic functions or a new conversion protocol.

A send captures its channel before evaluating the value, then sends only after
successful propagation. Nil/closed channels retain Go behavior when the send is
reached; failed propagation returns without blocking or sending. Dereference and
assertion failures likewise retain Go panics after successful propagation. A
single comma-ok assertion remains a tuple expression, not a scalar temporary.


## Indexes, slices and aggregate literals

Index and slice operands support array, pointer-to-array, slice, string and map
operations where Go permits them. A single map index remains a comma-ok-capable
expression. Addressable arrays retain their original storage; rvalue arrays keep
non-addressable indexed elements through a compiler-only value helper. An array
returned by a call or propagation cannot be sliced just because lowering created
a temporary variable. Bounds stay in the final Go expression, preserving constant
errors and runtime checks. Missing bounds retain Go length/capacity defaults.

Literal lowering preserves the final struct/array/slice/map syntax, including
constant keys, omitted element types and implicit pointer literals. It stages
runtime leaves and propagation branches, not a new map insertion algorithm.
Private scalar element/field types can be inferred using pure zero witnesses
through the exported container; those helpers never read user storage or call
user methods. When a source declaration shadows comparable, map witness helpers
use the qualified compiler-support constraint in github.com/hxsf/goxide/runtime.
Directory modules using that path need the same explicit runtime module dependency
as other generated runtime imports. This is a lowering detail, not a new source
protocol, stable ABI or zero-cost guarantee.

Tests assert Go's required lexical ordering for calls, receives and logical
operations, not a total order for variable reads, indexing or map insertions.
Nonconstant array len/cap operands remain evaluated even when removing the
propagation marker would otherwise make Go consider the operation constant.
Taking an address is newly supported only for composite literals (including
parentheses), not arbitrary propagation results. Unevaluated unsafe operands are
explicitly rejected: the current statement-prelude lowering cannot preserve their
non-evaluation while simply hoisting propagation. This remains an implementation
gap, not approval to evaluate them or a newly deferred language feature.
