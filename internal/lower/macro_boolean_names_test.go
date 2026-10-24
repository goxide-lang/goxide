package lower_test

import (
	"github.com/goxide-lang/goxide/internal/lower"
	"testing"
)

func TestMacroBooleanConstantsIgnoreCallerPackageShadow(t *testing.T) {
	root := t.TempDir()
	for name, source := range map[string]string{
		"go.mod": "module booleanfixture\ngo 1.27\n",
		"kit/macros.gox": `package kit
pub macro_rules! check { () => { true && !false }; }
`,
		"library/model.gox": `package library
use booleanfixture::kit;
const true = (0 != 0)
const false = (0 == 0)
pub func exercise() bool { return kit::check!() && !true && false }
`,
		"main.go": `package main
import("fmt";"booleanfixture/library")
func main(){fmt.Println(library.Exercise())}
`,
	} {
		writeDiagnosticModuleFile(t, root, name, []byte(source))
	}
	if err := lower.CompileModules([]lower.Module{{Path: "booleanfixture", Dir: root, Config: testConfig(t)}}); err != nil {
		t.Fatal(err)
	}
	runGo(t, root, "build", ".")
	if got := runGo(t, root, "run", "."); got != "true\n" {
		t.Fatalf("macro boolean output = %q", got)
	}
}
