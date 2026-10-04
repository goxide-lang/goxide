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

This is a checkpoint, **not completed migration or MVP acceptance**. Full final race/vet, regenerated browser archives and CLI/WASM parity, website catalog integration, and joint review with the independently owned goxide.toml task remain outstanding. No main merge or website deployment is performed.
