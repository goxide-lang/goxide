package lower_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestTraitGlobMethodLookup(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Label { func label(self Self) string }
pub trait Count[T any] { func count(self Self, value T) int }
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Label;
use associatedfixture::protocol::Count;
pub struct Value {}
impl Label for Value { func label(self Self) string { return "glob" } }
impl Count[int] for Value { func count(self Self, value int) int { return value + 1 } }
`,
		"facade/lib.gox": `package facade
pub use associatedfixture::protocol::Label as Show;
pub use associatedfixture::protocol::Count as Option;
`,
		"bridge/lib.gox": `package bridge
pub use associatedfixture::facade::*;
`,
		"direct.gox": `package main
use associatedfixture::protocol::*;
use associatedfixture::model;
func direct() string { value := model::Value{}; return value.label() }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::bridge::*;
use associatedfixture::model;
func main() { value := model::Value{}; fmt::Println(direct(), value.label(), value.count(41)) }
`,
	}
	runPrivateAssociatedFixture(t, sources, "glob glob 42\n")
}

func TestTraitGlobsRespectVisibilityAndShadowing(t *testing.T) {
	for _, test := range []struct {
		name, visibility, declarations, facade string
	}{
		{name: "private_trait"},
		{name: "local_declaration", visibility: "pub ", declarations: "struct Label {}"},
		{name: "explicit_alias", visibility: "pub ", declarations: "use associatedfixture::other::Label;"},
		{name: "private_declaration_shadows_reexport", visibility: "pub ", facade: "struct Label {}\npub use associatedfixture::model::*;"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := t.TempDir()
			if err := testdeps.WriteFile(filepath.Join(input, "go.mod"), []byte("module associatedfixture\n\ngo 1.27\n"), 0600); err != nil {
				t.Fatal(err)
			}
			writeDiagnosticModuleFile(t, input, "model/lib.gox", []byte(`package model
`+test.visibility+`trait Label { func label(self Self) string }
pub struct Value {}
impl Label for Value { func label(self Self) string { return "hidden" } }
`))
			writeDiagnosticModuleFile(t, input, "other/lib.gox", []byte("package other\npub struct Label {}\n"))
			target := "model"
			if test.facade != "" {
				target = "facade"
				writeDiagnosticModuleFile(t, input, "facade/lib.gox", []byte("package facade\n"+test.facade+"\n"))
			}
			writeDiagnosticModuleFile(t, input, "main.gox", []byte(`package main
use associatedfixture::`+target+`::*;
`+test.declarations+`
func main() { value := Value{}; _ = value.label() }
`))
			generated, err := lower.PrepareModule([]string{input}, "associatedfixture", testConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(generated) })
			command := exec.Command("go", "build", "./...")
			command.Dir = generated
			command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "value.label undefined") {
				t.Fatalf("hidden trait method should be unavailable: %v\n%s", err, output)
			}
		})
	}
}

func TestTraitGlobsPreserveMethodAmbiguity(t *testing.T) {
	input := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(input, "go.mod"), []byte("module associatedfixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writeDiagnosticModuleFile(t, input, "model/lib.gox", []byte(`package model
pub trait First { func label(self Self) string }
pub trait Second { func label(self Self) string }
pub struct Value {}
impl First for Value { func label(self Self) string { return "first" } }
impl Second for Value { func label(self Self) string { return "second" } }
`))
	writeDiagnosticModuleFile(t, input, "main.gox", []byte(`package main
use associatedfixture::model::*;
func main() { value := Value{}; _ = value.label() }
`))
	generated, err := lower.PrepareModule([]string{input}, "associatedfixture", testConfig(t))
	if generated != "" {
		defer os.RemoveAll(generated)
	}
	if err == nil || !strings.Contains(err.Error(), "ambiguous trait method") {
		t.Fatalf("two visible traits should remain ambiguous: %v", err)
	}
}
