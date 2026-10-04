#!/usr/bin/env bash
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo=$(git -C "$here" rev-parse --show-toplevel)
export GOTOOLCHAIN=local GOWORK=off GOPROXY=off
export GOCACHE=${GOCACHE:-/tmp/t0-gocache} GOPATH=${GOPATH:-/tmp/t0-gopath}
go_tool=${GO_BIN:-/workspace/goxide-environment/go/bin/go}
export PATH="$(dirname "$go_tool"):$PATH"
work=${1:-$(mktemp -d /tmp/t0-integration.XXXXXX)}
mkdir -p "$work"
if [[ -n $(ls -A "$work") ]]; then echo 'output directory must be empty' >&2; exit 2; fi
printf 'checkout: %s\n' "$(git -C "$repo" rev-parse HEAD)"
"$go_tool" version
"$go_tool" -C "$repo" build -o "$work/hgo" ./cmd/hgo
cp -R "$here/model" "$work/model-input"
"$go_tool" -C "$work/model-input" mod edit "-replace=github.com/hxsf/goxide=$repo"
"$work/hgo" transpile -go "$go_tool" -o "$work/model" "$work/model-input"
cp -R "$here/app" "$work/app-input"
"$go_tool" -C "$work/app-input" mod edit "-replace=github.com/hxsf/goxide=$repo" "-replace=trial.example/model=$work/model"
"$work/hgo" transpile -go "$go_tool" -o "$work/app" "$work/app-input"
for module in "$work/model" "$work/app"; do
    "$go_tool" -C "$module" test -race -count=1 -v ./...
    "$go_tool" -C "$module" vet ./...
done
"$go_tool" -C "$work/app" test -run '^$' -bench . -benchmem -benchtime=200ms -count=3 .
"$go_tool" -C "$work/app" test -run '^$' -fuzz FuzzPipeline -fuzztime=5s -parallel=2 .
"$go_tool" -C "$work/app" build -trimpath -buildvcs=false -o "$work/orders" .
"$work/orders"
printf 'Artifacts: %s\n' "$work"
