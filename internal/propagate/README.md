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
lexical function, including function literals. It supports assignments with one or multiple RHS expressions, including ordinary indirect destinations,
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
select candidates, and unevaluated
unsafe.Sizeof/Alignof/Offsetof operands. Use a preceding binding or explicit
match; these contexts are rejected rather than silently reordered. No public
Try/Residual, cross-family coercion, or risk/borrow analysis is introduced.

Diagnostics carry parser positions in the lowered input; preserving exact original
columns through macro, enum and marker rewrites remains the frontend source-map
limitation. Ordinary paths insert no runtime closure. The private embedded-pointer assignment fallback below uses typed question helpers while preserving the source expression frame. Tests execute generated
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
Directory modules using that path need the same explicit goxide module dependency
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


## Assignment places

The place pass freezes pointer/slice/map containers and indexes before RHS
propagation, preserving the final Go assignment and its left-to-right write
phase. Struct/array destination paths retain storage without taking an early
address of the full place. Final bounds/nil/map checks therefore remain in the
write phase; reading an intermediate pointer or container can still panic during
operand evaluation. Failed propagation skips destination writes, not user side
effects already performed. Compound assignments retain their final old-value
read. Tuple and comma-ok RHS remain intact.

Context witnesses apply only when the destination/key and expression have the
same static type; interface assignment and differently typed shift counts retain
their expression types. Ordinary readonly/writable checks still run after this
pass. No borrowing, exclusivity or global immutability guarantee is added.

Promoted fields through value embeddings use the ordinary place path. Imported
private embedded-pointer targets first use the bounded effects planner, retaining
the complete final assignment and returning normally on propagation failure.
The original syntax/type facts distinguish untyped contextual expressions from
named private scalars. Unsupported combinations use the explicit legacy frame
path described below, which preserves the complete original assignment,
leaving hidden pointer operands, contextual private scalar types, and final
writes to Go. Question helpers take source operands as arguments evaluated in
the original frame, preserving direct recover. A compiler-support Assignment
guard, deferred directly in the source function, catches only its own pending
failure token and fills the original return slot before prior source defers run.
Ordinary panics, invalid sums, Goexit and legacy panic(nil) stay outside this
protocol. No reflection, unsafe or producer cooperation is used.

The return-slot pointer is bound at function entry to avoid source shadowing.
Guards live in ordinary blocks so their declarations do not obstruct forward
goto. Original illegal bare returns remain rejected when naming an unnamed
result. Source closures retain their own targets. This fallback adds helper/defer
cost and internal panic on failure, and requires an explicit dependency on the
github.com/hxsf/goxide module for its runtime package. Guards remain until function exit;
this is neither a stable ABI nor a zero-cost claim.

Go specifies required call/receive/logical ordering and two-phase assignment,
not that all implicit pointer reads precede every RHS call. Tests permit the
allowed read-order results while requiring phase-one locations to survive earlier
phase-two writes. Loops/switch and unevaluated unsafe propagation remain separate
work; this fallback does not expand those contexts.


`question.go` separates one already-lowered operand's validation, payload and
failure conversion from its failure exit. Ordinary `expr` emits a return;
`frameExpr` explicitly emits guard.Fail without scanning generated return nodes.
Source operand scheduling is still owned by each caller. Branch containers are
fresh but expression nodes are shared read-only; emit is not a deep-clone API.
This is preparatory refactoring, not the no-panic private-target replacement.
That was the questionFlow-only stage. The subsequent effects planner now removes
guards from the existing private.hgo acceptance set and core benchmark fixtures;
unsupported combinations still take assignmentFrame, so universal private-target
performance acceptance remains open. See RFC 0005 section 6.9 for the exact scope.


The second effects group adds static non-generic/non-variadic function-result
witnesses for private logical types, native negative-count checks for contextual
shifts, and len/cap operands through the effects planner itself. Explicit array
and slice literals are evaluated before measuring them. We do not route these
through legacy expr, which can default private call-argument bool/mask types.
Tuple/unsafe and the unplanned witness/operand shapes remain explicit fallbacks;
RFC section 6.10 records those boundaries and the ordinary-expr gap separately.
