package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestPrintDefaultBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	source := filepath.Join(root, "examples/prototype/print_default.gox")
	input, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileAcceptanceLanguageFixture(source, append(input, []byte(printDefaultExtra)...))
	if err != nil {
		t.Fatal(err)
	}
	generated := t.TempDir()
	packagesWrite(t, generated, "go.mod", "module printdefault.example/business\ngo 1.27\n")
	packagesWrite(t, generated, "main.go", string(compiled.Go))
	packagesWrite(t, generated, "business_test.go", printDefaultAssertions)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", ".")
	for _, args := range [][]string{{"list", "-mod=readonly", "-m", "all"}, {"list", "-buildvcs=false", "-mod=readonly", "-deps", "./..."}} {
		graph := deliveryCommand(t, generated, tool, true, nil, args...)
		if strings.Contains(graph, "github.com/goxide-lang/goxide") {
			t.Fatalf("generated program depends on compiler: %s", graph)
		}
	}
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "build", "-o", cli, "./cmd/goxide")
	output := deliveryCommand(t, t.TempDir(), cli, true, nil, "run", "--roots", filepath.Join(root, "examples/prototype/goxide.roots.toml"), "--go", tool, "--runtime", root, source)
	for _, want := range []string{"startup=standby retries=0\n", "snapshot Settings {", "ready nightly active\n"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in real CLI output: %s", want, output)
		}
	}
}

func TestPrintDefaultExplicitRejections(t *testing.T) {
	cases := []struct{ name, source string }{
		{"print-no-format", `func f(){print!()}`},
		{"dynamic-format", `func f(){format:="{}";println!(format,1)}`},
		{"unused-argument", `func f(){println!("literal",1)}`},
		{"missing-argument", `func f(){println!("{}")}`},
		{"unclosed-brace", `func f(){println!("{")}`},
		{"position-format", `func f(){println!("{0}",1)}`},
		{"precision-format", `func f(){println!("{:.2}",1.5)}`},
		{"implicit-capture", `func f(){name:="job";_=name;println!("{name}")}`},
		{"stringer-not-display", `type Item int;func(Item)String()string{return "unsafe fallback"};func f(){println!("{}",Item(1))}`},
		{"error-not-display", `type Failure struct{};func(Failure)Error()string{return "unsafe fallback"};func f(){println!("{}",Failure{})}`},
		{"any-not-format", `func f(){var value any=1;println!("{:?}",value)}`},
		{"debug-not-display", "#[derive(Debug)]\ntype Value struct{N int}\nfunc f(){println!(\"{}\",Value{})}"},
		{"helper-not-debug-protocol", `type Value struct{N int};func DebugValue(*Value)string{return "not registered"};func f(){println!("{:?}",Value{})}`},
		{"go-statement", `func f(){go println!("later")}`},
		{"defer-statement", `func f(){defer println!("later")}`},
		{"default-enum-missing", "#[derive(Default)]\nenum E { A, B, }"},
		{"default-enum-many", "#[derive(Default)]\nenum E {\n#[default]\nA,\n#[default]\nB,\n}"},
		{"default-enum-payload-without-default", "#[derive(Default)]\nenum E {\n#[default]\nA(Result[int,string]),\n}"},
		{"default-without-derive", "enum E {\n#[default]\nA,\n}"},
		{"default-reference", "#[derive(Default)]\ntype Invalid struct { Value &int }"},
		{"default-pointer", "#[derive(Default)]\ntype Invalid struct { Value *int }"},
		{"default-slice", "#[derive(Default)]\ntype Invalid struct { Value []int }"},
		{"default-result", "#[derive(Default)]\ntype Invalid struct { Value Result[int,error] }"},
		{"default-nested-missing", "type Inner struct { N int }\n#[derive(Default)]\ntype Invalid struct { Value Inner }"},
		{"default-helper-not-protocol", "type Inner struct { N int };func DefaultInner()Inner{return Inner{N:7}}\n#[derive(Default)]\ntype Invalid struct { Value Inner }"},
		{"default-conflict", "#[derive(Default)]\ntype Value struct { N int };func DefaultValue()Value{return Value{}}"},
		{"default-generic", "#[derive(Default)]\ntype Value[T any] struct { N T }"},
		{"default-scalar-shadow", "type int string\n#[derive(Default)]\ntype Value struct { N int }"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), tc.name+".gox")
			_, err := compileAcceptanceLanguageFixture(filename, []byte("package rejected\n"+tc.source+"\n"))
			if err == nil {
				t.Fatal("unsupported operation accepted")
			}
			if !strings.Contains(err.Error(), filepath.Base(filename)) {
				t.Fatalf("source lost: %v", err)
			}
			want := "format"
			switch {
			case tc.name == "stringer-not-display" || tc.name == "error-not-display" || tc.name == "any-not-format" || tc.name == "debug-not-display" || tc.name == "helper-not-debug-protocol":
				want = "protocol"
			case tc.name == "go-statement" || tc.name == "defer-statement":
				want = "statement"
			case tc.name == "default-conflict":
				want = "conflict"
			case tc.name == "default-generic":
				want = "generic"
			case tc.name == "default-scalar-shadow":
				want = "shadow"
			case strings.HasPrefix(tc.name, "default-"):
				want = "default"
			case tc.name == "dynamic-format":
				want = "literal"
			}
			if !strings.Contains(strings.ToLower(err.Error()), want) {
				t.Fatalf("wrong rejection, wanted %s: %v", want, err)
			}
		})
	}
}

func TestPrintDefaultPackageScalarIdentity(t *testing.T) {
	_, _, err := testdeps.Package("printdefault.example/shadow", map[string][]byte{
		"shadow.gox":   []byte("package shadow\ntype int string\n"),
		"defaults.gox": []byte("package shadow\n#[derive(Default)]\ntype Value struct { N int }\n"),
	}, nil, nil)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "shadow") {
		t.Fatalf("package type named int acquired builtin Default: %v", err)
	}
}

const printDefaultExtra = `
var evaluationTrace string
func firstPrintArgument() int { evaluationTrace+="A";return 7 }
func secondPrintArgument() string { evaluationTrace+="B";return "next" }
func printOrder(){println!("{} {}",firstPrintArgument(),secondPrintArgument())}
func printForms(){print!("");print!("{{}}");println!();println!("");println!("tail\n")}
func invalidEnumPrint(){var invalid state;println!("{:?} {}",invalid,secondPrintArgument())}
func printOnce(){println!("written")}
func hygienicDefaults() Settings { DefaultSettings:=func()Settings{panic("captured caller helper")};_=DefaultSettings;return fresh_settings!() }
func hygienicPrinting(){print:=func(){panic("captured print")};println:=func(){panic("captured println")};_=print;_=println;print!("bound");println!(" builtin")}
func nestedPrinting(){println!("outer {}",func()int{println!("inner");return 9}())}
func hygienicDebug(){DebugSettings:=func(*Settings)string{panic("captured debug helper")};_=DebugSettings;println!("{:?}",DefaultSettings())}

#[derive(Default)]
type Empty struct{}
type NoDefault struct { Signal chan int }
#[derive(Default)]
type OptionalSettings struct { Value Option[NoDefault] }
func hasNoOptionalValue(value OptionalSettings)bool {match value.Value {Option[NoDefault]::Some(_)=>{return false},Option[NoDefault]::None=>{return true},};panic("unreachable")}
#[derive(serde::Deserialize)]
type WireDefault struct {
 #[serde(default)]
 State state
}
`

const printDefaultAssertions = `package main
import("errors";"io";"os";"strings";"testing";"github.com/goxide-lang/serde/json";"github.com/goxide-lang/std/formatting")
func capturePrint(t *testing.T,f func())string{t.Helper();r,w,err:=os.Pipe();if err!=nil{t.Fatal(err)};old:=os.Stdout;os.Stdout=w;defer func(){os.Stdout=old;r.Close();w.Close()}();f();w.Close();data,err:=io.ReadAll(r);if err!=nil{t.Fatal(err)};return string(data)}
func TestExplicitDefaultAndGoZero(t *testing.T){
 value:=DefaultSettings();if value.Name!=""||value.Limits.Retries!=0||value.Limits.Enabled||stateName(value.State)!="standby"{t.Fatalf("default: %#v",value)}
 if !hasNoOptionalValue(DefaultOptionalSettings()){t.Fatal("Option default must be None without T Default")};_=DefaultEmpty()
 if stateName(hygienicDefaults().State)!="standby"{t.Fatal("macro captured local Default helper")}
 var zero Settings;if zero.State!=nil||make([]Settings,1)[0].State!=nil||(map[string]Settings{})["missing"].State!=nil{t.Fatal("Default altered Go zero initialization")}
 rejected:=false;func(){defer func(){rejected=recover()!=nil}();_=stateName(zero.State)}();if !rejected{t.Fatal("Default legalized raw zero enum")}
 decoded,err:=serdejson.Unmarshal([]byte("{}"),DeserializeWireDefault);if err!=nil||stateName(decoded.State)!="standby"{t.Fatal("serde(default) did not call selected Default",err)}
}
func TestPrintFormsAndEvaluation(t *testing.T){
 if got:=capturePrint(t,printForms);got!="{}\n\ntail\n\n"{t.Fatalf("LF/braces: %q",got)}
 if got:=capturePrint(t,hygienicPrinting);got!="bound builtin\n"{t.Fatalf("builtin captured local binding: %q",got)}
 if got:=capturePrint(t,nestedPrinting);got!="inner\nouter 9\n"{t.Fatalf("nested direct statements: %q",got)}
 if got:=capturePrint(t,hygienicDebug);!strings.HasPrefix(got,"Settings {"){t.Fatalf("debug helper captured local binding: %q",got)}
 evaluationTrace="";if got:=capturePrint(t,printOrder);got!="7 next\n"||evaluationTrace!="AB"{t.Fatalf("order: %q %q",got,evaluationTrace)}
 evaluationTrace="";panicked:=false;got:=capturePrint(t,func(){defer func(){panicked=recover()!=nil}();invalidEnumPrint()});if !panicked||evaluationTrace!="B"||got!=""{t.Fatalf("formatter ran before all arguments: panic=%v trace=%q output=%q",panicked,evaluationTrace,got)}
}
func TestDefaultJSONAndMacroPrint(t *testing.T){
 output:=capturePrint(t,main);if !strings.HasPrefix(output,"startup=standby retries=0\n")||!strings.Contains(output,"snapshot Settings {")||!strings.HasSuffix(output,"ready nightly active\n"){t.Fatal(output)}
}
type partialWriter struct{data string;err error;short bool}
func(w *partialWriter)Write(p []byte)(int,error){n:=len(p)/2;w.data+=string(p[:n]);if w.short{return n,nil};return n,w.err}
func TestWriteFailureRetainsCauseAndPartialOutput(t *testing.T){
 cause:=errors.New("disk unavailable");for _,short:=range []bool{false,true}{w:=&partialWriter{err:cause,short:short};var recovered any;func(){defer func(){recovered=recover()}();formatting.WriteTo(w,"abcdef")}();err,ok:=recovered.(error);if !ok||w.data!="abc"{t.Fatalf("panic/partial: %v %q",recovered,w.data)};want:=cause;if short{want=io.ErrShortWrite};if !errors.Is(err,want){t.Fatalf("lost write cause: %v",err)}}
 file,err:=os.CreateTemp(t.TempDir(),"closed");if err!=nil{t.Fatal(err)};file.Close();old:=os.Stdout;os.Stdout=file;defer func(){os.Stdout=old}();var recovered any;func(){defer func(){recovered=recover()}();printOnce()}();causeError,ok:=recovered.(error);if !ok||!errors.Is(causeError,os.ErrClosed){t.Fatalf("real stdout failure swallowed: %v",recovered)}
}
`
