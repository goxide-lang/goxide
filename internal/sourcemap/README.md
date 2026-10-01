# Diagnostic provenance

`frontend.Result.Diagnostics` and source-map JSON `diagnostics` describe **final
Go byte offsets**. `Segment.Origin` is a half-open span in original user input;
`Stack` records outer-to-inner macro definitions and invocations. Missing Origin
and `Kind: "generated/unmapped"` explicitly mean no proven source location.
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
tokens. Unknown changed passes (including reference, enum, match and question
lowering) conservatively lose all token provenance for that pass. The stages are
listed in JSON; this is not complete lowering coverage. Synthetic macro
parentheses and generated infrastructure have no invented source location.

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
Go diagnostics, not every phase error.
