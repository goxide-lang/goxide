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

## First-flag follow-up: isolate part of the overhead

The original stage measured 39.31/21.00 ns (success/failure), against explicit
match at 26.64/12.69 ns. An independent same-run follow-up retained every
benchStep call, sum validation, condition and result construction. A
[hand-edited generated-Go probe](results/firstflag/probe.go.fixture) removed only
the first flag and moved post to the body tail for this capture-free benchmark.
This probe is **not production hgo support**: applying it to short-declaration
loops with captured iteration variables would violate the validated Go semantics.

| Path | Success median ns/op | Failure median ns/op |
| --- | ---: | ---: |
| Original generated loop | 38.90 | 18.41 |
| No-first / tail-post probe | 36.59 | 17.00 |
| Explicit match | 26.85 | 13.21 |

[Raw samples](results/firstflag/bench.txt) are non-race, GOMAXPROCS=1, 200ms × 5;
all are zero-allocation for these payloads. The probe's equivalence grid covers
n=0..19 and fail=-1..20 and passes [race](results/firstflag/race.txt).
[Compiler diagnostics excerpt](results/firstflag/inline-excerpt.txt) shows all
three outer functions exceed the inlining budget (costs 297, 287, 400 vs 80),
while benchStep is inlineable (22). Non-inlining is not unique to propagation.
[Original assembly](results/firstflag/propagated.asm.txt) and
[probe assembly](results/firstflag/probe.asm.txt) show removal of the first test
and flag save/restore, with stack frame 88 → 80 bytes. The remaining gap is not
explained by that branch alone; timings also include layout/register changes.
The assembly still contains convT64: small payload zero-allocation results do not
extend to arbitrary boxed sums.

Reproduce the mechanism comparison after running the regular script with
`--out /tmp/loop-control`; copy the two `.go.fixture` files from
`results/firstflag` into that generated directory as `probe.go` and `loop_test.go`
(overwriting only its benchmark test fixture), then run:

```sh
go test -race -count=1 ./...
GOMAXPROCS=1 go test -run '^$' -bench . -benchmem -count=5 -benchtime=200ms
go test -c -gcflags=-m=2 -o audit.test
go tool objdump -s '^loopfixture.propagatedLoop$' audit.test
```

Use the same real Go toolchain/environment as above. The [manifest](results/firstflag/manifest.json)
records the base revision and source hashes. Production loop lowering remains
unchanged; a safe broader optimization needs separate evidence and scope.
