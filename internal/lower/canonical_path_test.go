package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/compile"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestDeriveCanonicalAliasesAndReexports(t *testing.T) {
	root := t.TempDir()
	directory, facade := filepath.Join(root, "app"), filepath.Join(root, "facade")
	manifest := func(module, name string, dependencies ...string) string {
		return strings.Replace(testdeps.FormalManifest(t, module, name, dependencies...), "[dependencies.serde]", "[dependencies.wire]", 1)
	}
	writeDiagnosticModuleFile(t, directory, "goxide.toml", []byte(strings.Replace(manifest("fixture", "app", "[dependencies.facade]\npath=\"../facade\"\n"), "[lib]", "[[bin]]", 1)))
	writeDiagnosticModuleFile(t, facade, "goxide.toml", []byte(manifest("fixture/facade", "facade")))
	for name, source := range map[string]string{
		"facade/api.gox": `package facade
pub use wire::derive::Serialize as Save;
pub use wire::derive::Deserialize as Load;
pub use wire as Codec;
`,
		"main.gox": `package main
use wire::derive::Serialize as LocalSave;
use wire::derive::Deserialize as LocalLoad;
use wire;
use wire::json;
use fmt;
use facade::Save;
use facade::Load;
use facade as api;
#[derive(LocalSave,LocalLoad)]
type Direct struct{Value int}
#[derive(Save,Load)]
type Reexport struct{Value int}
#[derive(api::Codec::derive::Serialize,api::Codec::derive::Deserialize)]
type Namespace struct{Value int}
func main(){
    first,e1:=json::Unmarshal([]byte("{\"Value\":1}"),wire::Read[Direct])
    second,e2:=json::Unmarshal([]byte("{\"Value\":2}"),wire::Read[Reexport])
    third,e3:=json::Unmarshal([]byte("{\"Value\":3}"),wire::Read[Namespace])
    a,e4:=json::Marshal(&first,wire::Write[Direct])
    b,e5:=json::Marshal(&second,wire::Write[Reexport])
    c,e6:=json::Marshal(&third,wire::Write[Namespace])
    fmt::Println(string(a),string(b),string(c),e1==nil&&e2==nil&&e3==nil&&e4==nil&&e5==nil&&e6==nil)
}
`,
	} {
		if name == "facade/api.gox" {
			writeDiagnosticModuleFile(t, facade, "api.gox", []byte(source))
		} else {
			writeDiagnosticModuleFile(t, directory, name, []byte(source))
		}
	}
	first, calls, err := testdeps.PrepareFormalBinary(t, directory)
	if err != nil {
		t.Fatal(err)
	}
	if calls < 6 {
		t.Fatalf("canonical aliases invoked %d procedural derives for six registrations", calls)
	}
	facadeOutput := strings.TrimSpace(testdeps.RunFormalGo(t, first, "list", "-mod=readonly", "-m", "-f", "{{.Dir}}", "fixture/facade"))
	outputs := []string{filepath.Join(first.Root, "main.go"), filepath.Join(facadeOutput, "api.go")}
	before := make([][]byte, len(outputs))
	for i, name := range outputs {
		before[i], err = os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(string(before[1]), "type Save") || strings.Contains(string(before[1]), "type Load") {
		t.Fatalf("compile-time derive registration became a runtime type:\n%s", before[1])
	}
	if _, _, err := testdeps.PrepareFormalBinary(t, directory, compile.Options{Locked: true}); err != nil {
		t.Fatal(err)
	}
	for i, name := range outputs {
		after, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(before[i], after) {
			t.Fatalf("canonical generation changed %s: %v", name, err)
		}
	}
	testdeps.RunFormalGo(t, first, "build", "-mod=readonly", "./...")
	if got := testdeps.RunFormalGo(t, first, "run", "-mod=readonly", "."); got != "{\"Value\":1} {\"Value\":2} {\"Value\":3} true\n" {
		t.Fatalf("canonical derive aliases output = %q", got)
	}
}
