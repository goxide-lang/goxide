package acceptance_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercise the public CLI from an unrelated directory, then use native Go
// assertions to observe the generated program's bindings and evaluation order.
func TestMacroCopyCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "build", "-o", cli, "./cmd/goxide")
	outside := t.TempDir()

	t.Run("generated-module", func(t *testing.T) {
		source := t.TempDir()
		packagesWrite(t, source, "go.mod", "module copy.example/app\ngo 1.27\n")
		packagesWrite(t, source, "ops/macros.gox", macroCopyDefinitions)
		packagesWrite(t, source, "own/own.gox", `package own
func copy(dst,src []int)int{return 41}
macro_rules! own {($dst:expr,$src:expr)=>{copy($dst,$src)};}
func Run()int{copy:=func([]int,[]int)int{return -1};_=copy;return own!([]int{},[]int{})}
`)
		packagesWrite(t, source, "types.gox", "package main\ntype Items []int\ntype Record struct{copy int}\ntype State struct{Items []int}\n")
		packagesWrite(t, source, "bindings.gox", macroCopyBindings)
		packagesWrite(t, source, "main.gox", macroCopyProgram)
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
		packagesWrite(t, first, "copy_test.go", macroCopyAssertions)
		deliveryCommand(t, first, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
		deliveryCommand(t, first, tool, true, nil, "vet", "./...")
		if output := deliveryCommand(t, first, tool, true, nil, "run", "-race", "."); output != "2 [4 5 0] 2 ab\n" {
			t.Fatalf("unexpected generated application output: %q", output)
		}
	})

	t.Run("single-file-newlines", func(t *testing.T) {
		for _, newline := range []struct{ name, value string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
			t.Run(newline.name, func(t *testing.T) {
				source := t.TempDir()
				input := filepath.Join(source, "main.gox")
				packagesWrite(t, source, "main.gox", strings.ReplaceAll(`package main
import "fmt"
macro_rules! transfer {($dst:expr,$src:expr)=>{copy($dst,$src)};}
func main(){copy:=[]int{2};dst:=[]int{1};fmt.Println(transfer!(dst,copy),dst)}
`, "\n", newline.value))
				generated := t.TempDir()
				first := filepath.Join(generated, "main.go")
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
						t.Fatalf("single-file repeated output differs: %s", suffix)
					}
				}
				packagesWrite(t, generated, "go.mod", "module copy.example/single\ngo 1.27\n")
				if output := deliveryCommand(t, generated, tool, true, nil, "run", "."); output != "1 [2]\n" {
					t.Fatal(output)
				}
				packagesWrite(t, source, "invalid.gox", strings.ReplaceAll(`package main
macro_rules! transfer {($dst:expr,$src:expr)=>{copy($dst,$src)};}
func main(){_=transfer!([]int{},[]string{})}
`, "\n", newline.value))
				invalid := filepath.Join(source, "invalid.gox")
				output := deliveryCommand(t, outside, cli, false, nil, "build", "--go", tool, "--runtime", root, "-o", filepath.Join(t.TempDir(), "rejected"), invalid)
				// The Go compiler points at the synthetic capture parenthesis.
				// As documented in internal/sourcemap/README.md, it has no
				// invented source origin; LF and CRLF must report that honestly.
				if !strings.Contains(output, "different element types") || !strings.Contains(output, "generated/unmapped") {
					t.Fatalf("wrong native argument diagnostic: %s", output)
				}
				packagesWrite(t, source, "boundary.gox", strings.ReplaceAll(`package main
macro_rules! bad {()=>{copy};}
func main(){_=bad!()}
`, "\n", newline.value))
				boundary := filepath.Join(source, "boundary.gox")
				output = deliveryCommand(t, outside, cli, false, nil, "transpile", boundary)
				if !strings.Contains(output, "direct call") || !strings.Contains(output, boundary+":2:") || !strings.Contains(output, "macro bad defined at "+boundary+":2:") || !strings.Contains(output, "invoked at "+boundary+":3:") {
					t.Fatalf("lost macro source/stack diagnostic: %s", output)
				}
			})
		}
	})

	t.Run("readonly-boundaries", func(t *testing.T) {
		for _, tc := range []struct{ name, body string }{
			{"destination", "m.transfer!(r.Items,[]int{9})"},
			{"pointer-elements", "dst:=make([]*int,len(r.Pointers));m.transfer!(dst,r.Pointers);*dst[0]=9"},
			{"container-elements", "dst:=make([][]int,len(r.Rows));m.transfer!(dst,r.Rows);dst[0][0]=9"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				source := t.TempDir()
				packagesWrite(t, source, "go.mod", "module copy.example/readonly\ngo 1.27\n")
				packagesWrite(t, source, "ops/macros.gox", "package ops\npub macro_rules! transfer {($dst:expr,$src:expr)=>{copy($dst,$src)};}\n")
				packagesWrite(t, source, "app.gox", "package main\nimport m \"copy.example/readonly/ops\"\ntype State struct{Items []int;Pointers []*int;Rows [][]int}\nfunc Reject(r &State){"+tc.body+"}\nfunc main(){}\n")
				output := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
				if !strings.Contains(output, "readonly") || !strings.Contains(output, "app.gox") {
					t.Fatalf("lost readonly rejection: %s", output)
				}
			})
		}
	})

	t.Run("invalid-calls-and-boundaries", func(t *testing.T) {
		for _, tc := range []struct {
			name, declaration, template, body, want string
			syntheticArgument                       bool
		}{
			{"non-slice", "", "copy($s,$v)", "bad!(1,[]int{})", "argument must be a slice", true},
			{"different-element", "", "copy($s,$v)", "bad!([]int{},[]string{})", "different element types", true},
			{"string-rune", "", "copy($s,$v)", `bad!([]rune{},"a")`, "different element types", true},
			{"missing-argument", "", "copy($s)", "bad!([]int{},[]int{})", "not enough arguments", false},
			{"extra-argument", "", "copy($s,$v,1)", "bad!([]int{},[]int{})", "too many arguments", false},
			{"ellipsis", "", "copy($s,$v...)", "bad!([]int{},[]int{})", "invalid use of ...", false},
			{"local-type", "", "copy($s,$v)", "func()int{type copy int;var x copy;_=x;return bad!([]int{},[]int{})}()", "local named type", false},
			{"definition-var", "var copy=func([]int,[]int)int{return 9}", "copy($s,$v)", "bad!([]int{},[]int{})", "definition-site", false},
			{"definition-const", "const copy=1", "copy($s,$v)", "bad!([]int{},[]int{})", "definition-site", false},
			{"definition-type", "type copy int", "copy($s,$v)", "bad!([]int{},[]int{})", "definition-site", false},
			{"definition-import", `import copy "strings"`, "copy($s,$v)", "bad!([]int{},[]int{})", "definition-site", false},
			{"bare-builtin", "", "copy", "bad!(1,2)", "direct call", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				source := t.TempDir()
				packagesWrite(t, source, "invalid.gox", "package main\n"+tc.declaration+"\nmacro_rules! bad {($s:expr,$v:expr)=>{"+tc.template+"};}\nfunc main(){_="+tc.body+"}\n")
				input := filepath.Join(source, "invalid.gox")
				output := deliveryCommand(t, outside, cli, false, nil, "build", "--go", tool, "--runtime", root, "-o", filepath.Join(t.TempDir(), "rejected"), input)
				if !strings.Contains(output, tc.want) {
					t.Fatalf("want %q diagnostic, got %s", tc.want, output)
				}
				if tc.syntheticArgument {
					// This is the existing synthetic-parenthesis boundary, not a
					// loss of the template's copy token or macro call stack.
					if !strings.Contains(output, "generated/unmapped") {
						t.Fatalf("native argument diagnostic invented a source origin: %s", output)
					}
				} else if !strings.Contains(output, input+":3:") || !strings.Contains(output, "macro bad defined at "+input+":3:") || !strings.Contains(output, "invoked at "+input+":4:") {
					t.Fatalf("lost template source/stack diagnostic: %s", output)
				}
			})
		}
		for _, declaration := range []struct{ name, source string }{
			{"function", "func copy([]int,[]int)int{return 9}"},
			{"variable", "var copy=func([]int,[]int)int{return 9}"},
			{"type", "type copy int"},
		} {
			t.Run("caller-package-"+declaration.name, func(t *testing.T) {
				source := t.TempDir()
				packagesWrite(t, source, "go.mod", "module copy.example/rejected\ngo 1.27\n")
				packagesWrite(t, source, "ops/macros.gox", "package ops\npub macro_rules! push {($s:expr,$v:expr)=>{copy($s,$v)};}\n")
				packagesWrite(t, source, "app.gox", "package main\nimport m \"copy.example/rejected/ops\"\nfunc main(){_=m.push!([]int{},[]int{})}\n")
				packagesWrite(t, source, "shadow.go", "package main\n"+declaration.source+"\n")
				output := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
				if !strings.Contains(output, "caller package declaration") || !strings.Contains(output, "macros.gox:2:") || !strings.Contains(output, "app.gox:3:") {
					t.Fatalf("lost package-shadow diagnostic: %s", output)
				}
			})
		}
	})
}

const macroCopyDefinitions = `package ops
import "log/slog"
pub macro_rules! transfer {($dst:expr,$src:expr)=>{copy($dst,$src)};}
pub macro_rules! twice {($dst:expr,$src:expr)=>{transfer!($dst,$src)+transfer!($dst,$src)};}
pub macro_rules! question {($dst:expr,$src:expr)=>{copy($dst?,$src?)};}
pub macro_rules! local {($dst:expr,$src:expr)=>{func()int{copy:=func([]int,[]int)int{return 17};return copy($dst,$src)}()};}
pub macro_rules! log_if {($enabled:expr,$dst:expr,$src:expr)=>{if $enabled {slog.Info("copy","count",copy($dst,$src))}};}
`

const macroCopyBindings = `package main
import m "copy.example/app/ops"
import copy "strings"
func Bindings()int{
 total:=m.transfer!([]byte{0,0},copy.Repeat("x",2))
 dst:=[]int{0}
 copy:=func(n int)[]int{return []int{n}}
 total+=m.transfer!(dst,copy(7))+dst[0]
 {const copy=2;total+=m.transfer!(dst,[]int{copy})+dst[0]}
 switch copy:=any(3).(type){case int:total+=m.transfer!(dst,[]int{copy})+dst[0]}
 {copy:=4;r:=Record{copy:copy};total+=m.transfer!(dst,[]int{r.copy})+dst[0];total+=m.transfer!(dst,[]int{map[int]int{copy:7}[copy]})+dst[0]}
 return total
}
func Parameter(copy []int)int{return m.transfer!([]int{0},copy)}
`

const macroCopyProgram = `package main
import "fmt"
import m "copy.example/app/ops"
import o "copy.example/app/own"
func Transfer(dst Items,src []int)int{return m.transfer!(dst,src)}
func Bytes(dst []byte,src string)int{return m.transfer!(dst,src)}
func Pointers(dst,src []*int)int{return m.transfer!(dst,src)}
func Order(dst,src func()[]int)int{return m.transfer!(dst(),src())}
func Repeated(dst,src func()[]int)int{return m.twice!(dst(),src())}
func Statement(dst,src []int){m.transfer!(dst,src)}
func DeferCopy(dst,src func()[]int,after func()){defer m.transfer!(dst(),src());after()}
func DefinitionLocal()int{return m.local!([]int{},[]int{})}
func DefinitionFunction()int{return o.Run()}
func Snapshot(r &State)int{dst:=make([]int,len(r.Items));n:=m.transfer!(dst,r.Items);dst[0]++;return n*10+dst[0]}
var Trace string
var Storage [3]int
func Destination(ok bool)Option[[]int]{Trace+="D";if ok{return Option[[]int]::Some(Storage[:])};return Option[[]int]::None}
func Source(ok bool)Option[[]int]{Trace+="S";if ok{return Option[[]int]::Some([]int{4,5})};return Option[[]int]::None}
func Question(dstOK,srcOK bool)Option[int]{n:=m.question!(Destination(dstOK),Source(srcOK));Trace+="A";return Option[int]::Some(n)}
func QuestionCount(dstOK,srcOK bool)int{return match Question(dstOK,srcOK){Option[int]::Some(n)=>n,Option[int]::None=>-1,}}
func NestedQuestion(dstOK,srcOK bool)int{
 inner:=func()Option[int]{n:=m.question!(Destination(dstOK),Source(srcOK));Trace+="A";return Option[int]::Some(n)}
 result:=inner();Trace+="O"
 return match result{Option[int]::Some(n)=>n,Option[int]::None=>-1,}
}
func Logging(enabled func()bool,dst,src func()[]int){m.log_if!(enabled(),dst(),src())}
func main(){dst:=Items{0,0,0};n:=m.transfer!(dst,[]int{4,5});data:=[]byte{0,0};b:=Bytes(data,"abc");fmt.Println(n,dst,b,string(data))}
`

const macroCopyAssertions = `package main
import (
 "bytes"
 "context"
 "log/slog"
 "reflect"
 "strings"
 "testing"
)
func TestNativeCopyCountStorageAndOverlap(t *testing.T){
 backing:=[5]int{0,0,0,9,9};dst:=Items(backing[:3]);original:=&dst[0]
 n:=Transfer(dst,[]int{4,5});if n!=2||!reflect.DeepEqual(dst,Items{4,5,0})||len(dst)!=3||cap(dst)!=5||&dst[0]!=original||reflect.TypeOf(dst).Name()!="Items"{t.Fatalf("copy count/storage: %d %v",n,dst)}
 n=Transfer(dst[:1],[]int{6,7,8});if n!=1||!reflect.DeepEqual(dst,Items{6,5,0}){t.Fatal("short destination",n,dst)}
 for _,right:=range []bool{false,true}{values:=Items{1,2,3,4};if right{n=Transfer(values[1:],values[:3])}else{n=Transfer(values[:3],values[1:])};want:=Items{2,3,4,4};if right{want=Items{1,1,2,3}};if n!=3||!reflect.DeepEqual(values,want){t.Fatal("overlap",right,n,values)}}
 var nilSlice Items
 if Transfer(nilSlice,[]int{1})!=0||Transfer(dst,nil)!=0||Transfer(Items{},[]int{})!=0||nilSlice!=nil{t.Fatal("nil/empty changed")}
 data:=make([]byte,2,4);ptr:=&data[0];if Bytes(data,"abc")!=2||string(data)!="ab"||cap(data)!=4||&data[0]!=ptr||Bytes(nil,"")!=0{t.Fatal("string source",data)}
 Statement(dst,[]int{8});if dst[0]!=8{t.Fatal("copy statement did not mutate destination")}
}
func TestPointerSharingAndReadonlyScalarSnapshot(t *testing.T){
 value:=7;source:=[]*int{&value};destination:=make([]*int,1)
 if Pointers(destination,source)!=1||destination[0]!=source[0]{t.Fatal("copy changed pointer identity")};*destination[0]=9;if value!=9||*source[0]!=9{t.Fatal("copy deep-copied pointer elements")}
 state:=State{Items:[]int{5,6}};if Snapshot(&state)!=26||!reflect.DeepEqual(state.Items,[]int{5,6}){t.Fatal("readonly source changed",state)}
}
func TestBindingsAndEvaluationMultiplicity(t *testing.T){
 if Bindings()!=30||Parameter([]int{1})!=1||DefinitionLocal()!=17||DefinitionFunction()!=41{t.Fatal("binding changed",Bindings(),Parameter([]int{1}),DefinitionLocal(),DefinitionFunction())}
 trace:="";destination:=[]int{0};source:=[]int{7}
 dst:=func()[]int{trace+="D";return destination};src:=func()[]int{trace+="S";return source}
 if n:=Order(dst,src);n!=1||trace!="DS"||destination[0]!=7{t.Fatal("operand order/count",n,trace,destination)}
 trace="";if n:=Repeated(dst,src);n!=2||trace!="DSDS"{t.Fatal("template multiplicity",n,trace)}
}
func TestQuestionOperandsAndNearestFrame(t *testing.T){
 for _,nested:=range []bool{false,true}{for _,dstOK:=range []bool{false,true}{for _,srcOK:=range []bool{false,true}{
  Trace="";Storage=[3]int{9,9,9};run:=QuestionCount;if nested{run=NestedQuestion};got:=run(dstOK,srcOK)
  wantTrace:="D";wantCount:=-1;wantStorage:=[3]int{9,9,9};if dstOK{wantTrace+="S";if srcOK{wantTrace+="A";wantCount=2;wantStorage=[3]int{4,5,9}}};if nested{wantTrace+="O"}
  if got!=wantCount||Trace!=wantTrace||Storage!=wantStorage{t.Fatalf("nested=%v dst=%v src=%v count=%d trace=%q storage=%v",nested,dstOK,srcOK,got,Trace,Storage)}
 }}}
}
func TestDeferEvaluatesOperandsNowAndCopiesLater(t *testing.T){
 originalDestination:=[]int{0};destination:=originalDestination;originalSource:=[]int{1};source:=originalSource;trace:=""
 DeferCopy(func()[]int{trace+="D";return destination},func()[]int{trace+="S";return source},func(){
  if trace!="DS"||originalDestination[0]!=0{t.Fatal("copy happened before function return",trace,originalDestination)}
  trace+="A";originalSource[0]=7;source=[]int{8};destination=[]int{9}
 })
 if trace!="DSA"||originalDestination[0]!=7||destination[0]!=9{t.Fatal("defer changed operand descriptor/content timing",trace,originalDestination,destination)}
}
func TestConditionalLogSkipsBothCopyOperands(t *testing.T){
 previous:=slog.Default();defer slog.SetDefault(previous)
 for _,level:=range []slog.Level{slog.LevelError,slog.LevelInfo}{
  var output bytes.Buffer;slog.SetDefault(slog.New(slog.NewJSONHandler(&output,&slog.HandlerOptions{Level:level})))
  trace:="";destination:=[]int{9}
  Logging(func()bool{trace+="E";return slog.Default().Enabled(context.Background(),slog.LevelInfo)},func()[]int{trace+="D";return destination},func()[]int{trace+="S";return []int{2}})
  if level==slog.LevelError{if trace!="E"||output.Len()!=0||destination[0]!=9{t.Fatal("disabled operands executed",trace,output.String(),destination)};continue}
  if trace!="EDS"||destination[0]!=2||!strings.Contains(output.String(),"\"count\":1"){t.Fatal("enabled copy order/result",trace,output.String(),destination)}
 }
}
`
