# Finite expression match normalization

`Lower(filename, source) ([]byte, error)` runs before `enums.Lower`. It supports
whole initializers of single explicitly typed **local** declarations and whole
return expressions in functions or closures with one unnamed, Go-parseable result:

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

Not supported yet: inferred `:=` results; package initializers; named or multiple
return values; match nested in arguments, operators, short-circuit expressions or
another expression arm; expression blocks; complex result signatures that this
signature extractor cannot recover. These are rejected or left for the existing
enum parser to diagnose, never silently reordered. Statement matches pass through.
Result-type parsing does not implement general inference or generic instantiation.
Future generic/builtin normalization must precede this pass when its spelling is
not Go parseable. Source outside replaced expressions is byte-preserved, including
raw string CRLF. Normalization is not a precise source-map implementation: later
errors can point into normalized source, while errors from this pass use the input
filename, line and column.

`go test ./internal/matchexpr` runs an actual generated module with `go run -race`
and covers once-only target effects, guards, return/defer, closure targeting,
initializer scope, delegated exhaustiveness, rejected contexts and raw CRLF.
