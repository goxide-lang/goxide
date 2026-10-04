# Public library migration — integration in progress

The sole integration owner continued from `ab64d87807f95a23fe33b4203851c36b64f47f8b` on `integration/public-std-serde-20261004`. The environment's initial compiler checkout was not used as the migration baseline.

## Published library evidence

Both destination repositories were empty (`git ls-remote` and clean worktree inspection). Only the candidate provenance allowlists were copied: std 5 files, serde 13 files. Every SHA256 matched; serde source bytes matched the recorded source commit after the documented import substitution. Both licenses are byte-identical to the compiler's existing MPL 2.0 license. No compiler history, private provenance/HANDOFF, RFC, credential, tag, or release was published. `next` remains empty.

| Repository | Branch | Verified remote commit | Go-resolved version |
| --- | --- | --- | --- |
| github.com/goxide-lang/std | migration/shared-sum-abi | 3f2204770fd3b09cf9f5a1d9228464b7f7c1bdcd | v0.0.0-20261004120854-3f2204770fd3 |
| github.com/goxide-lang/serde | migration/core-json | a1b791e96d660c6be620e110c566a9a171631b7e | v0.0.0-20261004120911-a1b791e96d66 |

Normal Git push succeeded and `git ls-remote` confirmed each SHA. `go get` fetched both commits successfully and supplied actual pseudo-versions/checksums. Compiler go.mod has no local runtime replace. Each public library passed `go test -race ./...`, `go vet ./...`, and `GOOS=js GOARCH=wasm go build ./...` under Go 1.27.1.

## Integration changes and diagnostics

Generated serde imports and field protocol identity now use the public root package, JSON examples/tests use `/json`, and the old private copies are removed. Compiler-specific JSON scope documentation stays private in [serde-json.md](../serde-json.md). Generated fixture modules explicitly receive pinned public dependencies and checksums; this is test setup, not a production dependency fallback. The offline compiler importer still does not download or silently replace modules.

Initial diagnostics reproduced missing std dependencies and low-level importer failures. CLI package race completed successfully in this environment, so the old environment's module ziphash/planning failures have not reproduced. The initial diagnostic run overlapped subsequent workspace edits and is not final acceptance evidence.

Additional real regressions fixed: single-file sum derives lacked an importer; imported aliases lost access to a payload alias after family origin moved to std; unused candidate validator-owner imports survived lowering. Existing frontend, propagation and prototype race suites passed after those fixes. Cross-module producer/consumer/Go consumer race/vet succeeded, then exposed an obsolete rejection of a new `Option[bool]` instance. That input is now exercised positively with an additional typed-nil rejection, as required by local static instance validation in schema 7; coverage is retained.

Readability commits are integrated semantically: shared marker remains `isOption`/`isResult`; ValidatorPath is retained independently of ExportValidator; public full-digest names are unchanged. Final combined checks are still running.

## Final combined validation for the current manifest mode

Code commit `b8bb3a5e21ea84ef29490ba845c39ce04dbdc739` passed full `go test -race -tags hgoweb -count=1 ./...` with exit code 0. Both `go vet ./...` and `go vet -tags hgoweb ./...` passed. The preceding frozen run had exactly three top-level failures: the imported macro source-span loss, obsolete forged-Option marker, and a whitespace-sensitive assignment-guard assertion. Each was diagnosed, repaired without removing its scenario, separately rerun, then covered by this final full run. The 98 failures in the old handoff are not reported as this run's count.

See [machine-readable package/test summary](public-library-validation-20261004.json). Tests preserve exported full-digest validator identities, canonical shared family origin, concrete validator ownership, typed-nil rejection, nested payload validation, source mapping, break/recover, Go consumer identity, From, and explicit derives. A new artifact authorization test rebinds the source marker and artifact digest before attempting owner-path and unregistered-same-signature substitutions; it does not merely test a stale JSON digest. Candidate order is canonicalized by owner path/helper. Old schema 6 is explicitly rejected and producers are regenerated.

Website export archives and WASM were rebuilt from the same source/dependencies. The compiler digest is `212a777039f8cd995b9f993f4b38aff3909e643a60a151de0e9a9ce495e42580`. All 10 live examples execute under CLI and match browser output byte-for-byte; 8 adapter checks and 8 catalog negatives match native/browser results. Chromium QA passed filters, permalinks, snapshots, clipboard, 390px layout, edit/error recovery, timeout, interrupt, reset, no page/console errors and same-origin-only requests. The publish directory passed its 11-file allowlist and known secret/local-path audit. These are **local candidate build results**, not a deployment claim.

The minimum two-module order example independently passed generated-module race/vet, executable build and expected business output. Attempting hgo test against an ordinary-view producer still correctly rejects its changed serde dependency fingerprint; no compatibility or trust check was weakened. Documentation/design checks and all 13 Go mechanism commands passed; mechanism checks are not substituted for hgo execution.

## Remaining integration boundary

The first TOML-format task (`554c0957a05b3000c0d762f3c49d00042451dc4b`) is integrated and covered by the full tests above. The user subsequently approved a new unified authoring model: migrate go.mod/go.sum into goxide.toml + goxide.lock, with mixed .go/.hgo source. That additional lossless-migration interface is owned by the toolchain task and has not yet been delivered to this integration branch. Current root-go.mod code is a transition for the existing mode, not a permanent authority decision. No project parser/planner work is duplicated here. Public std/serde remain ordinary Go module distributions with go.mod.

Parent joint review and integration of that new interface remain. No main merge, tag/release, visibility change, or website deployment was performed. The live public site remains untouched.
