package acceptance_test

import (
	"bytes"
	"github.com/goxide-lang/goxide/internal/rootconfig"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercise the public CLI from an unrelated directory, then use native Go
// assertions to observe the generated program's bindings and evaluation order.
func TestMacroAppendCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "run", "./scripts/build.go", "-o", cli)
	outside := t.TempDir()

	t.Run("generated-module", func(t *testing.T) {
		source := t.TempDir()
		packagesWrite(t, source, "go.mod", "module append.example/app\ngo 1.27\n")
		writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
			"ops": {Module: "append.example/app", Package: "append.example/app/ops"},
			"own": {Module: "append.example/app", Package: "append.example/app/own"},
		})
		packagesWrite(t, source, "ops/macros.gox", macroAppendDefinitions)
		packagesWrite(t, source, "own/own.gox", `package own
func append(s []int,v int)[]int{return []int{41,v}}
macro_rules! own {($s:expr,$v:expr)=>{append($s,$v)};}
func Run()[]int{append:=func([]int,int)[]int{return []int{-1}};_=append;return own!([]int{},3)}
`)
		packagesWrite(t, source, "types.gox", "package main\ntype Items []int\ntype Record struct{append int}\n")
		packagesWrite(t, source, "bindings.gox", macroAppendBindings)
		packagesWrite(t, source, "main.gox", macroAppendProgram)
		first := filepath.Join(t.TempDir(), "generated")
		second := filepath.Join(t.TempDir(), "generated")
		deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", first, source)
		deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", second, source)
		if err := filepath.WalkDir(first, func(path string, entry os.DirEntry, walkErr error) error {
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
		}); err != nil {
			t.Fatal(err)
		}
		packagesWrite(t, first, "append_test.go", macroAppendAssertions)
		deliveryCommand(t, first, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
		deliveryCommand(t, first, tool, true, nil, "vet", "./...")
		if output := deliveryCommand(t, first, tool, true, nil, "run", "-race", "."); output != "[1 2 3] ab\n" {
			t.Fatalf("unexpected generated application output: %q", output)
		}
	})

	t.Run("single-file-newlines", func(t *testing.T) {
		for _, newline := range []struct{ name, value string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
			t.Run(newline.name, func(t *testing.T) {
				source := t.TempDir()
				writeMacroFixtureRoots(t, source, nil)
				input := filepath.Join(source, "main.gox")
				packagesWrite(t, source, "main.gox", strings.ReplaceAll(`package main
use fmt;
macro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};}
func main(){append:=2;fmt::Println(push!([]int{1},append))}
`, "\n", newline.value))
				generated := t.TempDir()
				first := filepath.Join(generated, "main.go")
				second := filepath.Join(t.TempDir(), "main.go")
				for _, output := range []string{first, second} {
					deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", output, input)
				}
				a, err := os.ReadFile(first)
				if err != nil {
					t.Fatal(err)
				}
				b, err := os.ReadFile(second)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(a, b) {
					t.Fatalf("single-file repeated output differs")
				}

				packagesWrite(t, generated, "go.mod", "module append.example/single\ngo 1.27\n")
				if output := deliveryCommand(t, generated, tool, true, nil, "run", "."); output != "[1 2]\n" {
					t.Fatal(output)
				}
				packagesWrite(t, source, "invalid.gox", strings.ReplaceAll(`package main
macro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};}
func main(){_=push!([]int{},"bad")}
`, "\n", newline.value))
				invalid := filepath.Join(source, "invalid.gox")
				output := deliveryCommand(t, outside, cli, false, nil, "build", "--go", tool, "--runtime", root, "-o", filepath.Join(t.TempDir(), "rejected"), invalid)
				if !strings.Contains(output, "cannot use") || !strings.Contains(output, "invalid.gox:") {
					t.Fatalf("wrong native argument diagnostic: %s", output)
				}
				packagesWrite(t, source, "boundary.gox", strings.ReplaceAll(`package main
macro_rules! bad {()=>{append};}
func main(){_=bad!()}
`, "\n", newline.value))
				boundary := filepath.Join(source, "boundary.gox")
				output = deliveryCommand(t, outside, cli, false, nil, "transpile", boundary)
				if !strings.Contains(output, "must be called") || !strings.Contains(output, "boundary.gox:2:") || !strings.Contains(output, "boundary.gox:3:") {
					t.Fatalf("lost macro source/stack diagnostic: %s", output)
				}
			})
		}
	})

	t.Run("namespace-alias-and-value-builtin", func(t *testing.T) {
		source := t.TempDir()
		writeMacroFixtureRoots(t, source, nil)
		packagesWrite(t, source, "main.gox", `package main
use strings as append;
macro_rules! push { ($s:expr,$v:expr)=>{append($s,$v)}; }
func main(){v:=push!([]int{1},2);if len(v)!=2||v[1]!=2||append::Repeat("x",2)!="xx"{panic("builtin or namespace binding")}}
`)
		binary := filepath.Join(t.TempDir(), "application")
		deliveryCommand(t, outside, cli, true, nil, "build", "--go", tool, "--runtime", root, "-o", binary, filepath.Join(source, "main.gox"))
		deliveryCommand(t, outside, binary, true, nil)
	})

	t.Run("invalid-calls-and-boundaries", func(t *testing.T) {
		for _, tc := range []struct {
			name, declaration, template, body, want string
			syntheticArgument                       bool
		}{
			{"non-slice", "", "append($s,$v)", "bad!(1,2)", "argument must be a slice", true},
			{"wrong-element", "", "append($s,$v)", `bad!([]int{},"bad")`, "cannot use", true},
			{"missing-slice", "", "append()", "bad!(1,2)", "not enough arguments", false},
			{"bad-spread", "", "append($s,$v...)", "bad!([]int{},1)", "cannot use", true},
			{"mixed-spread", "", "append($s,1,$v...)", "bad!([]int{},[]int{2})", "too many arguments", true},
			{"local-type", "", "append($s,$v)", "func()int{type append int;var x append;_=x;return len(bad!([]int{},1))}()", "local named type", false},
			{"definition-var", "var append=func([]int,int)[]int{return nil}", "append($s,$v)", "bad!([]int{},1)", "conflicts with a preimported name", false},
			{"definition-const", "const append=1", "append($s,$v)", "bad!([]int{},1)", "conflicts with a preimported name", false},
			{"definition-type", "type append int", "append($s,$v)", "bad!([]int{},1)", "conflicts with a preimported name", false},
			{"bare-builtin", "", "append", "bad!(1,2)", "must be called", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				source := t.TempDir()
				writeMacroFixtureRoots(t, source, nil)
				packagesWrite(t, source, "invalid.gox", "package main\n"+tc.declaration+"\nmacro_rules! bad {($s:expr,$v:expr)=>{"+tc.template+"};}\nfunc main(){_="+tc.body+"}\n")
				input := filepath.Join(source, "invalid.gox")
				output := deliveryCommand(t, outside, cli, false, nil, "build", "--go", tool, "--runtime", root, "-o", filepath.Join(t.TempDir(), "rejected"), input)
				if !strings.Contains(output, tc.want) {
					t.Fatalf("want %q diagnostic, got %s", tc.want, output)
				}
				if tc.declaration != "" {
					if !strings.Contains(output, "invalid.gox:2:") {
						t.Fatalf("package declaration diagnostic lost its source: %s", output)
					}
				} else if tc.syntheticArgument {
					if !strings.Contains(output, "invalid.gox:") {
						t.Fatalf("native argument diagnostic lost its input file: %s", output)
					}
				} else if !strings.Contains(output, "invalid.gox:3:") || !strings.Contains(output, "invalid.gox:4:") {
					t.Fatalf("lost template source/stack diagnostic: %s", output)
				}
			})
		}
		for _, declaration := range []struct{ name, source string }{
			{"function", "func append([]int,int)[]int{return nil}"},
			{"variable", "var append=func([]int,int)[]int{return nil}"},
			{"type", "type append int"},
		} {
			t.Run("caller-package-"+declaration.name, func(t *testing.T) {
				source := t.TempDir()
				packagesWrite(t, source, "go.mod", "module append.example/rejected\ngo 1.27\n")
				writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
					"ops": {Module: "append.example/rejected", Package: "append.example/rejected/ops"},
				})
				packagesWrite(t, source, "ops/macros.gox", "package ops\npub macro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};}\n")
				packagesWrite(t, source, "app.gox", "package main\nuse ops as m;\nfunc main(){_=m::push!([]int{},1)}\n")
				packagesWrite(t, source, "shadow.go", "package main\n"+declaration.source+"\n")
				output := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
				if !strings.Contains(output, "caller package declaration") || !strings.Contains(output, "macros.gox:2:") || !strings.Contains(output, "app.gox:3:") {
					t.Fatalf("lost package-shadow diagnostic: %s", output)
				}
			})
		}
	})
}

const macroAppendDefinitions = `package ops
use slog;
pub macro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};}
pub macro_rules! many {($s:expr,$($v:expr),*)=>{append($s,$($v),*)};}
pub macro_rules! extend {($s:expr,$tail:expr)=>{append($s,$tail...)};}
pub macro_rules! nested {($s:expr,$v:expr)=>{push!(push!($s,$v),$v)};}
pub macro_rules! question {($s:expr,$v:expr,$later:expr)=>{append($s,$v?,$later)};}
pub macro_rules! local {($s:expr,$v:expr)=>{func()[]int{append:=func([]int,int)[]int{return []int{17}};return append($s,$v)}()};}
pub macro_rules! log_if {($enabled:expr,$s:expr,$v:expr)=>{if $enabled {slog::Info("append","values",append($s,$v))}};}
`

const macroAppendBindings = `package main
use ops as m;
use strings as append;
func Bindings()int{
 imported:=m::push!([]int{},len(append::Repeat("x",2)))[0]
 append:=func(n int)int{return n+1}
 total:=imported+m::push!([]int{},append(3))[0]
 {const append=2;total+=m::push!([]int{},append)[0]}
 switch append:=any(3).(type){case int:total+=m::push!([]int{},append)[0]}
 {append:=4;r:=Record{append:append};total+=m::push!([]int{},r.append)[0];total+=m::push!([]int{},map[int]int{append:7}[append])[0]}
 return total
}
func Parameter(append int)[]int{return m::push!([]int{},append)}
`

const macroAppendProgram = `package main
use fmt;
use ops as m;
use own as o;
func Push(s Items,v int)Items{return m::push!(s,v)}
func Extend(s Items,tail []int)Items{return m::extend!(s,tail)}
func Bytes(s []byte,tail string)[]byte{return m::extend!(s,tail)}
func Order(base func()[]int,one func()int,two func()int)[]int{return m::many!(base(),one(),two())}
func Repeated(base []int,next func()int)[]int{return m::nested!(base,next())}
func DefinitionLocal()[]int{return m::local!([]int{},1)}
func DefinitionFunction()[]int{return o::Run()}
var Trace string
var Storage [4]int
func Base()[]int{Trace+="S";return Storage[:1]}
func Next(ok bool)Option[int]{Trace+="V";if ok{return Option[int]::Some(2)};return Option[int]::None}
func Later()int{Trace+="L";return 3}
func Question(ok bool)Option[[]int]{items:=m::question!(Base(),Next(ok),Later());Trace+="A";return Option[[]int]::Some(items)}
func QuestionLen(ok bool)int{return match Question(ok){Option[[]int]::Some(v)=>len(v),Option[[]int]::None=>-1,}}
func NestedQuestion(ok bool)int{
 inner:=func()Option[[]int]{items:=m::question!(Base(),Next(ok),Later());Trace+="A";return Option[[]int]::Some(items)}
 result:=inner();Trace+="O"
 return match result{Option[[]int]::Some(v)=>len(v),Option[[]int]::None=>-1,}
}
func Logging(enabled func()bool,base func()[]int,value func()int){m::log_if!(enabled(),base(),value())}
func main(){fmt::Println(m::many!([]int{1},2,3),string(Bytes([]byte("a"),"b")))}
`

const macroAppendAssertions = `package main
import (
 "bytes"
 "context"
 "log/slog"
 "reflect"
 "strings"
 "testing"
)
func TestNativeSliceSemantics(t *testing.T){
 backing:=[5]int{1,7,8,9,10};original:=Items(backing[:1]);result:=Push(original,2)
 if reflect.TypeOf(result).Name()!="Items"||len(result)!=2||cap(result)!=5||&result[0]!=&original[0]||backing[1]!=2||len(original)!=1{t.Fatalf("named/capacity semantics: %T %v %v",result,result,backing)}
 full:=Items([]int{1});grown:=Push(full,2);grown[0]=9;if full[0]!=1{t.Fatal("growth retained full backing")}
 overlap:=[6]int{1,2,3,4,5,6};got:=Extend(Items(overlap[:2]),overlap[1:4]);if !reflect.DeepEqual(got,Items{1,2,2,3,4}){t.Fatal(got)}
 var nilSlice Items
 if Extend(nilSlice,nil)!=nil||Extend(nilSlice,[]int{})!=nil{t.Fatal("nil slice changed")}
 empty:=make(Items,0,2);if Extend(empty,nil)==nil||cap(Extend(empty,nil))!=2{t.Fatal("empty slice changed")}
 if got:=Push(nilSlice,4);!reflect.DeepEqual(got,Items{4}){t.Fatal(got)}
 data:=make([]byte,1,4);data[0]='a';joined:=Bytes(data,"bc");if string(joined)!="abc"||&joined[0]!=&data[0]||Bytes(nil,"")!=nil{t.Fatalf("string spread: %q",joined)}
}
func TestBindingsAndTemplateMultiplicity(t *testing.T){
 if Bindings()!=22||!reflect.DeepEqual(Parameter(5),[]int{5})||!reflect.DeepEqual(DefinitionLocal(),[]int{17})||!reflect.DeepEqual(DefinitionFunction(),[]int{41,3}){t.Fatal("definition/capture/caller binding changed",Bindings(),Parameter(5),DefinitionLocal(),DefinitionFunction())}
 n:=0;got:=Repeated(nil,func()int{n++;return n});if n!=2||!reflect.DeepEqual(got,[]int{1,2}){t.Fatal("template evaluation multiplicity changed",n,got)}
 trace:="";got=Order(func()[]int{trace+="S";return []int{0}},func()int{trace+="1";return 1},func()int{trace+="2";return 2})
 if trace!="S12"||!reflect.DeepEqual(got,[]int{0,1,2}){t.Fatal("native operand order changed",trace,got)}
}
func TestQuestionFrameAndNoPartialAppend(t *testing.T){
 for _,nested:=range []bool{false,true}{for _,ok:=range []bool{false,true}{
  Trace="";Storage=[4]int{1,9,9,9};run:=QuestionLen;if nested{run=NestedQuestion};got:=run(ok)
  wantTrace:="SV";wantLen:=-1;wantStorage:=[4]int{1,9,9,9};if ok{wantTrace="SVLA";wantLen=3;wantStorage=[4]int{1,2,3,9}};if nested{wantTrace+="O"}
  if got!=wantLen||Trace!=wantTrace||Storage!=wantStorage{t.Fatalf("nested=%v ok=%v: len=%d trace=%q storage=%v",nested,ok,got,Trace,Storage)}
 }}
}
func TestConditionalLogSkipsAppendArguments(t *testing.T){
 previous:=slog.Default();defer slog.SetDefault(previous)
 for _,level:=range []slog.Level{slog.LevelError,slog.LevelInfo}{
  var output bytes.Buffer;slog.SetDefault(slog.New(slog.NewJSONHandler(&output,&slog.HandlerOptions{Level:level})))
  trace:="";backing:=[2]int{1,9}
  Logging(func()bool{trace+="E";return slog.Default().Enabled(context.Background(),slog.LevelInfo)},func()[]int{trace+="S";return backing[:1]},func()int{trace+="V";return 2})
  if level==slog.LevelError{if trace!="E"||output.Len()!=0||backing[1]!=9{t.Fatal("disabled arguments executed",trace,output.String(),backing)};continue}
  if trace!="ESV"||backing[1]!=2||!strings.Contains(output.String(),"\"values\":[1,2]"){t.Fatal("enabled append order/result changed",trace,output.String(),backing)}
 }
}
`
