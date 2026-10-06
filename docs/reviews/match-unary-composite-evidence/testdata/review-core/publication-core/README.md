# A04 independent review: core reproducer

This small package reproduces the reported function-type failure family and final lexical/flow increment: six function-type modules, two closure-order modules, and one flow module containing seven Test functions. It is not the complete historical review corpus, and module counts are not added to Test-function counts.

Prerequisites are an existing Go 1.27 toolchain, an already built goxide CLI, its matching source checkout, and cached pinned modules. The runner performs no installation, fetch, download, or global configuration change. It copies only the recorded std/serde version and checksum declarations into temporary probe modules. GOSUMDB is preserved. Optional GOCACHE/GOMODCACHE/TMPDIR overrides can select existing writable locations.

From this directory:

```sh
python3 reproduce.py --compiler /absolute/path/to/goxide \
  --compiler-source /absolute/path/to/matching/goxide-checkout \
  --out /absolute/path/to/new-review-output
```

The output directory must not exist. `--case function-shapes/slice-pointer` selects the smallest central regression; omit it for all nine modules. The runner records compiler hash/build information, source HEAD/status, exact commands, raw stdout/stderr, and exit statuses. A failing historical compiler returns a nonzero overall status; it is not mislabeled a harness success.

Observed source identities:

- `ee5949d80c11b513a40700f8ec03dd7050a77876`: five function-type shapes rejected; the returned-real-function control ran successfully.
- Candidate v3: structural function-type recognition fixed two shapes, but three pointer-element shapes still failed because their type `*` was split as binary multiplication. This dirty snapshot is not attributed to a Git commit.
- `f2d7a39c4b81a00fff191c89ac847b09af6a62e9`: all six shapes, two closure-order cases, and flow tests passed using candidate-v4 bytes bound to this exact production source.

The canonical compiler inventory has 123 files at the final source commit. The reviewer also compared `cmd/goxide-web/main.go`, so the broader byte-binding manifest has 124 files. That extra file is a website server source, excluded from the native compiler inventory and compiler digest.

This portable runner was prepared for publication after validation; it has been syntax-checked, not executed as another test run. Published input and assertion files are byte copies of the actual tested cases. Use the retained raw results for the recorded validation, and this runner for a fresh reproduction.
