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
and `if` initializers and conditions, for headers/bodies, expression/type switches,
and propagated slice/map/string/integer/channel/array/pointer-array range sources. An initializer and its condition prelude
remain in one lexical block enclosing both branches, including else-if chains. Calls freeze preceding evaluated runtime
arguments before later propagation. Declared function names have no mutable
runtime value and remain direct calls; function variables are still frozen.
Constants retain contextual types. A failing
`defer f(load()?)` does not register that defer, and earlier defers execute normally.

Explicitly unsupported: user generic functions; propagation in function-iterator
range sources, range assignment targets, select candidates, and unevaluated
unsafe.Sizeof/Alignof/Offsetof operands. Use a preceding binding or explicit
match; these contexts are rejected rather than silently reordered. No public
Try/Residual, cross-family coercion, or risk/borrow analysis is introduced.

Diagnostics carry parser positions in the lowered input; preserving exact original
columns through macro, enum and marker rewrites remains the frontend source-map
limitation. Ordinary paths insert no runtime closure. The private embedded-pointer assignment fallback below uses typed question helpers while preserving the source expression frame. Tests execute generated
Go with `go run -race`, including ordering, short circuit, closure and defer cases;
API negatives cover ambiguous/missing/two-hop From. These are compiler tests, not
claims about future general ownership analysis.

With a provenance map, AST stages freeze evidence before mutation and transport
only structurally verified output tokens. A question flow registers its fresh
validation, payload and exit scaffold from the original marker identifier's
complete macro context. These generated tokens keep no exact source point. The
already-lowered operand remains the same AST child, with its independent point
and context; it is not printed and reparsed into the scaffold. Ordinary, effect
plan and guard frame consumers use the same flow construction. Shared top-level
aliases/helpers and other temporary factories do not inherit a first-use or
nearby context; unresolved evidence remains generated/unmapped.

## Imported sums in directory compilation

An imported call can be used directly as `dep.Load()?`. Only families and concrete
instances registered by a successfully compiled producer in the same module or
a verified source-backed external artifact are eligible. The nominal Go package path and actual instantiated type must match;
a native Go type named `Result` does not authorize propagation. Concrete exported
validators preserve producer-side nested payload validation, including typed-nil
and forged case rejection. Success/error argument types must be nameable in the
consumer's file import scope; private or otherwise inaccessible argument types
receive an explicit limitation diagnostic. Generated package bindings preserve
protocol identity when a local variable shadows an import or payload type. Trusted
imported validators remain statically identified until their concrete sum type is
known, then use named package-level forwarding functions rather than function-value
aliases. This preserves producer validation and lets Go analyze payload escape;
it does not change the enum ABI or promise allocation-free Result returns.

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
guards from the existing private.gox acceptance set and core benchmark fixtures;
unsupported combinations still take assignmentFrame, so universal private-target
performance acceptance remains open. See RFC 0005 section 6.9 for the exact scope.


The second effects group adds static non-generic/non-variadic function-result
witnesses for private logical types, native negative-count checks for contextual
shifts, and len/cap operands through the effects planner itself. Explicit array
and slice literals are evaluated before measuring them. We do not route these
through legacy expr, which can default private call-argument bool/mask types.
Tuple/unsafe and the unplanned witness/operand shapes remain explicit fallbacks;
RFC section 6.10 records those boundaries and the ordinary-expr gap separately.

The third group uses `effect_multivalue.go` only for checked single-RHS
assignments: static tuple calls and map/assert/receive comma-ok operations.
A single multi-variable declaration retains inferred private return types;
comma-ok alone restores its untyped second boolean result. The final complete
assignment preserves Go's write phases. Nested tuple arguments, dynamic/method
calls, other unplanned targets/operands and unsafe chains remain explicit guard
routes. RFC section 6.11 supersedes the earlier blanket tuple/comma-ok fallback.

`loops.go` lowers for header propagation and slice/map range sources with ordinary
returns in the source function. Native For.Init short declarations remain in the
header to retain Go per-iteration identities. A moved post runs at the next body
entry before the moved condition, after Go's implicit iteration-variable copy.
The original body keeps its block. Loop labels distinguish goto reentry from
break/continue targets; original duplicate/blank-target legality is checked before
renaming. A range source without propagation is left untouched, including native
unevaluated array cases and integer/function iteration. Other propagated range
kinds and per-iteration targets are explicitly unimplemented. Private assignments
that still require a frame guard cannot introduce that guard into this new loop
path; closures retain their own lexical return target. See RFC section 6.12.

`switches.go` plans expression-switch initializer/tag and ordered case effects.
Single-case native comparisons retain Go contextual typing; a final native switch
keeps the original clause/default order and body scopes, including fallthrough.
Original header type errors are retained before case lists are erased. Supported
private case residuals use effects rather than defaulting a partial shift to int.
Unsafe conversion chains with propagation, type-switch propagation, and assignment
shapes needing a frame guard remain explicit control-flow gaps (RFC 6.13).

Type switches now lower initializer and asserted-interface effects while keeping
native guards/case lists, so concrete versus interface case bindings remain Go's
responsibility. `controlExpr` shares proven contextual planning across switches
and range sources. Range additionally supports propagated strings, integers,
channels, arrays and array pointers. Integer residuals stay in the original range
context; full array values are saved to retain required dereferences/copies.
Sources without propagation remain untouched, including constant-len exceptions.
Unevaluated propagation and function-iterator sources/iteration targets remain
explicit gaps; see RFC 6.14. No new frame guard is introduced in these paths.

Match result placeholders are resolved jointly with question signatures before
capturing expression facts. A distinct pending nominal type cannot be mistaken
for a user's interface. Arm types are rechecked without the provisional context,
then constant-aware assignments are checked against one concrete result type;
a successful slot resolution restarts package checking. Context witnesses never
read the assignment target at runtime. Compiler-owned reference match slots are
explicitly registered for refs.Finish; source names alone grant no initialization
exception. See RFC 0009's assignment/inference slice.

Ordinary logical expressions reuse the contextual effects planner when an
untyped left operand receives a private named bool type from a statically
witnessed right arm. Preflight occurs before either arm is rewritten. The witness
never executes the skipped arm; the temporary retains its private identity.
`propagate_private_bool_test.go` validates real CLI output and independent race,
including len/cap evaluation, nearest failure, and direct recover. Other missing
witness shapes and remaining assignment guards are unchanged.

The same registry is now recoverable from verified external v2 enum artifacts in
directory builds with source-backed local replace. A public transparent payload
alias may provide a nameable spelling while preserving its actual private type.
Importing a From helper still does not register a conversion. Consecutive question
signatures are resolved with a fresh package check after each replacement, so an
identity stub cannot make an inner Option retain its outer Option type. This adds
compile-time checks (up to one per question), not a runtime function frame.
