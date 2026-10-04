#!/usr/bin/env bash
set -euo pipefail

repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
export GOTOOLCHAIN=local GOWORK=off GOPROXY=off
go_tool=$(command -v "${GO_BIN:-go}")
case "$("$go_tool" version)" in
    *" go1.27."*) ;;
    *) echo 'Set GO_BIN to the real Go 1.27.x compiler (validated with 1.27.1).' >&2; exit 2 ;;
esac
export PATH="$(dirname -- "$go_tool"):$PATH"
work=${1:-$(mktemp -d "${TMPDIR:-/tmp}/goxide-enum-trial.XXXXXX")}
mkdir -p -- "$work"
work=$(cd -- "$work" && pwd)
if [[ -n $(ls -A -- "$work") ]]; then
    echo 'The output directory must be empty; existing files will not be overwritten.' >&2
    exit 2
fi
printf 'Source checkout: %s\n' "$(git -C "$repo" rev-parse HEAD)"
"$go_tool" version

# The model is one real Go module; app is a separate consumer module.
"$go_tool" -C "$repo" build -o "$work/goxide" ./cmd/goxide
"$work/goxide" transpile -o "$work/model" "$repo/examples/enum_trial/model"
cp -R -- "$repo/examples/enum_trial/app" "$work/input"
"$go_tool" -C "$work/input" mod edit \
    "-replace=github.com/hxsf/goxide=$repo" \
    "-replace=trial.example/model=$work/model"
"$work/goxide" transpile -o "$work/app" "$work/input"
for module in "$work/model" "$work/app"; do
    "$go_tool" -C "$module" test -race -count=1 -v ./...
    "$go_tool" -C "$module" vet ./...
done
"$go_tool" -C "$work/app" build -trimpath -buildvcs=false -o "$work/orders" .
"$work/orders"
printf '\nArtifacts retained in: %s\n' "$work"
printf 'Try another JSON input with: %s/orders '\''{"Name":"7","Retry":null}'\''\n' "$work"
