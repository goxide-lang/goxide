package lower_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestNativePackageTypesShareExternalExportIdentity(t *testing.T) {
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		root, bridge := t.TempDir(), t.TempDir()
		moduleText := fmt.Sprintf("module nativefixture\n\ngo 1.27\nrequire nativebridge v0.0.0\nreplace nativebridge => %s\n", filepath.ToSlash(bridge))
		if err := testdeps.WriteFile(filepath.Join(root, "go.mod"), []byte(moduleText), 0600); err != nil {
			t.Fatal(err)
		}
		for name, source := range map[string]string{
			"shared/value.go": `package shared
type Token struct{Value int}
type Box[T any] struct{Value T}
type Alias=Token
`,
			"main.gox": `package main
pub mod shared;
use nativebridge;use fmt;
trait Label {func label(self &Self)int}
impl Label for shared::Token {func label(self &Self)int{return self.Value}}
func Read[T Label](value T)int{return Label::label(&value)}
func Identity[T any](value T)T{return value}
func main(){
 var value shared::Box[shared::Alias]=nativebridge::Value()
 fmt::Println(Read(Identity(value.Value)))
}
`,
		} {
			writeDiagnosticModuleFile(t, root, name, []byte(source))
		}
		writeDiagnosticModuleFile(t, bridge, "go.mod", []byte(fmt.Sprintf("module nativebridge\n\ngo 1.27\nrequire nativefixture v0.0.0\nreplace nativefixture => %s\n", filepath.ToSlash(root))))
		writeDiagnosticModuleFile(t, bridge, "value.go", []byte(`package nativebridge
import "nativefixture/shared"
func Value()shared.Box[shared.Token]{return shared.Box[shared.Token]{Value:shared.Token{Value:42}}}
`))
		module := lower.Module{Path: "nativefixture", Dir: root, Config: testConfig(t), Roots: paths.Roots{"nativebridge": {ModulePath: "nativebridge", PackagePath: "nativebridge"}}}
		if err := lower.CompileModules([]lower.Module{module}); err != nil {
			t.Fatal(err)
		}
		generated, err := os.ReadFile(filepath.Join(root, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if attempt > 0 && !bytes.Equal(previous, generated) {
			t.Fatal("same native dependency graph changed generated Go")
		}
		previous = generated
		runGo(t, root, "build", "./...")
		if got := runGo(t, root, "run", "."); got != "42\n" {
			t.Fatalf("shared native type output = %q", got)
		}
	}
}
