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

func TestNativeGenericAliasesRetainArgumentMappingAndPermissions(t *testing.T) {
	for _, tc := range []struct{ name, extra, want string }{
		{"values", `func Write(value shared::Flip[int,&mut int]){*value.Left=7}
func Read(value shared::Flip[int,&int])int{return *value.Left}
func main(){
 var fixed shared::Fixed[int]=shared::Box[int,string]{Left:11,Right:"fixed"}
 var flip shared::Flip[string,int]=shared::Box[int,string]{Left:7,Right:"flip"}
 var repeat shared::Repeat[int]=shared::Box[int,int]{Left:9,Right:10}
 fmt::Println(Value::value(fixed),Value::value(flip),Value::value(repeat))
}`, ""},
		{"constraint", `func Bad(v shared::Numeric[string]){};func main(){}`, "does not satisfy"},
		{"readonly", `func Bad(value shared::Flip[int,&int]){*value.Left=7};func main(){}`, "read-only"},
		{"readonly_named", `func Bad(value shared::Box[&int,int]){*value.Left=7};func main(){}`, "read-only"},
		{"readonly_slice", `func Bad(value shared::List[&int]){*value[0]=7};func main(){}`, "read-only"},
		{"readonly_anonymous", `func Bad(value shared::Record[&int]){*value.Value=7};func main(){}`, "read-only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 1
			if tc.want == "" {
				attempts = 2
			}
			var previous []byte
			for attempt := 0; attempt < attempts; attempt++ {
				root := t.TempDir()
				if err := testdeps.WriteFile(filepath.Join(root, "go.mod"), []byte("module nativealias\n\ngo 1.27\n"), 0600); err != nil {
					t.Fatal(err)
				}
				writeDiagnosticModuleFile(t, root, "shared/types.go", []byte(`package shared
type Box[A,B any] struct{Left A;Right B}
type Fixed[T any]=Box[T,string]
type Flip[A,B any]=Box[B,A]
type Repeat[T any]=Box[T,T]
type Numeric[T ~int]=Box[T,string]
type List[T any] []T
type Record[T any]=struct{Value T}
`))
				writeDiagnosticModuleFile(t, root, "main.gox", []byte(`package main
pub mod shared;use fmt;
trait Value {type Out;func value(self Self)Self::Out}
impl[A any,B any] Value for shared::Box[A,B] {type Out=A;func value(self Self)A{return self.Left}}
type Pulse int
trait Measure {func measure(self Self)int}
impl Measure for Pulse {func measure(self Self)int{return int(self)}}
`+tc.extra))
				err := lower.CompileModules([]lower.Module{{Path: "nativealias", Dir: root, Config: testConfig(t)}})
				if tc.want != "" {
					if err == nil || !strings.Contains(err.Error(), "main.gox") || !strings.Contains(err.Error(), tc.want) {
						t.Fatalf("want source-located %s rejection, got %v", tc.want, err)
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
					t.Fatal("same native generic aliases changed generated Go")
				}
				previous = generated
				runGo(t, root, "build", "./...")
				if got := runGo(t, root, "run", "."); got != "11 7 9\n" {
					t.Fatalf("native generic alias output = %q", got)
				}
			}
		})
	}
}
