package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/paths"
	testdeps "github.com/hxsf/goxide/tests/testutil"
)

func TestDeriveCanonicalAliasesAndReexports(t *testing.T) {
	directory := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(directory, "go.mod"), []byte("module fixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"facade/api.gox": `package facade
pub use wire::Serialize as Save;
pub use wire::Deserialize as Load;
pub use wire as Codec;
`,
		"main.gox": `package main
use wire::Serialize as LocalSave;
use wire::Deserialize as LocalLoad;
use wire::json;
use facade::Save;
use facade::Load;
use facade as api;
#[derive(LocalSave,LocalLoad)]
type Direct struct{Value int}
#[derive(Save,Load)]
type Reexport struct{Value int}
#[derive(api::Codec::Serialize,api::Codec::Deserialize)]
type Namespace struct{Value int}
func main(){
    first,e1:=json::Unmarshal([]byte("{\"Value\":1}"),DeserializeDirect)
    second,e2:=json::Unmarshal([]byte("{\"Value\":2}"),DeserializeReexport)
    third,e3:=json::Unmarshal([]byte("{\"Value\":3}"),DeserializeNamespace)
    a,e4:=json::Marshal(&first,SerializeDirect)
    b,e5:=json::Marshal(&second,SerializeReexport)
    c,e6:=json::Marshal(&third,SerializeNamespace)
    println!("{} {} {} {}",string(a),string(b),string(c),e1==nil&&e2==nil&&e3==nil&&e4==nil&&e5==nil&&e6==nil)
}
`,
	} {
		writeDiagnosticModuleFile(t, directory, name, []byte(source))
	}
	roots := paths.Roots{
		"wire": {PackagePath: "github.com/goxide-lang/serde", ModulePath: "github.com/goxide-lang/serde", Children: map[string]paths.Namespace{
			"json": {PackagePath: "github.com/goxide-lang/serde/json"},
		}},
		"facade": {PackagePath: "fixture/facade", ModulePath: "fixture"},
	}
	if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: roots}}); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(directory, "facade/api.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(generated), "type Save") || strings.Contains(string(generated), "type Load") {
		t.Fatalf("compile-time derive registration became a runtime type:\n%s", generated)
	}
	runGo(t, directory, "build", "./...")
	if got := runGo(t, directory, "run", "."); got != "{\"Value\":1} {\"Value\":2} {\"Value\":3} true\n" {
		t.Fatalf("canonical derive aliases output = %q", got)
	}
}
