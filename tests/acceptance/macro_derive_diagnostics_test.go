package acceptance_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMacroDeriveDiagnosticCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "build", "-o", cli, "./cmd/goxide")
	outside := t.TempDir()
	const prefix = `package p
macro_rules! noise {()=>{
 _=1
 _=2
 _=3
};}
func Run(){noise!();noise!()}
`
	for _, tc := range []struct{ name, source, token, message string }{
		{"derive", prefix + "#[derive(Unknown)]\ntype Item struct{N int}\n", "#[derive(Unknown)]", "unknown derive or non-canonical owner: Unknown"},
		{"helper", prefix + "func CloneItem(){}\n#[derive(Clone)]\ntype Item struct{N int}\n", "#[derive(Clone)]", "generated helper CloneItem conflicts with a package declaration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, newline := range []string{"\n", "\r\n"} {
				source := strings.ReplaceAll(tc.source, "\n", newline)
				dir := t.TempDir()
				writeMacroFixtureRoots(t, dir, nil)
				input := filepath.Join(dir, "input.gox")
				packagesWrite(t, dir, "input.gox", source)
				output := deliveryCommand(t, outside, cli, false, nil, "transpile", input)
				start := strings.Index(source, tc.token)
				line := strings.Count(source[:start], "\n") + 1
				column := start - strings.LastIndex(source[:start], "\n")
				want := fmt.Sprintf("%s:%d:%d:", input, line, column)
				if !strings.Contains(output, want) || !strings.Contains(output, tc.message) {
					t.Fatalf("want %s %s, got %s", want, tc.message, output)
				}
			}
		})
	}
	// Existing concrete derive semantics remain executable after the diagnostic
	// change, including macro evaluation multiplicity and independent Clone data.
	dir := t.TempDir()
	writeMacroFixtureRoots(t, dir, nil)
	packagesWrite(t, dir, "go.mod", "module example.org/diagnostics\ngo 1.27\n")
	packagesWrite(t, dir, "main.gox", `package main
use fmt;
macro_rules! twice {($x:expr)=>{$x+$x};}
macro_rules! print_fn {()=>{fmt::Println};}
#[derive(Clone,Debug)]
type Item struct{Values []int}
#[derive(Default)]
type Settings struct{Count int}
func main(){n:=0;next:=func()int{n++;return 21};v:=Item{Values:[]int{twice!(next())}};c:=CloneItem(&v);c.Values[0]++;print:=print_fn!();print(n,v.Values[0],c.Values[0],DefaultSettings().Count,DebugItem(&v))}
`)
	generated := filepath.Join(t.TempDir(), "generated")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, dir)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "./...")
	deliveryCommand(t, generated, tool, true, nil, "vet", "./...")
	output := deliveryCommand(t, generated, tool, true, nil, "run", "-race", ".")
	if output != "2 42 43 0 Item { Values: [42] }\n" {
		t.Fatalf("legal behavior changed: %q", output)
	}
}
