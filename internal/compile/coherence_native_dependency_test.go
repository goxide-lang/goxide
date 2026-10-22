package compile

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/project"
)

func TestCoherenceRechecksNativeGoMethodEnvironment(t *testing.T) {
	root := t.TempDir()
	model := filepath.Join(root, "model")
	writeProjectFile(t, root, "native/go.mod", "module example.org/coherence-native\n\ngo 1.27\n")
	writeProjectFile(t, model, project.ManifestName, `[go_mod]
module="example.org/coherence-native-model"
go="1.27"
[lib]
name="model"
path="."
[dependencies.native]
path="../native"
`+pinnedStandardManifest(t))
	writeProjectFile(t, model, "lib.gox", `package model
use native;
pub trait Q {func value(self Self)int}
pub struct Box[T any]{pub Value T}
impl[T interface{Get()int}] Q for Box[T] {func value(self Self)int{return self.Value.Get()}}
impl Q for Box[native::Row] {func value(self Self)int{return 7}}
`)
	for _, fixture := range []struct{ name, method, want string }{
		{"missing-method-is-unknown", "", "cannot prove trait implementations disjoint"},
		{"new-method-creates-overlap", "func (Row) Get() int {return 1}", "overlapping applications"},
		{"contradictory-signature-is-disjoint", "func (Row) Get() string {return \"text\"}", ""},
		{"removing-method-invalidates-negative-proof", "", "cannot prove trait implementations disjoint"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			writeProjectFile(t, root, "native/native.go", "package native\ntype Row struct{}\n"+fixture.method+"\n")
			result, err := PrepareProject(model, Options{Lib: true, Offline: true})
			if fixture.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				configuredGo(t, result, "build", "-mod=readonly", result.Entry)
				return
			}
			var diagnostic check.Diagnostic
			if result != nil || !errors.As(err, &diagnostic) || diagnostic.Code != check.TraitInvalid || !strings.Contains(diagnostic.Message, fixture.want) {
				t.Fatalf("native method change was accepted or misdiagnosed: %v, %v; want %q", result, err, fixture.want)
			}
		})
	}
}
