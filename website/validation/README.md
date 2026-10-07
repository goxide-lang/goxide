# Reproduce the website validation

Run from a **clean committed checkout**, including untracked files. The checkout
must contain the full ancestor objects for compiler `afa3f0a52b2d8206d75df38d10833e537ea7a0dd`
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

The resolved output directory must also have no ancestor `go.mod`. The runner
checks this before creating output and records the module-free playground input
context in `identity.json`. Native playground and browser-QA temporary files must
keep the standalone CLI entry; an unrelated module above the output directory
would otherwise change configuration selection. Website examples and catalog
rejections copy their unchanged input bytes to the same standalone context before
invoking the real CLI. CLI/WASM output equality remains required; A06 configured
module recovery is separately exercised by native acceptance tests.

The seven required phases are WASM build, web build, native comparison, web race,
web vet, browser QA and catalog QA. The runner preserves the original full QA:
97 fixtures and 10 examples byte-for-byte, 13 native diagnostics in LF/CRLF twice (52 observations),
33 baseline/current Go outputs with both byte differences and complete parsed AST
proofs, and 5 baseline/current native program pairs. AST oracle controls run before
comparison: operator, channel direction, type-alias and ellipsis changes must fail;
optional trailing commas and layout/comments can compare equal. Additional child
order and malformed-input controls guard the comparison implementation.

A04 adds 8 current-only fixtures (4 successes and 4 frontend rejections) and one
JSON/enum/match/Clone/Debug example, bringing totals to 105 fixtures and 11 examples.
The frozen `legacy-matrix.json` guards the original input bytes and membership;
new A04 syntax never enters the 33 baseline/current comparisons or 5 native pairs.
`a04-matrix.json` defines independent stdout/error expectations. New fixtures run
with LF and CRLF twice (32 CLI observations), then 4 positive fixtures and the new
example execute as 5 current-only native programs. Their actual exit/stdout/stderr
and expected stdout are recorded separately in `native/a04/`. A failed observation
is saved before the gate fails; no current-only success is attributed to the baseline.
Browser QA adds 16 LF UI observations and 32 LF/CRLF real Worker observations, with
a successful recovery input after each one. The original 13-diagnostic groups keep
their separate 26 UI and 52 raw Worker observations.

A01 adds 12 current-only macro category fixtures (2 successes and 10 explicit
template type-slot rejections), bringing totals to 117 fixtures and 11 examples.
`a01-matrix.json` freezes the A04 input hashes and supplies independent stdout,
template `$` token and ordered macro-frame expectations. Its 48 LF/CRLF CLI,
24 LF UI and 48 real Worker observations each retain recovery checks; two positive
programs also execute natively with exact stdout/exit checks in `native/a01/`.
The opaque-expression controls include call-site local types, nested forwarding
and array lengths. These cases do not enter the legacy 33 comparisons or the A04
matrix, and do not claim general type fragments, ambiguous conversion/single-argument generic
classification or browser cross-package support. Explicit multi-argument generic
type slots (RFC FC-10) have separate native CLI acceptance coverage. Raw output stays under the
external validation directory, including partial Worker results on failure. The
Worker record has `complete`, `observations` and `failure` fields; a protocol error
retains the failed input/index and raw response and still fails the gate. The QA
phase first runs `node website/validation/worker-observations_test.mjs` to check
success, fatal, timeout, wrong-id, crash, readiness and page-evaluation paths.

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

```sh
# Workspace fixtures must discover their own go.work. Do not export GOWORK=off
# for the repository-wide gate; the website runner controls its own environment.
env -u GOWORK go test -race -count=1 -timeout 30m ./...
go vet ./...
python3 scripts/check_design.py
```

Use an installed Go 1.27 toolchain (`GOTOOLCHAIN=local`), the complete pinned module
cache and a writable, resolved temporary directory. A successful website run does
not substitute for these full-tree gates.
