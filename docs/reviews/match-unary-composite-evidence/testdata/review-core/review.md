# A04 independent review

Outcome: **no blocking findings after fixes**, bound to production `f2d7a39c4b81a00fff191c89ac847b09af6a62e9`. Contract `f1da21c21fc9456e257120fefd253f65a5e35d4d`; baseline `2cd3b71e4cdeff0fe8ea9ae0a2f460ac52ee416d`.

`final-source-binding.json` byte-binds **124 source files** from the frozen candidate-v4 source: **123 canonical compiler inputs**, plus the separately checked auxiliary website server file `cmd/goxide-web/main.go`. The server file is not a compiler input and is excluded from the compiler digest. All bytes match Git blobs. The canonical compiler subset is **123 inputs**, with identical complete file sets and digest `aeaaa2ca69563c064b47ecb757415e1a8478b771248e79144cecf9d9b79bc1e8`; the additional bound Go file is cmd/goxide-web/main.go, which is excluded from the native compiler inventory. Tested candidate CLI SHA-256: `e47bb03919439488f6ddc68ef7a3ec25ca5141e41e585e6c9636c8043f3e5924`. The binary was built from the frozen dirty snapshot, not falsely described as built at the subsequent commit. Full file manifest and build environment are retained.

## Findings and actual red → green evidence

- Mixed real closure before/after a sibling composite match: independently reproduced two candidate-v1 failures; both run correctly at v2 and final-bound v4. Real closure boundaries are tested by execution.
- Function-type family: `[]*func`, `[](func)`, `map[string]*func`, `map[*func]int`, `[1]*func` failed on ee5949d-bound v2; returned-real-function was the passing control. Structural FuncType-role repair alone gave v3 three red/three green. Moving proven whole-literal handling before binary splitting fixed the remaining pointer-type stars. Final-bound v4 gives **six actual CLI→generated-module race passes** on the same six inputs.

The five shapes are one blocking family, not five separate findings. Raw v1/v2/v3 failures are retained.

## Evidence, kept in separate units

| Group | Actual observation |
| --- | --- |
| Existing compiler baseline | 9 new contexts reject; 1 direct-match control compiles and runs under race |
| v2/ee5949d core runtime | 10 CLI→generated-module race cases pass: typed peer through unary, contextual shift/complement, distinct map key/value types, elided pointer literal, alias/nil, direct recover, goto |
| v2/ee5949d type errors | 14 intended type/field/key rejections plus explicit readonly rejection; includes four unselected inner/outer overflow/nominal failures after pending-context deferral |
| Nested positive counterpart | 1 generated-module race case passes for outer typed match containing inner composite match |
| Final function-type increment | 6 generated-module race cases pass |
| Final lexical/flow increment | 2 closure-order modules pass race; 1 flow module passes 7 Test functions covering call/guard order, key/value question failure, nearest user closure, typed nil, invalid enum, named return old slot/direct recover, loop targets |
| Extra type exploration | 6 normal generated-module Go tests at v1, **not race**, plus an overflow rejection; historical evidence only |

No totals combine reruns, compile cases, modules, or Test functions. Full tree race/vet/design and website/CI validation belong to the parent task, not this review.

## Scope and limits

The source delta from ee5949d is recorded in review.json. Prior typed-context evidence is labeled with its source; final v4 reruns target the changed literal/function scanner and lexical scope, with complete production byte binding.

Initial harness mistakes (missing pub; trimpath binary without GOROOT; missing pinned public-module checksum entries) are preserved and excluded from feature failures. An inferred native pointer `r := &x` is a valid permissive control, not a readonly failure; the negative uses explicit `var r &int = &x`.

Fractional-shift and readonly diagnostics retain generated/unmapped labels; no exact source coordinate is invented. The old shadowed-local nominal-name limitation was compared against an old direct-assignment control and remains a pre-existing engineering item, without claiming MVP deferral. This review does not close all A04/A01/A03/MVP work or add excluded unary/control-header/representation semantics.

## Core publication bundle

`publication-manifest.json` gives original source paths, destination-relative paths, sizes and SHA-256 values for byte-for-byte publication. `publication-core/reproduce.py` runs the six function-type cases plus the two closure-order and one flow module using an explicitly supplied compiler/source checkout. Its input and assertion files are byte copies of tested artifacts. The portable wrapper was syntax-checked only, not counted as another validation run. This minimal bundle is explicitly a core subset, not the entire historical probe corpus.
