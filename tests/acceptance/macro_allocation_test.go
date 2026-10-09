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

// Allocation type slots have a narrower contract than arbitrary Go expressions.
// Exercise that distinction through the actual CLI and independently execute
// the generated module, including definitions in a separate source package.
func TestMacroAllocationCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "build", "-o", cli, "./cmd/goxide")
	outside := t.TempDir()

	t.Run("generated-module", func(t *testing.T) {
		source := t.TempDir()
		packagesWrite(t, source, "go.mod", "module allocation.example/app\ngo 1.27\n")
		writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
			"ops":       {Module: "allocation.example/app", Package: "allocation.example/app/ops"},
			"own":       {Module: "allocation.example/app", Package: "allocation.example/app/own"},
			"primitive": {Module: "allocation.example/app", Package: "allocation.example/app/primitive"},
		})
		definitions := macroAllocationDefinitions
		var calls []string
		for _, name := range []string{"bool", "string", "byte", "rune", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128", "any", "error"} {
			definitions += fmt.Sprintf("pub macro_rules! zero_%s {()=>{new(%s)};}\n", name, name)
			calls = append(calls, "m::zero_"+name+"!()")
		}
		packagesWrite(t, source, "ops/macros.gox", definitions)
		packagesWrite(t, source, "own/own.gox", `package own
func make(n int)int{return n+40}
func new(n int)int{return n+50}
macro_rules! allocate {($x:expr)=>{make($x)+new($x)};}
func Run()int{make:=func(int)int{return -1};new:=func(int)int{return -2};_=make;_=new;return allocate!(3)}
`)
		// Primitive-name shadowing and enhanced references are intentionally in
		// separate packages: refs already rejects their combination in one package.
		packagesWrite(t, source, "primitive/bindings.gox", macroAllocationBindings)
		packagesWrite(t, source, "bindings.gox", macroAllocationCallerBindings)
		packagesWrite(t, source, "main.gox", macroAllocationProgram+"\nfunc Primitives()[]any{return []any{"+strings.Join(calls, ",")+"}}\n")
		first := filepath.Join(t.TempDir(), "generated")
		second := filepath.Join(t.TempDir(), "generated")
		deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", first, source)
		deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", second, source)
		maps := 0
		if err := filepath.WalkDir(first, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !(strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".map.json")) {
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
			if strings.HasSuffix(path, ".map.json") {
				maps++
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if maps == 0 {
			t.Fatal("CLI emitted no source maps")
		}
		packagesWrite(t, first, "allocation_test.go", macroAllocationAssertions)
		deliveryCommand(t, first, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
		deliveryCommand(t, first, tool, true, nil, "vet", "./...")
		if output := deliveryCommand(t, first, tool, true, nil, "run", "-race", "."); output != "2 4 3 0\n" {
			t.Fatalf("unexpected generated application output: %q", output)
		}
	})

	t.Run("type-slots-are-not-captures", func(t *testing.T) {
		for _, tc := range []struct{ name, declaration, pattern, template, args, want string }{
			{"expr-new", "", "$T:expr", "new($T)", "int", "captured types"},
			{"ident-new", "", "$T:ident", "new($T)", "int", "captured types"},
			{"tt-new", "", "$T:tt", "new($T)", "(int)", "captured types"},
			{"expr-slice", "", "$T:expr", "new([]$T)", "int", "captured types"},
			{"ident-pointer", "", "$T:ident", "new(*$T)", "int", "captured types"},
			{"tt-map-element", "", "$T:tt", "make(map[string]$T)", "(int)", "captured types"},
			{"expr-make", "", "$T:expr", "make($T,1)", "[]int", "captured types"},
			{"ident-make", "", "$T:ident", "make($T,1)", "Slice", "captured types"},
			{"tt-make", "", "$T:tt", "make($T,1)", "([]int)", "captured types"},
			{"native-generic-expr", "", "$T:expr", "new($T)", "Generic[int]", "captured types"},
			{"native-generic-tt", "", "$T:tt", "new([]$T)", "(Generic[int])", "captured types"},
			{"native-named", "", "", "new(Local)", "", "fixed predeclared"},
			{"native-generic-fixed", "", "", "new(Generic[int])", "", "fixed predeclared"},
			{"imported-named", `use time;`, "", "new(time::Duration)", "", "direct function calls"},
			{"anonymous-struct", "", "", "new(struct{N int})", "", "unsupported"},
			{"anonymous-interface", "", "", "new(interface{})", "", "unsupported"},
			{"anonymous-function", "", "", "new(func())", "", "unsupported"},
			{"template-value", "", "", "func()*int{int:=7;return new(int)}()", "", "template-local binding"},
			{"definition-function-type-name", "func int()int{return 7}", "", "new(int)", "", "definition-site"},
			// Go 1.27 permits new(expr). These are macro engineering boundaries,
			// deliberately checked before the native Go build, not Go errors.
			{"new-expression-literal", "", "", "new(7)", "", "new(expr)"},
			{"new-expression-call", "func value()int{return 7}", "", "new(value())", "", "new(expr)"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				source := t.TempDir()
				packagesWrite(t, source, "go.mod", "module allocation.example/rejected\ngo 1.27\n")
				writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
					"ops": {Module: "allocation.example/rejected", Package: "allocation.example/rejected/ops"},
				})
				// Keep generic declarations in native .go so a generic declaration
				// prohibition elsewhere cannot mask a leaked allocation type slot.
				packagesWrite(t, source, "native.go", "package main\ntype Generic[T any] struct{Value T}\ntype Local int\ntype Slice []int\n")
				packagesWrite(t, source, "bad.gox", "package main\n"+tc.declaration+"\nmacro_rules! bad {("+tc.pattern+")=>{"+tc.template+"};}\nfunc main(){_=bad!("+tc.args+")}\n")
				output := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
				if !strings.Contains(output, tc.want) || !strings.Contains(output, "bad.gox:3:") || !strings.Contains(output, "macro bad defined at") || !strings.Contains(output, "bad.gox:4:") {
					t.Fatalf("lost allocation type-slot rejection %q: %s", tc.want, output)
				}
			})
		}
	})

	t.Run("native-type-errors", func(t *testing.T) {
		for _, tc := range []struct{ name, template, want string }{
			{"make-scalar", "make(int)", "cannot make"},
			{"make-array", "make([3]int,1)", "cannot make"},
			{"missing-length", "make([]int)", "expects 2 or 3 arguments"},
			{"negative-length", "make([]int,-1)", "negative"},
			{"length-over-capacity", "make([]int,2,1)", "length and capacity swapped"},
			{"bad-map-key", "make(map[[]int]int)", "invalid map key"},
			{"bad-array-length", "new([1.5]int)", "array length"},
			{"extra-new-argument", "new(int,1)", "too many arguments"},
			{"extra-make-argument", "make([]int,1,2,3)", "expects 2 or 3 arguments"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				source := t.TempDir()
				writeMacroFixtureRoots(t, source, nil)
				packagesWrite(t, source, "bad.gox", "package main\nmacro_rules! bad {()=>{"+tc.template+"};}\nfunc main(){_=bad!()}\n")
				input := filepath.Join(source, "bad.gox")
				output := deliveryCommand(t, outside, cli, false, nil, "build", "--go", tool, "--runtime", root, "-o", filepath.Join(t.TempDir(), "rejected"), input)
				if !strings.Contains(output, tc.want) || !strings.Contains(output, "generated Go build") {
					t.Fatalf("expected native Go rejection %q, got %s", tc.want, output)
				}
				if !strings.Contains(output, input+":") && !strings.Contains(output, "generated/unmapped") {
					t.Fatalf("native diagnostic has no honest provenance: %s", output)
				}
			})
		}
	})

	t.Run("namespace-alias-and-value-builtin", func(t *testing.T) {
		source := t.TempDir()
		writeMacroFixtureRoots(t, source, nil)
		packagesWrite(t, source, "main.gox", `package main
use strings as make;
use strings as new;
macro_rules! slice {($n:expr)=>{make([]int,$n)};}
macro_rules! pointer {()=>{new(int)};}
func main(){v:=slice!(2);p:=pointer!();*p=7;if len(v)!=2||v[0]!=0||*p!=7||make::Repeat("x",2)!=new::Repeat("x",2){panic("builtin or namespace binding")}}
`)
		binary := filepath.Join(t.TempDir(), "application")
		deliveryCommand(t, outside, cli, true, nil, "build", "--go", tool, "--runtime", root, "-o", binary, filepath.Join(source, "main.gox"))
		deliveryCommand(t, outside, binary, true, nil)
	})

	t.Run("binding-boundaries", func(t *testing.T) {
		for _, builtin := range []string{"make", "new"} {
			for _, declaration := range []struct{ name, source string }{
				{"variable", "var %s=func(int)int{return 9}"},
				{"constant", "const %s=1"},
				{"type", "type %s int"},
			} {
				t.Run(builtin+"-definition-"+declaration.name, func(t *testing.T) {
					source := t.TempDir()
					writeMacroFixtureRoots(t, source, nil)
					packagesWrite(t, source, "bad.gox", "package main\n"+fmt.Sprintf(declaration.source, builtin)+"\nmacro_rules! bad {()=>{"+builtin+"(int)};}\nfunc main(){_=bad!()}\n")
					output := deliveryCommand(t, outside, cli, false, nil, "transpile", filepath.Join(source, "bad.gox"))
					if !strings.Contains(output, "definition-site") || !strings.Contains(output, "bad.gox:3:") || !strings.Contains(output, "bad.gox:4:") {
						t.Fatal(output)
					}
				})
			}
			for _, declaration := range []struct{ name, source string }{
				{"function", "func %s(int)int{return 9}"},
				{"type", "type %s int"},
			} {
				t.Run(builtin+"-caller-package-"+declaration.name, func(t *testing.T) {
					source := t.TempDir()
					packagesWrite(t, source, "go.mod", "module allocation.example/shadow\ngo 1.27\n")
					writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
						"ops": {Module: "allocation.example/shadow", Package: "allocation.example/shadow/ops"},
					})
					body := "new(int)"
					if builtin == "make" {
						body = "make([]int,1)"
					}
					packagesWrite(t, source, "ops/macros.gox", "package ops\npub macro_rules! bad {()=>{"+body+"};}\n")
					packagesWrite(t, source, "main.gox", "package main\nuse ops as m;\nfunc main(){_=m::bad!()}\n")
					packagesWrite(t, source, "shadow.go", "package main\n"+fmt.Sprintf(declaration.source, builtin)+"\n")
					output := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
					if !strings.Contains(output, "caller package declaration") || !strings.Contains(output, "macros.gox:2:") || !strings.Contains(output, "main.gox:3:") {
						t.Fatal(output)
					}
				})
			}
		}
	})

	t.Run("local-type-and-bare-builtin-boundaries", func(t *testing.T) {
		for _, builtin := range []string{"make", "new"} {
			for _, bare := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-bare-%v", builtin, bare), func(t *testing.T) {
					source := t.TempDir()
					writeMacroFixtureRoots(t, source, nil)
					template := "new(int)"
					if builtin == "make" {
						template = "make([]int,1)"
					}
					body := "type " + builtin + " int;var v " + builtin + ";_=v;_=bad!()"
					want := "local named type"
					if bare {
						template = builtin
						body = "_=bad!()"
						want = "direct call"
					}
					packagesWrite(t, source, "bad.gox", "package main\nmacro_rules! bad {()=>{"+template+"};}\nfunc main(){"+body+"}\n")
					output := deliveryCommand(t, outside, cli, false, nil, "transpile", filepath.Join(source, "bad.gox"))
					if !strings.Contains(output, want) || !strings.Contains(output, "bad.gox:2:") || !strings.Contains(output, "bad.gox:3:") {
						t.Fatal(output)
					}
				})
			}
		}
	})

	t.Run("primitive-package-and-import-boundaries", func(t *testing.T) {
		for _, where := range []string{"definition", "caller-package", "caller-import"} {
			t.Run(where, func(t *testing.T) {
				source := t.TempDir()
				packagesWrite(t, source, "go.mod", "module allocation.example/primitive\ngo 1.27\n")
				writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
					"ops": {Module: "allocation.example/primitive", Package: "allocation.example/primitive/ops"},
				})
				packagesWrite(t, source, "ops/macros.gox", "package ops\npub macro_rules! zero {()=>{new(int)};}\n")
				caller := "package main\nuse ops as m;\nfunc main(){_=m::zero!()}\n"
				want := "caller package declaration"
				switch where {
				case "definition":
					packagesWrite(t, source, "ops/shadow.go", "package ops\ntype int string\n")
					want = "definition-site declaration"
				case "caller-package":
					packagesWrite(t, source, "shadow.go", "package main\ntype int string\n")
				case "caller-import":
					caller = "package main\nuse ops as m;\nuse strings as int;\nfunc main(){_=int::Repeat(\"x\",1);_=m::zero!()}\n"
					want = "namespace int conflicts with a predeclared type"
				}
				packagesWrite(t, source, "main.gox", caller)
				output := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
				if where == "caller-import" {
					if !strings.Contains(output, want) || !strings.Contains(output, "main.gox:3:") {
						t.Fatal(output)
					}
					return
				}
				if !strings.Contains(output, want) || !strings.Contains(output, "macros.gox:2:") || !strings.Contains(output, "main.gox:") {
					t.Fatal(output)
				}
			})
		}
	})

	t.Run("readonly-allocation-does-not-grant-write", func(t *testing.T) {
		source := t.TempDir()
		writeMacroFixtureRoots(t, source, nil)
		packagesWrite(t, source, "bad.gox", `package main
macro_rules! allocate {($n:expr)=>{make([]int,$n)};}
type State struct{Items []int}
func Reject(r &State){r.Items=allocate!(1)}
func main(){}
`)
		output := deliveryCommand(t, outside, cli, false, nil, "transpile", filepath.Join(source, "bad.gox"))
		if !strings.Contains(output, "readonly") || !strings.Contains(output, "bad.gox") {
			t.Fatalf("allocation erased readonly permission: %s", output)
		}
	})

	t.Run("single-file-newlines-and-source", func(t *testing.T) {
		for _, newline := range []struct{ name, value string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
			t.Run(newline.name, func(t *testing.T) {
				source := t.TempDir()
				writeMacroFixtureRoots(t, source, nil)
				input := filepath.Join(source, "main.gox")
				packagesWrite(t, source, "main.gox", strings.ReplaceAll(`package main
use fmt;
macro_rules! allocate {($n:expr)=>{make([]int,$n)};}
macro_rules! pointer {()=>{new(int)};}
func main(){type int string;v:=pointer!();*v=3;fmt::Println(len(allocate!(2)),*v)}
`, "\n", newline.value))
				first := filepath.Join(t.TempDir(), "main.go")
				second := filepath.Join(t.TempDir(), "main.go")
				for _, output := range []string{first, second} {
					deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", output, "--source-map", output+".map.json", input)
				}
				for _, suffix := range []string{"", ".map.json"} {
					a, err := os.ReadFile(first + suffix)
					if err != nil {
						t.Fatal(err)
					}
					b, err := os.ReadFile(second + suffix)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(a, b) {
						t.Fatalf("repeated output differs: %s", suffix)
					}
				}
				generated := filepath.Dir(first)
				packagesWrite(t, generated, "go.mod", "module allocation.example/single\ngo 1.27\n")
				if output := deliveryCommand(t, generated, tool, true, nil, "run", "."); output != "2 3\n" {
					t.Fatal(output)
				}
				packagesWrite(t, source, "bad.gox", strings.ReplaceAll("package main\nmacro_rules! bad {($T:ident)=>{new($T)};}\nfunc main(){_=bad!(int)}\n", "\n", newline.value))
				bad := filepath.Join(source, "bad.gox")
				output := deliveryCommand(t, outside, cli, false, nil, "transpile", bad)
				if !strings.Contains(output, "captured types") || !strings.Contains(output, bad+":2:") || !strings.Contains(output, "macro bad defined at "+bad+":2:") || !strings.Contains(output, "invoked at "+bad+":3:") {
					t.Fatalf("lost allocation slot source/stack: %s", output)
				}
			})
		}
	})
}

const macroAllocationDefinitions = `package ops
pub macro_rules! slice {($n:expr)=>{make([]int,$n)};}
pub macro_rules! bytes {($n:expr,$c:expr)=>{make([]byte,$n,$c)};}
pub macro_rules! mapping {()=>{make(map[string][]byte)};}
pub macro_rules! channel {($n:expr)=>{make(chan int,$n)};}
pub macro_rules! receive {($n:expr)=>{make(<-chan int,$n)};}
pub macro_rules! pointer {()=>{new(int)};}
pub macro_rules! parenthesized {()=>{new((int))};}
pub macro_rules! double_pointer {()=>{new(*int)};}
pub macro_rules! slice_pointer {()=>{new([]int)};}
pub macro_rules! array {($n:expr)=>{new([$n]int)};}
pub macro_rules! local_array {()=>{func()*[3]int{const size=3;return new([size]int)}()};}
pub macro_rules! compound {($n:expr)=>{make([][2]*int,$n)};}
pub macro_rules! channel_array {()=>{new([2]chan<- int)};}
pub macro_rules! nested {($n:expr)=>{slice!($n)};}
pub macro_rules! repeated {($n:expr)=>{len(slice!($n))+len(slice!($n))};}
pub macro_rules! question {($n:expr,$c:expr)=>{make([]byte,$n?,$c?)};}
pub macro_rules! local_functions {($x:expr)=>{func()int{make:=func(n int)int{return n+1};new:=func(n int)int{return n+2};return make($x)+new($x)}()};}
pub macro_rules! call {($f:ident,$x:expr)=>{$f($x)};}
`

const macroAllocationBindings = `package primitive
use ops as m;
use reflect;
func PrimitiveShadow()string{
 type int string
 original:=int("kept");p:=m::pointer!();*p=7
 return reflect::TypeOf(*p).Name()+"/"+reflect::TypeOf(original).Name()+"/"+string(original)
}
`

const macroAllocationCallerBindings = `package main
use ops as m;
func BuiltinShadows()int{
 make:=func(n int)int{return n+10};new:=func(n int)int{return n+20}
 a:=m::slice!(2);p:=m::pointer!();*p=3
 return len(a)+*p+m::call!(make,4)+m::call!(new,5)+m::local_functions!(3)
}
`

const macroAllocationProgram = `package main
use fmt;
use ops as m;
use own as o;
use primitive as p;
func PrimitiveShadow()string{return p::PrimitiveShadow()}
func Slice(n int)[]int{return m::nested!(n)}
func Bytes(n,c int)[]byte{return m::bytes!(n,c)}
func Mapping()map[string][]byte{return m::mapping!()}
func Channel(n int)chan int{return m::channel!(n)}
func Receive(n int)<-chan int{return m::receive!(n)}
func Pointer()*int{return m::pointer!()}
func Parenthesized()*int{return m::parenthesized!()}
func DoublePointer()**int{return m::double_pointer!()}
func SlicePointer()*[]int{return m::slice_pointer!()}
func Array()*[3]int{return m::array!(1+2)}
func LocalArray()*[3]int{return m::local_array!()}
func Compound(n int)[][2]*int{return m::compound!(n)}
func ChannelArray()*[2]chan<- int{return m::channel_array!()}
func Order(n,c func()int)[]byte{return m::bytes!(n(),c())}
func Repeated(n func()int)int{return m::repeated!(n())}
func DefinitionFunctions()int{return o::Run()}
func ReadonlySize(r &[]int)[]byte{return m::bytes!(len(*r),cap(*r))}
var Trace string
func Size(ok bool,tag string,value int)Option[int]{Trace+=tag;if ok{return Option[int]::Some(value)};return Option[int]::None}
func Question(nOK,cOK bool)Option[int]{v:=m::question!(Size(nOK,"N",2),Size(cOK,"C",4));Trace+="A";return Option[int]::Some(len(v)+cap(v))}
func QuestionSize(nOK,cOK bool)int{return match Question(nOK,cOK){Option[int]::Some(n)=>n,Option[int]::None=>-1,}}
func NestedQuestion(nOK,cOK bool)int{inner:=func()Option[int]{v:=m::question!(Size(nOK,"N",2),Size(cOK,"C",4));Trace+="A";return Option[int]::Some(len(v)+cap(v))};result:=inner();Trace+="O";return match result{Option[int]::Some(n)=>n,Option[int]::None=>-1,}}
func main(){v:=m::bytes!(2,4);fmt::Println(len(v),cap(v),len(m::array!(3)),*m::pointer!())}
`

const macroAllocationAssertions = `package main
import("reflect";"testing")
func TestStorageKindsZeroValuesAndIndependence(t *testing.T){
 a,b:=Slice(2),Slice(2);if len(a)!=2||cap(a)!=2||&a[0]==&b[0]||a[0]!=0{t.Fatal(a,b)};a[0]=7;if b[0]!=0{t.Fatal("slice allocations alias")}
 empty:=Slice(0);if empty==nil||len(empty)!=0{t.Fatal("make returned nil slice")}
 first,second:=Mapping(),Mapping();if first==nil||second==nil{t.Fatal("nil map")};first["x"]=[]byte{1};if len(second)!=0{t.Fatal("maps alias")}
 ch:=Channel(2);ch<-4;ch<-5;if cap(ch)!=2||<-ch!=4||<-ch!=5{t.Fatal("channel allocation")};if cap(Receive(3))!=3{t.Fatal("receive-only channel")}
 p,q:=Pointer(),Pointer();if p==q||*p!=0||*Parenthesized()!=0{t.Fatal("pointer allocation")};*p=9;if *q!=0{t.Fatal("pointers alias")}
 if *DoublePointer()!=nil||*SlicePointer()!=nil||*Array()!=[3]int{}||*LocalArray()!=[3]int{}{t.Fatal("pointer/array zero values")}
 compound:=Compound(2);if len(compound)!=2||compound[0][0]!=nil||*ChannelArray()!=[2]chan<- int{}{t.Fatal("recursive fixed types")}
}
func TestEveryPredeclaredType(t *testing.T){
 want:=[]string{"*bool","*string","*uint8","*int32","*int","*int8","*int16","*int32","*int64","*uint","*uint8","*uint16","*uint32","*uint64","*uintptr","*float32","*float64","*complex64","*complex128","*interface {}","*error"}
 got:=Primitives();if len(got)!=len(want){t.Fatal(len(got))};for i,p:=range got{value:=reflect.ValueOf(p);if value.Type().String()!=want[i]||value.IsNil()||!value.Elem().IsZero(){t.Fatalf("primitive %d: %T %v",i,p,p)}}
}
func TestSizeOrderMultiplicityAndBindings(t *testing.T){
 // Compare the generated operation with the native implementation of this
 // toolchain, and assert each operand executes once.
 trace:="";nCalls,cCalls:=0,0
 size:=func()int{trace+="N";nCalls++;return 2};space:=func()int{trace+="C";cCalls++;return 4}
 native:=make([]byte,size(),space());nativeTrace:=trace
 if nCalls!=1||cCalls!=1{t.Fatal("native operand count",nCalls,cCalls)}
 trace="";nCalls=0;cCalls=0;v:=Order(size,space)
 if trace!=nativeTrace||nCalls!=1||cCalls!=1||len(v)!=len(native)||cap(v)!=cap(native){t.Fatal("native make behavior changed",trace,nativeTrace,nCalls,cCalls,len(v),cap(v))}
 calls:=0;if n:=Repeated(func()int{calls++;return calls});calls!=2||n!=3{t.Fatal("template multiplicity",calls,n)}
 if PrimitiveShadow()!="int/int/kept"||BuiltinShadows()!=53||DefinitionFunctions()!=96{t.Fatal("binding changed",PrimitiveShadow(),BuiltinShadows(),DefinitionFunctions())}
 original:=make([]int,2,4);v=ReadonlySize(&original);v[0]=7;if len(v)!=2||cap(v)!=4||original[0]!=0{t.Fatal("readonly sizing or new storage",v,original)}
}
func TestQuestionKeepsNearestFrameAndSkipsLaterSizes(t *testing.T){
 for _,nested:=range []bool{false,true}{for _,nOK:=range []bool{false,true}{for _,cOK:=range []bool{false,true}{Trace="";run:=QuestionSize;if nested{run=NestedQuestion};got:=run(nOK,cOK);want:=-1;wantTrace:="N";if nOK{wantTrace+="C";if cOK{want=6;wantTrace+="A"}};if nested{wantTrace+="O"};if got!=want||Trace!=wantTrace{t.Fatalf("nested=%v len=%v cap=%v result=%d trace=%q",nested,nOK,cOK,got,Trace)}}}}
}
`
