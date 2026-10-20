package lower_test

import (
	"errors"
	"go/importer"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestReferenceSourcePackageReturnRetainsPermission(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "kit"), 0700); err != nil {
		t.Fatal(err)
	}
	const source = `package main
use referencefixture::kit;
func main() {
    value := 1
    view := kit::Read(&value)
    /*bad*/*view/*end*/ = 2
}
`
	for name, content := range map[string]string{
		"main.gox":    source,
		"kit/api.gox": "package kit\npub func Read(value &int) &int { return value }\n",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := lower.PrepareModule([]string{directory}, "referencefixture", testConfig(t))
	if output != "" {
		t.Cleanup(func() { os.RemoveAll(output) })
		t.Fatalf("rejected imported reference emitted module %s", output)
	}
	var diagnostic check.Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Code != check.ReferencePermission {
		t.Fatalf("missing imported readonly return rejection: %v", err)
	}
	start := uint32(strings.Index(source, "/*bad*/") + len("/*bad*/"))
	end := uint32(strings.Index(source, "/*end*/"))
	if diagnostic.File != "referencefixture/main.gox" || diagnostic.Span.Start < start || diagnostic.Span.Start >= diagnostic.Span.End || diagnostic.Span.End > end || diagnostic.Origin.File != diagnostic.File || diagnostic.Origin.Span != diagnostic.Span {
		t.Fatalf("imported reference diagnostic does not identify consumer write [%d:%d]: %#v", start, end, diagnostic)
	}
}

func TestReferenceRejectsPermissionLossAtSourceOperation(t *testing.T) {
	const declarations = `
type Record struct { Count int; Items []int; Counts map[string]int; Pointer *int }
func native(value *int) {}
func writable(value &mut int) {}
func store(value Record) {}
func (value &mut Record) Bump() { value.Count++ }
`
	for _, test := range []struct{ name, code, source string }{
		{"scalar-write", check.ReferencePermission, `func f(value &int) { /*bad*/*value/*end*/ = 1 }`},
		{"scalar-increment", check.ReferencePermission, `func f(value &int) { /*bad*/(*value)/*end*/++ }`},
		{"copied-reference", check.ReferencePermission, `func f(value &int) { copied:=value; /*bad*/*copied/*end*/ = 1 }`},
		{"named-reference", check.ReferencePermission, `type Read &int; func f(value Read) { /*bad*/*value/*end*/ = 1 }`},
		{"field-projection", check.ReferencePermission, `func f(value &Record) { /*bad*/value.Items[0]/*end*/ = 1 }`},
		{"shallow-copy", check.ReferencePermission, `func f(value &Record) { copied:=*value; /*bad*/copied.Items[0]/*end*/ = 1 }`},
		{"constructed-enum-shared-copy", check.ReferencePermission, `enum Box { Items([]int), }; func f() { value:=Box::Items([]int{1}); var view &Box=&value; copied:=*view; match copied { Box::Items(items) => { /*bad*/items[0]/*end*/ = 2 } } }`},
		{"sibling-field", check.ReferencePermission, `func f(value &Record) { copied:=*value; copied.Items=[]int{1}; /*bad*/copied.Counts["x"]/*end*/ = 1 }`},
		{"upgrade", check.ReferencePermission, `func f(value &int) { var writable &mut int = /*bad*/value/*end*/; _=writable }`},
		{"reborrow-upgrade", check.ReferencePermission, `func f(value &int) { _ = /*bad*/&mut *value/*end*/ }`},
		{"pointer-erasure", check.ReferencePermission, `func f(value &int) { var pointer *int = /*bad*/value/*end*/; _=pointer }`},
		{"call-erasure", check.ReferencePermission, `func f(value &int) { native(/*bad*/value/*end*/) }`},
		{"call-upgrade", check.ReferencePermission, `func f(value &int) { writable(/*bad*/value/*end*/) }`},
		{"return-erasure", check.ReferencePermission, `func f(value &int) *int { return /*bad*/value/*end*/ }`},
		{"interface-erasure", check.ReferencePermission, `func f(value &int) { var hidden any = /*bad*/value/*end*/; _=hidden }`},
		{"shallow-call-erasure", check.ReferencePermission, `func f(value &Record) { store(/*bad*/*value/*end*/) }`},
		{"shared-store-erasure", check.ReferencePermission, `func f(destination *Record, source &Record) { destination.Items = /*bad*/source.Items/*end*/ }`},
		{"receiver-body", check.ReferencePermission, `func (value &Record) Bad() { /*bad*/value.Count/*end*/ = 1 }`},
		{"mutable-method-value", check.ReferencePermission, `func f(value &Record) { call:=/*bad*/value.Bump/*end*/; call() }`},
		{"branch-join", check.ReferencePermission, `func f(value &Record, replace bool) { items:=value.Items; if replace {items=[]int{1}}; /*bad*/items[0]/*end*/ = 1 }`},
		{"zero-loop", check.ReferencePermission, `func f(value &Record, count int) { items:=value.Items; for i:=0;i<count;i++ {items=[]int{1}}; /*bad*/items[0]/*end*/ = 1 }`},
		{"loop-backedge", check.ReferencePermission, `func f(value &Record) { items:=[]int{1}; for i:=0;i<2;i++ { /*bad*/items[0]/*end*/ = 1; items=value.Items } }`},
		{"range-shared-element", check.ReferencePermission, `func f(value &[]Record) { for _, item := range *value { /*bad*/item.Items[0]/*end*/ = 1 } }`},
		{"exposed-slot", check.ReferencePermission, `func f(value &Record) { items:=[]int{1}; address:=&items; items=value.Items; /*bad*/(*address)[0]/*end*/ = 1 }`},
		{"closure-writeback", check.ReferencePermission, `func f(source &Record) { items:=[]int{1}; install:=func(){items=source.Items}; install(); /*bad*/items[0]/*end*/ = 2 }`},
		{"manual-equality-method", check.ReferencePermission, `enum Manual { Count(int), }; impl PartialEq for Manual { func equals(self Manual,other Manual) bool { match self { Manual::Count(ref mut value) => { *value+=1 } }; return true } }; func f(value &Manual) { copied:=*value; _=/*bad*/copied.equals(copied)/*end*/ }`},
		{"manual-equality-function", check.ReferencePermission, `enum Manual { Count(int), }; impl PartialEq for Manual { func equals(self Manual,other Manual) bool { match self { Manual::Count(ref mut value) => { *value+=1 } }; return true } }; func f(value &Manual) { copied:=*value; _=/*bad*/EqualsManual(copied,copied)/*end*/ }`},
		{"derived-equality-manual-field", check.ReferencePermission, `enum Manual { Count(int), }; impl PartialEq for Manual { func equals(self Manual,other Manual) bool { match self { Manual::Count(ref mut value) => { *value+=1 } }; return true } }; #[derive(PartialEq)] type Envelope struct { Value Manual }; func f(value &Envelope) { copied:=*value; _=/*bad*/copied.equals(copied)/*end*/ }`},
		{"map-address", check.ReferenceAddress, `func f() { values:=map[string]int{"x":1}; _ = /*bad*/&mut values["x"]/*end*/ }`},
		{"temporary-address", check.ReferenceAddress, `func makeValue() int {return 1}; func f() { _ = /*bad*/&mut makeValue()/*end*/ }`},
		{"temporary-array", check.ReferenceAddress, `func makeArray() [1]int {return [1]int{1}}; func f() { _ = /*bad*/&mut makeArray()[0]/*end*/ }`},
		{"nested-reference-pattern", check.ReferencePattern, `enum Inner { Item(int), }; enum Outer { Hold(Inner), }; func f(value Outer) { match value { Outer::Hold(/*bad*/Inner::Item(ref item)/*end*/) => { _=item } } }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "package references\n" + declarations + test.source + "\n"
			const filename = "referencefixture/main.gox"
			file, diagnostics := parser.ParseFile(filename, []byte(source))
			if len(diagnostics) != 0 {
				t.Fatalf("parse: %v", diagnostics)
			}
			expanded, err := expand.Expand([]*ast.File{file}, nil)
			if err != nil {
				t.Fatal(err)
			}
			resolved := resolve.ResolveExpanded(map[string]*ast.File{filename: file}, expanded, coreFixtureImporter{importer.Default()})
			if len(resolved.Diagnostics) != 0 {
				t.Fatalf("resolve: %v", resolved.Diagnostics)
			}
			files := map[string]*ast.File{filename: expanded.Files[0]}
			info, checked := check.Check(files, resolved)
			start := uint32(strings.Index(source, "/*bad*/") + len("/*bad*/"))
			end := uint32(strings.Index(source, "/*end*/"))
			found := false
			for _, diagnostic := range checked {
				if diagnostic.Code != test.code {
					continue
				}
				if diagnostic.File != filename || diagnostic.Span.Start < start || diagnostic.Span.Start >= diagnostic.Span.End || diagnostic.Span.End > end || diagnostic.Origin.File != filename || diagnostic.Origin.Span != diagnostic.Span {
					t.Fatalf("%s diagnostic points outside the marked source operation [%d:%d]: %#v", test.code, start, end, diagnostic)
				}
				found = true
			}
			if !found {
				t.Fatalf("missing %s rejection: %v", test.code, checked)
			}
			output, err := lower.Lower(files, info)
			if output != nil || err == nil {
				t.Fatalf("rejected reference operation emitted partial Go: output=%v error=%v", output, err)
			}
		})
	}
}
