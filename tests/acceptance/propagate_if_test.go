package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPropagationIfInitializerRace(t *testing.T) {
	enumGeneratedModule(t, propagationSource+`
func discardedInit(ok bool)Option[int]{if optional(ok)?;true{return Option[int]::Some(2)};return Option[int]::None}
func discardedStatement(ok bool)Option[int]{optional(ok)?;return Option[int]::Some(2)}
func initialized(initOK,condOK,choose bool,trace *[]string)Result[string,error]{
 defer func(){*trace=append(*trace,"cleanup")}()
 n:=90
 if n:=load(initOK,trace)?; choose && truth(condOK,trace)? {
  *trace=append(*trace,"then")
  defer func(){*trace=append(*trace,"then-defer")}()
  if n!=4{panic("inner scope")}
 } else if m:=mark("else-init",trace); m==1 {
  if n!=4{panic("else scope")};*trace=append(*trace,"else")
 }
 if n!=90{panic("leaked scope")}
 n=91
 return Result[string,error]::Ok("done")
}
func conditionOnly(trace *[]string)Result[string,error]{
 if n:=mark("init",trace); truth(true,trace)? && n==1{return Result[string,error]::Ok("yes")}
 return Result[string,error]::Ok("no")
}
func elseChain(first bool,trace *[]string)Option[int]{
 if first{return Option[int]::Some(9)} else if n:=optional(true)?;len(n)>0{return Option[int]::Some(len(n))}
 return Option[int]::None
}
func optionInit(ok bool)Option[int]{if s:=optional(ok)?;len(s)>0{return Option[int]::Some(len(s))};return Option[int]::Some(0)}
func assignmentInit(trace *[]string)Result[string,error]{n:=0;if n=load(true,trace)?;n!=4{panic("assignment")};if n!=4{panic("lost assignment")};return Result[string,error]::Ok("done")}
func nestedClosure(trace *[]string)Result[string,error]{f:=func()Option[int]{if s:=optional(false)?;len(s)>0{panic("bad")};return Option[int]::Some(1)};_=f();*trace=append(*trace,"outer");return Result[string,error]::Ok("done")}
`, `package fixture
import("reflect";"testing")
func TestInitializerOrderScopeAndCleanup(t *testing.T){
 for _,tc:=range []struct{init,cond,choose bool;text string;trace []string}{
 {false,true,true,"failed",[]string{"load","cleanup"}},
 {true,false,true,"failed",[]string{"load","load","cleanup"}},
 {true,true,true,"done",[]string{"load","load","then","then-defer","cleanup"}},
 {true,false,false,"done",[]string{"load","else-init","else","cleanup"}},
 }{trace:=[]string{};got:=resultText(initialized(tc.init,tc.cond,tc.choose,&trace));if got!=tc.text||!reflect.DeepEqual(trace,tc.trace){t.Fatalf("got %s %v want %s %v",got,trace,tc.text,tc.trace)}}
}
func TestConditionAfterOrdinaryInitializer(t *testing.T){trace:=[]string{};if resultText(conditionOnly(&trace))!="yes"||!reflect.DeepEqual(trace,[]string{"init","load"}){t.Fatal(trace)}}
func TestOptionAndElseIf(t *testing.T){if optionInt(discardedInit(true))!=2||optionInt(discardedInit(false))!=-1||optionInt(discardedStatement(true))!=2||optionInt(discardedStatement(false))!=-1{t.Fatal("discarded payload")};trace:=[]string{};if optionInt(elseChain(true,&trace))!=9||optionInt(elseChain(false,&trace))!=3||optionInt(optionInit(false))!=-1||optionInt(optionInit(true))!=3{t.Fatal("Option/else chain")}}
func TestAssignmentAndClosure(t *testing.T){trace:=[]string{};if resultText(assignmentInit(&trace))!="done"||resultText(nestedClosure(&trace))!="done"||!reflect.DeepEqual(trace,[]string{"load","outer"}){t.Fatal(trace)}}
`, false)
}

func TestPropagationIfInitializerRejections(t *testing.T) {
	for name, body := range map[string]string{
		"scope":  `func f()Option[int]{if n:=Option[int]::Some(1)?;n>0{};return Option[int]::Some(n)}`,
		"family": `func f()Option[int]{if n:=Result[int,error]::Ok(1)?;n>0{};return Option[int]::None}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceLanguageFixture("if_bad.gox", []byte("package fixture\n"+body))
			if err == nil || !strings.Contains(err.Error(), "if_bad.gox") {
				t.Fatalf("expected located rejection: %v", err)
			}
		})
	}
}

func TestPropagationIfInstalledCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin := t.TempDir()
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "install", "./cmd/goxide")
	cli := filepath.Join(bin, "goxide")
	outside := t.TempDir()
	source := filepath.Join(root, "examples/prototype/if_propagation.gox")
	generated := filepath.Join(outside, "main.go")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "--roots", filepath.Join(root, "examples/prototype/goxide.roots.toml"), "-o", generated, source)
	want := "port 8080\ndisabled\ninvalid port\n"
	if got := deliveryCommand(t, root, tool, true, nil, "run", "-race", generated); got != want {
		t.Fatalf("generated output: %q", got)
	}
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--roots", filepath.Join(root, "examples/prototype/goxide.roots.toml"), "--runtime", root, "--go", tool, source); got != want {
		t.Fatalf("CLI output: %q", got)
	}
}
