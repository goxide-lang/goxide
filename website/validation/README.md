# Reproduce the website validation

Run from a **clean committed checkout**, including untracked files. The checkout
must contain the full ancestor objects for compiler `e36d23dacee64d19b34aac639900c3f5c29e55c2`
and baseline `fd8380c39876c96441857a737be8e32c322a0467`. The runner never fetches
objects, installs tools, downloads dependencies, deploys assets or triggers CI.
It needs Python 3.11+, Git, installed Go 1.27 with a working race/C toolchain,
Node.js 18+, an existing Chromium executable, and Playwright/Playwright-core matching
`website/package-lock.json` (1.58.2). Go's module cache must already contain the
complete source graph pinned in `go.mod`/`go.sum`, including platform-specific
module sources needed by project snapshot checks. Missing cached dependencies
fail with `GOPROXY=off`; a warm build cache alone is insufficient.

```sh
python3 -B website/validation/run.py \
  --out /absolute/new-validation-directory \
  --chromium /absolute/existing/chromium \
  --playwright-node-modules /absolute/existing/node_modules
```

The output parent must exist; the output directory must not exist and must be
outside this checkout. Each run gets fresh evidence and isolated committed
`current` and `baseline` checkouts. Optional `--go`, `--node`, `--go-cache`,
`--mod-cache` select existing tools/caches; otherwise Go uses its configured
cache paths. `--port` selects an unused loopback port (default 18084). The runner
copies only the explicitly selected installed Playwright packages; it does not
copy an earlier task's compiler, website source, overlay, oracle or build outputs.
All generated assets, executable files, temporary projects and QA artifacts stay
under the output directory. The selected caches may acquire build cache entries.
The source checkout and all tracked snapshot inputs must remain unchanged.

The seven required phases are WASM build, web build, native comparison, web race,
web vet, browser QA and catalog QA. The runner executes the unchanged full QA:
97 fixtures, 10 examples, 13 native diagnostics in LF/CRLF twice (52 observations),
33 baseline/current Go outputs with both byte differences and complete parsed AST
proofs, and 5 baseline/current native program pairs. AST oracle controls run before
comparison: operator, channel direction, type-alias and ellipsis changes must fail;
optional trailing commas and layout/comments can compare equal. Additional child
order and malformed-input controls guard the comparison implementation.

`summary.json` is authoritative: only all seven successful phases plus final
identity verification produce `passed: true` and exit 0. Any failed command keeps
separate stdout/stderr, argv, cwd, exit status and time in `logs/` and `commands.json`;
launch errors, preflight failures and blocked dependent phases remain explicit.
Independent phases can continue after a native-comparison failure. Output folders
are never reused or cleaned, so failed runs cannot be replaced with later success.
The runner stops its loopback web service even when QA fails.

`identity.json` separates the **validation tool/site commit and tree** from the
**compiler pin and production digest**, and records tools, caches, LICENSE origin,
public library identities, generated WASM/export identities and matrix counts.
`source-inputs.json` hashes every tracked source/test/QA/tool/document/snapshot input;
`current-inputs.json`, `baseline-inputs.json` and `playwright-inputs.json` record
materialized inputs. Native binary identities and original generated Go files,
byte diffs, AST proofs, diagnostic observations and stdout/stderr pairs are in
`native/`. Byte equality is never inferred from AST equality: the reviewed compiler
pair previously had 27 equal and 6 different outputs, with all 33 ASTs equal.

CLI/WASM parity compares the **same current compiler's** output byte-for-byte.
Baseline/current AST/behavior comparisons are separate evidence. Final Go type
rejections run natively; the WASM frontend does not perform that last Go compilation.
UI inputs verify LF because textareas normalize newlines; raw real Worker inputs
verify LF/CRLF. Cross-package CLI diagnostics and effect/frame propagation consumers
are covered by separate native acceptance tests, not this single-file browser suite.
No Linux or hosted CI success is implied by a successful Mac run.

Small checks, before the full run:

```sh
python3 -B website/validation/run_test.py
python3 -B website/build_support_test.py
go test -count=1 ./website/validation
```

The Go package includes AST controls and invokes the Python runner's failure-path
checks, so these tests also run under `go test -race -count=1 -timeout 30m ./...`.
The full compiler race/vet/design gates remain separate from the seven website
phases and must be recorded for the exact implementation commit.
