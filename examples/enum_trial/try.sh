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

# Compile both Goxide modules from source; the exported Go modules stay separate.
"$go_tool" -C "$repo" run ./scripts/build.go -o "$work/goxide"
mkdir -p -- "$work/source"
for module in model app; do
    cp -R -- "$repo/examples/enum_trial/$module" "$work/source/$module"
    rm -- "$work/source/$module/go.mod" "$work/source/$module/go.sum" "$work/source/$module/goxide.roots.toml"
done

write_dependencies() {
    local dependency pin alias
    for dependency in github.com/goxide-lang/std github.com/goxide-lang/serde; do
        alias=${dependency##*/}
        pin=$("$go_tool" -C "$repo" list -m -f '{{.Version}}' "$dependency")
        printf '\n[dependencies.%s]\ngo = "%s"\nversion = "%s"\n' "$alias" "$dependency" "$pin"
    done
}
cat > "$work/source/model/goxide.toml" <<'EOF'
[go_mod]
module = "trial.example/model"
go = "1.27"

[lib]
name = "config"
path = "config"

EOF
write_dependencies >> "$work/source/model/goxide.toml"
cat > "$work/source/app/goxide.toml" <<'EOF'
[go_mod]
module = "trial.example/app"
go = "1.27"

[[bin]]
name = "orders"
path = "."

[dependencies.config]
path = "../model"
module = "trial.example/model"

[dependencies.errors]
go = "errors"

[dependencies.fmt]
go = "fmt"

[dependencies.os]
go = "os"

[dependencies.strconv]
go = "strconv"
EOF
write_dependencies >> "$work/source/app/goxide.toml"
app_module=$("$work/goxide" transpile --offline -o "$work/generated" "$work/source/app")
model_module=$("$go_tool" -C "$app_module" list -m -f '{{.Dir}}' trial.example/model)
for module in "$model_module" "$app_module"; do
    "$go_tool" -C "$module" test -race -count=1 -v ./...
    "$go_tool" -C "$module" vet ./...
done
"$go_tool" -C "$app_module" build -trimpath -buildvcs=false -o "$work/orders" .
"$work/orders"
printf '\nArtifacts retained in: %s\n' "$work"
printf 'Try another JSON input with: %s/orders '\''{"Name":"7","Retry":null}'\''\n' "$work"
