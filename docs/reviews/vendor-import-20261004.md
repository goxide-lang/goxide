# Native vendor import: implementation and verification

Base: unified model `69c1ab0d6769facd266886deec90337fe4b52498`. Branch: `feat/vendor-import`. The [RFC 0025 vendor contract](../rfcs/0025-unified-project.md#vendor-自动迁入扩展2026-10-04实施合同) was committed first (`7ea4888`); implementation followed in `0575910`, review fixes in `d534c71`. This is an implementation handoff, not a website deployment or release tag.

## Behavior

`import-go` defaults to a read-only plan. With `--out NEWDIR`, it stages source and builds every explicitly selected target before publishing the new directory; existing destinations and author configurations are not overwritten. Original go.mod/go.sum/vendor remain unchanged.

```sh
hgo import-go --go "$GO_BIN" --bin app=cmd/app OLD_ROOT
hgo import-go --go "$GO_BIN" --out NEW_ROOT --bin app=cmd/app --offline OLD_ROOT
hgo run --go "$GO_BIN" --bin app --offline --locked NEW_ROOT
hgo test --go "$GO_BIN" --bin app --offline --locked NEW_ROOT
```

A vendor input automatically adds `[vendor] mode = "go-mod"`. Real Go checks explicit requirements, versions and replacements against modules.txt. Generated modules use `-mod=vendor`; planning never invokes module download/tidy/vendor or fills missing packages from caches/network. Local replacement spelling is preserved because it is part of the vendor consistency contract; the original replacement directories need not remain available for vendor builds.

The lock stores the supplied package/module view and actual file bytes/permissions in a separate vendor record. A local modification is retained, not represented as an upstream h1 checksum. go.sum contributes import provenance only in this mode. Plan output lists native policy, vendor module metadata, file digests, licenses and an explicit unknown upstream-difference status. Bytes are hashed as bytes, including invalid UTF-8 binary resources. Content changes affect the build, metadata and script configuration identities; --locked rejects changed bytes/policy.

Strict checks reject malformed/unknown metadata, workspace vendor, escaping paths, symlinks (including modules.txt itself), special files, nested modules/enhanced source, unowned files, missing package sources and undeclared Go package directories. Native Go remains responsible for the current target's package/build/embed consistency. Go source lowering only grants write permission to its own generated copy; source files and vendor permissions remain preserved.

## Evidence

All Go execution uses `/workspace/goxide-environment/go/bin/go`, Go 1.27.1, GOTOOLCHAIN=local and GOWORK=off. Fixtures are local modules/Git-free file proxies; vendor E2E switches to a new empty GOMODCACHE and GOPROXY=off after creating the source vendor, so module cache warming cannot mask missing vendor support.

- Full `go test -race ./... -count=1`: passed, including CLI 325.589s and acceptance 353.641s; no package failures.
- After source permission/diagnostic fixes, full affected-package race rerun: CLI 308.548s, project 1.194s, frontend 54.247s, all passed.
- Final vendor feature race group: 20.546s, passed, including binary corruption, read-only license, modules.txt symlink, native Go run/test equivalence, cold offline migration, local and version replacement, multi-bin/example/root cfg, authorization and artifact configuration isolation.
- Exact old-version plus unused replacement fixture: 3.215s race, passed. Replacement-only footer records remain provenance, not selected package versions.
- `go vet ./...`: passed after production changes.
- Design checker: Markdown/RFC validation and 13 actual Go commands passed, including expected failures.
- Rebuilt js/wasm playground with real export archives; Node smoke compared ten existing website examples byte-for-byte with CLI generated Go and verified syntax rejection. No site catalog, published assets, hosting or live status changed.
- Independent review independently confirmed Go's vendor consistency check and passed vendor race (8.242s), then final stabilization/read-only cold migration regressions (11.988s). Review found umask permission loss and premature transient-version rejection; both fixed. Binary digest repair additionally reviewed with no remaining finding.

Real E2E checks include modified vendored code despite unchanged upstream go.sum, missing packages, extra Go directories/files, bad versions/explicit/replace metadata, workspace markers, symlinks, nested modules, dual manifests, existing destinations, embed resources, alternate build tags/platform files, unavailable local replace paths, script trust, repeated locked builds and changed content creating a different artifact configuration. Source hashes and destination sentinel files are checked on failure.

## Boundaries

Supports modern single-module `go mod vendor` views (root Go >= 1.17 and Go version metadata for listed package modules), not workspace vendoring or unknown future modules.txt rules. Root features, mixed .go/.hgo and explicit targets work; vendored sources are native Go only. Enhanced dependency graphs cannot be mixed with vendor mode, including a regular root depending on a vendor-author project; rejection occurs after version stabilization and before scripts.

Existing unsupported go.mod directives remain explicit errors: retract/tool/godebug/ignore/module deprecation and unknown directives. Old JSON/format 1/old enhanced lock upgrades are unchanged. No claims are made that vendor is a complete module graph, that dependency tests or omitted packages can be recovered, that upstream bytes match the supplied view, or that host validation proves cross-platform completeness. Resource files in declared packages are preserved; undeclared Go source directories are conservatively rejected even if intended as data. Adding a new dependency requires an explicit vendor update outside this importer.
