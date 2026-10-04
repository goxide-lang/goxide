# Unified project model: implementation and verification

Historical snapshot for the initial unified model. Its blanket vendor-import exclusion is superseded by the [native vendor import extension](vendor-import-20261004.md); other boundaries remain as stated.

This branch is based on public std/serde integration `e4d7200bc25c8daf646bf5e3b665b4b13e135e6f`. It preserves schema 7 and the published std/serde module versions. The authoring contract is [RFC 0025](../rfcs/0025-unified-project.md); feature/target semantics remain [RFC 0022](../rfcs/0022-project-toolchain.md).

## Delivered behavior

- Format 2 explicitly owns module, Go/toolchain suggestion, native require/indirect, replace and exclude alongside features, enhanced dependencies and targets. Author roots reject go.mod/go.sum and legacy hgo.lock; native Go dependencies remain supported.
- Source Go module metadata is synthesized only in generated planning/compilation views. All graph nodes receive metadata anchors before source generation. Pure TOML path and Git sources do not need checked-in Go metadata.
- Published enhanced source retains Go proxy transport metadata. Its native policy must agree with TOML independent of directive order; unsupported directives and module deprecation are rejected rather than discarded.
- Schema 2 goxide.lock records actual MVS plans, checksums, exact Git commits and optional import provenance. Verified historical checksum entries survive configuration A/B/A switches; imported historical sums are validation input, not a selected-version list.
- `import-go --plan` reads a plain Go module and prints candidate TOML. `--out NEWDIR` copies into staging, builds every explicitly selected target, and publishes only on success. It retains original files, rejects existing destinations/configuration and unsupported workspace/vendor/directives, and preserves local replace identity using absolute paths.

## Commands

```sh
hgo import-go --go "$GO_BIN" --plan --bin app=cmd/app OLD_ROOT
hgo import-go --go "$GO_BIN" --out NEW_ROOT --bin app=cmd/app --offline OLD_ROOT
hgo run --go "$GO_BIN" --bin app --offline --locked NEW_ROOT
hgo build --go "$GO_BIN" --bin web --features server PROJECT
hgo run --go "$GO_BIN" --example demo --no-default-features PROJECT
```

Use repeated `--bin name=path` for migration of multiple commands, or `--lib path` for a library. Regular build commands select one named bin/example at a time. Examples are explicit build targets and are not automatically published.

## Evidence

All Go checks use `/workspace/goxide-environment/go/bin/go` (Go 1.27.1), with GOTOOLCHAIN=local and GOWORK=off. Fixtures use temporary modules, local Git and file proxies; no private dependencies or credentials are used.

- Full integrated race coverage: first run passed CLI (296.210s), project/buildscript and the remaining unaffected packages. Eight packages started before the newly integrated public std/serde cache was warmed and failed offline; all eight affected packages passed on rerun with that exact cache populated (enums 6.745s, equality 18.346s, frontend 47.240s, macros 3.677s, propagate 5.801s, typecheck 1.271s, acceptance 315.746s, prototype 6.848s). This is full package coverage across the initial run and affected-package rerun, not a claim that the cold first command passed.
- Full CLI race suite after managed preflight and checksum migration: passed (295.545s). The final nested-manifest boundary tightening additionally passed the migration/build-script regression group (CLI 149.349s, buildscript 1.026s).
- `go vet ./...`: passed after final production edits.
- `python3 scripts/check_design.py`: passed; Markdown links/RFC structure plus 13 real Go commands, including expected failures.
- New checksum import race test: passed (4.116s), covering cold offline failure, warm import, first offline locked run, incorrect checksum rejection and original source hashes.
- Independent review: transport ordering and deprecated/unsupported directive loss found and fixed. Reviewer independently passed migration/source protection/pure TOML sources (10.873s) and transport/failure checks (project 1.038s, CLI 1.988s); final anchor placement, example locks and nested TOML module boundary guards reviewed without remaining findings.
- Repository examples: hello with defaults -> `Hello, goxide!`; defaults off -> `hello`; inspect -> `pretty: true`; greet -> `example: Hello, goxide!`; library build and locked hello passed. Build-script example generated/embedded Greeting v1, then reported a cache hit with --locked. Public std/serde cache was explicitly warmed before offline checks.

The two committed sample locks were regenerated from actual selected graphs and contain no staging paths. The old author go.mod/go.sum and sample legacy lock are removed only as reviewed repository fixture changes; the migration command never removes users' originals.

## Limits

The import command accepts a plain Go project, not an automatic upgrade of existing format 1/JSON projects or old enhanced locks. It rejects workspace, vendor, retract/tool/godebug/ignore/module deprecation and unknown directives. Publishing adapters, target-specific or mutually exclusive features, workspace resolvers, cross compilation and more permissive package-level interleaving remain outside this change. Go proxy source transport still requires valid Go module metadata; pure TOML Git/local authoring is a distinct input channel. Locks require warmed dependency bytes for offline builds and do not freeze local path contents.
