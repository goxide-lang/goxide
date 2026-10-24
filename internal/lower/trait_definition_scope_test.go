package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestStaticTraitLookupKeepsDefinitionScope(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		name := "isolated_helpers"
		if conflict {
			name = "ambiguous_definition_bounds"
		}
		t.Run(name, func(t *testing.T) {
			var previous []byte
			for attempt := 0; attempt < 2; attempt++ {
				root := t.TempDir()
				if err := testdeps.WriteFile(filepath.Join(root, "go.mod"), []byte("module staticscope\ngo 1.27\n"), 0600); err != nil {
					t.Fatal(err)
				}
				writeDiagnosticModuleFile(t, root, "first/lib.gox", []byte(`package first
pub trait Ready{func value()int}
impl[T any] Ready for []T{func value()int{return 11}}
pub func Read[T Ready]()int{return T::value()}
`))
				writeDiagnosticModuleFile(t, root, "second/lib.gox", []byte(`package second
pub trait Ready{func value()int}
impl[T any] Ready for []T{func value()int{return 22}}
pub func Read[T Ready]()int{return T::value()}
`))
				extra := ""
				if conflict {
					extra = "func Conflict[T any]()int where T:api::Ready+other::Ready{return T::value()}\n"
				}
				writeDiagnosticModuleFile(t, root, "main.gox", []byte(`package main
pub mod first;pub mod second;
use first as api;use second as other;use fmt;
func Relay[T api::Ready]()int{return T::value()}
func Both[T any]()int where T:api::Ready+other::Ready{return T::value::<api::Ready>()+T::value::<other::Ready>()}
func main(){fmt::Println(api::Read[[]int](),other::Read[[]int](),Relay[[]int](),Both[[]int]())}
`+extra))
				err := lower.CompileModules([]lower.Module{{Path: "staticscope", Dir: root, Config: testConfig(t)}})
				if conflict {
					if err == nil || !strings.Contains(err.Error(), "main.gox:7:") || !strings.Contains(err.Error(), "ambiguous trait member") {
						t.Fatalf("competing definition bounds should remain ambiguous: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				generated, err := os.ReadFile(filepath.Join(root, "main.go"))
				if err != nil {
					t.Fatal(err)
				}
				if attempt > 0 && !bytes.Equal(previous, generated) {
					t.Fatal("definition-scoped trait selection changed generated Go")
				}
				previous = generated
				runGo(t, root, "build", "./...")
				if got := runGo(t, root, "run", "."); got != "11 22 11 33\n" {
					t.Fatalf("definition-scoped helpers returned %q", got)
				}
			}
		})
	}
}

func TestStaticTraitLookupInfersPrivateSourceInputs(t *testing.T) {
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		root := t.TempDir()
		if err := testdeps.WriteFile(filepath.Join(root, "go.mod"), []byte("module staticinput\ngo 1.27\n"), 0600); err != nil {
			t.Fatal(err)
		}
		writeDiagnosticModuleFile(t, root, "model/lib.gox", []byte(`package model
pub trait Convert[S any]{func make(value S)Self}
pub struct Count{pub Value int}
type privateSource int
impl Convert[int] for Count{func make(value int)Self{return Self{Value:value+100}}}
impl Convert[privateSource] for Count{func make(value privateSource)Self{return Self{Value:int(value)+200}}}
pub func Source(value int)privateSource{return privateSource(value)}
`))
		writeDiagnosticModuleFile(t, root, "main.gox", []byte(`package main
pub mod model;
use fmt;
func main(){
 var plain func(int)model::Count=model::Count::make
 fmt::Println(model::Count::make(model::Source(7)).Value,model::Count::make(7).Value,plain(9).Value)
}
`))
		if err := lower.CompileModules([]lower.Module{{Path: "staticinput", Dir: root, Config: testConfig(t)}}); err != nil {
			t.Fatal(err)
		}
		generated, err := os.ReadFile(filepath.Join(root, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if attempt > 0 && !bytes.Equal(previous, generated) {
			t.Fatal("static input selection changed generated Go")
		}
		previous = generated
		runGo(t, root, "build", "./...")
		if got := runGo(t, root, "run", "."); got != "207 107 109\n" {
			t.Fatalf("private and primitive source conversions returned %q", got)
		}
	}
}
