#!/usr/bin/env bash
# Run only on artifacts produced by run.sh; never invoke hgo on the edited copy.
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
source_app=${1:?usage: compare_direct.sh GENERATED_APP NEW_EXPERIMENT_DIR}
experiment=${2:?usage: compare_direct.sh GENERATED_APP NEW_EXPERIMENT_DIR}
go_tool=${GO_BIN:-/workspace/goxide-environment/go/bin/go}
export GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOMAXPROCS=1
export GOCACHE=${GOCACHE:-/tmp/t0-gocache} GOPATH=${GOPATH:-/tmp/t0-gopath}
python3 "$here/direct_calls.py" "$source_app" "$experiment"
"$go_tool" -C "$experiment" test -race -count=1 ./... > "$experiment/checks.txt" 2>&1
"$go_tool" -C "$experiment" vet ./... >> "$experiment/checks.txt" 2>&1
for variant in original direct; do
    app=$source_app
    if [[ $variant == direct ]]; then app=$experiment; fi
    "$go_tool" -C "$app" test -run '^$' -bench . -benchmem -benchtime=200ms -count=3 . > "$experiment/$variant-bench.txt"
    "$go_tool" -C "$app" test -run '^$' -gcflags='-m=2' . > "$experiment/$variant-escape.txt" 2>&1
done
printf 'Experiment passed race/vet. Evidence: %s/{original,direct}-{bench,escape}.txt\n' "$experiment"
