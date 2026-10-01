package frontend

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPropagationAliasIdentity(t *testing.T) {
	for name, source := range map[string]string{
		"return-alias":  `type Out = Result[int,error];func f() Out {n:=Result[int,error]::Ok(1)?;return Result[int,error]::Ok(n)}`,
		"payload-alias": `type Number = int;func source()Result[Number,error]{return Result[Number,error]::Ok(1)};func f()Result[int,error]{n:=source()?;return Result[int,error]::Ok(n)}`,
		"import-alias":  `import tm "time";func f()Result[tm.Duration,error]{n:=Result[tm.Duration,error]::Ok(1)?;return Result[tm.Duration,error]::Ok(n)}`,
		"from-alias": `type WrappedAlias = Wrapped
#[derive(Error)]
type Wrapped struct {
#[from]
Cause error
}
func f()Result[int,WrappedAlias]{n:=Result[int,error]::Err(nil)?;return Result[int,WrappedAlias]::Ok(n)}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Compile("alias.hgo", []byte("package p\n"+source)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompleteSumAndMatchTypecheck(t *testing.T) {
	for name, source := range map[string]string{
		"builtin-payload": `func f()Option[int]{return Option[int]::Some("bad")}`,
		"match-arm":       `enum Value {N(int),};func f(v Value)int{return match v {Value::N(_) => "bad",}}`,
		"unrelated-error": `func f()Option[int]{n:=Option[int]::Some(1)?;return Option[int]::Some(n)};func bad()int{return "bad"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile("invalid.hgo", []byte("package p\n"+source))
			if err == nil || !strings.Contains(err.Error(), "invalid.hgo") {
				t.Fatalf("need located final type error: %v", err)
			}
		})
	}
}

func TestPropagationMatchCompositionRace(t *testing.T) {
	src := `package main
var calls int
#[derive(Error)]
type AppError struct {
#[from]
Cause error
}
func converted()Result[int,AppError]{
 FromAppError:=func(error)AppError{panic("captured local conversion")};_=FromAppError
 _=Result[int,error]::Err(nil)?
 return Result[int,AppError]::Ok(0)
}
func conversionOK(v Result[int,AppError])bool{return match v {Result[int,AppError]::Err(e) => e.Cause==nil,Result[int,AppError]::Ok(_) => false,}}
func load(fail bool)Result[int,string]{calls++;if fail{return Result[int,string]::Err("bad")};return Result[int,string]::Ok(7)}
func compute(v Option[int],fail bool)Result[int,string]{
 var n int = match v {Option[int]::Some(_) => load(fail)?,Option[int]::None => 0,}
 return Result[int,string]::Ok(n)
}
func answer(v Result[int,string])int{return match v {Result[int,string]::Ok(n) => n,Result[int,string]::Err(_) => -1,}}
func main(){if !conversionOK(converted()){panic("wrong conversion")};if answer(compute(Option[int]::Some(1),false))!=7||calls!=1{panic("success")};if answer(compute(Option[int]::Some(1),true))!=-1||calls!=2{panic("failure")};if answer(compute(Option[int]::None,true))!=0||calls!=2{panic("unselected arm evaluated")}}
`
	r, err := Compile("composition.hgo", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "main.go"), r.Go, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module composition\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "-race", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, r.Go)
	}
}
