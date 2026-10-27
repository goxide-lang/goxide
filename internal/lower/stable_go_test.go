package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestStableGoLibraryWithoutConsumerDemands(t *testing.T) {
	const library = `package library
pub trait Default {func value()Self}
impl Default for int {func value()Self{return 7}}
pub trait Clone {func clone(self &Self)Self}
impl Clone for int {func clone(self &Self)Self{return *self+1}}
impl[T Clone] Clone for []T {
 func clone(self &Self)Self {
  result:=make([]T,len(*self))
  for i:=range *self {result[i]=Clone::clone(&(*self)[i])}
  return result
 }
}
pub func New[T Default]()T{return T::value()}
pub func Copy[T Clone](value &T)T{return Clone::clone(value)}
`
	const prefix = `package main
use fmt;
use library;
struct User {Value int}
impl library::Clone for User {func clone(self &Self)Self{return User{Value:self.Value+10}}}
`
	runStableGoLibraryFixture(t, library, prefix, []stableGoCase{
		{"builtin", `func main(){values:=[]int{1,2};copied:=library::Copy(&values);fmt::Println(library::New[int](),copied[0],copied[1])}`, "7 2 3\n"},
		{"consumer type", `func main(){values:=[]User{{Value:1},{Value:2}};copied:=library::Copy(&values);fmt::Println(library::New[int](),copied[0].Value,copied[1].Value)}`, "7 11 12\n"},
	})
}

type stableGoCase struct{ name, consumer, want string }

func runStableGoLibraryFixture(t *testing.T, library, prefix string, cases []stableGoCase) {
	t.Helper()
	roots := paths.Roots{"library": {PackagePath: "stablefixture/library", ModulePath: "stablefixture"}}
	var frozen []byte
	compile := func(t *testing.T, consumer string) (string, []byte) {
		t.Helper()
		directory := t.TempDir()
		if err := testdeps.WriteFile(filepath.Join(directory, "go.mod"), []byte("module stablefixture\n\ngo 1.27\n"), 0600); err != nil {
			t.Fatal(err)
		}
		writeDiagnosticModuleFile(t, directory, "library/lib.gox", []byte(library))
		if consumer != "" {
			writeDiagnosticModuleFile(t, directory, "main.gox", []byte(prefix+consumer))
			if frozen != nil {
				writeDiagnosticModuleFile(t, directory, "library/lib_gox.go", frozen)
			}
		}
		if err := lower.CompileModules([]analysis.Module{{Path: "stablefixture", Dir: directory, Config: testConfig(t), Roots: roots, StableGo: true}}); err != nil {
			t.Fatal(err)
		}
		generated, err := os.ReadFile(filepath.Join(directory, "library/lib_gox.go"))
		if err != nil {
			t.Fatal(err)
		}
		return directory, generated
	}
	published, libraryGo := compile(t, "")
	frozen = libraryGo
	runGo(t, published, "build", "-mod=readonly", "./...")
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var previous []byte
			for attempt := 0; attempt < 2; attempt++ {
				directory, _ := compile(t, test.consumer)
				consumerGo, err := os.ReadFile(filepath.Join(directory, "main_gox.go"))
				if err != nil {
					t.Fatal(err)
				}
				if attempt > 0 && !bytes.Equal(previous, consumerGo) {
					t.Fatal("same consumer changed generated Go")
				}
				previous = consumerGo
				// Keep exactly the library built before either consumer existed.
				writeDiagnosticModuleFile(t, directory, "library/lib_gox.go", libraryGo)
				runGo(t, directory, "build", "-mod=readonly", "./...")
				if got := runGo(t, directory, "run", "-mod=readonly", "."); got != test.want {
					t.Fatalf("frozen library output = %q, want %q", got, test.want)
				}
			}
		})
	}
}

func TestStableGoConstLibraryWithoutConsumerDemands(t *testing.T) {
	const library = `package library
pub trait Clone {func clone(self &Self)Self}
impl Clone for int {func clone(self &Self)Self{return *self+1}}
impl[T Clone,const N int] Clone for [N]T {
 func clone(self &Self)Self {
  var result [N]T
  for i:=range *self {result[i]=Clone::clone(&(*self)[i])}
  return result
 }
}
pub func Copy[T Clone](value &T)T{return Clone::clone(value)}
pub func Size[const N int]()int{return N}
pub func Identity[T any,const N int](values [N]T)[N]T{return values}
pub func RecordLength[const N int](value struct{Values [N]int})int{return len(value.Values)}
pub func RecordFirst[const N int](value struct{Nested struct{Values [N]int}})int{return value.Nested.Values[0]}
pub func InterfaceLength[const N int](value interface{Values()[N]int})int{return len(value.Values())}
pub func EmbeddedInterfaceLength[const N int](value interface{interface{Values()[N]int}})int{return len(value.Values())}
struct hidden {pub Value int}
impl Clone for hidden {func clone(self &Self)Self{return hidden{Value:self.Value+100}}}
pub func HiddenValues()[2]hidden{return [2]hidden{{Value:1},{Value:2}}}
pub trait Read {func read(self Self)int}
impl Read for int {func read(self Self)int{return self}}
impl Read for hidden {func read(self Self)int{return self.Value}}
impl[T Read,const N int] Read for [N]T {func read(self Self)int{total:=0;for _,value:=range self{total+=Read::read(value)};return total}}
pub func Total[T Read](value T)int{return Read::read(value)}
pub trait Equal {func equal(self Self,other Self)bool}
pub trait Key where Self: Equal,Self: comparable {}
impl Equal for int {func equal(self Self,other Self)bool{return self==other}}
impl Key for int {}
impl[T Equal,const N int] Equal for [N]T {func equal(self Self,other Self)bool{for i:=range self{if !Equal::equal(self[i],other[i]){return false}};return true}}
impl[T comparable,const N int] Key for [N]T where T: Key {}
pub func Same[T Key](left,right T)bool{return Equal::equal(left,right)}
`
	const prefix = `package main
use fmt;
use library;
use library::Clone;
use library::Read;
struct User {Value int}
impl library::Clone for User {func clone(self &Self)Self{return User{Value:self.Value+10}}}
`
	runStableGoLibraryFixture(t, library, prefix, []stableGoCase{
		{"array builtin", `func main(){values:=[2]int{1,2};copied:=library::Copy(&values);same:=library::Identity[int,2](copied);fmt::Println(library::Size[2](),same[0],same[1],values[0])}`, "2 2 3 1\n"},
		{"array consumer type", `func main(){values:=[2]User{{Value:1},{Value:2}};copied:=library::Copy(&values);fmt::Println(copied[0].Value,copied[1].Value)}`, "11 12\n"},
		{"zero and nested", `func main(){empty:=[0]int{};zero:=library::Copy(&empty);nested:=[2][2]int{{1,2},{3,4}};copy:=library::Copy(&nested);fmt::Println(len(zero),copy[0][0],copy[1][1])}`, "0 2 5\n"},
		{"runtime and method value", `func main(){values:=[2]int{20,22};method:=values.read;var view Read=values;fmt::Println(method(),view.read(),library::Total(values))}`, "42 42 42\n"},
		{"private runtime view", `func main(){values:=library::HiddenValues();method:=values.read;var view Read=values;fmt::Println(method(),view.read(),library::Total(values))}`, "3 3 3\n"},
		{"private element", `func main(){values:=library::HiddenValues();copied:=library::Copy(&values);fmt::Println(copied[0].Value,copied[1].Value)}`, "101 102\n"},
		{"array inherited bound", `func main(){left:=[2]int{1,2};right:=[2]int{1,3};fmt::Println(library::Same(left,left),library::Same(left,right))}`, "true false\n"},
		{"anonymous struct arrays", `func main(){value:=struct{Values [2]int}{Values:[2]int{1,2}};nested:=struct{Nested struct{Values [2]int}}{Nested:value};fmt::Println(library::RecordLength[2](value),library::RecordFirst[2](nested))}`, "2 1\n"},
		{"interface array methods", `struct Provider {Calls *int};func (p Provider) Values()[2]int{*p.Calls=*p.Calls+1;return [2]int{1,2}};func main(){calls:=0;value:=Provider{Calls:&calls};fmt::Println(library::InterfaceLength[2](value),library::EmbeddedInterfaceLength[2](value),calls)}`, "2 2 2\n"},
	})
}
