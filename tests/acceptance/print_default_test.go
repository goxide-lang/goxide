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
	compiled, err := testdeps.FormalBinarySource(t, source, append(input, []byte(printDefaultExtra)...))
	if err != nil {
		t.Fatal(err)
	}
	generated := compiled.Project.Root
	packagesWrite(t, generated, "business_test.go", printDefaultAssertions)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", ".")
	for _, args := range [][]string{{"list", "-mod=readonly", "-m", "all"}, {"list", "-buildvcs=false", "-mod=readonly", "-deps", "./..."}} {
		graph := deliveryCommand(t, generated, tool, true, nil, args...)
		if strings.Contains(graph, "github.com/goxide-lang/goxide") {
			t.Fatalf("generated program depends on compiler: %s", graph)
		}
	}
	cli := acceptanceCLI(t)
	native := t.TempDir()
	packagesWrite(t, native, "go.mod", "module printdefault.example/native\ngo 1.27\n"+stdFixtureGoRequirements(t))
	writeStdNativeRoots(t, native)
	packagesWrite(t, native, "main.gox", string(input))
	output := deliveryCommand(t, t.TempDir(), cli, true, nil, "run", "--go", tool, filepath.Join(native, "main.gox"))
	for _, want := range []string{"startup=standby retries=0\n", "snapshot Settings {", "ready nightly active\n"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in real CLI output: %s", want, output)
		}
	}
}

func TestPrintDefaultExplicitRejections(t *testing.T) {
	cases := []struct {
		name, source, location string
		want                   []string
	}{
		{"print-no-format", "func f(){std::print!()}", "2:10:", []string{"macro expansion failed: print requires a format string literal"}},
		{"dynamic-format", "func f(){format:=\"{}\";std::println!(format,1)}", "2:23:", []string{"macro expansion failed: print format must be a string literal"}},
		{"unused-argument", "func f(){std::println!(\"literal\",1)}", "2:10:", []string{"macro expansion failed: format has 0 placeholders but 1 arguments"}},
		{"missing-argument", "func f(){std::println!(\"{}\")}", "2:10:", []string{"macro expansion failed: format has 1 placeholders but 0 arguments"}},
		{"unclosed-brace", "func f(){std::println!(\"{\")}", "2:10:", []string{"macro expansion failed: unsupported or unmatched format brace"}},
		{"position-format", "func f(){std::println!(\"{0}\",1)}", "2:10:", []string{"macro expansion failed: unsupported or unmatched format brace"}},
		{"precision-format", "func f(){std::println!(\"{:.2}\",1.5)}", "2:10:", []string{"macro expansion failed: unsupported or unmatched format brace"}},
		{"implicit-capture", "func f(){name:=\"job\";_=name;std::println!(\"{name}\")}", "2:29:", []string{"macro expansion failed: unsupported or unmatched format brace"}},
		{"stringer-not-display", "type Item int;func(Item)String()string{return \"unsafe fallback\"};func f(){std::println!(\"{}\",Item(1))}", "2:75:", []string{"no explicit implementation for qualified method display"}},
		{"error-not-display", "type Failure struct{};func(Failure)Error()string{return \"unsafe fallback\"};func f(){std::println!(\"{}\",Failure{})}", "2:85:", []string{"no explicit implementation for qualified method display"}},
		{"any-not-format", "func f(){var value any=1;std::println!(\"{:?}\",value)}", "2:26:", []string{"no explicit implementation for qualified method debug:", "(interface{})"}},
		{"debug-not-display", "#[derive(std::derive::Debug)]\ntype Value struct{N int}\nfunc f(){std::println!(\"{}\",Value{})}", "4:10:", []string{"no explicit implementation for qualified method display"}},
		{"helper-not-debug-protocol", "type Value struct{N int};func DebugValue(*Value)string{return \"not registered\"};func f(){std::println!(\"{:?}\",Value{})}", "2:90:", []string{"no explicit implementation for qualified method debug:", "(fixture::Value)"}},
		{"go-statement", "func f(){go std::println!(\"later\")}", "2:13:", []string{"expected expr: expected expression"}},
		{"defer-statement", "func f(){defer std::println!(\"later\")}", "2:16:", []string{"expected expr: expected expression"}},
		{"default-enum-payload-without-default", "#[derive(std::derive::Default)]\nenum E {\n#[default]\nA(Result[int,string]),\n}", "2:1:", []string{"missing trait implementation:", "Default", "Result[int,string]"}},
		{"default-without-derive", "enum E {\n#[default]\nA,\n}", "3:1:", []string{"unsupported attribute or invalid attribute position: default"}},
		{"default-reference", "#[derive(std::derive::Default)]\ntype Invalid struct { Value &int }", "2:1:", []string{"type does not explicitly implement Default:", "(*int)"}},
		{"default-pointer", "#[derive(std::derive::Default)]\ntype Invalid struct { Value *int }", "2:1:", []string{"missing trait implementation:", "Default", "(*int)"}},
		{"default-slice", "#[derive(std::derive::Default)]\ntype Invalid struct { Value []int }", "2:1:", []string{"missing trait implementation:", "Default", "([]int)"}},
		{"default-result", "#[derive(std::derive::Default)]\ntype Invalid struct { Value Result[int,error] }", "2:1:", []string{"missing trait implementation:", "Default", "Result[int,::error]"}},
		{"default-nested-missing", "type Inner struct { N int }\n#[derive(std::derive::Default)]\ntype Invalid struct { Value Inner }", "3:1:", []string{"missing trait implementation:", "Default", "(fixture::Inner)"}},
		{"default-helper-not-protocol", "type Inner struct { N int };func DefaultInner()Inner{return Inner{N:7}}\n#[derive(std::derive::Default)]\ntype Invalid struct { Value Inner }", "3:1:", []string{"missing trait implementation:", "Default", "(fixture::Inner)"}},
		{"default-conflict", "#[derive(std::derive::Default)]\ntype Value struct { N int };impl std::Default for Value{func Default()Self{return Self{}}}", "2:1:", []string{"duplicate trait implementation: overlapping applications:"}},
		{"default-generic-missing-bound", "#[derive(std::derive::Default)]\ntype Value[T any] struct { N T }\nfunc bad(){_=std::default_value[Value[chan int]]()}", "4:14:", []string{"type does not explicitly implement Default:", "missing trait implementation:"}},
		{"default-scalar-shadow", "type int string\n#[derive(std::derive::Default)]\ntype Value struct { N int }", "2:6:", []string{"package declaration \"int\" conflicts with a preimported name"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), tc.name+".gox")
			_, err := testdeps.FormalSource(t, filename, []byte("package rejected; use std; use std::PartialEq;\n"+tc.source+"\n"))
			if err == nil || !strings.Contains(err.Error(), filepath.Base(filename)+":"+tc.location) {
				t.Fatalf("want rejection at %s, got %v", tc.location, err)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("want diagnostic containing %q, got %v", want, err)
				}
			}
		})
	}
}

func TestPrintDefaultPackageScalarIdentity(t *testing.T) {
	root := t.TempDir()
	writeStdFormalManifest(t, root, "printdefault.example/shadow", "shadow", nil)
	packagesWrite(t, root, "shadow.gox", "package shadow; type int string\n")
	packagesWrite(t, root, "defaults.gox", "package shadow; use std;\n#[derive(std::derive::Default)]\ntype Value struct { N int }\n")
	_, _, err := testdeps.PrepareFormal(t, root)
	if err == nil || !strings.Contains(err.Error(), "conflicts with a preimported name") {
		t.Fatalf("package type named int acquired builtin Default: %v", err)
	}
}

const printDefaultExtra = `
var evaluationTrace string
func firstPrintArgument() int { evaluationTrace+="A";return 7 }
func secondPrintArgument() string { evaluationTrace+="B";return "next" }
func printOrder(){std::println!("{} {}",firstPrintArgument(),secondPrintArgument())}
func printForms(){std::print!("");std::print!("{{}}");std::println!();std::println!("");std::println!("tail\n")}
func invalidEnumPrint(){var invalid state;std::println!("{:?} {}",invalid,secondPrintArgument())}
func printOnce(){std::println!("written")}
func hygienicDefaults() Settings { DefaultSettings:=func()Settings{panic("captured caller helper")};_=DefaultSettings;return fresh_settings!() }
func hygienicPrinting(){print:=func(){panic("captured print")};println:=func(){panic("captured println")};_=print;_=println;std::print!("bound");std::println!(" builtin")}
func nestedPrinting(){std::println!("outer {}",func()int{std::println!("inner");return 9}())}
func hygienicDebug(){DebugSettings:=func(*Settings)string{panic("captured debug helper")};_=DebugSettings;std::println!("{:?}",Settings::initial())}

#[derive(std::derive::Default)]
type Empty struct{}
type NoDefault struct { Signal chan int }
#[derive(std::derive::Default)]
type OptionalSettings struct { Value Option[NoDefault] }
func hasNoOptionalValue(value OptionalSettings)bool {match value.Value {Option[NoDefault]::Some(_)=>{return false},Option[NoDefault]::None=>{return true},};panic("unreachable")}
#[derive(serde::derive::Deserialize)]
type WireDefault struct {
 #[serde(default)]
 State state
}
impl Empty {pub func initial()Self{return std::default_value[Self]()}}
impl OptionalSettings {pub func initial()Self{return std::default_value[Self]()}}
impl WireDefault {pub func decode(d serde::Deserializer)(Self,error){return serde::Read[Self](d)}}
`

const printDefaultAssertions = `package main
import("errors";"io";"os";"strings";"testing";"github.com/goxide-lang/serde/json";"github.com/goxide-lang/std/formatting")
func capturePrint(t *testing.T,f func())string{t.Helper();r,w,err:=os.Pipe();if err!=nil{t.Fatal(err)};old:=os.Stdout;os.Stdout=w;defer func(){os.Stdout=old;r.Close();w.Close()}();f();w.Close();data,err:=io.ReadAll(r);if err!=nil{t.Fatal(err)};return string(data)}
func TestExplicitDefaultAndGoZero(t *testing.T){
 value:=(SettingsOps{}).Initial();if value.Name!=""||value.Limits.Retries!=0||value.Limits.Enabled||stateName(value.State)!="standby"{t.Fatalf("default: %#v",value)}
 if !hasNoOptionalValue((OptionalSettingsOps{}).Initial()){t.Fatal("Option default must be None without T Default")};_=(EmptyOps{}).Initial()
 if stateName(hygienicDefaults().State)!="standby"{t.Fatal("macro captured local Default helper")}
 var zero Settings;if zero.State!=nil||make([]Settings,1)[0].State!=nil||(map[string]Settings{})["missing"].State!=nil{t.Fatal("Default altered Go zero initialization")}
 rejected:=false;func(){defer func(){rejected=recover()!=nil}();_=stateName(zero.State)}();if !rejected{t.Fatal("Default legalized raw zero enum")}
 decoded,err:=serdejson.Unmarshal([]byte("{}"),(WireDefaultOps{}).Decode);if err!=nil||stateName(decoded.State)!="standby"{t.Fatal("serde(default) did not call selected Default",err)}
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
