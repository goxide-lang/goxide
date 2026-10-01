# Executable pattern-macro prototype

`Expand(filename, source)` expands local `macro_rules!` definitions before the
frontend reads derive attributes. `ExpandWithMap` additionally returns token
output ranges, original file/line/column and the immediate invocation line.
Untouched source whitespace and attribute lines are retained. This is a bounded
prototype of the design, not a complete Rust macro implementation or stable external
macro ABI. Go compiler diagnostics for appended helper aliases are not fully
source-mapped. Nested expansion stacks are available; complete coverage across
all later lowering passes remains future work.

```go
macro_rules! event {
    ($message:expr, $($field:expr),*) => {
        fmt.Println($message, $($field),*)
    };
}
event!("request", "id", 7)
```

Supported patterns are literal tokens, `ident`, `expr`, and restricted `tt`
captures, plus terminal comma-separated repetition of one capture (`*`/`+`).
Template repetition can contain several tokens and scalar captures. Nested macro
invocations are supported; nested repetitions, optional repetitions and trailing
commas not explicitly present in a pattern are not supported. `expr` must be
followed by a comma or pattern end. Rules are tried in source order and must
consume all input. Expressions preserve precedence when inserted into templates.
`tt` supports expression tokens or parenthesized argument groups, not arbitrary
statement/type token injection. Larger syntax fragments need future parser work.

Templates can be Go-shaped expressions or statements, including explicit closures,
`return`, and `defer`. The expander inserts no implicit function/closure boundary.
A template using `$x` twice evaluates it twice; hygiene does not imply once-only
evaluation. Bind once in the template when that behavior is wanted.

Template locals are parsed into Go AST scope/object identities before replacing
caller placeholders. Binding identities receive collision-free deterministic
output names; captured tokens keep caller spelling and origin. Same-package
non-generic function references and imported **function** calls use package-level
aliases, so a caller-local variable cannot shadow a definition helper. Imported
helpers are verified with Go export data; missing export data is a diagnostic
with an explicit-capture alternative. Mutable imported function-valued variables
are rejected, avoiding an accidental snapshot at package initialization.

There is deliberately no permissive free-name fallback. Unsupported free names
(including builtin functions such as `len`), generic helpers, imported types and
variables, identifier-keyed literals, local type/struct/interface declarations,
labels, and external precompiled macro metadata are diagnosed. Pass unsupported call-site
values/functions as explicit captures. Primitive types/constants used by a
template have definition-site aliases. A local variable shadowing an import inside
the template takes precedence over the import. Ordinary Go compilation still
checks types, arity, target-context legality and unused declarations.

Limits are 200,000 input/output tokens, 4,096 expansions, token-tree nesting 256,
and expansion traversal depth 64. Directory compilation shares input/output and
expansion-count budgets across the build; declaration collection is not charged
a second time for the final pass. Output and recursion failures return no success
result. Generated code, aliases and names are deterministic; no environment or
filesystem access is available to user-defined macros themselves. The compiler's
Go import resolver reads dependency export data.

Tests compile and run expansion results to check caller/template capture,
definition-helper shadowing, separate local scopes, once/twice evaluation,
expression precedence, repetition and nesting, captured identifiers, log-style
fields, and actual caller `return`/`defer` behavior. Negative tests exercise pattern,
fragment, free-name, empty repetition and resource errors. These tests do not
claim complete macro hygiene for the unsupported syntax categories above.

Fixed built-in `Option[T]` / `Result[T,E]` template types and qualified
constructors are supported through definition-site type aliases and typed
constructor helpers. Type arguments currently admit builtin Go scalar types and
pointer/array/slice/map/channel combinations, not caller-captured or unknown named
types. Ordinary user enum constructors in templates remain a located diagnostic.
The next enum lowering pass processes helper bodies; macro output never forges
reserved enum ABI names. Constructor arguments and trusted compiler question
markers remain at the original call position, inside an explicit template closure
when present. No helper changes the nearest propagation/return target. None is a
unit variant written `Option[int]::None`, without parentheses.

`event_if!` in [conditional_log.hgo](../../examples/prototype/conditional_log.hgo)
uses an explicit enabled expression and ordinary `if`: disabled message/fields
are not evaluated, enabled expressions retain template order. This does not
change existing eager `event!` semantics and does not implement tracing spans.

## Source packages in one Go module

Directory mode exports `pub macro_rules! name { ... }`. A normal explicit Go
import enables `alias.name!(...)`; its alias is file-local and its identity is
the canonical module import path. The design's `import macro` and `m::name!`
spellings remain drafts. Runtime helpers follow current Go visibility: use an
exported function such as `Emit`, or expose a public shim around private state.

```go
// logkit/macros.hgo
package logkit
pub macro_rules! event_if {
    ($enabled:expr, $message:expr) => {
        if $enabled { Emit($message) }
    };
}
// logkit/runtime.hgo
package logkit
func Emit(message string) { /* ordinary runtime implementation */ }
// caller/main.hgo
package caller
import logs "example.org/app/logkit"
func Run() { logs.event_if!(true, "request") }
```

Definitions and runtime function declarations are shared across selected files in
a package. Each definition retains its own file's imports; private nested macros
resolve there, never to a caller macro with the same name. Direct access to a
private imported macro is rejected. A runtime helper must be public when its
macro expands in another package; macro expansion grants no private access.
Imported function calls are checked through the existing Go importer. Only
already-declared dependencies may be introduced into generated runtime imports.
An ordinary import used only for macros becomes a blank runtime import, preserving
the explicit dependency without causing an unused-import error.

Registries are opaque compiler-owned source data, published internally only after
the defining package passes checking. Native Go declarations or fabricated export
metadata cannot create macros. External precompiled macro distribution and user
procedural macros are outside this prototype. Module cycles use the existing Go
module dependency traversal; macro recursion is bounded by expansion budgets.

Cross-file definition tokens preserve original file spans. Caller fragments keep
their own origin and expansion stack; original/prepared stage flags prevent
reinterpreting another file's offset in the caller buffer. Synthetic imports,
helper aliases and unmapped propagation syntax have no claimed source span.
The registry is immutable during expansion, and generated file-specific names
avoid collisions across package files. See [the package tests](package_test.go)
and [the module application](../../examples/macro_packages/main.hgo).
