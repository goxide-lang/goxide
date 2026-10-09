package acceptance_test

import (
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Independent acceptance: the generated program itself is race-instrumented.
func macroRaceModule(t *testing.T, source, assertions string) {
	t.Helper()
	result, err := compileAcceptanceLanguageFixture("acceptance.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeMacroFixtureRoots(t, dir, nil)
	for name, data := range map[string]string{
		"go.mod":       "module acceptancefixture\n\ngo 1.27\n",
		"generated.go": string(result.Go), "generated_test.go": assertions,
	} {
		if err := testdeps.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "test", "-race", "-count=1", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated race module: %v\n%s\n%s", err, output, result.Go)
	}
	t.Logf("generated go test -race -count=1 -v .:\n%s", output)
}

func TestMacroAcceptanceRace(t *testing.T) {
	macroRaceModule(t, `package fixture
use slog;
func helper(x int) int { return x+3 }
macro_rules! twice {($x:expr)=>{$x+$x};}
macro_rules! once {($x:expr)=>{func() int { temporary := $x; return temporary+temporary }()};}
macro_rules! bound {($x:expr)=>{helper($x)};}
macro_rules! nested {($x:expr)=>{twice!(bound!($x))};}
macro_rules! repeated {($f:ident,$($x:expr),*)=>{$($f($x));*};}
macro_rules! choice {($x:expr)=>{1};($y:expr)=>{2};}
macro_rules! logevent {($message:expr,$($field:expr),*)=>{slog::Info($message,$($field),*)};}
func exercise() (int,int,int,int,int) {
 temporary:=100
 count:=0
 next:=func()int{count++;return temporary}
 a:=twice!(next())
 b:=once!(next())
 helper:=func(int)int{return -1000};_ = helper
 c:=nested!(2)
 total:=0;add:=func(n int){total=total*10+n};repeated!(add,1,2,3)
 return a+b,count,c,total,choice!(0)
}
func emit(next func()int) {logevent!("disabled","n",next())}
`, `package fixture
import("testing";"log/slog";"io")
func TestHygieneAndEvaluation(t *testing.T){
 a,n,c,r,rule:=exercise();if a!=400||n!=3||c!=10||r!=123||rule!=1 {t.Fatalf("got %d %d %d %d %d",a,n,c,r,rule)}
}
func TestDisabledLogArgumentsAreEager(t *testing.T){
 old:=slog.Default();defer slog.SetDefault(old)
 slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard,&slog.HandlerOptions{Level:slog.LevelError})))
 n:=0;emit(func()int{n++;return n});if n!=1{t.Fatalf("argument evaluations = %d",n)}
}
`)
}

func TestCloneAndErrorAcceptanceRace(t *testing.T) {
	macroRaceModule(t, `package fixture
#[derive(Clone)]
type Leaf struct { Values []int }
#[derive(Clone)]
type Tree struct { Rows [][]int; Index map[string][]Leaf; Pair [2][]int }
#[derive(Error)]
#[error("operation failed")]
type Failure struct {
 #[from]
 Cause error
}
`, `package fixture
import("testing";"errors")
type inner struct{}
func(*inner)Error()string{return "inner"}
func TestCloneNestedAndAliasPolicy(t *testing.T){
 shared:=[]int{1};original:=Tree{Rows:[][]int{shared,shared},Index:map[string][]Leaf{"k":{{Values:[]int{2}}}},Pair:[2][]int{[]int{3},nil}}
 copied:=CloneTree(&original)
 copied.Rows[0][0]=9;copied.Index["k"][0].Values[0]=8;copied.Pair[0][0]=7
 if original.Rows[0][0]!=1||original.Index["k"][0].Values[0]!=2||original.Pair[0][0]!=3 {t.Fatal("Clone retained nested backing storage")}
 if copied.Rows[1][0]!=1 {t.Fatal("Clone unexpectedly preserves source alias topology")}
 zero:=CloneTree(&Tree{});if zero.Rows!=nil||zero.Index!=nil||zero.Pair[0]!=nil{t.Fatal("nil changed")}
 empty:=CloneTree(&Tree{Rows:[][]int{},Index:map[string][]Leaf{}});if empty.Rows==nil||empty.Index==nil{t.Fatal("empty changed to nil")}
}
func TestErrorChainsAndNil(t *testing.T){
 cause:=&inner{};wrapped:=FromFailure(cause);var got *inner
 if !errors.Is(wrapped,cause)||!errors.As(wrapped,&got)||got!=cause{t.Fatal("chain broken")}
 nilWrapped:=FromFailure(nil);if nilWrapped.Unwrap()!=nil||nilWrapped.Error()!="operation failed" {t.Fatal("nil source broken")}
 var typed *inner;withTyped:=FromFailure(typed);if withTyped.Unwrap()==nil {t.Fatal("typed nil collapsed")}
}
`)
}

func TestMacroAndCloneExplicitRejections(t *testing.T) {
	cases := map[string]string{
		"nested repetition": `package fixture
macro_rules! bad {($($($x:expr),*);*)=>{1};}
func f(){_=bad!(1)}
`,
		"ambiguous fragment boundary": `package fixture
macro_rules! bad {($x:expr $y:expr)=>{$x};}
func f(){_=bad!(1 2)}
`,
		"no matching rule": `package fixture
macro_rules! bad {($x:ident)=>{1};}
func f(){_=bad!(42)}
`,
		"pointer clone": `package fixture
#[derive(Clone)]
type Bad struct { Value *int }
`,
		"external clone": `package fixture
use sync;
#[derive(Clone)]
type Bad struct { Mutex sync::Mutex }
`,
		"generic clone": `package fixture
#[derive(Clone)]
type Bad[T any] struct { Value T }
`,
		"typed from unsupported": `package fixture
#[derive(Error)]
type Bad struct {
 #[from]
 Cause *Other
}

type Other struct{}
func(*Other)Error()string{return "other"}
`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceLanguageFixture("rejected.gox", []byte(source))
			if err == nil || !strings.Contains(err.Error(), "rejected.gox:") {
				t.Fatalf("expected located rejection; got %v", err)
			}
			if name == "no matching rule" && !strings.Contains(err.Error(), "rejected.gox:3:12:") {
				t.Fatalf("diagnostic lost invocation location: %v", err)
			}
			t.Log(err)
		})
	}
}

func TestMacroRawLiteralCRLFRace(t *testing.T) {
	source := "package fixture\r\nmacro_rules! identity {($x:expr)=>{$x};}\r\nfunc raw() string {return identity!(`first\r\nsecond`)}\r\nfunc plain() string {return `third\r\nfourth`}\r\n"
	macroRaceModule(t, source, `package fixture
import "testing"
func TestRawLiteralCRLF(t *testing.T){if raw()!="first\nsecond"||plain()!="third\nfourth"{t.Fatalf("raw=%q plain=%q",raw(),plain())}}
`)
}
