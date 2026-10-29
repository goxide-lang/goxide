package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPropagationSwitchRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	source, e := os.ReadFile(filepath.Join(filepath.Dir(here), "../../examples/prototype/switch_propagation.gox"))
	if e != nil {
		t.Fatal(e)
	}
	r, e := compileAcceptanceLanguageFixture("switch.gox", source)
	if e != nil {
		t.Fatal(e)
	}
	for _, marker := range []string{".Catch(", ".Fail(", ".Assignment["} {
		if strings.Contains(string(r.Go), marker) {
			t.Fatal("switch introduced guard", marker)
		}
	}
	enumGeneratedModule(t, string(source), switchAssertions, false)
}

const switchAssertions = `package main
import("testing";"reflect")
func TestSwitchOrder(t *testing.T){for _,tc:=range []struct{tag,fail,want int;trace []int}{
 {1,4,8,[]int{1,1,10,99}}, {2,4,8,[]int{2,1,2,10,99}}, {3,0,8,[]int{3,1,2,3,20,99}}, {4,0,8,[]int{4,1,2,3,30,99}},
 {2,1,-1,[]int{2,99}}, {2,2,-1,[]int{2,1,99}}, {2,3,-1,[]int{2,1,2,99}}, {4,4,-1,[]int{4,1,2,3,99}},
 }{trace:=[]int{};if n:=swValue(switchOrder(tc.tag,tc.fail,&trace));n!=tc.want||!reflect.DeepEqual(trace,tc.trace){t.Fatal(tc,n,trace)}}}
func TestSwitchFallthrough(t *testing.T){trace:=[]int{};if n:=swValue(switchFallthrough(1,&trace));n!=8||!reflect.DeepEqual(trace,[]int{1,10,20,30}){t.Fatal(n,trace)};trace=nil;if n:=swValue(switchFallthrough(4,&trace));n!=-1||!reflect.DeepEqual(trace,[]int{1,3}){t.Fatal(n,trace)}}
func TestSwitchTaglessAndScope(t *testing.T){if n:=swValue(switchNamedBool());n!=2{t.Fatal(n)};trace:=[]int{};if n:=swValue(switchTagless(&trace));n!=1||!reflect.DeepEqual(trace,[]int{1}){t.Fatal(n,trace)};trace=nil;if n:=swValue(switchScope(&trace));n!=99||!reflect.DeepEqual(trace,[]int{2,2}){t.Fatal(n,trace)}}
func TestSwitchLabels(t *testing.T){entries:=0;trace:=[]int{};if n:=swValue(switchLabels(&entries,&trace));n!=2||entries!=2||!reflect.DeepEqual(trace,[]int{1,1}){t.Fatal(n,entries,trace)};if n:=swValue(switchLoop());n!=2{t.Fatal(n)}}
func TestSwitchInterface(t *testing.T){for _,tc:=range []struct{tag,value any;want int}{{nil,nil,1},{nil,1,2},{1,2,3},{1,1,1}}{trace:=[]int{};if n:=swValue(switchAny(tc.tag,tc.value,&trace));n!=1||!reflect.DeepEqual(trace,[]int{tc.want}){t.Fatal(tc,n,trace)}};trace:=[]int{};func(){defer func(){if recover()==nil{t.Error("native comparison panic lost")}}();_=switchAny([]int{1},[]int{1},&trace)}();if len(trace)!=0{t.Fatal(trace)}}
func TestSwitchNearestFunctionAndBody(t *testing.T){trace:=[]int{};if n:=swValue(switchClosure(&trace));n!=8||!reflect.DeepEqual(trace,[]int{1,2}){t.Fatal(n,trace)};trace=nil;if n:=swValue(switchBody(&trace));n!=-1||!reflect.DeepEqual(trace,[]int{7}){t.Fatal(n,trace)};if n:=swValue(switchTerminating());n!=7{t.Fatal(n)}}
`

func TestPropagationSwitchRejections(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate-case":    `func f()Option[int]{switch Option[int]::Some(1)?{case 1:case 1:case Option[int]::Some(2)?:};return Option[int]::None}`,
		"duplicate-default": `func f()Option[int]{switch Option[int]::Some(1)?{default:default:};return Option[int]::None}`,
		"wrong-type":        `func f()Option[int]{switch 1{case Option[string]::Some("x")?:};return Option[int]::None}`,
		"incomparable":      `func f()Option[int]{switch []int{}{case Option[[]int]::Some([]int{})?:};return Option[int]::None}`,
		"fallthrough-last":  `func f()Option[int]{switch Option[int]::Some(1)?{case Option[int]::Some(1)?:fallthrough};return Option[int]::None}`,
		"continue-switch":   `func f()Option[int]{S:switch Option[int]::Some(1)?{case Option[int]::Some(1)?:continue S};return Option[int]::None}`,
		"scope":             `func f()Option[int]{switch n:=Option[int]::Some(1)?;n{};return Option[int]::Some(n)}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, e := compileAcceptanceLanguageFixture("switch_bad.gox", []byte("package fixture;"+body))
			if e == nil || !strings.Contains(e.Error(), "switch_bad.gox") {
				t.Fatalf("expected located rejection: %v", e)
			}
		})
	}
}

func TestPropagationSwitchInstalledCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := acceptanceCLI(t)
	outside := t.TempDir()
	source := filepath.Join(root, "examples/prototype/switch_propagation.gox")
	generated := filepath.Join(outside, "main.go")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "--roots", filepath.Join(root, "examples/prototype/goxide.roots.toml"), "-o", generated, source)
	want := "8 [2 1 2 10 99]\n"
	if got := deliveryCommand(t, root, tool, true, nil, "run", "-race", generated); got != want {
		t.Fatalf("generated output %q", got)
	}
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--roots", filepath.Join(root, "examples/prototype/goxide.roots.toml"), "--runtime", root, "--go", tool, source); got != want {
		t.Fatalf("CLI output %q", got)
	}
}
