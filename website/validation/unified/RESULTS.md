# Website release validation — unified project compiler

Compiler baseline: 69c1ab0d6769facd266886deec90337fe4b52498. Website changes do not modify production compiler code. Public std 3f2204770fd3 and serde a1b791e96d66 were fetched through ordinary Go modules and verified with go mod verify; public Git refs independently match. No local replace or copied compiler history is published.

The integrated compiler's full race evidence is inherited from docs/reviews/unified-project-20261004.md: the first cold-cache command had eight affected package failures; those eight passed after dependency cache warming, while other packages passed initially. This is combined package coverage across runs, not one all-green cold command. This website task does not claim another full-tree race run.

Current release-specific checks:

- Real CLI build, transpile and execution for all ten single-file examples, exact expected stdout; ordinary Option/Result context constructors and inferred patterns now appear in the public examples.
- Ten catalog negative diagnostics match browser output; eight existing Result adapter checks also pass.
- Targeted CLI race group passed (71.527s): import-go plan/publish/mixed/locked, source protection/failures, checksums/offline, pure TOML Git/local sources, target boundaries, features and version/script/target integration.
- Real minimal feature project uses a TOML author manifest, no root Go metadata, actual schema 2 lock and A/B/A runs. Real import-go example checks read-only plan, unchanged originals, new output directory, target Go metadata, and first offline+locked execution. Its stdout is captured, with only local project paths normalized; verification assertions are identified separately.
- Browser general suite passes ten examples, eight adapter cases, edit/error recovery, rapid input, clipboard, size/unsupported imports/XSS, keyboard, 390px, test-only watchdog, interrupt/reset and no external requests.
- Catalog browser suite passes 22 topics, ten examples, ten rejection cases, filters, permalink restoration, real snapshots, clipboard, mobile layout, unavailable-manifest state and stale/corrupt WASM fail-closed. WASM hash is recomputed by browser and Worker before use.
- Independent read-only reviewer verified exact production source bytes/file inventory against baseline, module/schema/hash identity, limited public allowlist, privacy scope and status claims. Six inference/readability tests and two import-go tests passed independently (non-race). The stdout wording finding was repaired and independently confirmed resolved; no remaining blocker.

Only 11 static assets plus the public README are published. The manifest includes compiler/site identities, actual WASM SHA-256, export-data digest, public module versions and checksums. RFC 0024 remains proposed; next has no published API; browser cannot run Go, scripts or projects. RFC 0025 CLI status is separate from browser support. Final source-commit rebuild and official Pages acceptance are recorded in the release owner's evidence directory, avoiding recursive websiteCommit changes.
