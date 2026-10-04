# Expression switch propagation evidence

`run.py` builds the real goxide CLI and transpiles the
[switch example](../../../examples/prototype/switch_propagation.gox). It checks
that generated code contains no Assignment/Catch/Fail, runs its separate module
with race and vet, then measures non-race benchmarks. This module's one
input-grid equivalence test is separate from the six main switch behavior tests
and the cross-package private-type acceptance test.

```sh
PATH=/workspace/goxide-environment/go/bin:$PATH GOCACHE=/tmp/goxide-cache \
  python3 tests/research/switch_propagation/run.py --count 5 --benchtime 200ms
```

Pass `--go /path/to/go` elsewhere. The [manifest](results/manifest.json) records
parent revision, dirty stage files, toolchain and exact commands.
[Raw samples](results/bench.txt), [summary](results/summary.json),
[race](results/race.txt) and [vet](results/vet.txt) are retained.

Both source implementations evaluate the same Option-returning tag and cases,
validate/extract with propagation or explicit match, and return identical small
integer payloads. Success matches the second case; failure occurs while obtaining
that second case after the first misses. Tests compare n=0..4 and fail=-1..4.
A global sink retains the results. Timing uses GOMAXPROCS=1, no race instrumentation,
200ms × 5.

| Case | Propagated median ns/op | Explicit match median ns/op | Both B/op, allocs/op |
| --- | ---: | ---: | --- |
| Second case success | 10.500 | 9.392 | 0, 0 |
| Second case failure | 8.094 | 7.895 | 0, 0 |

Every sample is zero-allocation for this fixture. These results do not establish
speed parity, arbitrary payload allocation behavior, or support for all case
expressions. This is real goxide evidence, unlike the separately labeled hand-edited
Go probe in the loop performance follow-up.
