package lower_test

import (
	"os"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
)

func TestExplicitImportedGoNamesEndToEnd(t *testing.T) {
	source := t.TempDir()
	writeDiagnosticModuleFile(t, source, "main.gox", []byte(`package main
use fixture::lib::answer;
use fixture::lib::answer as call;
use fixture::lib::item;
use fixture::lib::item as alias;
use fmt;
func main() {
	var first item = 2
	var second alias = 3
	fmt::Println(answer(), call(), first, second)
}
`))
	writeDiagnosticModuleFile(t, source, "lib/lib.gox", []byte(`package lib
pub type item = int
pub func answer() int { return 42 }
`))
	directory, err := lower.PrepareModule([]string{source}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	runGo(t, directory, "build", "./...")
	if got := runGo(t, directory, "run", "."); got != "42 42 2 3\n" {
		t.Fatalf("explicit imported items output = %q", got)
	}
}
