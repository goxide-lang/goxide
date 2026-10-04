package macros

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/enums"
)

func TestBuiltinSumTemplateDefinitionBindings(t *testing.T) {
	source := `package main
import "fmt"
macro_rules! some {($v:expr)=>{func()Option[int]{return Option[int]::Some($v)}()};}
macro_rules! none {()=>{Option[int]::None};}
macro_rules! okay {($v:expr)=>{func()Result[int,error]{return Result[int,error]::Ok($v)}()};}
func unwrap(v Option[int])int{match v {Option[int]::Some(n)=>{return n},Option[int]::None=>{return -1},};panic("bad")}
func result(v Result[int,error])int{match v {Result[int,error]::Ok(n)=>{return n},Result[int,error]::Err(_)=>{return -2},};panic("bad")}
func main(){Option,Result,int,error:=1,2,3,4;_,_,_,_=Option,Result,int,error;fmt.Println(unwrap(some!(7)),unwrap(none!()),result(okay!(9)))}
`
	expanded, err := Expand("sum.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(expanded), "__goxide_enum_") {
		t.Fatal("macro pass leaked reserved enum names")
	}
	lower, err := enums.Lower("sum.gox", expanded)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "main.go")
	if err = os.WriteFile(p, lower.Source, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", p).CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s\n%s", err, out, lower.Source)
	}
	if string(out) != "7 -1 9\n" {
		t.Fatalf("wrong bindings: %s", out)
	}
}
func TestSumHelperDoesNotMovePropagation(t *testing.T) {
	r, err := ExpandWithIntrinsics("sum.gox", []byte(`package p
macro_rules! wrap {($v:expr)=>{func()Result[int,error]{return Result[int,error]::Ok(__goxide_question($v))}()};}
func source()Result[int,error]{return Result[int,error]::Ok(1)}
func caller(){_=wrap!(source())}
`), []string{"__goxide_question"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(r.Source)
	start := strings.Index(s, "func caller")
	if start < 0 || strings.Count(s, "__goxide_question(") != 1 {
		t.Fatalf("lost or duplicated marker: %s", s)
	}
	marker := strings.Index(s, "__goxide_question(")
	helper := strings.Index(s[start:], "\nfunc goxideMacroGenerated") + start
	if helper < start || marker > helper {
		t.Fatalf("propagation moved into helper: %s", s)
	}
}
func TestUnsupportedSumTemplateTypes(t *testing.T) {
	for _, body := range []string{
		`func()Result[T,error]{return Result[T,error]::Ok(1)}()`,
		`func()Option[$t]{return Option[$t]::None}()`,
		`Option[int]::Missing`, `Option[int]::None()`, `Result[int]::Ok(1)`, `E::Ready`,
	} {
		src := "package p\nmacro_rules! m {($t:ident)=>{" + body + "};}\nfunc f(){_=m!(int)}"
		if _, e := Expand("bad_sum.gox", []byte(src)); e == nil || !strings.Contains(e.Error(), "bad_sum.gox") {
			t.Fatalf("expected located error for %s: %v", body, e)
		}
	}
}
