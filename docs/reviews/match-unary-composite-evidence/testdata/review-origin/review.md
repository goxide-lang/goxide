Source: `f2d7a39c4b81a00fff191c89ac847b09af6a62e9`  
Verdict: **no blocking findings in this review scope**.  
Independent binary SHA-256: `245d995482476e305fd947f26a7fb64ff45ab5ebbdecc69348fcc44df54434b9`.  
Exact source archive, per-file hashes: `f2d7a39-source-manifest.json`.

# A04 independent provenance / atom review — final

Review is limited to compiler-generated atom identity, source accuracy, and real-user-function ownership. No production edits, full suite, browser, Linux, commit or push performed by this reviewer.

## Identified and addressed during review

The first candidate used `strings.Contains(types.TypeString(t, nil), matchPending)`. A legal named type `X__goxide_match_expr_pendingY` was accepted in the old typed-local match context but rejected in the new slice-literal context. The raw old/new observation is retained in `pending-type-name-control.json`. Production now compares against the actual package seed type identity and recursively inspects component/type argument types. Final exact-SHA reproduction passes in both old and new contexts; the old control generated output stays byte-identical.

## Provenance controls

Four forged marker/atom/origin inputs, each LF and CRLF, are rejected during original-source reserved-identifier scanning. Marker text inside a string is accepted. No user-supplied fake filename is promoted into a diagnostic source point.

Six actual macro cases, each LF and CRLF, cover original match-point overflow/bool failures, original literal errors after new or old match lowering, two-frame nested macros, and a non-exhaustive generated statement match. Exact original literal spans and macro definition/callsite spans are independently checked against fixture byte offsets. The generated statement-match exhaustiveness diagnostic retains explicit intermediate/unmapped labeling; it is not claimed to recover an original source point.

Initial macro templates containing literal `match` syntax were rejected earlier by pre-existing macro limitations and are retained as fixture development evidence, not new A04 macro integration coverage. Two initial bool message assertions expected an exclamation mark; their raw failed expectations remain, and the accurate assignability-to-bool diagnostic was then checked.

## Function boundary / frame ownership

A mixed enclosing composite contains outer matches plus a real inner user closure with its own match. The outer function returns Option[int], the closure Result[int,string], and their question operators come from two different macros. Runtime assertions verify OBIDZ for the success/inner-failure path and O for outer failure. The inner failure therefore remains in the inner user function.

The generated Go AST independently locates that inner function. For each LF/CRLF source map, all 105 tokens with macro stacks are checked against that AST range: 47 outerPayload tokens outside, 58 innerPayload tokens inside. All frame spans independently match source bytes. Eight precise macro source tokens have exact original spelling, and 26 compiler-generated identifiers have nil Origin. This checks frame transport without treating generated points as exact source positions.

## Scope limits

No assertion that all generated diagnostics are mapped. Statement-match exhaustiveness can still be intermediate/unmapped. Macro syntax acceptance outside the approved A04 slice is not expanded. General type/order checks and full validation belong to other reviewers/root.

## Final frozen-source checks

All 30 runner assertions pass: 12 reservation/string/old-point controls, 12 macro source diagnostics, two seed-type identity controls, three generated runtime programs, and one source-map bundle containing independent LF and CRLF checks. This is a bounded independent probe set, not a full suite result. Raw results are in `final-observations.json`; the final runner output is `f2d7a39-probe.log`.

The earlier exact commit ee5949d had 29 passing checks in this review scope, but the primary reviewer separately found `[]*func()int` misclassified as a function body. No approval was issued for that source; `review-ee5949d.json` and its prefixed evidence retain that history. The f2d7a39 fix uses parsed complete composite types and function signatures, skipping only type-role `func` tokens. It explicitly preserves FuncLit boundaries in expression positions. The literal type is recognized before scanning expression operators, so a pointer star inside `[]*func()int` cannot become multiplication.

The final independent runtime additionally exercises a function returning a function that constructs `[]*func()int`, both selected pointer and nil branches, plus the existing slice/array/map function-type cases and real inner question propagation. It passes. The new owner classification does not generate source locations or merge source stacks; the existing Fragments transports are preserved. No new blocker remains in the provenance/atom scope.
