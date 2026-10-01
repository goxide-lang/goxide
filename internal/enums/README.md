# Local enum / statement match prototype

`Lower(filename, source) (Result, error)` returns lowered Go source and structured
`Decl` / `Variant` metadata. Positions refer to the original input; payloads carry
both source spelling and a Go expression AST. This implements a finite part of
D12/D21 and experiments with T03–T05, **not a frozen enum ABI**.

```hgo
enum Event { Idle, Item(int), Failed(string), }
func handle(e Event) string {
    match e {
        Event::Item(n) if n > 0 => { return "positive" },
        Event::Item(_) => { return "other" },
        Event::Idle => { return "idle" },
        Event::Failed(message) => { return message },
    }
    panic("unreachable") // Go does not prove this if-chain exhaustive.
}
```

Supported: package-local non-generic enums with unit or one scalar payload; an
acyclic nested local enum may also be a payload. Constructors return the nominal
enum interface. Statement matches support qualified variants, one payload binding
or `_`, whole-value bindings / `_`, and source-order guards. At least one qualified
variant is required to establish the target enum type; wildcard-only matches are
explicitly rejected. Unguarded arms determine coverage and unreachable-arm errors.

The target is evaluated once into a **typed enum** temporary. A precise value-case
validator rejects nil, typed nil, pointers to cases, embedded/forged cases, and
invalid nested enum payloads **before any guard or body**, even for wildcard arms.
Matching copies payloads for each arm. Independent guarded `if` statements and a
matched flag preserve outer `return`, `defer`, `break`, and `continue`; no hidden
loop, branch switch, or immediately invoked closure encloses user bodies.

Checks occur at match use points. Uninitialized enum storage and calls to ordinary
functions without a match are not globally validated. This is not all public
boundary validation, ownership analysis, or whole-object graph validation. Payload
and nominal assignment type errors are currently rejected by generated Go's type
checker. Generated helper/case names reserve `__hgo_enum_` and are recorded in
metadata; they are unexported, so Go interoperability currently covers **same-package
mechanism tests**, not external Go callers constructing exported cases.

Explicitly unsupported: `pub` declarations and generic/cross-package enums; recursive, pointer, container,
struct, interface, or named-alias payloads; nested/constant/or/range patterns;
expression matches (including `return match` and assignment matches); wildcard-only
type inference; Option/Result special nil conventions or `?`; constant-backed enum
syntax. Statement targets use ordinary expression spelling (parenthesize composite
literals if needed to delimit the match body). Full Go package/symbol resolution,
user-visible stable ABI names, LSP source maps, and exhaustive-path return analysis
remain future work. The normal Go parser still rejects malformed host syntax.

`go test ./internal/enums` executes real lowered modules via `go run -race` and
`go test -race`, checks control-flow/guard effects and illegal runtime cases, and
expects an actual Go compile failure for cross-enum assignment. Lexer coverage
includes comments, strings, raw strings, and CRLF. These are implementation tests,
not evidence that every design-document enum rule has shipped.
