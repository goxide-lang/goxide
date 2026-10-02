# Finite expression match normalization

`Lower(filename, source) ([]byte, error)` runs before `enums.Lower`. It supports
whole initializers of single typed or inferred **local** declarations, single
identifier assignments and short declarations, and whole
return expressions in functions or closures with one unnamed, Go-parseable result.
It also supports recursive complete-arm matches and same-file known-signature
call arguments (see the bounded composition rules below):

```hgo
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
There is no generated closure, no synthetic loop around an arm, and no change of
`return`/`defer` target. This is an implementation subset, not a new match semantics.

Composition accepts complete direct calls in return/expression statements and
recursively in complete arms/arguments. Callees must be same-file non-method,
non-generic, non-variadic function declarations without compiler directives.
Names appearing as bindings or function values anywhere in the file fall outside
this finite resolution subset; ordinary Go calls are unaffected. Package-level
parameter aliases preserve declaration-scope type identity; typed argument
staging preserves contextual constants, nil and order. New statement-call
expansions use ordinary blocks to preserve surrounding goto scope. Root-call
search stays inside the nearest function, including inline closures.

Not supported yet: complex/multiple assignment targets, control-header match
declarations, package
initializers; whole matches in named or multiple returns; match inside operators,
short-circuit expressions or composite literals; dynamic/method/cross-file/generic/
variadic callees; labelled standalone calls and control headers; expression blocks;
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
