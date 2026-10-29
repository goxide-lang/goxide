package acceptance_test

import (
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
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
	enumFormalStdModule(t, `package fixture
use std;
#[derive(std::derive::Clone)]
type Leaf struct { Values []int }
#[derive(std::derive::Clone)]
type Tree struct { Rows [][]int; Index map[string][]Leaf; Pair [2][]int }
#[derive(std::derive::Error)]
#[error("operation failed")]
type Failure struct {
 #[from]
 Cause error
}
impl Tree { pub func copy(value &Self) Self { return std::Clone::clone(value) } }
impl Failure { pub func wrap(cause error) Self { return Failure::from::<std::From[error]>(cause) } }
`, `package fixture
import("testing";"errors")
type inner struct{}
func(*inner)Error()string{return "inner"}
func TestCloneNestedAndAliasPolicy(t *testing.T){
 shared:=[]int{1};original:=Tree{Rows:[][]int{shared,shared},Index:map[string][]Leaf{"k":{{Values:[]int{2}}}},Pair:[2][]int{[]int{3},nil}}
 copied:=TreeOps{}.Copy(&original)
 copied.Rows[0][0]=9;copied.Index["k"][0].Values[0]=8;copied.Pair[0][0]=7
 if original.Rows[0][0]!=1||original.Index["k"][0].Values[0]!=2||original.Pair[0][0]!=3 {t.Fatal("Clone retained nested backing storage")}
 if copied.Rows[1][0]!=1 {t.Fatal("Clone unexpectedly preserves source alias topology")}
 zero:=TreeOps{}.Copy(&Tree{});if zero.Rows!=nil||zero.Index!=nil||zero.Pair[0]!=nil{t.Fatal("nil changed")}
 empty:=TreeOps{}.Copy(&Tree{Rows:[][]int{},Index:map[string][]Leaf{}});if empty.Rows==nil||empty.Index==nil{t.Fatal("empty changed to nil")}
}
func TestErrorChainsAndNil(t *testing.T){
 cause:=&inner{};wrapped:=FailureOps{}.Wrap(cause);var got *inner
 if !errors.Is(wrapped,cause)||!errors.As(wrapped,&got)||got!=cause{t.Fatal("chain broken")}
 nilWrapped:=FailureOps{}.Wrap(nil);if nilWrapped.Unwrap()!=nil||nilWrapped.Error()!="operation failed" {t.Fatal("nil source broken")}
 var typed *inner;withTyped:=FailureOps{}.Wrap(typed);if withTyped.Unwrap()==nil {t.Fatal("typed nil collapsed")}
}
`, false)
}

func TestMacroAndCloneExplicitRejections(t *testing.T) {
	cases := map[string]struct {
		source, location string
		want             []string
	}{
		"nested repetition": {`package fixture
macro_rules! bad {($($($x:expr),*);*)=>{1};}
func f(){_=bad!(1)}
`, "2:19:", []string{"matcher repetition must consume input"}},
		"ambiguous fragment boundary": {`package fixture
macro_rules! bad {($x:expr $y:expr)=>{$x};}
func f(){_=bad!(1 2)}
`, "2:19:", []string{"ambiguous fragment boundary: expr capture requires a separator"}},
		"no matching rule": {`package fixture
macro_rules! bad {($x:ident)=>{1};}
func f(){_=bad!(42)}
`, "2:1:", []string{"expected ident fragment for $x", "called at rejected.gox:3:12"}},
		"pointer clone": {`package fixture; use std;
#[derive(std::derive::Clone)]
type Bad struct { Value *int }
`, "2:1:", []string{"missing trait implementation:", "Clone", "(*int)"}},
		"external clone": {`package fixture; use std;
use sync;
#[derive(std::derive::Clone)]
type Bad struct { Mutex sync::Mutex }
`, "3:1:", []string{"missing trait implementation:", "Clone", "(sync::Mutex)"}},
		"from non-error": {`package fixture; use std;
#[derive(std::derive::Error)]
#[error("bad")]
type Bad struct {
 #[from]
 Cause int
}
`, "2:1:", []string{"*int does not satisfy", "*error"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var err error
			if name == "pointer clone" || name == "external clone" || name == "from non-error" {
				_, err = testdeps.FormalSource(t, "rejected.gox", []byte(tc.source), "[dependencies.sync]\ngo=\"sync\"\n")
			} else {
				_, err = compileAcceptanceLanguageFixture("rejected.gox", []byte(tc.source))
			}
			if err == nil || !strings.Contains(err.Error(), "rejected.gox:"+tc.location) {
				t.Fatalf("want macro rejection at %s, got %v", tc.location, err)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("want diagnostic containing %q, got %v", want, err)
				}
			}
		})
	}
}

func TestGenericCloneAcceptanceRace(t *testing.T) {
	enumFormalStdModule(t, `package fixture; use std;
#[derive(std::derive::Clone)]
type Box[T any] struct{Value T}
func Copy(value Box[[]int])Box[[]int]{return std::Clone::clone(&value)}
`, `package fixture
import "testing"
func TestConcreteGenericClone(t *testing.T){
 original:=Box[[]int]{Value:[]int{3}}
 copied:=Copy(original);copied.Value[0]=8
 if original.Value[0]!=3||copied.Value[0]!=8{t.Fatal("generic clone retained mutable storage")}
 if Copy(Box[[]int]{}).Value!=nil{t.Fatal("nil slice changed")}
 if Copy(Box[[]int]{Value:[]int{}}).Value==nil{t.Fatal("empty slice changed")}
}
`, false)
}

func TestMacroRawLiteralCRLFRace(t *testing.T) {
	source := "package fixture\r\nmacro_rules! identity {($x:expr)=>{$x};}\r\nfunc raw() string {return identity!(`first\r\nsecond`)}\r\nfunc plain() string {return `third\r\nfourth`}\r\n"
	macroRaceModule(t, source, `package fixture
import "testing"
func TestRawLiteralCRLF(t *testing.T){if raw()!="first\nsecond"||plain()!="third\nfourth"{t.Fatalf("raw=%q plain=%q",raw(),plain())}}
`)
}
