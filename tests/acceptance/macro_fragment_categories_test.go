package acceptance_test

import (
	"bytes"
	"fmt"
	"github.com/hxsf/goxide/internal/rootconfig"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// RFC0011 FC-01..10: use the real CLI from an unrelated cwd, then execute
// generated Go independently. A macro-category error must precede Go compilation.
func TestMacroFragmentCategoriesCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "build", "-o", cli, "./cmd/goxide")
	outside := t.TempDir()

	t.Run("FC-06-09-caller-expressions-and-definition-bindings", func(t *testing.T) {
		source := t.TempDir()
		packagesWrite(t, source, "go.mod", "module fragments.example/app\ngo 1.27\n")
		writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
			"ops": {Module: "fragments.example/app", Package: "fragments.example/app/ops"},
		})
		packagesWrite(t, source, "ops/macros.gox", `package ops
func Helper(n int)int{return n+10}
pub macro_rules! identity {($x:expr)=>{$x};}
pub macro_rules! nested {($x:expr)=>{identity!($x)};}
pub macro_rules! size {($x:expr)=>{len($x)};}
pub macro_rules! bound {($x:expr)=>{Helper($x)};}
pub macro_rules! array {($n:expr)=>{new([$n]int)};}
pub macro_rules! pointer_call {($f:expr,$x:expr)=>{(*$f)($x)};}
pub macro_rules! value_case {($x:expr,$n:expr)=>{func()int{switch $x{case $n:return 7};return 0}()};}
pub macro_rules! typed {($x:expr)=>{func()int{var value int=$x;return value}()};}
`)
		packagesWrite(t, source, "main.gox", `package main
use fmt;
use reflect;
use ops as m;
func main(){
 type Local int
 calls:=0
 next:=func()int{calls++;return 3}
 values:=m::nested!(make([]Local,2))
 if len(values)!=2||values[0]!=0{panic("allocation")}
 if reflect::TypeOf(values).Elem().Name()!="Local"{panic("caller type")}
 n:=m::identity!(func(value Local)Local{return value}(Local(3)))
 a:=m::array!(len([2]Local{}))
 if len(a)!=2||n!=Local(3){panic("opaque type syntax")}
 if m::identity!(any(Local(4)).(Local))!=Local(4){panic("opaque assertion")}
 f:=func(n int)int{return n+1}
 if m::pointer_call!(&f,4)!=5||m::value_case!(3,3)!=7{panic("expression roles")}
 if m::typed!(next())!=3||calls!=1{panic("evaluation count")}
 Helper:=func(int)int{return -1};_=Helper
 if m::bound!(2)!=12{panic("definition binding")}
 {len:=func(string)int{return 90};if m::size!("abc")!=3||len("x")!=90{panic("local value binding")}}
 {type len int;if reflect::TypeOf(m::identity!(len(4))).Name()!="len"{panic("capture type identity")}}
 if m::size!("abcd")!=4{panic("unrelated type scope")}
 {make:=func()[]Local{calls++;return []Local{9}};if m::identity!(make())[0]!=9||calls!=2{panic("captured callee binding")}}
 fmt::Println("fragment categories and bindings ok")
}
`)
		first := filepath.Join(t.TempDir(), "generated")
		second := filepath.Join(t.TempDir(), "generated")
		for _, output := range []string{first, second} {
			deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", output, source)
		}
		err := filepath.WalkDir(first, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !(strings.HasSuffix(path, ".go")) {
				return nil
			}
			rel, err := filepath.Rel(first, path)
			if err != nil {
				return err
			}
			a, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(filepath.Join(second, rel))
			if err != nil {
				return err
			}
			if !bytes.Equal(a, b) {
				t.Errorf("repeated CLI output differs: %s", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		packagesWrite(t, first, "main_test.go", "package main\nimport \"testing\"\nfunc TestBehavior(t *testing.T){main()}\n")
		deliveryCommand(t, first, tool, true, nil, "test", "-race", "-count=1", "./...")
		deliveryCommand(t, first, tool, true, nil, "vet", "./...")
		if output := deliveryCommand(t, first, tool, true, nil, "run", "-race", "."); output != "fragment categories and bindings ok\n" {
			t.Fatalf("unexpected generated program output: %q", output)
		}
	})

	t.Run("FC-01-05-type-slots-rejected-before-Go", func(t *testing.T) {
		cases := []struct{ name, template string }{
			{"var", "func()int{var v $T;_=v;return 0}()"},
			{"parameter", "func(v $T)int{return 0}(1)"},
			{"result", "func()$T{return 1}()"},
			{"named-result", "func()(v $T){return 1}()"},
			{"variadic", "func(v ...$T)int{return 0}(1)"},
			{"slice", "func()int{var v []$T;_=v;return 0}()"},
			{"array", "func()int{var v [2]$T;_=v;return 0}()"},
			{"map-key", "func()int{var v map[$T]int;_=v;return 0}()"},
			{"map-value", "func()int{var v map[int]$T;_=v;return 0}()"},
			{"channel", "func()int{var v chan $T;_=v;return 0}()"},
			{"pointer", "func()int{var v *$T;_=v;return 0}()"},
			{"assertion", "any(1).($T)"},
			{"type-switch", "func()int{switch any(1).(type){case $T:return 1};return 0}()"},
			{"literal", "[]$T{1}"},
			{"array-length-type", "func()int{var v [len([2]$T{})]int;return len(v)}()"},
		}
		for _, tc := range cases {
			for _, ending := range []string{"LF", "CRLF"} {
				t.Run(tc.name+"-"+ending, func(t *testing.T) {
					source := t.TempDir()
					writeMacroFixtureRoots(t, source, nil)
					text := "package main\nmacro_rules! bad {($T:expr)=>{" + tc.template + "};}\nfunc main(){_=bad!(int)}\n"
					if ending == "CRLF" {
						text = strings.ReplaceAll(text, "\n", "\r\n")
					}
					packagesWrite(t, source, "bad.gox", text)
					input := filepath.Join(source, "bad.gox")
					output := deliveryCommand(t, outside, cli, false, nil, "transpile", input)
					for _, want := range []string{"type", "fragment", "bad.gox:2:", "bad.gox:3:"} {
						if !strings.Contains(output, want) {
							t.Fatalf("lost category diagnostic %q: %s", want, output)
						}
					}
					if strings.Contains(output, "generated Go build") {
						t.Fatalf("category error deferred to Go: %s", output)
					}
				})
			}
		}
	})

	t.Run("cross-package-nested-fragments", func(t *testing.T) {
		for _, fragment := range []string{"expr", "ident", "tt"} {
			t.Run(fragment, func(t *testing.T) {
				source := t.TempDir()
				packagesWrite(t, source, "go.mod", "module fragments.example/rejected\ngo 1.27\n")
				writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
					"ops": {Module: "fragments.example/rejected", Package: "fragments.example/rejected/ops"},
				})
				packagesWrite(t, source, "ops/macros.gox", fmt.Sprintf("package ops\nmacro_rules! inner {($T:%s)=>{func()int{var value $T;_=value;return 0}()};}\npub macro_rules! outer {($T:%s)=>{inner!($T)};}\n", fragment, fragment))
				packagesWrite(t, source, "main.gox", "package main\nuse ops as m;\nfunc main(){_=m::outer!(int)}\n")
				output := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
				for _, want := range []string{"type", "fragment", "ops/macros.gox:2:", "ops/macros.gox:3:", "main.gox:3:"} {
					if !strings.Contains(output, want) {
						t.Fatalf("lost package/nested category provenance %q: %s", want, output)
					}
				}
			})
		}
	})

	t.Run("named-type-and-alias-boundary", func(t *testing.T) {
		for _, declaration := range []string{"type len int", "type len = int"} {
			source := t.TempDir()
			writeMacroFixtureRoots(t, source, nil)
			packagesWrite(t, source, "bad.gox", "package main\nmacro_rules! size {($x:expr)=>{len($x)};}\nfunc main(){"+declaration+";var n len;_=n;_=size!(\"abc\")}\n")
			output := deliveryCommand(t, outside, cli, false, nil, "transpile", filepath.Join(source, "bad.gox"))
			if !strings.Contains(output, "local named type") || !strings.Contains(output, "bad.gox:2:") {
				t.Fatalf("unsafe type identity rewrite instead of explicit boundary: %s", output)
			}
		}
	})

	t.Run("FC-10-multiple-type-arguments", func(t *testing.T) {
		// Native .go supplies the generic declaration so an unrelated Goxide
		// generic-declaration boundary cannot mask the template category error.
		helper := "package main\nimport \"fmt\"\nfunc Pair[A,B any](a A,b B)string{return fmt.Sprintf(\"%T=%v %T=%v\",a,a,b,b)}\n"
		for _, fragment := range []string{"expr", "ident", "tt"} {
			t.Run(fragment, func(t *testing.T) {
				source := t.TempDir()
				writeMacroFixtureRoots(t, source, nil)
				packagesWrite(t, source, "go.mod", "module fragments.example/generic\ngo 1.27\n")
				packagesWrite(t, source, "helper.go", helper)
				packagesWrite(t, source, "main.gox", fmt.Sprintf("package main\nmacro_rules! bad {($f:ident,$A:%s,$B:%s)=>{$f[$A,$B](7,\"x\")};}\nfunc main(){_=bad!(Pair,int,string)}\n", fragment, fragment))
				output := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
				for _, want := range []string{"type", "fragment", "main.gox:2:", "main.gox:3:"} {
					if !strings.Contains(output, want) {
						t.Fatalf("lost explicit generic-argument category rejection %q: %s", want, output)
					}
				}
			})
		}
		source := t.TempDir()
		writeMacroFixtureRoots(t, source, nil)
		packagesWrite(t, source, "go.mod", "module fragments.example/generic\ngo 1.27\n")
		packagesWrite(t, source, "helper.go", helper)
		packagesWrite(t, source, "main.gox", `package main
use fmt;
macro_rules! keep {($x:expr)=>{$x};}
macro_rules! invoke {($f:expr,$a:expr,$b:expr)=>{$f($a,$b)};}
func main(){fmt::Println(keep!(Pair[int,string](7,"x")));fmt::Println(invoke!(Pair[int,string],9,"y"))}
`)
		generated := filepath.Join(t.TempDir(), "generated")
		deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source)
		if output := deliveryCommand(t, generated, tool, true, nil, "run", "-race", "."); output != "int=7 string=x\nint=9 string=y\n" {
			t.Fatalf("whole generic capture lost caller identity: %q", output)
		}
	})
}
