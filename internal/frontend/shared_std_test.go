package frontend

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The public module is deliberately not assigned a fabricated release version.
// Migration tests use the reviewed isolated library candidate explicitly.
func sharedStdModule(t *testing.T, module string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	candidate := filepath.Join(filepath.Dir(file), "../../tests/research/shared_std_candidate")
	dir := t.TempDir()
	sharedStdWrite(t, dir, "go.mod", "module "+module+"\ngo 1.27\nrequire github.com/goxide-lang/std v0.0.0\nreplace github.com/goxide-lang/std => "+filepath.ToSlash(candidate)+"\n")
	return dir
}
func sharedStdWrite(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func sharedStdGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, out)
	}
}
func TestSharedStdCrossModuleAndContext(t *testing.T) {
	producer := sharedStdModule(t, "example.org/producer")
	sharedStdWrite(t, producer, "internal/source/value.hgo", `package source
func Make()Result[int,error]{return Result::Ok(7)}
`)
	sharedStdWrite(t, producer, "value.hgo", `package producer
import "example.org/producer/internal/source"
func Make()Result[int,error]{return source.Make()}
func Optional()Option[int]{return Option::Some(3)}
`)
	output := filepath.Join(t.TempDir(), "producer")
	if _, err := CompileModuleWithOptions(producer, output, ModuleOptions{}); err != nil {
		t.Fatal(err)
	}
	sharedStdGo(t, output, "test", "-race", "./...")
	sharedStdGo(t, output, "vet", "./...")
	consumer := sharedStdModule(t, "example.org/consumer")
	f, err := os.OpenFile(filepath.Join(consumer, "go.mod"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("require example.org/producer v0.0.0\nreplace example.org/producer => " + filepath.ToSlash(output) + "\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	sharedStdWrite(t, consumer, "z_init.hgo", `package consumer
var Later = Option::Some(11)
`)
	sharedStdWrite(t, consumer, "a_use.hgo", `package consumer
func CrossFile()int{return match Later {Option::Some(n)=>n,Option::None=>0,}}
`)
	sharedStdWrite(t, consumer, "native.go", `package consumer
import "example.org/producer"
type NativeOption = producer.Option[int]
`)
	sharedStdWrite(t, consumer, "value.hgo", `package consumer
import "example.org/producer"
#[derive(Default,Clone,Debug,PartialEq,Eq)]
type Holder struct{V NativeOption}
type Failure struct{}
func(*Failure)Error()string{return "failed"}
func pointerCall()(int,*Failure){return 12,nil}
func interfaceCall()(int,error){var e *Failure;return 13,e}
func PointerAdapter()Result[int,*Failure]{return go_result!(pointerCall())}
func InterfaceAdapter()Result[int,error]{return go_result!(interfaceCall())}
func Pair()(Option[int],Result[string,error]){return Option::Some(2),Result::Ok("pair")}
func Many(v ...Option[int])int{return Take(v[0])}
func Direct()Result[int,error]{return producer.Make()}
func Qualified(v producer.Result[int,error])int{return match v {producer.Result[int,error]::Ok(n)=>n,producer.Result[int,error]::Err(_)=>-1,}}
func Propagate()Result[int,error]{n:=producer.Make()?;return Result::Ok(n)}
func Absent()Option[string]{return Option::None}
func Take(v Option[int])int{return match v {Option::Some(n)=>n,Option::None=>0,}}
func Context()int{var x Option[int]=Option::Some(3);x=Option::Some(4);return Take(Option::Some(Take(x)+1))}
func Arm(x Option[int])Option[int]{match x {Option::Some(n)=>{return Option::Some(n)},Option::None=>{return Option::None},};return Option::None}
func Narrow(x Option[int])Option[uint8]{return match x {Option::Some(_)=>Option::Some(3),Option::None=>Option::None,}}
func ExprArm(x Option[int])Option[int]{return match x {Option::Some(n)=>Option::Some(n),Option::None=>Option::None,}}
func Closure()Option[int]{f:=func()Option[int]{return Option::Some(6)};return f()}
`)
	target := filepath.Join(t.TempDir(), "consumer")
	if _, err := CompileModuleWithOptions(consumer, target, ModuleOptions{}); err != nil {
		t.Fatal(err)
	}
	sharedStdWrite(t, target, "value_test.go", `package consumer
import("testing";"example.org/producer";"github.com/goxide-lang/std/sum")
func TestShared(t *testing.T){if PointerAdapter().(sum.Ok[int,*Failure]).Value!=12{t.Fatal("static pointer nil")};e:=InterfaceAdapter().(sum.Err[int,error]).Value;if e==nil||e.(*Failure)!=nil{t.Fatal("static interface typednil")};first,second:=Pair();if Many(first)!=2||second.(sum.Ok[string,error]).Value!="pair"{t.Fatal("tuple context")};h:=Holder{V:producer.Optional()};c:=CloneHolder(&h);if Take(c.V)!=3||DefaultHolder().V!=nil||DebugHolder(&c)!="Holder { V: Option::Some(3) }"||!EqualsHolder(h,c){t.Fatal("protocols")};if Narrow(producer.Optional()).(sum.Some[uint8]).Value!=3{t.Fatal("constant context")};var v Result[int,error]=producer.Make();if v.(sum.Ok[int,error]).Value!=7||Direct().(sum.Ok[int,error]).Value!=7||Propagate().(sum.Ok[int,error]).Value!=7||Qualified(v)!=7{t.Fatal("identity or propagation")};if Absent()!=nil||Context()!=5||CrossFile()!=11||Take(Arm(producer.Optional()))!=3||Take(Closure())!=6||Take(ExprArm(producer.Optional()))!=3{t.Fatal("context")}}
`)
	sharedStdGo(t, target, "test", "-race", "./...")
	sharedStdGo(t, target, "vet", "./...")
	// Old nominal artifacts must request regeneration, rather than mixing ABIs.
	artifact := filepath.Join(output, enumMetadataFilename)
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"Version": 7`, `"Version": 6`, 1))
	if err = os.WriteFile(artifact, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = CompileModuleWithOptions(consumer, filepath.Join(t.TempDir(), "stale"), ModuleOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported metadata version") {
		t.Fatalf("old schema accepted: %v", err)
	}
}
func TestSharedStdInsufficientInference(t *testing.T) {
	for name, body := range map[string]string{
		"none":         `func f(){x:=Option::None;_=x}`,
		"ok":           `func f(){x:=Result::Ok(1);_=x}`,
		"err":          `func f(){x:=Result::Err(nil);_=x}`,
		"nil-some":     `func f(){x:=Option::Some(nil);_=x}`,
		"outer-return": `func f()Result[int,error]{x:=Result::Ok(1);return x}`,
		"wrong-family": `func f()Option[int]{return Result::Ok(1)}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := sharedStdModule(t, "example.org/negative")
			sharedStdWrite(t, dir, "bad.hgo", "package negative\n"+body)
			_, err := CompileModuleWithOptions(dir, filepath.Join(t.TempDir(), "out"), ModuleOptions{})
			if err == nil {
				t.Fatal("accepted unconstrained constructor")
			}
			if !strings.Contains(err.Error(), "infer all sum type arguments") && !strings.Contains(err.Error(), "family conflicts") {
				t.Fatalf("unexpected diagnostic: %v", err)
			}
		})
	}
}

func TestSharedStdContextChecksPayload(t *testing.T) {
	for name, body := range map[string]string{
		"overflow":      `func F(x Option[int])Option[uint8]{return match x {Option::Some(_)=>Option::Some(256),Option::None=>Option::None,}}`,
		"wrong-payload": `func F()Option[int]{return Option::Some("wrong")}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := sharedStdModule(t, "example.org/negative")
			sharedStdWrite(t, dir, "bad.hgo", "package negative\n"+body)
			_, err := CompileModuleWithOptions(dir, filepath.Join(t.TempDir(), "out"), ModuleOptions{})
			if err == nil || (name == "overflow" && !strings.Contains(err.Error(), "overflows")) || (name == "wrong-payload" && !strings.Contains(err.Error(), "cannot use")) {
				t.Fatalf("missing payload diagnostic: %v", err)
			}
		})
	}
}
