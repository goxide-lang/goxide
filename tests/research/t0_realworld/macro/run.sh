#!/usr/bin/env bash
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(cd -- "$here/../../../.." && pwd)
GO_BIN=${GO_BIN:-/workspace/goxide-environment/go/bin/go}
export GOCACHE=${GOCACHE:-/tmp/t0-gocache} GOPATH=${GOPATH:-/tmp/t0-gopath}
export GOTOOLCHAIN=local GOPROXY=off GOWORK=off
work=${1:-$(mktemp -d /tmp/t0-macro-review.XXXXXX)}
mkdir -p "$work"
work=$(cd -- "$work" && pwd)
printf 'checkout=%s\nwork=%s\n' "$(git -C "$root" rev-parse HEAD)" "$work"
"$GO_BIN" version
(cd "$root" && "$GO_BIN" build -o "$work/hgo" ./cmd/hgo)
failures=0
for name in shadow_constants shadow_types; do
    if "$work/hgo" transpile -go "$GO_BIN" -o "$work/$name" "$here/$name" > "$work/$name.compile.log" 2>&1; then
        # Also accepts a future implementation that preserves definition identity.
        (cd "$work/$name" && "$GO_BIN" run -race .) | tee "$work/$name.actual.txt"
        if ! diff -u "$here/$name/expected.txt" "$work/$name.actual.txt"; then failures=$((failures+1)); fi
        if ! (cd "$work/$name" && "$GO_BIN" vet ./...); then failures=$((failures+1)); fi
    else
        cat "$work/$name.compile.log"
        if ! rg -q 'template primitive .*shadowed by a caller package declaration' "$work/$name.compile.log" ||
           ! rg -q 'macros.hgo:[0-9]+:[0-9]+:' "$work/$name.compile.log" ||
           ! rg -q 'invoked at .*main.hgo:[0-9]+:[0-9]+' "$work/$name.compile.log"; then
            printf '%s: unexpected or unlocated rejection\n' "$name"
            failures=$((failures+1))
        fi
    fi
done
"$work/hgo" transpile -go "$GO_BIN" -o "$work/differential" "$here/differential"
(cd "$work/differential" && "$GO_BIN" test -race -count=1 ./...)
(cd "$work/differential" && "$GO_BIN" vet ./...)
(cd "$work/differential" && "$GO_BIN" test -run '^$' -fuzz '^FuzzFlow$' -fuzztime=3s -parallel=2)
(cd "$work/differential" && "$GO_BIN" test -run '^$' -bench . -benchmem -count=2 -benchtime=200ms)
(cd "$here" && "$GO_BIN" test -run '^$' -bench . -benchmem -benchtime=1x -count=2)
printf 'Regression failures: %s\n' "$failures"
test "$failures" -eq 0
