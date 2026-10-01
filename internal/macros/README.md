# Executable local pattern-macro prototype

`Expand(filename, source)` expands local `macro_rules!` definitions before the
frontend reads derive attributes. `ExpandWithMap` additionally returns token
output ranges, original file/line/column and the immediate invocation line.
Untouched source whitespace and attribute lines are retained. This is a bounded
prototype of the design, not a complete Rust macro implementation or cross-package
macro ABI. Go compiler diagnostics for appended helper aliases are not fully
source-mapped; full expansion-stack source maps remain future work.

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
labels, and cross-package macro imports are diagnosed. Pass unsupported call-site
values/functions as explicit captures. Primitive types/constants used by a
template have definition-site aliases. A local variable shadowing an import inside
the template takes precedence over the import. Ordinary Go compilation still
checks types, arity, target-context legality and unused declarations.

Limits are 200,000 input/output tokens, 4,096 expansions, token-tree nesting 256,
and expansion traversal depth 64. Output and recursion failures return no success
result. Generated code, aliases and names are deterministic; no environment or
filesystem access is available to user-defined macros themselves. The compiler's
Go import resolver reads dependency export data.

Tests compile and run expansion results to check caller/template capture,
definition-helper shadowing, separate local scopes, once/twice evaluation,
expression precedence, repetition and nesting, captured identifiers, log-style
fields, and actual caller `return`/`defer` behavior. Negative tests exercise pattern,
fragment, free-name, empty repetition and resource errors. These tests do not
claim complete macro hygiene for the unsupported syntax categories above.
