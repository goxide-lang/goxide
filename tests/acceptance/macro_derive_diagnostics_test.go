package acceptance_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMacroDeriveDiagnosticCLI(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := acceptanceCLI(t)
	outside := t.TempDir()
	const prefix = `package p; use std; use std::PartialEq;
macro_rules! noise {()=>{
 _=1
 _=2
 _=3
};}
func Run(){noise!();noise!()}
`
	for _, tc := range []struct{ name, source, token, message string }{
		{"derive", prefix + "#[derive(Unknown)]\ntype Item struct{N int}\n", "#[derive(Unknown)]", "unknown derive: Unknown"},
		{"overlap", prefix + "#[derive(std::derive::Clone)]\ntype Item struct{N int}\nimpl std::Clone for Item{func clone(self &Self)Self{return *self}}\n", "#[derive(std::derive::Clone)]", "overlapping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, newline := range []string{"\n", "\r\n"} {
				source := strings.ReplaceAll(tc.source, "\n", newline)
				dir := t.TempDir()
				writeStdNativeRoots(t, dir)
				packagesWrite(t, dir, "go.mod", "module example.org/diagnostics\ngo 1.27\n"+stdFixtureGoRequirements(t))
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
	writeStdFormalBinaryManifest(t, dir, "example.org/diagnostics", "diagnostics")
	packagesWrite(t, dir, "main.gox", `package main; use std; use std::PartialEq;
use fmt;
macro_rules! twice {($x:expr)=>{$x+$x};}
macro_rules! print_fn {()=>{fmt::Println};}
#[derive(std::derive::Clone,std::derive::Debug)]
type Item struct{Values []int}
#[derive(std::derive::Default)]
type Settings struct{Count int}
func main(){n:=0;next:=func()int{n++;return 21};v:=Item{Values:[]int{twice!(next())}};c:=Item::copy(&v);c.Values[0]++;print:=print_fn!();print(n,v.Values[0],c.Values[0],Settings::initial().Count,Item::describe(&v))}

impl Item {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}

impl Settings {
 pub func initial() Self { return std::default_value[Self]() }
}
`)
	generated := filepath.Join(t.TempDir(), "generated")
	generated = strings.TrimSpace(deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, dir))
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "./...")
	deliveryCommand(t, generated, tool, true, nil, "vet", "./...")
	output := deliveryCommand(t, generated, tool, true, nil, "run", "-race", ".")
	if output != "2 42 43 0 Item { Values: [42] }\n" {
		t.Fatalf("legal behavior changed: %q", output)
	}
}
