# Loop propagation evidence

This is an actual hgo → Go experiment, not a hand-written Go lowering probe.
`run.py` builds the repository CLI, transpiles the
[loop example](../../../examples/prototype/loop_propagation.hgo), checks for absence
of Assignment/Catch/Fail, runs a separate generated module with race and vet, then
runs non-race benchmarks. The main acceptance module separately exercises ten
behavioral tests; the research module's one equivalence test is not ten additional
feature tests.

```sh
PATH=/workspace/goxide-environment/go/bin:$PATH GOCACHE=/tmp/goxide-cache \
  python3 tests/research/loop_propagation/run.py --count 5 --benchtime 200ms
```

For another environment pass `--go /path/to/go`. The script rejects a non-Go
executable. [Manifest](results/manifest.json) records toolchain, commands, parent
revision and dirty stage files. [Raw benchmarks](results/bench.txt),
[summary](results/summary.json), [race output](results/race.txt) and
[vet output](results/vet.txt) are retained.

Both implementations check the same Option values in init, each condition and
post. Success runs 16 body iterations; failure occurs at post value 7. Inputs,
small integer payloads and final results are identical; a global sink retains the
result. The explicit variant uses match for each protocol check, not native Go
error tuples. There is no race instrumentation in the timing run.

| Case | Propagated median ns/op | Explicit match median ns/op | Both B/op, allocs/op |
| --- | ---: | ---: | --- |
| Success | 39.31 | 26.64 | 0, 0 |
| Failure | 21.00 | 12.69 | 0, 0 |

All five samples of every group meet the zero-allocation check. The propagated
loop is slower in these measurements; this is allocation evidence, not timing
parity or a speedup claim. The result does not cover escaping iteration-variable
captures, larger boxed values, Result error construction, or unimplemented loop
shapes. Performance tuning is separate from preserving those semantics.
