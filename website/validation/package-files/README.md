# Same-package Playground fixture

The five authored `.gox` files in `example/` exercise per-file import aliases,
shared declarations and macros, formal std/serde derives, JSON round trips, and
a trait with its implementation and generic calls split across files.

`website/procedural.py` copies these source bytes into the fixed browser consumer,
runs the current stamped CLI, compares two generations and checks this output:

```text
42 GOXIDE Item { Value: 21 }
Ada Ada
```

The fixture's `go.mod` and `go.sum` record the same public std/serde selections as
the compiler. The build rejects pin drift or local replacements. Browser inputs
contain only the `.gox` files; dependencies come from the fixed consumer snapshot.

After the [website build and preview](../../README.md), run the current gate:

```sh
QA_URL=http://127.0.0.1:8084 npm test --prefix website
```

`catalog-qa.mjs` checks byte-for-byte Worker/CLI output, source and generated tabs,
complete clipboard copies, undo and selection preservation, file add/delete,
filename validation, diagnostics, recovery, Unicode package names, late Worker
results, read-only dependency identity and narrow layouts. Chromium keeps its
sandbox enabled; `CHROMIUM` selects an existing browser executable.
