# RFC catalog validation — 2026-10-04

Compiler source remains ae61ec628b55e0fc66ce96d47261c0a6809c0280; no compiler changes. Website source and compiler identity are recorded separately in the public manifest.

- Schema/evidence validation: 21 capability cards cover RFC 0001–0024 and selected research notes. Each card has scope, status, typical usage, limitations, and fixed-revision private evidence. Public export removes private evidence paths/full RFCs.
- Ten single-file examples: real CLI transpile/run with exact stdout assertions. Chromium output matches CLI byte-for-byte for every advertised example.
- Eight catalog rejection cases: CLI and WASM diagnostics agree. Existing eight go_result positive/negative cases also pass.
- Minimal feature/bin project: default true and no-default-features false, both executed in an isolated temporary directory. Reexports remains a deliberately incomplete hgo/Go source snapshot, not a runnable browser project.
- Selected CLI tests pass: TestProjectTargetsAndFeatures, TestVersionFeaturesScriptTargetIntegration, TestBuildScriptCrossTargetRejectedBeforeExecutionCLI.
- General browser suite passes: edits, recovery, rapid edits, clipboard, size rejection, unsupported import, XSS text, keyboard, 390px, watchdog (test-only Worker interception), cancellation/reset, no external requests.
- Catalog browser suite passes: topic/status/support/search filters, empty state, permalink reveals filtered card, all example links, snapshots, 390px, matching version, unavailable manifest state; no console/page errors or compilation uploads.
- Desktop and mobile screenshots inspected. Generated Go is collapsed by default; long source lines scroll within cards rather than widening the page.

Reproduction: build.py, catalog.py, qa.mjs, catalog-qa.mjs, hosting/audit.py. Final branch publication and official HTTPS verification are recorded outside this source commit to avoid recursive websiteCommit updates. This record does not claim whole compiler suite was rerun for a website-only change.
