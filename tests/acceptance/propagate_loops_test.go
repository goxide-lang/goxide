package acceptance_test

import (
	"github.com/hxsf/goxide/internal/frontend"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPropagationLoopsRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	source, err := os.ReadFile(filepath.Join(filepath.Dir(here), "../../examples/prototype/loop_propagation.hgo"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := frontend.Compile("loops.hgo", source)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{".Catch(", ".Fail(", ".Assignment["} {
		if strings.Contains(string(result.Go), marker) {
			t.Fatalf("loop introduced guard %s", marker)
		}
	}
	enumGeneratedModule(t, string(source), loopAssertions, false)
}

const loopAssertions = `package main
import("testing";"reflect")
func TestForPhases(t *testing.T){for _,tc:=range []struct{fail,want int;trace []int;captures []int}{
 {0,2,[]int{10,100,300,201,101,301,202,102,302,203,103,900},[]int{0,1,2}},
 {1,-1,[]int{10,900},nil},
 {2,-1,[]int{10,100,300,201,101,900},[]int{0}},
 {3,-1,[]int{10,100,300,201,101,301,202,900},[]int{0,1}},
 }{trace:=[]int{};var captures []func()int;got:=loopValue(forPhases(tc.fail,&trace,&captures));var values []int;for _,f:=range captures{values=append(values,f())};if got!=tc.want||!reflect.DeepEqual(trace,tc.trace)||!reflect.DeepEqual(values,tc.captures){t.Fatal(tc.fail,got,trace,values)}}}
func TestForPostAndCondition(t *testing.T){trace:=[]int{};if n:=loopValue(postOnly(&trace));n!=3||!reflect.DeepEqual(trace,[]int{201,202,203}){t.Fatal(n,trace)};trace=nil;if n:=loopValue(conditionOnly(&trace));n!=3||!reflect.DeepEqual(trace,[]int{100,101,102,103}){t.Fatal(n,trace)};trace=nil;if n:=loopValue(unboundedPost(&trace));n!=-1||!reflect.DeepEqual(trace,[]int{201,202}){t.Fatal(n,trace)}}
func TestForLabelsAndReentry(t *testing.T){entries:=0;trace:=[]int{};if n:=loopValue(labeledFor(&entries,&trace));n!=2||entries!=2||!reflect.DeepEqual(trace,[]int{10,11,20,21}){t.Fatal(n,entries,trace)}}
func TestRangeSourcesAndCapture(t *testing.T){for _,ok:=range []bool{false,true}{calls:=0;var fs []func()int;got:=loopValue(sliceLoop(ok,&calls,&fs));want:=-1;if ok{want=12};if got!=want||calls!=1{t.Fatal(got,calls)};if ok{var values []int;for _,f:=range fs{values=append(values,f())};if !reflect.DeepEqual(values,[]int{2,14,26}){t.Fatal(values)}};calls=0;got=loopValue(mapLoop(ok,&calls));want=-1;if ok{want=11};if got!=want||calls!=1{t.Fatal(got,calls)}}}
func TestRangeAssignmentAndReentry(t *testing.T){calls:=0;var fs []func()int;if n:=loopValue(rangeAssignment(&calls,&fs));n!=6||calls!=1{t.Fatal(n,calls)};for _,f:=range fs{if f()!=6{t.Fatal("assignment range unexpectedly fresh variable")}};calls=0;trace:=[]int{};if n:=loopValue(labeledRange(&calls,&trace));n!=2||calls!=2||!reflect.DeepEqual(trace,[]int{12,14,22,24}){t.Fatal(n,calls,trace)}}
func TestLoopNearestFunctionAndNativeRange(t *testing.T){trace:=[]int{};if n:=loopValue(loopClosure(&trace));n!=8||!reflect.DeepEqual(trace,[]int{1,2}){t.Fatal(n,trace)};if n:=loopValue(nilAndNativeRange());n!=9{t.Fatal(n)}}
func TestForIterationAddressIdentity(t *testing.T){var saved,postSaved []*int;if n:=loopValue(postIdentity(&saved,&postSaved));n!=1||len(saved)!=3||len(postSaved)!=3{t.Fatal(n,saved,postSaved)};for i,p:=range saved{if *p!=i{t.Fatal("body capture changed",i,*p)};if i>0&&p!=postSaved[i-1]{t.Fatal("post must see next iteration variable",i)}};if *postSaved[2]!=3||postSaved[2]==saved[2]{t.Fatal("last post copy lost")}}
func TestLoopIteratorBodyAndBreak(t *testing.T){for _,fail:=range []bool{false,true}{trace:=[]int{};n:=loopValue(nativeIteratorBody(fail,&trace));want:=6;tr:=[]int{11,12,13,9};if fail{want=-1;tr=[]int{11,12,9}};if n!=want||!reflect.DeepEqual(trace,tr){t.Fatal(n,trace)}};trace:=[]int{};if n:=loopValue(initAssignment(&trace));n!=2||!reflect.DeepEqual(trace,[]int{10}){t.Fatal(n,trace)};trace=nil;if n:=loopValue(breakBeforePost(&trace));n!=1||!reflect.DeepEqual(trace,[]int{100}){t.Fatal(n,trace)}}
func TestLoopSingleUseLabels(t *testing.T){trace:=[]int{};if n:=loopValue(loopDefers(&trace));n!=-1||!reflect.DeepEqual(trace,[]int{1,0,9}){t.Fatal(n,trace)};if n:=loopValue(blankLoopLabels());n!=1{t.Fatal(n)};if n:=loopValue(onlyContinueLabel());n!=3{t.Fatal(n)};entries:=0;if n:=loopValue(onlyGotoLabel(&entries));n!=2||entries!=2{t.Fatal(n,entries)}}
func TestLoopBenchmarkEquivalence(t *testing.T){for _,fail:=range []int{-1,0,7,16}{a,b:=loopValue(propagatedLoop(16,fail)),loopValue(explicitLoop(16,fail));if a!=b{t.Fatal(fail,a,b)}}}

`

func TestPropagationLoopRejections(t *testing.T) {
	for name, body := range map[string]string{
		"scope":              `func f()Option[int]{for i:=Option[int]::Some(0)?;i<1;i++{};return Option[int]::Some(i)}`,
		"post-declaration":   `func f()Option[int]{for ;;i:=Option[int]::Some(0)?{_=i};return Option[int]::None}`,
		"integer-source":     `func f()Option[int]{for range Option[int]::Some(3)?{};return Option[int]::None}`,
		"array-source":       `func f()Option[int]{for range Option[[3]int]::Some([3]int{})?{};return Option[int]::None}`,
		"iterator-source":    `func f()Option[int]{for range Option[func(func(int)bool)]::None?{};return Option[int]::None}`,
		"range-target":       `func f()Option[int]{s:=[]int{0};for s[Option[int]::Some(0)?]=range []int{1}{};return Option[int]::None}`,
		"duplicate-label":    `func f()Option[int]{L:for Option[bool]::Some(true)?{break L};L:for Option[bool]::Some(true)?{break L};return Option[int]::None}`,
		"blank-label-target": `func f()Option[int]{_:for Option[bool]::Some(true)?{break _};return Option[int]::None}`,
		"unused-label":       `func f()Option[int]{L:for Option[bool]::Some(false)?{};return Option[int]::None}`,
		"goto-scope":         `func f()Option[int]{goto L;{L:for Option[bool]::Some(false)?{}};return Option[int]::None}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := frontend.Compile("loop_bad.hgo", []byte("package fixture;"+body))
			if err == nil || !strings.Contains(err.Error(), "loop_bad.hgo") {
				t.Fatalf("expected located rejection: %v", err)
			}
		})
	}
}

func TestPropagationLoopInstalledCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	bin := t.TempDir()
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "install", "./cmd/hgo")
	cli := filepath.Join(bin, "hgo")
	outside := t.TempDir()
	source := filepath.Join(root, "examples/prototype/loop_propagation.hgo")
	generated := filepath.Join(outside, "main.go")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source)
	want := "2 [10 100 300 201 101 301 202 102 302 203 103 900]\n"
	if got := deliveryCommand(t, root, tool, true, nil, "run", "-race", generated); got != want {
		t.Fatalf("generated output %q", got)
	}
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "-runtime", root, "-go", tool, source); got != want {
		t.Fatalf("CLI output %q", got)
	}
}
