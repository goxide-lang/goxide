package acceptance_test

import (
	"github.com/hxsf/goxide/internal/frontend"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPropagationTypeRangeRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	source, e := os.ReadFile(filepath.Join(filepath.Dir(here), "../../examples/prototype/type_range_propagation.gox"))
	if e != nil {
		t.Fatal(e)
	}
	r, e := frontend.Compile("type_range.gox", source)
	if e != nil {
		t.Fatal(e)
	}
	for _, marker := range []string{".Catch(", ".Fail(", ".Assignment["} {
		if strings.Contains(string(r.Go), marker) {
			t.Fatal("introduced guard", marker)
		}
	}
	enumGeneratedModule(t, string(source), typeRangeAssertions, false)
}

const typeRangeAssertions = `package main
import("testing";"reflect")
func TestTypeCaseBindings(t *testing.T){for _,tc:=range []struct{v any;want int}{{3,14},{"abc",13},{(*trThing)(nil),20},{&trThing{4},14},{true,30},{1.5,31},{nil,40},{[]int{1},50}}{trace:=[]int{};if n:=trValue(typeKinds(tc.v,true,true,&trace));n!=tc.want||!reflect.DeepEqual(trace,[]int{1,2,9}){t.Fatal(tc,n,trace)}};if trValue(typeNoBinding(1))!=1||trValue(typeNoBinding(nil))!=2{t.Fatal("unbound type guard")}}
func TestTypeFailureScopeAndLabels(t *testing.T){for _,tc:=range []struct{init,subject bool;trace []int}{{false,true,[]int{1,9}},{true,false,[]int{1,2,9}}}{trace:=[]int{};if n:=trValue(typeKinds(1,tc.init,tc.subject,&trace));n!=-1||!reflect.DeepEqual(trace,tc.trace){t.Fatal(n,trace)}};entries:=0;if n:=trValue(typeLabels(&entries));n!=2||entries!=2{t.Fatal(n,entries)};trace:=[]int{};if n:=trValue(typeClosure(&trace));n!=8||!reflect.DeepEqual(trace,[]int{1,2}){t.Fatal(n,trace)}}
func TestRangeString(t *testing.T){for _,ok:=range []bool{false,true}{calls:=0;var fs []func()int;n:=trValue(rangeString(ok,&calls,&fs));if calls!=1{t.Fatal(calls)};if !ok{if n!=-1||len(fs)!=0{t.Fatal(n,fs)};continue};if n!=20088||len(fs)!=2||fs[0]()!=97||fs[1]()!=19991{t.Fatal(n,len(fs))}}}
func TestRangeIntegers(t *testing.T){for _,limit:=range []int{-2,0,3}{var fs []func()int;n:=trValue(rangeInteger(limit,true,&fs));want:=0;if limit==3{want=3};if n!=want{t.Fatal(limit,n)};for i,f:=range fs{if f()!=i{t.Fatal(i,f())}}};var fs []func()int;if n:=trValue(rangeInteger(3,false,&fs));n!=-1||len(fs)!=0{t.Fatal(n,fs)};if n:=trValue(rangeIntegerContext(63));n!=1{t.Fatal("uint64 context lost",n)}}
func TestRangeChannels(t *testing.T){ready:=make(chan bool,1);done:=make(chan int,1);ch:=make(chan int);go func(){done<-trValue(rangeChannel(ch,true,ready))}();<-ready;ch<-2;ch<-5;close(ch);if n:=<-done;n!=7{t.Fatal(n)};if n:=trValue(rangeChannel(nil,false,ready));n!=-1{t.Fatal(n)};<-ready;closed:=make(chan int);close(closed);if n:=trValue(rangeChannel(closed,true,ready));n!=0{t.Fatal(n)};<-ready;buffered:=make(chan int,2);buffered<-4;buffered<-9;if n:=trValue(rangeChannelBreak(buffered));n!=4||len(buffered)!=1{t.Fatal(n,len(buffered))}}
func TestRangeArrayValueAndPointer(t *testing.T){for _,pointer:=range []bool{false,true}{a:=[3]int{1,2,3};trace:=[]int{};n:=trValue(rangeArray(&a,true,pointer,&trace));want:=6;if pointer{want=103};if n!=want||!reflect.DeepEqual(trace,[]int{0,1,2}){t.Fatal(pointer,n,trace)}};trace:=[]int{};if n:=trValue(rangeArray(nil,false,false,&trace));n!=-1||len(trace)!=0{t.Fatal(n,trace)}}
func TestRangeArrayEvaluation(t *testing.T){if n:=trValue(rangeArrayIndex(nil,true,false));n!=3{t.Fatal(n)};if n:=trValue(rangeArrayIndex(nil,false,true));n!=-1{t.Fatal(n)};func(){defer func(){if recover()==nil{t.Error("required full array dereference was skipped")}}();_=rangeArrayIndex(nil,true,true)}();if n:=trValue(rangeNativeUnevaluated());n!=3{t.Fatal(n)}}
func TestTypeRangeReviewBoundaries(t *testing.T){func(){defer func(){if recover()==nil{t.Error("zero array nil dereference skipped")}}();_=rangeZeroArray(__goxide_enum_ctor_Option_Some[*[0]int](nil))}();if n:=trValue(rangeBlankArray(__goxide_enum_ctor_Option_Some[*[2]int](nil)));n!=1{t.Fatal(n)};if n:=trValue(typeCapturedInit());n!=14{t.Fatal(n)};if n:=trValue(rangeArrayPayload());n!=6{t.Fatal(n)};trace:=[]int{};func(){defer func(){if recover()==nil{t.Error("two-value nil pointer range did not dereference")}}();_=rangeArray(nil,true,true,&trace)}();if len(trace)!=0{t.Fatal(trace)}}

`

func TestPropagationTypeRangeRejections(t *testing.T) {
	for name, body := range map[string]string{
		"case-variable-scope":   `func f()Option[int]{switch x:=Option[any]::Some(1)?.(type){case int:_=x};return Option[int]::Some(x)}`,
		"multi-case-interface":  `func f()Option[int]{switch x:=Option[any]::Some(1)?.(type){case int,string:return Option[int]::Some(x+1)};return Option[int]::None}`,
		"non-interface-subject": `func f()Option[int]{switch Option[int]::Some(1)?.(type){default:};return Option[int]::None}`,
		"duplicate-type":        `func f()Option[int]{switch Option[any]::Some(1)?.(type){case int:case int:};return Option[int]::None}`,
		"type-fallthrough":      `func f()Option[int]{switch Option[any]::Some(1)?.(type){case int:fallthrough;default:};return Option[int]::None}`,
		"type-continue":         `func f()Option[int]{L:switch Option[any]::Some(1)?.(type){default:continue L};return Option[int]::None}`,
		"integer-wrong-target":  `func f()Option[int]{var i int;for i=range Option[uint64]::Some(3)?{};return Option[int]::None}`,
		"channel-sendonly":      `func f()Option[int]{for range Option[chan<- int]::None?{};return Option[int]::None}`,
		"integer-two-vars":      `func f()Option[int]{for i,v:=range Option[int]::Some(3)?{_,_=i,v};return Option[int]::None}`,
		"iterator-source":       `func f()Option[int]{for range Option[func(func(int)bool)]::None?{};return Option[int]::None}`,
		"unevaluated-question":  `func f()Option[int]{for range [1]uintptr{unsafe.Sizeof(Option[int]::None?)}{};return Option[int]::None}`,
	} {
		t.Run(name, func(t *testing.T) {
			imports := ""
			if name == "unevaluated-question" {
				imports = `import "unsafe";`
			}
			_, e := frontend.Compile("type_range_bad.gox", []byte("package fixture;"+imports+body))
			if e == nil || !strings.Contains(e.Error(), "type_range_bad.gox") {
				t.Fatalf("expected located rejection: %v", e)
			}
		})
	}
}

func TestPropagationTypeRangeInstalledCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	bin := t.TempDir()
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "install", "./cmd/goxide")
	cli := filepath.Join(bin, "goxide")
	outside := t.TempDir()
	source := filepath.Join(root, "examples/prototype/type_range_propagation.gox")
	generated := filepath.Join(outside, "main.go")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source)
	want := "14 [1 2 9]\n"
	if got := deliveryCommand(t, root, tool, true, nil, "run", "-race", generated); got != want {
		t.Fatalf("generated output %q", got)
	}
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--runtime", root, "--go", tool, source); got != want {
		t.Fatalf("CLI output %q", got)
	}
}
