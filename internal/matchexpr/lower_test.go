package matchexpr
import (
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
 "github.com/hxsf/goxide/internal/enums"
)
func TestGeneratedRace(t *testing.T) {
 s:=`package main
 import "fmt"
 enum E { Idle, Item(int), }
 var calls int
 func next() E { calls++; return E::Item(7) }
 func result() int {
 defer func(){calls+=10}()
 return match next() { E::Item(v) if v<0 => -v, E::Item(v) => v+1, E::Idle => 0 }
 }
 func main() {
 x:=40
 { var x int = match E::Idle { E::Idle => x+2, E::Item(v) => v }; if x!=42 {panic(x)} }
 if result()!=8||calls!=11 {panic(calls)}
 f:=func() string {return match E::Idle { E::Idle => "ok", E::Item(_) => "bad" }}
 fmt.Print(f())
 }
 `
 out,e:=Lower("sample.hgo",[]byte(s));if e!=nil {t.Fatal(e)};r,e:=enums.Lower("sample.hgo",out);if e!=nil {t.Fatal(e)}
 dir:=t.TempDir();os.WriteFile(filepath.Join(dir,"main.go"),r.Source,0600);os.WriteFile(filepath.Join(dir,"go.mod"),[]byte("module check\ngo 1.27\n"),0600)
 tool, e:=exec.LookPath("go");if e!=nil {t.Fatal(e)};cmd:=exec.Command(tool,"run","-race",".");cmd.Dir=dir;cmd.Env=append(os.Environ(),"GOTOOLCHAIN=local","GOWORK=off","GOPROXY=off");b,e:=cmd.CombinedOutput();if e!=nil {t.Fatalf("%v\n%s\n%s",e,b,r.Source)};if string(b)!="ok" {t.Fatal(string(b))}
}
func TestReject(t *testing.T) {
 for _,body:=range []string{
 `x:=match e { E::Idle => 1 }; _=x`,
 `var x = match e { E::Idle => 1 }; _=x`,
 `var x int = match e { E::Idle => { 1 } }; _=x`,
 `var x int = match e { E::Idle => 1 } + 2; _=x`,
 `consume(match e { E::Idle => 1 })`,
 } {t.Run(body,func(t *testing.T){if _,e:=Lower("bad.hgo",[]byte("package p\nfunc f(){"+body+"}"));e==nil {t.Fatal("accepted")}})}
 if _,e:=Lower("bad.hgo",[]byte("package p\nfunc f()(x int){return match e { E::Idle => 1 }}"));e==nil {t.Fatal("named result accepted")}
}
func TestCoverageDelegated(t *testing.T) {
 out,e:=Lower("bad.hgo",[]byte("package p\nenum E {Idle, Item(int)}\nfunc f(e E) int {return match e { E::Idle => 1 }}"));if e!=nil {t.Fatal(e)};if _,e=enums.Lower("bad.hgo",out);e==nil||!strings.Contains(e.Error(),"non-exhaustive") {t.Fatal(e)}
}
func TestPreserveRawCRLF(t *testing.T) {
 src:=[]byte("package p\r\nvar text = `a\r\nb`\r\n// match nope\r\n")
 out,e:=Lower("raw.hgo",src);if e!=nil||string(out)!=string(src) {t.Fatalf("%q %v",out,e)}
}
