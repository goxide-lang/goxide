# Local enum / statement match prototype

`Lower(filename, source) (Result, error)` returns lowered Go source and structured
`Decl` / `Variant` metadata. Positions refer to the original input; payloads carry
both source spelling and a Go expression AST. This implements a finite part of
D12/D21 and experiments with T03–T05, **not a frozen enum ABI**.

```goxide
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

Supported: package-local non-generic enums with unit or one finite value payload:
scalars, local structs, arrays, slices, maps, and acyclic nested local enums. Constructors return the nominal
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
checker. Generated helper/case names reserve `__goxide_enum_` and are recorded in
metadata. Directory compilation additionally emits a finite export protocol for
`pub enum`: public constructors, validators, and `GoxideCase...` aliases to the private
value cases. The marker stays private and nominal; pointer and embedded cases are
still rejected by validation. These generated names are a prototype protocol.

Directory compilation supports `alias.Enum::Variant` constructors and statement
or supported expression matches for directly imported `pub enum` declarations.
Metadata is keyed by canonical module import path and bound separately for each
file alias. Generated package bindings prevent local aliases from capturing case
projections or validators. Matching two packages’ identically named enums remains
a type error. Private enums and ordinary Go enum lookalikes have no metadata.

Explicitly unsupported: user generic enums; recursive, pointer, enum-bearing container,
unknown external or general interface payloads; nested/constant/or/range patterns;
expression matches are handled by a separate frontend stage; wildcard-only
type inference; generalized propagation protocols. Statement targets use ordinary expression spelling (parenthesize composite
literals if needed to delimit the match body). Full Go package/symbol resolution,
user-visible stable ABI names, LSP source maps, and exhaustive-path return analysis
remain future work. The normal Go parser still rejects malformed host syntax.

`go test ./internal/enums` executes real lowered modules via `go run -race` and
`go test -race`, checks control-flow/guard effects and illegal runtime cases, and
expects an actual Go compile failure for cross-enum assignment. Lexer coverage
includes comments, strings, raw strings, and CRLF. These are implementation tests,
not evidence that every design-document enum rule has shipped.

## Built-in sum subset

`Option[T]` and `Result[T,E]` now support explicit concrete instantiation, e.g.
`Option[int]::Some(4)`, `Option[int]::None`, `Result[int,error]::Err(err)`, and
matching with the same qualified spelling. These use the existing nominal enum
interface/value-case model and `Decl`/`Variant` metadata. `Builtin(family,args)`
returns canonical instance metadata (`Family`, `TypeArgs`, cases, constructors,
validator). All generic marker signatures carry all type parameters: even an Err
case's success parameter participates in assignment identity.

Option's only None representation is nil. Result nil is invalid. Validators reject
case pointers, typed-nil case pointers, and embedded/forged cases before matching.
A typed-nil **error payload** remains an ordinary retained payload; it is not None
or an invalid Result. Instance validators recursively inspect directly nested known
local enums and built-in sums, including named fields of finite local value structs
and their aliases. Grouped Go type declarations are inspected as well. Validation
runs before match guards or propagation exposes a payload, not when every value is
constructed. Enum-bearing pointer/slice/map/array/channel projections, embedded or
blank enum-bearing fields, and recursive sum projections remain explicitly
unsupported; ordinary non-enum Go structs/containers remain usable. Dynamic
interface contents and external object graphs are not traversed. Source reference
payloads (`Option[&int]`, for example) are separately rejected: supporting ordinary
sum payloads beside scalar references does not authorize storing new references
inside sums. This is boundary validation, not ownership analysis.

User generic functions and declarations remain unsupported, as do inferred generic
constructors and builtin serde derives. In directory mode, the propagation stage
can consume imported concrete builtin instances emitted by a compiled dependency;
qualified builtin constructor/pattern syntax is not yet supported. Built-in
names cannot be replaced by user declarations when using this subset. The local enum extension described below reuses the same value projection checks. The dedicated `?` stage consumes the same
case/validator metadata; this module alone does not implement early return.

Tests include generated modules run with `-race`, nil and typed-nil validation,
nested builtin validation, and a real Go compile rejection for assigning an Err
case across different success parameters.

## Local payload and derive extension

Local single-payload variants now accept finite local named value structs, arrays,
slices and maps of supported value types. Structs can carry ordinary derive field
attributes. Direct nested enums and enum-bearing named value fields are validated
before match/Debug/Clone exposes the payload. Enum-bearing containers and pointers,
recursive payload graphs, external unknown types and generic local declarations
remain rejected. Constructors and match bindings retain Go value-copy semantics;
only an explicit Clone helper recursively copies supported containers.

`#[derive(Debug, Clone)] enum Command { Idle, Run(Job), }` generates
`DebugCommand(*Command) string` and `CloneCommand(*Command) Command`.
Debug prints `Command::Idle` or `Command::Run(Job { ... })`; payload fields follow
the existing struct strategy (declaration order, quoted strings, sorted string map
keys, nil distinguished from empty). Payload named types need the corresponding
derive. Invalid enum representations panic in these helpers, as at match use
points. Serde continues to report errors and retains the existing externally
tagged unit/newtype protocol. A struct/array newtype payload is **not** a newly
implemented struct/tuple variant protocol or source syntax.

Error variants use explicit, standalone prototype attributes:

```goxide
#[derive(Error)]
enum AppError {
    #[error("input failed")]
    #[from]
    Input(error),
    #[error("invalid count {Value}")]
    Count(int),
    #[error("missing")]
    Missing,
}
```

Every variant requires a literal message. `{Value}` supports the same built-in
scalar formatting as struct Error; there is no implicit Stringer/debug fallback.
`#[source]` and `#[from]` require a single static `error` payload. From implies
source; at most one variant has From, producing `FromAppError(error) AppError`
and registering the existing single-step `?` conversion. Each case implements
Error/Unwrap and the enum interface includes those methods. Unwrap preserves the
source interface, including typed nil; source-less variants return nil. No dynamic
error classification, transparent struct forwarding, concrete source conversion,
or conversion chaining is inferred. Attribute spelling and helper ABI remain
prototype conventions, not new frozen language rules.


## Closed variants and external wire mappings

Open raw/C enums are withdrawn. `enum State string {...}` and integer base declarations are rejected. All enum values use declared variants; wildcard-only matching resolves nominal identity and validates the closed representation before guards/bodies.

Unit enums may derive Serialize/Deserialize with `#[serde(rename="pending")]` or a complete `#[serde(value=1)]` mapping. Numeric mappings require signed int64 decimal literals on every unit variant; mixed, duplicate, missing or payload mappings are rejected. Unknown inputs return UnknownVariant instead of an unknown enum value. Type/range failures retain their distinct errors. Clone/Debug/match use source variant identity; the codec does not change Default or create a raw cast.

## External concrete enum artifacts

Directory builds now publish `.goxide.enums.json` and `GoxideEnumMetadata` for public
concrete enums and transparent public type aliases. The frontend restores these
before match syntax lowering, using actual imported Go types to verify enum,
case, constructor, validator and payload identity. Private payload names are not
serialized or rewritten into consumer source: case fields preserve their actual
Go types and inferred access remains possible. A new defined interface type is
not authorized as an alias of a closed enum.

The internal v1 envelope binds package identity/name, a logical marker digest,
selected Go file hashes, root/transitive source and build configuration, plus
marked dependency artifacts. It reuses the offline importer fingerprint mechanism
also used by reference artifacts; both markers are added before either final
artifact is hashed. Missing/stale/mismatched artifacts produce consumer import
location diagnostics. Hashes detect consistency errors, not malicious-producer
forgery. External cgo inputs remain subject to the existing fingerprint limits.

Tested delivery uses independent go.mod modules with local replace. Native Go can
consume the generated API without the JSON. Existing producer serde/Clone/Default
helpers remain callable; consumer derive protocols and external generic builtin
Option/Result family metadata are not restored by this concrete-enum schema.
Single-file compilation does not acquire the directory import registry here.

Transparent aliases bind to the defining package's helpers. If Go's internal
package rule prevents that origin import, ordinary public API calls still work;
using that alias's enum constructor/pattern syntax receives an explicit unsupported
diagnostic. Public relay helpers are an outstanding design/engineering option, not
an approved permanent exclusion. This stage retains value cases and does not add
Copy/move, new borrow semantics, comparison policy or universal entry validation.

## External builtin families

Protocol v2 adds registry-backed Option/Result families and concrete producer
validators. Generic origin, phantom arguments, case/constructor types and instance
validator signatures are checked using actual Go identities. Transparent generic
aliases retain their source instantiation (and constraints), while canonical
argument substitution visits type references only. Constructors keep existing
helper behavior; match/question operations require the matching instance validator.
This does not introduce validation at every public API entry.

Exported payload aliases can expose otherwise private named types. Unnameable
payloads remain a documented limitation for enhanced operations; ordinary API
calls remain valid. Internal facade syntax, external consumer derives and
single-file metadata recovery remain separate work. See
[builtin module acceptance](../../tests/acceptance/builtin_external_test.go).
