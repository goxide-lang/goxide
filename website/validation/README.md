# Website validation inputs

The current browser gate is `npm test --prefix website`, using the locked Node
Playwright package and a sandboxed Chromium. See [the website guide](../README.md)
for the matching build, static audit, preview server and verification commands.

`browser/goxide.toml` fixes the package identity and publicly pinned dependencies
for every editable example, package fixture and catalog rejection. The website
build runs these inputs with the stamped CLI, checks expected native output and
byte-identical repeated generation, and freezes their source graph and procedural
macro artifacts for the browser compiler.

[package-files/example](package-files/README.md) supplies the five-file package
example. `catalog-qa.mjs` compares every generated file with that native baseline
and exercises editing, add/delete, diagnostics, recovery and read-only dependencies.
It also checks all catalog rejections and runs nested declarative-macro errors,
unrelated errors and successful recovery in one real Worker, reusing the same
filename with LF and CRLF source bytes.

`browser/edits/serde_new_types.gox` introduces a new consumer type and generic
arguments after the fixed macro snapshot is built. The build runs it natively
and records its repeated Go output in `editingChecks`; the browser gate applies
that edit, compares every byte, then resets to the original example. This checks
that dependency instances follow the current source instead of a prior consumer.

`worker-observations.mjs` retains raw responses before failing an assertion;
its small control tests cover crashes, wrong IDs, missing readiness and timeouts:

```sh
node --test website/validation/worker-observations_test.mjs
```

Compiler semantics and native runtime behavior are tested in `internal/` and
`tests/acceptance/`. Browser output parity does not mean the browser executes Go.
