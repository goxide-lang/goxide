package propagate

import (
	"github.com/hxsf/goxide/internal/testdeps"
	"go/ast"
	"go/token"
	"os/exec"
	"path/filepath"
	"testing"
)

// This is a generated-Go control-flow test, not an additional hgo feature
// acceptance. The same plan is emitted with two exits to check evaluation and
// isolation: validation/conversion panics must never reach the exit observer.
func TestQuestionFlowExitOrderingRace(t *testing.T) {
	q := &questionFlow{
		setup: parseStatements(`input := operand(); validate(input); var payload int`),
		value: parseExpr("payload"), failure: parseExpr("converted(input)"),
		resultSubject: parseStatements(`switch failure := input.(type) {}`)[0].(*ast.TypeSwitchStmt).Assign,
		resultOK:      parseExpr("good"), resultErr: parseExpr("bad"),
		success: parseStatements(`payload = int(failure)`),
	}
	option := &questionFlow{
		setup: parseStatements(`input := optionOperand(); var payload int`),
		value: parseExpr("payload"), failure: parseExpr("none()"),
		optionCondition: parseExpr("input == nil"), success: parseStatements(`payload = *input`),
	}
	observed := func(value ast.Expr) ast.Stmt {
		return &ast.BlockStmt{List: []ast.Stmt{
			// Bind the failure first, just as a call's argument is evaluated before
			// entering guard.Fail. An exception here must not set the observer flag.
			&ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("result")}, Tok: token.DEFINE, Rhs: []ast.Expr{value}},
			parseStatements(`trace += "E"`)[0], returnFailure(ast.NewIdent("result")),
		}}
	}
	source := `package main
import "testing"
type good int
type bad int
var mode int
var trace string
func operand() any { trace+="O"; return func() any {if mode==0{return good(7)};return bad(1)}() }
func validate(any) {trace+="V";if mode==2{panic("validator")}}
func converted(any) int {trace+="C";if mode==3{panic("conversion")};return -1}
func optionOperand()*int{trace+="O";if mode==0{n:=7;return &n};return nil}
func none()int{trace+="N";return -1}
func observe(f func()int)(n int,p any){defer func(){p=recover()}();n=f();return}
`
	for _, family := range []struct {
		name string
		flow *questionFlow
	}{{"Result", q}, {"Option", option}} {
		for _, exit := range []struct {
			name string
			fn   failureExit
		}{{"Return", returnFailure}, {"Observed", observed}, {"ReturnAgain", returnFailure}} {
			body := family.flow.emit(exit.fn)
			body = append(body, returnFailure(family.flow.value))
			source += "func " + family.name + exit.name + "()int" + printNode(&ast.BlockStmt{List: body}) + "\n"
		}
	}
	source += `func TestResult(t *testing.T){for _,f:=range []struct{name string;call func()int;mark string}{{"return",ResultReturn,""},{"observed",ResultObserved,"E"},{"return-again",ResultReturnAgain,""}}{for m:=0;m<4;m++{mode=m;trace="";n,p:=observe(f.call);want:="OV";if m==0{if n!=7||p!=nil{t.Fatal(f.name,m,n,p)}};if m==1{want+="C"+f.mark;if n!=-1||p!=nil{t.Fatal(f.name,m,n,p)}};if m==2&&p!="validator"{t.Fatal(f.name,m,p)};if m==3{want+="C";if p!="conversion"{t.Fatal(f.name,m,p)}};if trace!=want{t.Fatal(f.name,m,trace,want)}}}}
func TestOption(t *testing.T){for _,f:=range []struct{call func()int;mark string}{{OptionReturn,""},{OptionObserved,"E"},{OptionReturnAgain,""}}{for m:=0;m<2;m++{mode=m;trace="";n:=f.call();want:="O";if m==0&&n!=7{t.Fatal(n)};if m==1{want+="N"+f.mark;if n!=-1{t.Fatal(n)}};if trace!=want{t.Fatal(trace,want)}}}}
`
	dir := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(dir, "flow_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module flow.example/probe\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-race", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, source)
	}
}
