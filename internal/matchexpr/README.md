# Finite expression match normalization

`Lower(filename, source) ([]byte, error)` runs before `enums.Lower`. It supports
whole initializers of single typed or inferred **local** declarations, single
identifier assignments and short declarations, and whole
return expressions in functions or closures with one named or unnamed, Go-parseable result.
It also supports recursive complete-arm matches, arguments whose actual Go call
signature can be resolved, and the bounded unary/composite forms below:

```goxide
enum State { Empty, Count(int), }
func size(s State) int {
    var base int = match s {
        State::Count(n) if n > 0 => n,
        State::Count(_) => 0,
        State::Empty => 0,
    }
    return match s { State::Count(_) => base + 1, State::Empty => base }
}
```

Normalization declares an explicitly typed private temporary, turns expression
arms into assignments within the existing statement match, then declares the user
variable or returns the temporary. The user variable's scope still begins **after**
its initializer: an outer variable of the same name is accessible in target and
arms. Target evaluation, guard ordering, exhaustive coverage, pattern binding and
invalid enum values remain the existing enum engine's responsibility. Only the
selected arm evaluates; Go type checking checks assignment of each arm value.
The emitted Go has no generated match closure, no synthetic loop around an arm, and no change of
`return`/`defer` target. This is an implementation subset, not a new match semantics.

Composition uses the actual typed Go call signature for parameter context. It
accepts static functions, function variables, field functions, direct methods,
method expressions, returned functions, variadic arguments/spreads, and explicit
generic calls whose declarations are available to Go typing. No callee name scan
or copied signature spelling authorizes a call. Compiler-directed local functions
retain an explicit boundary. Expression-statement expansions are block scoped.

Not supported yet: target forms beyond a single identifier subscript, multiple assignment
targets, control-header match declarations, package initializers; whole matches in
multiple returns; address/dereference/receive around match, new postfix receiver/index
combinations, and go/defer headers; unresolved/unnameable parameter types; user generic definitions in goxide; labelled standalone calls and control headers; expression blocks;
unsupported complex signatures. These are
implementation gaps, not language prohibitions or approved deferrals. Statement
matches pass through. All enum validation, guard order and exhaustiveness remain
in the enum engine. Nested match and argument `?` use the original function's
return path, without an IIFE.
Result-type parsing does not implement general inference or generic instantiation.
Future generic/builtin normalization must precede this pass when its spelling is
not Go parseable. Source outside replaced expressions is byte-preserved, including
raw string CRLF. Normalization is not a precise source-map implementation: later
errors can point into normalized source, while errors from this pass use the input
filename, line and column.

`go test ./internal/matchexpr` runs an actual generated module with `go run -race`
and covers once-only target effects, guards, return/defer, closure targeting,
initializer scope, delegated exhaustiveness, rejected contexts and raw CRLF.

## Assignment contexts and finite inference

`x = match ...` uses the existing local variable's actual Go type. It introduces
no runtime read or address of x: the provisional context witness is deleted by
the type stage. A block contains the result temporary, and the final assignment
is outside arm bindings, so a pattern named x cannot capture the destination.
`x := match ...` and `var x = match ...` declare x only after evaluating the
initializer. Existing outer x bindings remain visible inside arms, and Go still
rejects a short declaration with no new variable.

`propagate` resolves result slots jointly with question signatures. Each arm is
checked without a provisional assignment context, then all typed results must
have identical Go types (aliases preserve identity). Untyped constants/nil must
assign to that type. With no typed candidate, all non-nil arms must have the same
Go default type. Exact assignment checks enforce constant range and contextual
runtime shifts. There is no numeric widening or implicit common interface.
An unresolved, mixed, or unnameable result requests an explicit typed local at the
match's source location. File-scope aliases prevent package/builtin shadowing;
local types are accepted only if their spelling resolves to the identical type.
Private or shadowed unnameable types remain a diagnosed gap.

All provisional markers/types disappear before emission and permission checking.
Only compiler-registered exhaustive match reference temporaries may start without
an initializer; normal uninitialized reference declarations remain unsupported.
Registration is per file and passed explicitly, not inferred from a name prefix.
Read/write reference results retain existing qualifiers. Imported reference-call
results still meet the existing metadata/type-stage ordering boundary; no raw
pointer permission erasure is authorized by inference.

`match_assignment_test.go` exercises actual CLI-generated packages with independent
race/vet, deterministic output, nominal types/import aliases, constants, scope,
recover/defer, nested matches, question dependencies and reference rejections.

## Binary expressions and indexed assignments

Binary arithmetic, shifts, comparisons and short-circuit operators now compose in
single-result returns, local initializers/assignments, and the existing static
call/arm subset. A single `a[index()] = match ...` accepts slice, map, array and
pointer-to-array targets. The original target remains in the AST until typed
`propagate.place` captures its operands: arrays keep storage identity, slice/map
headers and indices are saved once, and bounds/nil write checks remain after RHS
effects. Broader destination extraction remains an implementation gap.

Compiler-owned function-literal syntax temporarily carries an expression atom's
slot and statement match through Go parsing/type checking. Its reserved atom
prefix is distinct from ordinary result slots; user closures are never treated
as atoms. All atoms are removed into `(prelude, value)` in the original user
function before Go emission. Short-circuit right preludes stay inside their
conditional branch. `?` uses the original function target and direct `recover`
keeps its call frame. Ordinary assignment and return expansions are block-scoped
so generated temporaries do not invalidate forward goto.

The expected numeric/boolean type comes from the actual enclosing signature or
assignment and actual typed operator peers, including sibling match dependencies.
Available question payload signatures resolve first, then concrete arms before
untyped defaults; questions depending on a pending match wait for the next pass. Peer discovery follows the real
binary tree, stops across a comparison result or shift count, and never evaluates
a witness. A comparison operand does not inherit the function's bool result;
returning any does not force arithmetic operands to any. The existing exact
assignment, nominal identity, constant range and reference qualification rules
still apply. Private/unnameable result identities remain a diagnosed gap.

`tests/acceptance/match_binary_index_test.go` exercises actual generated modules
with independent race/vet, deterministic output, storage/order/panic/failure
checks, local/nominal type contexts and readonly negatives. Optional optimized
benchmarks are enabled by `GOXIDE_MATCH_BENCH=1`; production measurements and the
separate earlier pure-Go IIFE research are recorded under
`tests/research/match_lowering`.

## Named returns and typed function variables

Whole `return match ...` reads the actual single-result signature through the
same typed context resolver as embedded returns, including a named result and
locally shadowed type spelling. Arm evaluation finishes in a private block before
an ordinary `return temporary` assigns the named result slot. Defers observe that
assignment; a panic during an arm leaves the prior slot value. Generated locals
do not invalidate a forward goto. Direct recover remains in the user call frame.

Identifier function variables with known non-variadic signatures accept match
arguments in return/statement calls and the existing local/arm/binary contexts.
The typed pass follows the enclosing actual call and checks the variable object,
exact arity and signature parameter; a same-spelled declaration or builtin is not
a substitute. Signature witnesses never execute. The existing propagation pass
captures the function value before arguments, stages arguments in lexical order,
and delays nil-function invocation until arguments finish. Failed `?` returns from
the nearest user function before later arguments or invocation. Compiler atoms
are always removed; no generated IIFE, reflection or runtime type erasure is used.

The following unified-signature stage adds selector/factory/variadic and native
explicit generic calls. Multiple-result `return match` remains a located diagnostic. Parameter type errors and constant overflow also
report the original match location. `tests/acceptance/match_named_dynamic_test.go`
uses real CLI output, independent generated-module race/vet, deterministic output,
positive runtime assertions and negative source-location assertions.

## Unified call contexts and receiver evaluation

The parser preserves the complete callee expression and argument expansion.
`propagate.matchArgumentType` resolves its actual Go signature, checks arity and
selects the fixed parameter, variadic element, or expanded slice type. Builtin and
type-conversion calls remain explicitly unsupported contexts. Native Go generic
instantiations use the same path; goxide user generic definitions remain unsupported.
Public transparent aliases preserve private nominal parameter identity; completely
unnameable parameters retain a diagnostic rather than type erasure.

Function values are captured once before arguments. Direct methods retain direct
selection: receiver calls, receives and short-circuit expressions are evaluated
first; remaining receiver reads/indexing/assertions/implicit dereferences occur
on the final call. Operations required to execute an earlier receiver call still
execute with it. This is a consistent choice within Go's partially specified
order, not a promise to reproduce every optimizer's ordinary-read/panic timing.
In particular default and race builds can order a native type assertion differently.
The receiver is not converted into a method value, and original Go addressability
is checked before emission. Match failure remains a return from the user function.

`match_call_signatures_test.go` supplies true CLI output, generated race/vet,
positive/negative scope and permission tests, deterministic output and optional
optimized microbenchmarks (`GOXIDE_MATCH_CALL_BENCH=1`). See the corresponding RFC
0009 stage for measured scope and remaining boundaries.

## Unary scalar operations and composite literals

Scalar `+`, `-`, `!` and `^` accept single-value matches. Struct, slice, array and
map literals recursively accept matches in their value slots, and map key slots;
ordinary Go nested literal type elision and implicit pointer elements retain their
existing syntax and identity. These forms compose in supported local declarations,
assignments, single-result returns, call arguments and expression arms. Struct
field selectors and array/slice constant indices do not become runtime expressions.

Each slot takes its expected type from the actual `go/types` composite field or
element role. Map key/value identities remain distinct; nominal types, transparent
aliases, locally shadowed spelling, contextual shifts and nil retain the existing
exact assignment rules. Typed peers can propagate through scalar unary operators;
comparison results and shift counts still delimit the operand domain. Pending
compiler types are recognized by real type identity, not by substrings in legal
user type names. A known outer context is installed before resolving a nested match,
then final typing checks every arm, including unselected ones.

Real nested function bodies normalize before enclosing expressions, and their
completed edits are composed without changing source ownership. A function field
with its own match and `?` can coexist with outer literal matches: each question
still exits its own user function. The existing propagation literal planner stages
calls/receives in the approved order, preserves short-circuit branches and skips
remaining fields/elements after failure. It leaves Go's ordinary-read and map
insertion timing choices to Go. Generated atoms do not remain as runtime functions.

`tests/acceptance/match_unary_composite_test.go` runs the real CLI from an unrelated
directory, compares all generated artifacts, and race-tests/vets/runs a generated
module. It joins JSON, a closed enum, match-based object assembly, Clone, Debug and
JSON output; covers failure/cause/path, nil/empty, once-only effects, direct recover,
defer, goto, loop control and existing indexed storage; and checks real type and
permission rejection separately from unsupported syntax. The earlier literal
rejection is preserved as a positively executed scenario.

This does not add block arms, multi-result matching, complex places, arbitrary
postfix/receiver contexts, reference/receive unary matching, or new enum/derive
protocols. Remaining forms are implementation work, not language prohibitions.
