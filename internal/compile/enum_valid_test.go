package compile

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnumValidCrossModulePrivateTypes(t *testing.T) {
	for _, mode := range []string{"ordinary", "owner_bool_shadow", "generic_bool_parameter"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			model, bridge, app := filepath.Join(root, "model"), filepath.Join(root, "bridge"), filepath.Join(root, "app")
			writeProjectFile(t, model, "goxide.toml", `[go_mod]
module="example.org/enumvalid/zmodel"
go="1.27"
[lib]
name="model"
path="."
`)
			owner := `package model
pub enum Event[T any] { Payload(T) }
enum secret[T any] { secretValue(T) }
type opaque struct { Value int }
pub func MakeSecret() secret[opaque] { return secret[opaque]::secretValue(opaque{Value: 1}) }
pub func MakePublic() Event[opaque] { return Event[opaque]::Payload(opaque{Value: 2}) }
`
			if mode == "generic_bool_parameter" {
				owner = strings.Replace(owner, "enum secret[T any] { secretValue(T) }", "enum secret[bool any] { secretValue(bool) }", 1)
			}
			writeProjectFile(t, model, "model.gox", owner)
			if mode == "owner_bool_shadow" {
				writeProjectFile(t, model, "shadow.go", "package model\ntype bool int\n")
			}
			// The owner initially has no caller requesting a validity helper.
			if _, err := PrepareProject(model, Options{Offline: true, Lib: true}); err != nil {
				t.Fatal(err)
			}
			writeProjectFile(t, bridge, "goxide.toml", `[go_mod]
module="example.org/enumvalid/mbridge"
go="1.27"
[lib]
name="bridge"
path="."
[dependencies.model]
path="../model"
`)
			writeProjectFile(t, bridge, "bridge.gox", `package bridge
use model;
pub func Valid() bool { return enum_valid(model::MakeSecret()) && enum_valid(model::MakePublic()) }
`)
			writeProjectFile(t, app, "goxide.toml", `[go_mod]
module="example.org/enumvalid/aapp"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies.bridge]
path="../bridge"
`)
			writeProjectFile(t, app, "main.go", `package main
import (
 "fmt"
 "example.org/enumvalid/mbridge"
)
func main() { fmt.Println(bridge.Valid()) }
`)
			first, err := PrepareProject(app, Options{Offline: true})
			if err != nil {
				t.Fatal(err)
			}
			snapshot := projectSnapshot(t, filepath.Dir(first.Root))
			featureGo(t, first.Root, "build", "-o", filepath.Join(root, "consumer"), "./...")
			if got := featureGo(t, first.Root, "run", first.Entry); got != "true\n" {
				t.Fatalf("cross-module enum validity = %q", got)
			}
			lock, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
			if err != nil {
				t.Fatal(err)
			}
			second, err := PrepareProject(app, Options{Offline: true, Locked: true})
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
			if err != nil || !bytes.Equal(lock, after) || !reflect.DeepEqual(snapshot, projectSnapshot(t, filepath.Dir(second.Root))) {
				t.Fatalf("locked replay changed enum helper output or dependency lock: %v", err)
			}
		})
	}
}
