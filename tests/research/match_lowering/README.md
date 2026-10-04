# Match expression and addressable enum probes

Research at `6380b6788333485a2fdd24e6558da0aba264df68`. These are lowering
candidates, not newly implemented goxide syntax. They do not freeze the enum ABI.

The [Go source](match.go.fixture) and [tests](match_test.go.fixture) compare typed
IIFEs with statement preludes using integer tag switches. They deliberately omit
the current goxide interface enum representation, validators and permission checks;
timings are not end-to-end compiler output measurements. Pointer cases return
existing globals, not newly allocated values. Shared-machine microbenchmarks do
not establish a stable speed advantage.

Go 1.27.1 linux/amd64, GOMAXPROCS=1, non-race, 5 samples × 200ms:

| Case | IIFE median ns/op | Prelude median ns/op | Both B/op, allocs/op |
| --- | ---: | ---: | --- |
| int | 0.572 | 0.5744 | 0, 0 |
| named Word | 0.6387 | 0.5539 | 0, 0 |
| existing pointer | 0.6233 | 0.7079 | 0, 0 |
| function argument | 3.008 | 2.923 | 0, 0 |
| short circuit | 0.6613 | 0.6875 | 0, 0 |
| indexed assignment | 0.6703 | 0.612 | 0, 0 |

[Raw samples](results/bench.txt), [40-arm samples](results/large-bench.txt),
[inline/escape diagnostics](results/escape.txt) and [semantic output](results/semantic.txt)
are retained. Diagnostics show simple literals inline; argument-case outer functions
both exceed their budget. The 40-arm IIFE literal also inlines in this toolchain,
while the statement function does not. This is an observed compiler decision,
not a promise for arbitrary arms or future Go versions.

Reproduce the pure Go module in a new temporary directory:

```sh
probe_dir=$(mktemp -d)
cp tests/research/match_lowering/match.go.fixture "$probe_dir/match.go"
cp tests/research/match_lowering/match_test.go.fixture "$probe_dir/match_test.go"
printf 'module probe.example/iife\ngo 1.27\n' > "$probe_dir/go.mod"
go -C "$probe_dir" test -race -count=1 ./...
go -C "$probe_dir" vet ./...
GOMAXPROCS=1 go -C "$probe_dir" test -run '^$' -bench=. -benchmem -count=5 -benchtime=200ms
go -C "$probe_dir" test -gcflags=-m=2 -run '^Test'
```

The semantic tests include once-only/selected effects, argument order, short
circuiting and indexed write panic timing. They also demonstrate that wrapping
`recover()` changes its meaning. Expression-only arms therefore still need a
frame-sensitive exclusion; absence of `?` alone is insufficient.

Separate **actual goxide** evidence uses [app.gox](goxide/app.gox.fixture) and
[model.gox](goxide/model/model.gox.fixture) in module `iife.example/app`. Explicit
user-written IIFEs containing statement match successfully return int and an
imported named uint64 type, including `1<<63`; generated Go passes a separate race
assertion. The [reference-result version](reference-iife-rejected.gox.fixture)
currently fails in refs.Prepare. Raw `*int` benchmark results do not establish
goxide reference-IIFE support or permission preservation. No automatic expression
wrapping has been added to the frontend.

The [addressable enum probe](place.go.fixture) is a separate pure Go program:
`go run -race tests/research/match_lowering/place.go.fixture` is not a valid command;
copy it to a `.go` file first. It validates independent value copies, stable payload
slots across whole enum replacement, and ordinary shallow slice copying. See the
[design assessment](../../../docs/rfcs/research/match-place-and-expression-options.md)
for API and comparison consequences; it is not goxide borrow-match acceptance.

Actual goxide reproduction from the repository root (use the real Go executable):

```sh
source_dir=$(mktemp -d)
output_parent=$(mktemp -d)
mkdir "$source_dir/model"
cp tests/research/match_lowering/goxide/go.mod.fixture "$source_dir/go.mod"
cp tests/research/match_lowering/goxide/app.gox.fixture "$source_dir/app.gox"
cp tests/research/match_lowering/goxide/model/model.gox.fixture "$source_dir/model/model.gox"
go run ./cmd/goxide transpile -o "$output_parent/generated" "$source_dir"
cp tests/research/match_lowering/goxide/iife_test.go.fixture "$output_parent/generated/iife_test.go"
go -C "$output_parent/generated" test -race -count=1 ./...
go -C "$output_parent/generated" vet ./...
```

[Actual generated-module race](results/goxide-race.txt) and [vet](results/goxide-vet.txt)
are separate from [pure mechanism race](results/mechanism-race.txt) and
[mechanism vet](results/mechanism-vet.txt). Replacing source app.gox with the
reference-result fixture demonstrates the current frontend limitation; it must
not be described as a successful permission test.

The [existing comparison source](comparison.gox.fixture) is actual goxide, separately
transpiled and executed with race: [output](results/comparison-race.txt). It proves
that unit-case equality and enum/nil comparison are currently accepted, even when
another variant contains a slice; comparing the active slice case panics. A tagged
struct migration must account for these existing source programs. The separate
[payload-place output](results/place-race.txt) only validates the Go storage model.

## Production binary/index preludes

The later production slice implements binary-expression and simple indexed-target
matches as statement preludes in the original function. The frontend's temporary
function-literal atoms are compile-time syntax only and are absent from emitted
Go. It does not change enum storage. Unlike the earlier integer-tag IIFE probe,
these benchmarks use actual goxide CLI output, including current interface-enum
validators, against explicit goxide statement-match baselines.

[Executable acceptance/benchmark fixture](../../acceptance/match_binary_index_test.go)
and [raw optimized samples](results/binary-index/bench.txt): Go 1.27.1 linux/amd64,
GOMAXPROCS=1, default optimization, non-race, 5 × 200ms.

| Case | Expression median ns/op | Explicit match median ns/op | Both B/op, allocs/op |
| --- | ---: | ---: | --- |
| `return 10 + match` | 3.173 | 3.247 | 0, 0 |
| indexed assignment | 4.489 | 4.504 | 0, 0 |

These small shared-machine samples show no allocation overhead in the measured
cases; they do not establish a stable speed difference or arbitrary-arm guarantee.
No new runtime IIFE is needed to obtain these results. The independent generated
module also runs four race tests and vet, covering effects/storage/failure and
function-frame semantics; the earlier pure-Go IIFE results remain separate.

Reproduce from the repository with the real Go toolchain on PATH:

```sh
GOMAXPROCS=1 GOXIDE_MATCH_BENCH=1 go test ./tests/acceptance \
  -run '^TestMatchBinaryAndIndexedModules/execution$' -count=1 -v
```
