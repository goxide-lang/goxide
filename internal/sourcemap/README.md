# Diagnostic provenance

`frontend.Result.Diagnostics` and source-map JSON `diagnostics` describe **final
Go byte offsets**. `Segment.Origin` is a half-open span in original user input;
`Stack` records outer-to-inner macro definitions and invocations. Missing Origin
and `Kind: "generated/unmapped"` explicitly mean no proven source location.
An independently proven expansion stack can still accompany an unmapped token;
its definition and invocation spans do not establish an origin for that token.
`deriveFields` records original field **type** spans, independently of generated
helper output. `sourcemap.Error` exposes those spans for derive validation errors.

The legacy `macroTokens` array is a different coordinate space:
`OutputStart/OutputEnd` refer to macro expansion output, while
`SourceStart/SourceEnd` and stack spans refer to **propagate.Prepare output**.
The JSON `macroTokenInputStage` declares this. These equal original input spans
only when preparation did not rewrite the input; consumers must not mistake
them for final Go or universally original coordinates.

The compiler composes whole unchanged token streams, explicit attribute/struct
sugar slices, known import/helper concatenations, and go/format passes.
Go formatting can remove redundant parentheses; `Format` is exclusively for
that known transformation and retains non-parenthesis origins only if their
complete ordered token streams are equal. It never searches for equal repeated
tokens. Enum/statement-match and expression-match renderers now explicitly
transport copied byte ranges, including nested arm bodies and guards. Question
preparation transports the operand and untouched source; question lowering
transports surviving AST node identities through a fully checked ordered AST
stream. Generated nodes with coincidentally identical positions do not count.
Macro diagnostic stack endpoints are translated back through question preparation;
the legacy macroTokens coordinate space is unchanged. Reference lowering still
loses precise origins on substantive changes, and reconstructed question AST
expressions may remain unmapped. The stages are listed in JSON; this is not
complete lowering coverage. Synthetic macro
parentheses and generated infrastructure have no invented source location.

AST transformations can use `CaptureAST` before mutation and `ASTEvidence.Print`
after printing. The snapshot freezes each supported token slot's kind, spelling,
point origin and context, keyed by AST object identity. `Context(node, slot)`
requires an explicit owned slot such as `IdentToken` or `OperatorToken`;
`BinaryExpr.Pos()` and `CallExpr.Pos()` are not operation witnesses. The explicit
slot table never treats general range ends or a child's position as an owned
token. Modified token spellings keep only their independently proven context;
an observed rewrite cannot later regain its old point origin by changing back.
This invalidation also occurs when that print attempt fails parsing or structural
validation. Each captured token requires one winning input segment to cover its
whole range; a later segment intersecting only part of that token invalidates
both point and context. A wider covering segment can prove context, but only an
exact token segment can preserve an Origin. That original span must have a file,
a nonnegative start, a strictly later end and positive line and column. Its width
need not equal the generated token's width: an upstream producer can explicitly
prove a renamed or causal token's different original span.
Input map data and each emitted map are copied, so later mutations do not alter
the frozen evidence.

`Generated(node, context)` registers only that new node's own tokens. It never
adds an Origin and never recursively fills child evidence. `GeneratedTree`
requires a closed, newly constructed compiler template, registered before any
source operands are spliced into its holes. If any node already has a record,
the whole registration fails: existing records are preserved and every new node
gets an unknown tombstone. Unknown and conflicting registrations cannot later
be upgraded. Repeating the same handle is idempotent; two distinct handles are
not merged even if their displayed stacks are identical. A handle from another
registry cannot authorize a binding. Only known nonempty complete stacks are
transferable; a nil Stack does not prove a known empty context. Shared helpers
with multiple callers therefore remain outside this single-cause contract.

`Print` compares the actual transformed AST with the parsed output by concrete
node type, child field role and order, and semantic scalars, including operators,
literal kinds, channel direction, slice mode, call ellipsis and type aliases.
Positions, comments and parser-derived caches are not semantic syntax. Limited
printer normalizations cover empty parameter/result lists and empty statements.
Transparent parenthesis chains are compared through their complete inner
structure; when chain lengths differ, no parenthesis in that chain is mapped.
Other structurally paired tokens can retain their evidence. Unsupported
structural changes, including import reordering or number-spelling
normalization, currently fail closed for the whole output stage. No token-value
search, nearest position, longest stack or common-prefix stack inference occurs.

Each reparse requires a new snapshot from the emitted map. Generated context is
not promoted to an original point just because the node is now parsed input.
The caller must provide the matching parser FileSet, root and map for that
stage; bounds and lexical checks cannot detect deliberately mismatched FileSets
whose filenames, positions and source bytes are all identical, because Go AST
nodes do not carry their FileSet identity. Foreign node lookups and context
handles are rejected, and generated-node registration is an explicit warrant by
the lowerer, not a way to rediscover source ownership from another parsed tree.

Derive attachment is restricted to the actual appended helper region and the
requested derive. A selector maps to a field type only when its lexically bound
receiver has that model's type; similarly named serializer methods or user
functions do not acquire derive provenance. The mapping is causal (`derive
field`), not a claim the generated selector text appeared in the source.

CLI `run`/`build` preserves original Go stderr and appends mapped diagnostic
lines. Only physical `main.go` and `./main.go` line-and-column compiler errors
are recognized, because that is the CLI's generated filename. Diagnostics from
other files, user line directives, runtime traces, and other formats remain raw.
Compiler-generated helpers no longer emit misleading `//line` directives.
Legacy lowering errors preserve their owning phase's error text, wrapped with
`generated/unmapped; intermediate coordinates` when their original span is not
proven. The structured original-span API covers derive type validation and final
Go diagnostics and final go/types errors, not every phase error. A real error
located on a synthetic macro parenthesis explicitly stays generated/unmapped,
with any independently proven macro expansion frames shown below it.
