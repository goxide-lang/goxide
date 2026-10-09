# Same-package playground acceptance

This focused suite compares real browser Worker output with the current native
CLI, then exercises the actual page. It preserves the legacy single-file examples
and checks same-package file scopes, cross-file macros/derive, filenames in
diagnostics, invalid inputs, LF/CRLF, recovery, repeat compilation, source/output
tabs, file editing/add/delete, example changes, and read-only dependency identity.
Saved events from real Workers test both old request IDs and obsolete Worker
instances deterministically. Helper-file clipboard copy, 390px mobile selectors
and overflow, served WASM/dependency source hashes, and actual toolchain/module
versions are also checked. The browser is launched with its sandbox enabled.

Use an already-built matching website and CLI; serve the generated website on
loopback in the tool-approved context. No installation, network module fetch,
full-tree validation, deployment, or build scripts are performed by this runner.
The output directory must not already exist and should be outside the checkout.

```sh
source /workspace/.cloud-setup/env.sh
export GOROOT=/workspace/.cloud-setup/toolchains/go1.27.1
export GO_BIN="$GOROOT/bin/go"
export PATH="$GOROOT/bin:$PATH"
hash -r
"$GO_BIN" version
python3 -B website/validation/package-files/run.py \
  --url http://127.0.0.1:18085 \
  --cli /absolute/path/to/current/goxide \
  --go "$GO_BIN" \
  --out /tmp/goxide-package-files-acceptance
```

`summary.json` is authoritative; `passed: true` requires native expectations,
Worker byte parity, all UI checks, and no page exceptions. Native commands and
their stdout/stderr, complete Worker observations, manifest identity, and a page
screenshot remain in that output directory. Go subprocesses use native
`GOMAXPROCS=4`; this runner does not invoke Node Go WASM. The test fixture's
`go.mod` fixes the CLI package path to `playground`; only its `.gox` files become
editable browser inputs. Expected native stdout is:

```text
42 GOXIDE Item { Value: 21 }
```
