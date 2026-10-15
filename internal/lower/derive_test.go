package lower_test

import (
	"bytes"
	"fmt"
	"go/importer"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
	testdeps "github.com/hxsf/goxide/tests/testutil"
)

func TestDerivedDefaultCallsFieldProtocolsInOrder(t *testing.T) {
	runDeriveFixture(t, "default", "true UNTITLED 2 [label step label step]\nnone\nUNTITLED4\n")
}
func TestDerivedCloneAndDebugPreserveContainerAndVariantSemantics(t *testing.T) {
	runDeriveFixture(t, "clone_debug", "shared b c\nRecord { Values: [Item { Text: \"shared\" }], Table: {\"key\": [Item { Text: \"b\" }]}, Fixed: [Item { Text: \"c\" }], Choice: Option::Some([]), Outcome: Result::Err(nil) }\nState::Pair(Item { Text: \"quoted\" }, [1, 2]) State::Pair(Item { Text: \"quoted\" }, [9, 2])\ntrue true none\nfalse false true\naccount { id: 7, name: \"hidden\" }\nGrouped { Value: 11 }\nState::Empty State::Named { Name: \"secret\", Count: 2 }\n")
}
func TestDerivedEqualityUsesProtocolsAndDistinguishesNilAndEmpty(t *testing.T) {
	runDeriveFixture(t, "equality", "true true false\nfalse\ntrue false\nfalse\nfalse\nfalse\nfalse [left right]\ntrue\ntrue\n")
}

func runDeriveFixture(t *testing.T, fixture, want string) {
	t.Helper()
	directory := filepath.Join("testdata", "derive", fixture)
	names, err := filepath.Glob(filepath.Join(directory, "*.gox"))
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sources["derivefixture/"+filepath.Base(name)] = string(data)
	}
	output, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	again, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output, again) {
		t.Fatal("same sources produced different Go bytes")
	}
	generated := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(generated, "go.mod"), []byte("module derivefixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, data := range output {
		basename := strings.TrimSuffix(filepath.Base(name), ".gox")
		golden := filepath.Join(directory, basename+".golden.go")
		if os.Getenv("GOXIDE_UPDATE_DERIVE_GOLDEN") == "1" {
			if err := os.WriteFile(golden, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		wantGo, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, wantGo) {
			t.Fatalf("generated code differs from %s:\n%s", golden, data)
		}
		if err := os.WriteFile(filepath.Join(generated, basename+".go"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	runGo(t, generated, "build", "./...")
	if got := runGo(t, generated, "run", "."); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func lowerDeriveSources(sources map[string]string) (map[string][]byte, error) {
	var names []string
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	originals := map[string]*ast.File{}
	var input []*ast.File
	for _, name := range names {
		f, ds := parser.ParseFile(name, []byte(sources[name]))
		if len(ds) > 0 {
			return nil, fmt.Errorf("parse: %v", ds)
		}
		originals[name] = f
		input = append(input, f)
	}
	expanded, err := expand.Expand(input, nil)
	if err != nil {
		return nil, err
	}
	resolved := resolve.ResolveExpanded(originals, expanded, coreFixtureImporter{importer.Default()})
	if len(resolved.Diagnostics) > 0 {
		return nil, fmt.Errorf("resolve: %v", resolved.Diagnostics)
	}
	files := map[string]*ast.File{}
	for i, name := range names {
		files[name] = expanded.Files[i]
	}
	info, ds := check.Check(files, resolved)
	if len(ds) > 0 {
		return nil, fmt.Errorf("check: %v", ds)
	}
	return lower.Lower(files, info)
}

func TestDeriveRejectsMissingOrConflictingFieldProtocols(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"missing_clone", "type Inner struct { Value int }; #[derive(Clone)] type Outer struct { Inner Inner }", "Clone"},
		{"missing_debug", "#[derive(Debug)] type Value struct { Value chan int }", "Debug"},
		{"default_container", "#[derive(Default)] type Value struct { Values []int }", "Default"},
		{"default_result", "#[derive(Default)] type Value struct { Result Result[int,string] }", "Default"},
		{"selected_payload", "#[derive(Default)] enum Value { #[default] Selected(chan int), Other }", "Default"},
		{"missing_default_variant", "#[derive(Default)] enum Value { A, B }", "default"},
		{"duplicate_default_variant", "#[derive(Default)] enum Value { #[default] A, #[default] B }", "default"},
		{"float_eq", "#[derive(PartialEq,Eq)] type Value struct { Value float64 }", "Eq"},
		{"unstable_map_key", "#[derive(PartialEq)] type Value struct { Value map[float64]int }", "map key"},
		{"recursive_clone", "#[derive(Clone)] type Value struct { Values []Value }", "recursive"},
		{"manual_default_conflict", "#[derive(Default)] type Value struct {}; impl Default for Value {func default() Value {return Value{}}}", "duplicate"},
		{"helper_conflict", "#[derive(Default)] type Value struct {}; func DefaultValue() Value {return Value{}}", "conflict"},
		{"manual_eq_proof", "#[derive(Eq)] type Value int; impl PartialEq for Value {func equals(self Value,other Value)bool{return true}}", "manual PartialEq"},
		{"manual_map_key", "type Key int; impl PartialEq for Key {func equals(self Key,other Key)bool{return true}}; impl Eq for Key {}; #[derive(PartialEq)] type Value struct { Map map[Key]int }", "stable native"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := lowerDeriveSources(map[string]string{"derivefixture/main.gox": "package main\n" + test.source + "\n"})
			if err == nil || output != nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("output=%v error=%v, want %q", output, err, test.want)
			}
		})
	}
}
