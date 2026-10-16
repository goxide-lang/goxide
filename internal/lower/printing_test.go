package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
	testdeps "github.com/hxsf/goxide/tests/testutil"
)

func TestPrintFormattingAndMacroArguments(t *testing.T) {
	module, err := supportModule(t, `package main
var trace string
func next(value int) int { trace += string(rune('0'+value)); return value }
macro_rules! note { ($value:expr) => { println!("seen {}", $value) }; }
#[derive(Debug)]
type Record struct { Text string }
func fail() Result[int,string] { return Result[int,string]::Err("stopped") }
func skipped() Result[int,string] {
    println!("{} {}", fail()?, next(9))
    return Result[int,string]::Ok(0)
}
func main() {
    formatting := "local"
    println := 7
    print!("")
    println!()
    println!("{{{}}} {:?} {}", next(1), "quoted", next(2))
    note!(next(3))
    println!("{:?}", Record{Text:"secret"})
    _ = skipped()
    println!("{} {} {}", trace, formatting, println)
}
`)
	if err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", "./...")
	want := "\n{1} \"quoted\" 2\nseen 3\nRecord { Text: \"secret\" }\n123 local 7\n"
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("print output = %q, want %q", got, want)
	}
}

func TestPrintRejectsInvalidFormatAndPlacement(t *testing.T) {
	for _, test := range []struct{ name, body, want string }{
		{"missing-literal", `print!()`, "format string literal"},
		{"dynamic", `format := "{}"; println!(format, 1)`, "string literal"},
		{"unmatched", `println!("{")`, "format brace"},
		{"count", `println!("{}", 1, 2)`, "placeholders"},
		{"stringer", `println!("{}", Value{})`, "formatting protocol"},
		{"value", `_ = println!("x")`, "direct statement"},
		{"defer", `defer println!("x")`, "requires a direct statement"},
		{"initializer", `if println!("x"); true {}`, "direct statement"},
	} {
		t.Run(test.name, func(t *testing.T) {
			module, err := supportModule(t, "package main\ntype Value struct{}\nfunc (Value) String() string { return \"hidden fallback\" }\nfunc main(){"+test.body+"}\n")
			if module != "" || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("module=%q error=%v, want %q", module, err, test.want)
			}
		})
	}
}

func supportModule(t *testing.T, source string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module supportfixture\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.gox"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "goxide.roots.toml"), []byte(`[roots.serde]
module = "github.com/goxide-lang/serde"
package = "github.com/goxide-lang/serde"
[roots.serde.namespaces]
json = "github.com/goxide-lang/serde/json"
`), 0600); err != nil {
		t.Fatal(err)
	}
	module, err := lower.PrepareModule([]string{dir}, "supportfixture", testConfig(t))
	if module != "" {
		t.Cleanup(func() { os.RemoveAll(module) })
	}
	return module, err
}
