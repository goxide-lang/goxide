package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestNativeSelectedBlankImportRetainsSourceDependency(t *testing.T) {
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		root := t.TempDir()
		if err := testdeps.WriteFile(filepath.Join(root, "go.mod"), []byte("module associatedfixture\n\ngo 1.27\n"), 0600); err != nil {
			t.Fatal(err)
		}
		for name, source := range map[string]string{
			"model/value.gox": `package model
pub func marker()int{return 1}
`,
			"native/value.go": `package native
import _ "associatedfixture/model"
type Token struct{Value int}
func Value()Token{return Token{Value:7}}
`,
			"main.gox": `package main
pub mod native;pub mod model;
use fmt;
func main(){fmt::Println(native::Value().Value)}
`,
		} {
			writeDiagnosticModuleFile(t, root, name, []byte(source))
		}
		if err := lower.CompileModules([]analysis.Module{{Path: "associatedfixture", Dir: root, Config: testConfig(t)}}); err != nil {
			t.Fatal(err)
		}
		generated, err := os.ReadFile(filepath.Join(root, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if attempt > 0 && !bytes.Equal(previous, generated) {
			t.Fatal("native blank import changed generated Go")
		}
		previous = generated
		runGo(t, root, "build", "./...")
		if got := runGo(t, root, "run", "."); got != "7\n" {
			t.Fatalf("output=%q", got)
		}
	}
}
