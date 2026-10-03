package macros

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func run(t *testing.T, src, want string) {
	t.Helper()
	r, e := ExpandWithMap("input.hgo", []byte(src))
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Origins) == 0 {
		t.Fatal("missing origins")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "main.go")
	if e = os.WriteFile(p, r.Source, 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", p)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("run: %s: %s\n%s", e, out, r.Source)
	}
	if string(out) != want {
		t.Fatalf("got %q want %q\n%s", out, want, r.Source)
	}
}
func TestHygieneEvaluationAndHelperShadow(t *testing.T) {
	run(t, `package main
import "fmt"
func twice(v int) int {return v*2}
macro_rules! calculate {
 ($x:expr) => {func() int {
  saved := 1
  value := $x
  return twice(value)+saved
 }()};
}
func main(){
 saved:=41
 twice:=func(int)int{return -1000};_ = twice
 calls:=0
 next:=func()int{calls++;return 1}
 first:=calculate!(saved+next())
 second:=calculate!(1)
 fmt.Println(first,second,calls,saved)
}
`, "85 3 1 41\n")
}
func TestRepetitionNestedInvocation(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! double { ($x:expr) => { ($x)+($x) }; }
macro_rules! values { ($($x:expr),*) => { []int{ $($x),* } }; }
macro_rules! wrap { ($x:expr) => { double!($x) }; }
func main(){ fmt.Println(values!(1,wrap!(2),3), values!()) }
`, "[1 4 3] []\n")
}
func TestLogAndReturnDefer(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! event { ($msg:expr, $($field:expr),*) => {fmt.Println($msg,$($field),*)}; }
macro_rules! leave { ($x:expr) => {return $x}; }
macro_rules! cleanup { ($f:ident) => {defer $f()}; }
func work()int { done:=func(){fmt.Println("done")}; cleanup!(done);event!("request", "id", 7);leave!(9) }
func main(){fmt:=0;_ = fmt; n:=work();event!("result", n)}
`, "request id 7\ndone\nresult 9\n")
}
func TestTemplateRepetitionEvaluationCount(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! duplicate {($x:expr)=>{($x)+($x)};}
func main(){n:=0;next:=func()int{n++;return n};v:=duplicate!(next());fmt.Println(v,n)}
`, "3 2\n")
}
func TestIdentAndTT(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! call {($f:ident, $t:tt)=>{$f$t};}
func main(){print:=func(x int){fmt.Println(x)};call!(print,(7))}
`, "7\n")
}
func TestErrors(t *testing.T) {
	cases := []struct{ src, needle string }{
		{`macro_rules! x {($a:expr $b:expr)=>{$a};}`, "ambiguous"},
		{`macro_rules! x {($a:unknown)=>{$a};}`, "unsupported fragment"},
		{`macro_rules! x {($a:ident)=>{$a};} func f(){x!(1)}`, "no rule matches"},
		{`macro_rules! x {($a:expr)=>{secret($a)};} func f(){x!(1)}`, "unsupported free template name"},
		{`macro_rules! x {($($a:expr),*)=>{$a};} func f(){x!(1,2)}`, "outside repetition"},
		{`macro_rules! x {()=>{x!()};} func f(){x!()}`, "depth budget"},
		{`macro_rules! x {()=>{1};} func f(){other!()}`, "unknown macro"},
		{`macro_rules! x {($a:expr,$a:expr)=>{1};}`, "duplicate capture"},
		{`macro_rules! x {($($a:expr),+)=>{[]int{$($a),+}};} func f(){x!()}`, "no rule matches"},
		{`macro_rules! x {()=>{append()};} func f(){x!()}`, "unsupported free template name"},
	}
	for _, c := range cases {
		t.Run(c.needle, func(t *testing.T) {
			_, e := Expand("bad.hgo", []byte("package p\n"+c.src))
			if e == nil || !strings.Contains(e.Error(), c.needle) || !strings.Contains(e.Error(), "bad.hgo") {
				t.Fatalf("got %v want %s", e, c.needle)
			}
		})
	}
}
func TestPreserveAttributeAndOrigins(t *testing.T) {
	src := `package p
macro_rules! identity {($x:expr)=>{$x};}
#[derive(Serialize)]
type Item struct { Value int }
func f()int{return identity!(7)}
`
	r, e := ExpandWithMap("shape.hgo", []byte(src))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(r.Source), "#[derive(Serialize)]\ntype Item") {
		t.Fatalf("attribute changed: %s", r.Source)
	}
	found := false
	for _, o := range r.Origins {
		if o.Line == 5 && string(r.Source[o.OutputStart:o.OutputEnd]) == "7" {
			found = true
		}
	}
	if !found {
		t.Fatal("capture origin missing")
	}
}

func TestExpressionPrecedence(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! scale {($x:expr)=>{$x*2};}
func main(){fmt.Println(scale!(1+2))}
`, "6\n")
}
func TestNestedLocalScopes(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! scoped {($x:expr)=>{func()int{value:=$x;{value:=9;_ = value};return value}()};}
func main(){value:=7;fmt.Println(scoped!(value),value)}
`, "7 7\n")
}
func TestCapturedBinder(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! bind {($name:ident,$x:expr)=>{$name := $x};}
func main(){bind!(result,7);fmt.Println(result)}
`, "7\n")
}
