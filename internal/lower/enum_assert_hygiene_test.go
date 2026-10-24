package lower_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestEnumAssertionImplicitTypesDoNotCaptureLocals(t *testing.T) {
	const source = `package main
use fmt;
struct User{ID int}
enum Nested[T any]{Carry(T)}
struct Record{Values []Nested[int];Table map[string]Nested[User];Fixed [1]Nested[int];Next []Record}
enum E{Variant,Payload(Record)}
func parameter(value E,Variant E)bool{__goxide_enum_assert(value);__goxide_enum_assert(Variant);return true}
func result(value E)(Variant bool){__goxide_enum_assert(value);return true}
func primitive(value E)(int bool){__goxide_enum_assert(value);return true}
func named(value E)(User bool){__goxide_enum_assert(value);return true}
func rejects(f func())(failed bool){defer func(){failed=recover()!=nil}();f();return false}
func main(){
 good:=E::Payload(Record{Values:[]Nested[int]{Nested[int]::Carry(1)},Table:map[string]Nested[User]{"a":Nested[User]::Carry(User{ID:2})},Fixed:[1]Nested[int]{Nested[int]::Carry(3)}})
 if !parameter(good,E::Variant)||!result(good)||!primitive(good)||!named(good){panic("valid enum")}
 var absent Nested[int]
 invalid:=E::Payload(Record{Values:[]Nested[int]{absent}})
 if !rejects(func(){parameter(invalid,E::Variant)})||!rejects(func(){result(invalid)})||!rejects(func(){primitive(invalid)})||!rejects(func(){named(invalid)}){panic("nested enum validation")}
 fmt::Println("hygienic finite assertions passed")
}
`
	sources := map[string]string{"assertnames/main.gox": source}
	first, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	second, err := lowerDeriveSources(sources)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("assertion generation changed: %v", err)
	}
	module := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module assertnames\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "main.go"), first["assertnames/main.gox"], 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", ".")
	if got := runGo(t, module, "run", "."); got != "hygienic finite assertions passed\n" {
		t.Fatalf("assertion output = %q", got)
	}
}
