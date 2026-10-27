package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestTraitPrerequisitesKeepDefinitionPathsThroughGoAliases(t *testing.T) {
	sources := map[string]string{
		"model/lib.gox": `package model
pub trait Mark {}
impl Mark for int {}
pub trait Copy {func copy(self &Self)Self}
pub trait Inspect {func inspect(self &Self)int}
pub enum E {Value(int)}
impl Copy for E where int:Mark {func copy(self &Self)Self{return match *self{E::Value(n)=>E::Value(n)}}}
impl Inspect for E where int:Mark {
 func inspect(self &Self)int{return match *self{E::Value(n)=>n}}
}
pub func CopyLocal(value E)E{return Copy::copy(&value)}
pub func Read[T Inspect](value &T)int{return Inspect::inspect(value)}
`,
		"relay/lib.gox": "package relay\n",
		"relay/relay.go": `package relay
import "aliasfixture/model"
type E=model.E
func Get()E{return &model.Value{Value:7}}
`,
		"main.gox": `package main
pub mod model;pub mod relay;
use fmt;
func main(){
 var value relay::E=model::E::Value(7)
 copied:=model::Copy::copy(&value)
 local:=model::CopyLocal(copied)
 var view model::Inspect = value
 fmt::Println(model::Read(&local),view.inspect())
}
`,
	}
	var before map[string][]byte
	for attempt := 0; attempt < 2; attempt++ {
		generated := t.TempDir()
		if err := testdeps.WriteFile(filepath.Join(generated, "go.mod"), []byte("module aliasfixture\n\ngo 1.27\n"), 0600); err != nil {
			t.Fatal(err)
		}
		for name, source := range sources {
			writeDiagnosticModuleFile(t, generated, name, []byte(source))
		}
		if err := lower.CompileModules([]analysis.Module{{Path: "aliasfixture", Dir: generated, Config: testConfig(t)}}); err != nil {
			t.Fatal(err)
		}
		files := map[string][]byte{}
		for name := range sources {
			if strings.HasSuffix(name, ".gox") {
				name = strings.TrimSuffix(name, ".gox") + ".go"
			}
			data, err := os.ReadFile(filepath.Join(generated, name))
			if err != nil {
				t.Fatal(err)
			}
			files[name] = data
			if attempt > 0 && !bytes.Equal(before[name], data) {
				t.Fatalf("same aliases changed generated %s", name)
			}
		}
		before = files
		if attempt == 0 {
			runGo(t, generated, "build", "./...")
			if got := runGo(t, generated, "run", "."); got != "7 7\n" {
				t.Fatalf("alias-backed protocol output = %q", got)
			}
		}
	}
}
