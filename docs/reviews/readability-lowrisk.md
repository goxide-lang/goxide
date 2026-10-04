# Generated Go readability: bounded lowering changes

Baseline: ae61ec628b55e0fc66ce96d47261c0a6809c0280. This work does not
change the shared-stdlib design, enum case identity, or public HgoInst names.

## Before / after contract

1. A concrete builtin validator currently calls its family validator, then emits
   a payload type switch even when every arm is only `_ = payload`. Generate that
   second switch only when at least one payload needs validation. Always retain
   the family validation call; nil Option is None, whereas nil Result is invalid.
2. Separate a builtin's private validator name from its exported HgoInst SHA-256
   identity. Use a readable family prefix plus a deterministic short digest for
   the private name, with explicit collision rejection, never silent merging.
   Reserved generated identifiers cannot be captured by source bindings. Public
   metadata continues to store the original full-digest exported name.
3. For fully qualified, unguarded variant arms only, lower a match from
   `if !matched { if payload, ok := saved.(Case); ok { matched = true; body } }`
   to `if payload, ok := saved.(Case); ok { body } else if ...`.
   Save the target once and validate before dispatch. Keep guarded and inferred
   matches on the existing path. Never introduce a switch or closure around arm
   bodies: unlabelled break/continue and direct recover keep their user targets.

Coordination: enums/builtins.go and enums/lower.go are shared with the std work.
The name interface must expose private Validator separately from ExportValidator;
frontend export collection, imported-family fallback and package deduplication
must consume the latter rather than reverse-engineering a private spelling.
No natural-import rewrite, general temp renaming, ABI shortening, or global
formatting pass is included.

## Validation plan

Use the real /workspace/goxide-environment/go/bin/go with writable /tmp caches.
Run full local race, vet and scripts/check_design.py; test frontend source maps,
package determinism, cross-module artifacts, macro hygiene, Result propagation,
evaluation once, nested validation and control-flow/recover semantics. Compare
native and WASM compilation for the five website examples without publishing.
Independent review precedes pushing the isolated compiler branch. Benchmark
numbers describe measurements, not a promised runtime improvement.
