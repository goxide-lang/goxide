package compile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// All business derives and printing run the publicly pinned std macro crate.
// These assertions replace the former builtin lower tests, retaining their
// runtime results while no longer requiring generated helper names or bodies.
func TestFormalStdPublishedProtocols(t *testing.T) {
	dependencies := strings.Replace(pinnedStandardManifest(t), "[dependencies.standard]", "[dependencies.std]", 1)
	formalStdAcceptance(t, dependencies)
}

func formalStdAcceptance(t *testing.T, dependencies string) {
	t.Helper()
	compilerSDKForTest(t)
	root := t.TempDir()
	deriveCalls, printCalls := 0, 0
	run := testWASI(t, func(request pm.Request) {
		if request.Kind() == pm.Derive {
			deriveCalls++
		}
		if request.Kind() == pm.Function {
			printCalls++
		}
	})
	manifest := func(name string) string {
		return fmt.Sprintf("[go_mod]\nmodule=%q\ngo=\"1.27\"\n[[bin]]\nname=\"fixture\"\npath=\".\"\n", "example.org/formal-std/"+name) + dependencies
	}
	check := func(t *testing.T, name string, sources map[string]string, want string) {
		t.Helper()
		directory := filepath.Join(root, name)
		writeProjectFile(t, directory, project.ManifestName, manifest(name))
		for file, source := range sources {
			writeProjectFile(t, directory, file, source)
		}
		first, err := PrepareProject(directory, Options{Offline: true, RunProcMacro: run})
		if err != nil {
			t.Fatal(err)
		}
		binary := filepath.Join(root, "consumer-"+name)
		if first.Config.Environment.Target.OS == "windows" {
			binary += ".exe"
		}
		configuredGo(t, first, "build", "-mod=readonly", "-o", binary, first.Entry)
		command := exec.Command(binary)
		command.Dir, command.Env = first.Root, project.GoEnv(true, first.Config.Environment.ExecEnv())
		if got, err := command.CombinedOutput(); err != nil || string(got) != want {
			t.Fatalf("formal std output = %q, want %q: %v", got, want, err)
		}
		before := projectSnapshot(t, filepath.Dir(first.Root))
		lock, err := os.ReadFile(filepath.Join(directory, "goxide.lock"))
		if err != nil {
			t.Fatal(err)
		}
		second, err := PrepareProject(directory, Options{Offline: true, Locked: true, RunProcMacro: run})
		if err != nil {
			t.Fatal(err)
		}
		if first.Root != second.Root || !reflect.DeepEqual(before, projectSnapshot(t, filepath.Dir(second.Root))) {
			t.Fatal("locked replay changed formal std artifacts")
		}
		after, err := os.ReadFile(filepath.Join(directory, "goxide.lock"))
		if err != nil || !bytes.Equal(lock, after) {
			t.Fatalf("locked replay changed formal std lock: %v", err)
		}
	}
	fixture := func(name string) map[string]string {
		t.Helper()
		files, err := filepath.Glob(filepath.Join("..", "lower", "testdata", "derive", name, "*.gox"))
		if err != nil || len(files) == 0 {
			t.Fatalf("formal fixture %s: %v", name, err)
		}
		sources := map[string]string{}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			sources[filepath.Base(file)] = string(data)
		}
		return sources
	}
	for _, test := range []struct{ name, want string }{
		{"default", "true UNTITLED 2 [label step label step]\nnone\nUNTITLED4\n"},
		{"clone_debug", "shared b c\nRecord { Values: [Item { Text: \"shared\" }], Table: {\"key\": [Item { Text: \"b\" }]}, Fixed: [Item { Text: \"c\" }], Choice: Option::Some([]), Outcome: Result::Err(nil) }\nState::Pair(Item { Text: \"quoted\" }, [1, 2]) State::Pair(Item { Text: \"quoted\" }, [9, 2])\ntrue true none\nfalse false true\naccount { id: 7, name: \"hidden\" }\nGrouped { Value: 11 }\nState::Empty State::Named { Name: \"secret\", Count: 2 }\n"},
		{"equality", "true true false\nfalse\ntrue false\nfalse\nfalse\nfalse\nfalse [left right]\ntrue\ntrue\n"},
		{"equality_flow", "true [left right] false [left right] true true 7\n"},
		{"error", "input failed true bad count 3 cannot open data true\n"},
		{"printing", "\n{1} \"quoted\" 2\nseen 3\nRecord { Text: \"secret\" }\n(1+2i) (3-4i)\nrecovered caller frame\n123 local 7\n"},
		{"formal", "Record { First: 7, Second: \"hello\" } | Box { Value: 9 } | Choice::Value(4)\n"},
	} {
		t.Run(test.name, func(t *testing.T) { check(t, test.name, fixture(test.name), test.want) })
	}
	for _, test := range []struct{ name, parameter, result string }{
		{"parameter-panic", "panic", "bool"},
		{"parameter-nil", "nil", "bool"},
		{"parameter-variant", "Variant", "bool"},
		{"result-panic", "other", "(panic bool)"},
		{"result-nil", "other", "(nil bool)"},
		{"result-variant", "other", "(Variant bool)"},
		{"parameter-type-argument", "int", "bool"},
		{"result-type-argument", "other", "(int bool)"},
		{"parameter-named-argument", "User", "bool"},
		{"result-named-argument", "other", "(User bool)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources := fixture("hygiene")
			sources["main.gox"] = strings.NewReplacer("PARAM", test.parameter, "RESULT", test.result).Replace(sources["main.gox"])
			check(t, test.name, sources, "true true 1\n")
		})
	}
	// Former helper names are now ordinary user names. #[from] emits the same
	// canonical From implementation used by ordinary Result propagation.
	t.Run("ordinary-protocols", func(t *testing.T) {
		check(t, "ordinary-protocols", map[string]string{"main.gox": `package main
use std;
use errors;
#[derive(std::derive::Default)] struct Value{}
func DefaultValue() Value{return Value{}}
#[derive(std::derive::Error)] #[error("wrapped")]
struct Failure{#[from] Cause error}
func load(value Result[int,error]) Result[int,Failure]{return Result[int,Failure]::Ok(value?)}
func main(){
 _=DefaultValue();_=std::default_value[Value]()
 cause:=errors::New("cause")
 wrapped:=match load(Result[int,error]::Err(cause)){Result::Err(value)=>value,_=>Failure{}}
 if wrapped.Error()!="wrapped"||!errors::Is(wrapped,cause){panic("canonical From propagation")}
}
`}, "")
	})
	t.Run("prelude-equality", func(t *testing.T) {
		check(t, "prelude-equality", map[string]string{"main.gox": `package main
func main(){
 if !([]int{}).equals([]int{}) || ([]int{1}).equals([]int{2}) { panic("slice equality") }
 if !Option[int]::None.equals(Option[int]::None) || Option[int]::None.equals(Option[int]::Some(1)) { panic("option equality") }
}
`}, "")
	})
	for _, test := range formalStdRejections() {
		t.Run(test.name, func(t *testing.T) {
			directory := filepath.Join(root, "negative", test.name)
			writeProjectFile(t, directory, project.ManifestName, manifest("negative-"+test.name))
			writeProjectFile(t, directory, "main.gox", "package main\nuse std;\n"+test.declaration+"\nfunc main(){"+test.body+"}\n")
			_, err := PrepareProject(directory, Options{Offline: true, RunProcMacro: run})
			if err == nil || !strings.Contains(err.Error(), test.diagnostic) {
				t.Fatalf("formal std rejection = %v, expected %q", err, test.diagnostic)
			}
		})
	}
	t.Run("macro-transport", func(t *testing.T) {
		if deriveCalls == 0 || printCalls == 0 {
			t.Fatalf("formal std did not execute WASI derive and function macros: %d/%d", deriveCalls, printCalls)
		}
	})
}

type formalStdRejection struct{ name, declaration, body, diagnostic string }

func formalStdRejections() []formalStdRejection {
	return []formalStdRejection{
		{"immutable-enum-slot", `enum Value{Item(int)}`, `value:=Value::Item(1);match &mut value{Value::Item(slot)=>{*slot=2}}`, "enum payload slots are immutable; use a value pattern"},
		{"missing-clone", `struct Inner{Value int}; #[derive(std::derive::Clone)] struct Outer{Inner Inner}`, "", "prerequisites"},
		{"missing-debug", `#[derive(std::derive::Debug)] struct Value{Value chan int}`, "", "prerequisites"},
		{"default-container", `#[derive(std::derive::Default)] struct Value{Values []int}`, "", "prerequisites"},
		{"default-result", `#[derive(std::derive::Default)] struct Value{Value Result[int,string]}`, "", "prerequisites"},
		{"selected-payload", `#[derive(std::derive::Default)] enum Value{#[default] Selected(chan int),Other}`, "", "prerequisites"},
		{"missing-default-variant", `#[derive(std::derive::Default)] enum Value{A,B}`, "", "exactly one"},
		{"duplicate-default-variant", `#[derive(std::derive::Default)] enum Value{#[default] A,#[default] B}`, "", "exactly one"},
		{"float-eq", `#[derive(std::derive::PartialEq,std::derive::Eq)] struct Value{Value float64}`, "", "prerequisites"},
		{"unstable-map-key", `#[derive(std::derive::PartialEq)] struct Value{Value map[float64]int}`, "", "prerequisites"},
		{"recursive-clone", `#[derive(std::derive::Clone)] struct Value{Values []Value}`, "", "recursive"},
		{"manual-default-conflict", `#[derive(std::derive::Default)] struct Value{}; impl std::Default for Value{func Default()Self{return Self{}}}`, "", "overlapping"},
		{"manual-eq-proof", `#[derive(std::derive::Eq)] type Value int; impl std::PartialEq for Value{func equals(self Self,other Self)bool{return true}}`, "", "prerequisites"},
		{"manual-map-key", `type Key int; impl std::PartialEq for Key{func equals(self Self,other Self)bool{return true}}; impl std::Eq for Key{}; #[derive(std::derive::PartialEq)] struct Value{Map map[Key]int}`, "", "prerequisites"},
		{"clone-defined-key", `type Key string; #[derive(std::derive::Clone)] struct Value{Value map[Key]int}`, "", "prerequisites"},
		{"clone-complex", `#[derive(std::derive::Clone)] struct Value{Value complex64}`, "", "prerequisites"},
		{"debug-map-key", `#[derive(std::derive::Debug)] struct Value{Value map[int]int}`, "", "prerequisites"},
		{"native-key-float", `#[derive(std::derive::PartialEq,std::derive::NativeKey)] struct Value{Value float64}`, "", "prerequisites"},
		{"native-key-noncomparable", `type Value []int; impl std::PartialEq for Value{func equals(self Self,other Self)bool{return len(self)==len(other)}};impl std::NativeKey for Value{}`, "", "comparable"},
		{"native-key-enum", `#[derive(std::derive::PartialEq,std::derive::NativeKey)] enum Value{Unit}`, "", "cannot be derived"},
		{"error-complex", `#[derive(std::derive::Error)] #[error("{Value}")] struct E{Value complex128}`, "", "does not satisfy"},
		{"source-type", `#[derive(std::derive::Error)] enum E{#[error("x")] #[source] A(int)}`, "", "does not satisfy"},
		{"duplicate-from", `#[derive(std::derive::Error)] enum E{#[error("x")] #[from] A(error),#[error("y")] #[from] B(error)}`, "", "at most one from"},
		{"unknown-field", `#[derive(std::derive::Error)] enum E{#[error("{Missing}")] A(int)}`, "", "unknown error placeholder"},
		{"from-extra-field", `#[derive(std::derive::Error)] #[error("x")] struct E{#[from] Cause error;Extra int}`, "", "exactly one error field"},
		{"from-enum", `#[derive(std::derive::Error)] #[from] enum E{#[error("failure")] Failure}`, "", "belong"},
		{"source-struct", `#[derive(std::derive::Error)] #[error("x")] #[source] struct E{Cause error}`, "", "belong"},
		{"message-field", `#[derive(std::derive::Error)] #[error("x")] struct E{#[error("field")] Cause error}`, "", "belong"},
		{"existing-error", `#[derive(std::derive::Error)] #[error("x")] struct E{}; func(E)Error()string{return "manual"}`, "", "duplicate declaration"},
		{"existing-unwrap", `#[derive(std::derive::Error)] #[error("x")] struct E{#[source]Cause error}; func(E)Unwrap()error{return nil}`, "", "duplicate declaration"},
		{"print-missing-literal", "", `std::print!()`, "format string literal"},
		{"print-dynamic", "", `format:="{}";std::println!(format,1)`, "string literal"},
		{"print-unmatched", "", `std::println!("{")`, "format brace"},
		{"print-count", "", `std::println!("{}",1,2)`, "placeholders"},
		{"print-stringer", `struct Value{};func(Value)String()string{return "fallback"}`, `std::println!("{}",Value{})`, "no explicit implementation"},
		{"print-expression", "", `_ = std::println!("x")`, "expected expression"},
		{"print-defer", "", `defer std::println!("x")`, "expected expression"},
		{"print-initializer", "", `if std::println!("x");true{}`, "exactly one statement"},
		{"no-defined-method", `type Plain int`, `_ = Plain(1).equals(Plain(1))`, "no explicit implementation for selected trait application"},
	}
}
