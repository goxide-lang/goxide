# Module and re-export research probes

These are hand-written Go lowering experiments and a Python binding-graph model,
not implemented hgo syntax. See the [research RFC](../../../docs/rfcs/research/0007-modules-and-reexports.md).

Run with the real Go compiler:

```sh
python3 tests/research/mod-feasibility/run.py --go /path/to/go \
  --report /tmp/mod-feasibility-results.json
python3 tests/research/mod-feasibility/reexports.py
```

`run.py` creates disposable modules and runs both positive and expected-negative
cases. It disables network module resolution and VCS stamping; it never mutates
the production frontend. Successful applicable cases use `go test -race` and
`go vet`; negative cases require a specific compiler diagnostic. The recorded
`results.json` came from Go 1.27.1 and includes command exit statuses and outputs.
Set GOCACHE/GOMODCACHE to writable existing locations if your sandbox requires it.

`reexports.py` has no lexer/parser or scope checker. Its input is an already
authorized declaration graph; it only explores identity-preserving name binding,
finite fixed-point glob propagation, and conflict rules. Glob-cycle convergence
does not imply a corresponding Go package cycle can compile.
