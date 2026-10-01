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
lexical function, including function literals. It supports one RHS assignments,
one-spec variable declarations, returns, calls (including `defer` and `go` argument
evaluation), ordinary unary/binary/selector expressions, short-circuit booleans,
and `if` conditions without initializers. Calls freeze preceding evaluated runtime
arguments before later propagation. Constants retain contextual types. A failing
`defer f(load()?)` does not register that defer, and earlier defers execute normally.

Explicitly unsupported: user generic functions; propagation inside loops, switches,
indexes, slices, composite literals, assignment destinations, parallel RHS lists,
or an if condition sharing an initializer. Use a preceding binding or explicit
match; these contexts are rejected rather than silently reordered. No public
Try/Residual, cross-family coercion, or risk/borrow analysis is introduced.

Diagnostics carry parser positions in the lowered input; preserving exact original
columns through macro, enum and marker rewrites remains the frontend source-map
limitation. No runtime closure is inserted by propagation. Tests execute generated
Go with `go run -race`, including ordering, short circuit, closure and defer cases;
API negatives cover ambiguous/missing/two-hop From. These are compiler tests, not
claims about future general ownership analysis.
