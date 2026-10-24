package procmacro_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

const root = "example.org/macros"

func source(t *testing.T, name, body string) *ast.File {
	t.Helper()
	file, diagnostics := parser.ParseFile(name, []byte("package macros\n"+body))
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == parser.Error {
			t.Fatalf("parse %s: %v", name, diagnostic)
		}
	}
	return file
}

func TestCollectKeepsSourceAndConsumerNamesSeparate(t *testing.T) {
	a := source(t, root+"/a.gox", `#[proc_macro] pub func identity() {}
pub func helper() {}
#[proc_macro_attribute] pub func trace() {}`)
	b := source(t, root+"/b.gox", `#[proc_macro_derive(Describe, attributes(describe, default,))]
pub func deriveDescription() {}`)
	files := map[string]*ast.File{root + "/b.gox": b, root + "/a.gox": a,
		root + "/child/helper.gox": source(t, root+"/child/helper.gox", `pub func helper() {}`)}
	entries, err := procmacro.Collect(files, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %v", entries)
	}
	for index, want := range []struct {
		name, function string
		kind           pm.Kind
	}{{"identity", "identity", pm.Function}, {"trace", "trace", pm.Attribute}, {"Describe", "deriveDescription", pm.Derive}} {
		got := entries[index]
		if got.Name != want.name || got.Kind != want.kind || got.Function.Name.Name != want.function || got.Attribute != got.Function.Attrs[0] {
			t.Fatalf("entry %d = %+v", index, got)
		}
	}
	if entries[0].File != a || entries[2].File != b || !reflect.DeepEqual(entries[2].Helpers, []string{"describe", "default"}) {
		t.Fatalf("source identity or helpers lost: %+v", entries[2])
	}
	if len(a.Items) != 3 || len(b.Items) != 1 {
		t.Fatal("collection changed producer declarations")
	}
}

func TestCollectUsesConfiguredSource(t *testing.T) {
	name := root + "/macros.gox"
	file := source(t, name, `#[cfg(any())] #[proc_macro] func removed() {}
#[cfg_attr(all(), proc_macro)] pub func selected() {}`)
	if _, err := expand.Configure(file, config.Config{}); err != nil {
		t.Fatal(err)
	}
	entries, err := procmacro.Collect(map[string]*ast.File{name: file}, root, true)
	if err != nil || len(entries) != 1 || entries[0].Name != "selected" {
		t.Fatalf("entries = %v, error = %v", entries, err)
	}
}

func TestCollectRejectsInvalidRegistration(t *testing.T) {
	for _, test := range []struct {
		name, body, suffix, want string
		disabled                 bool
	}{
		{name: "ordinary lib", body: `#[proc_macro] pub func entry() {}`, disabled: true, want: "proc_macro lib"},
		{name: "uppercase is not explicit pub", body: `#[proc_macro] func Entry() {}`, want: "explicit pub"},
		{name: "private", body: `#[proc_macro] func entry() {}`, want: "explicit pub"},
		{name: "generic", body: `#[proc_macro] pub func entry[T any]() {}`, want: "non-generic"},
		{name: "receiver", body: `#[proc_macro] pub func (self Handler) entry() {}`, want: "no receiver"},
		{name: "missing body", body: `#[proc_macro] pub func entry()`, want: "requires a body"},
		{name: "child package", body: `#[proc_macro] pub func entry() {}`, suffix: "/child", want: "root package"},
		{name: "impl member", body: `impl Handler { #[proc_macro] pub func entry() {} }`, want: "root package"},
		{name: "type", body: `#[proc_macro] pub type Entry int`, want: "explicit functions"},
		{name: "two registrations", body: `#[proc_macro] #[proc_macro_attribute] pub func entry() {}`, want: "exactly one"},
		{name: "function arguments", body: `#[proc_macro(entry)] pub func entry() {}`, want: "does not accept arguments"},
		{name: "attribute arguments", body: `#[proc_macro_attribute()] pub func entry() {}`, want: "does not accept arguments"},
		{name: "missing derive name", body: `#[proc_macro_derive] pub func entry() {}`, want: "requires Name"},
		{name: "qualified derive name", body: `#[proc_macro_derive(other::Describe)] pub func entry() {}`, want: "requires Name"},
		{name: "unknown registration option", body: `#[proc_macro_derive(Describe, helpers(describe))] pub func entry() {}`, want: "requires Name"},
		{name: "qualified helper", body: `#[proc_macro_derive(Describe, attributes(other::describe))] pub func entry() {}`, want: "requires Name"},
		{name: "duplicate helper", body: `#[proc_macro_derive(Describe, attributes(describe, describe))] pub func entry() {}`, want: "duplicate procedural macro helper"},
		{name: "blank name", body: `#[proc_macro] pub func _() {}`, want: "name cannot be _"},
		{name: "duplicate names across kinds", body: `#[proc_macro] pub func Describe() {}
#[proc_macro_derive(Describe)] pub func deriveDescription() {}`, want: "duplicate procedural macro name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			name := root + test.suffix + "/input.gox"
			file := source(t, name, test.body)
			_, err := procmacro.Collect(map[string]*ast.File{name: file}, root, !test.disabled)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCollectRejectsReservedNames(t *testing.T) {
	for _, name := range []string{"cfg", "go_result", "go_result_keep"} {
		file := source(t, root+"/input.gox", "#[proc_macro_derive("+name+")] pub func entry() {}")
		if _, err := procmacro.Collect(map[string]*ast.File{root + "/input.gox": file}, root, true); err == nil || !strings.Contains(err.Error(), "reserved built-in") {
			t.Fatalf("reserved %s: %v", name, err)
		}
	}
}

func TestLibraryProtocolNamesAreOrdinaryRegistrations(t *testing.T) {
	for _, owner := range []string{root, "github.com/goxide-lang/serde", "github.com/goxide-lang/std/macros"} {
		for _, name := range []string{"Serialize", "Deserialize", "Default", "Clone", "Debug", "Error", "PartialEq", "Eq", "print", "println"} {
			file := source(t, owner+"/input.gox", "#[proc_macro_derive("+name+")] pub func entry() {}")
			entries, err := procmacro.Collect(map[string]*ast.File{owner + "/input.gox": file}, owner, true)
			if err != nil || len(entries) != 1 || entries[0].Name != name {
				t.Fatalf("library %s cannot register %s: %v", owner, name, err)
			}
		}
	}
}

func TestCollectRejectsGeneratedRegistration(t *testing.T) {
	for _, place := range []string{"file", "function", "attribute"} {
		t.Run(place, func(t *testing.T) {
			file := source(t, root+"/input.gox", `#[proc_macro] pub func entry() {}`)
			function := file.Items[0].(*ast.FuncDecl)
			switch place {
			case "file":
				file.Expn = 1
			case "function":
				function.Expn = 1
			case "attribute":
				function.Attrs[0].Expn = 1
			}
			if _, err := procmacro.Collect(map[string]*ast.File{root + "/input.gox": file}, root, true); err == nil || !strings.Contains(err.Error(), "macro output") {
				t.Fatalf("generated registration accepted: %v", err)
			}
		})
	}
}

func TestStripPreservesOtherAttributesAndFunction(t *testing.T) {
	file := source(t, root+"/input.gox", `#[other] #[proc_macro] pub func entry() {}`)
	function := file.Items[0].(*ast.FuncDecl)
	other := function.Attrs[0]
	entries, err := procmacro.Collect(map[string]*ast.File{root + "/input.gox": file}, root, true)
	if err != nil {
		t.Fatal(err)
	}
	procmacro.Strip(entries)
	procmacro.Strip(entries)
	if len(function.Attrs) != 1 || function.Attrs[0] != other || file.Items[0] != function {
		t.Fatal("strip changed ordinary source declarations or attributes")
	}
}
