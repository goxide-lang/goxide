package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestManualDefaultInstalledBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin := t.TempDir()
	deliveryCommand(t, root, tool, true, nil, "run", "./scripts/build.go", "-o", filepath.Join(bin, "goxide"))
	cli := filepath.Join(bin, "goxide")
	source := t.TempDir()
	singleExternalCopyTree(t, filepath.Join(root, "examples/manual_default"), source)
	outside := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "first")
	generated := strings.TrimSpace(deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", bundle, source))
	again := filepath.Join(t.TempDir(), "second")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", again, source)
	singleExternalSameTree(t, bundle, again)
	packagesWrite(t, generated, "business_test.go", manualDefaultAssertions)
	deliveryCommand(t, generated, tool, true, nil, "build", "-mod=readonly", "./...")
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	want := "service 8080 service 8080 service 9090 SCSC\n"
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--go", tool, source); got != want {
		t.Fatalf("CLI output %q, want %q", got, want)
	}

	// Explicit Default registration now crosses source packages. Defining an
	// implementation for a foreign target remains unsupported.
	foreign := t.TempDir()
	writeStdFormalBinaryManifest(t, foreign, "manualdefault.example/rejected", "rejected")
	packagesWrite(t, foreign, "config/types.gox", "package config; use std; use std::PartialEq;\ntype Settings struct { Port int }\nimpl std::Default for Settings { func r#default() Settings { return Settings{Port:9090} } }\n\nimpl Settings {\n pub func initial() Self { return std::default_value[Self]() }\n}\n")
	for i, body := range []string{
		"#[derive(std::derive::Default)]\ntype Service struct { Value config::Settings }\nimpl Service{pub func initial()Self{return std::default_value[Self]()}}\n",
		"impl std::Default for config::Settings { func r#default() config::Settings { return config::Settings{} } }\n",
	} {
		packagesWrite(t, foreign, "main.gox", "package main; use std; use std::PartialEq;\nmod config;\n"+body+"func main(){}\n")
		destination := filepath.Join(t.TempDir(), "generated")
		output := deliveryCommand(t, outside, cli, i == 0, nil, "transpile", "-o", destination, foreign)
		if i == 0 {
			destination = strings.TrimSpace(output)
			packagesWrite(t, destination, "default_test.go", `package main
import "testing"
func TestRegisteredForeignDefault(t *testing.T){if (ServiceOps{}).Initial().Value.Port!=9090{t.Fatal("foreign Default not called")}}
`)
			deliveryCommand(t, destination, tool, true, nil, "test", "-race", "-count=1", "./...")
			continue
		}
		if !strings.Contains(strings.ToLower(output), "default") || !strings.Contains(output, "main.gox:") {
			t.Fatalf("cross-package rejection lost protocol/source: %s", output)
		}
	}
}

func TestManualDefaultExplicitRejections(t *testing.T) {
	cases := []struct{ name, source string }{
		{"parameters", `type Settings struct { Port int }
impl std::Default for Settings { func r#default(port int) Settings { return Settings{Port:port} } }`},
		{"wrong-result", `type Settings struct { Port int }
impl std::Default for Settings { func r#default() int { return 8080 } }`},
		{"pointer-result", `type Settings struct { Port int }
impl std::Default for Settings { func r#default() *Settings { return &Settings{} } }`},
		{"duplicate-manual", `type Settings struct { Port int }
impl std::Default for Settings { func r#default() Settings { return Settings{} } }
impl std::Default for Settings { func r#default() Settings { return Settings{} } }`},
		{"manual-and-derived", "#[derive(std::derive::Default)]\ntype Settings struct { Port int }\nimpl std::Default for Settings { func r#default() Settings { return Settings{} } }"},
		{"ordinary-function-not-protocol", "type Settings struct { Port int }\nfunc DefaultSettings() Settings { return Settings{Port:8080} }\n#[derive(std::derive::Default)]\ntype Service struct { Value Settings }"},
		{"selected-payload-missing-default", "type Unsupported struct { Label string }\n#[derive(std::derive::Default)]\npub enum Mode {\n#[default]\nReady(Unsupported),\n}"},
		{"default-on-struct", "#[default]\ntype Settings struct { Port int }"},
		{"default-on-field", "#[derive(std::derive::Default)]\ntype Settings struct {\n#[default]\nPort int\n}"},
		{"payload-default-without-derive", "pub enum Mode {\n#[default]\nReady(int),\n}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), tc.name+".gox")
			_, err := testdeps.FormalSource(t, filename, []byte("package rejected; use std; use std::PartialEq;\n"+tc.source+"\n"))
			want := "default"
			if tc.name == "duplicate-manual" || tc.name == "manual-and-derived" {
				want = "duplicate trait implementation: overlapping applications"
			}
			if err == nil || !strings.Contains(err.Error(), filepath.Base(filename)+":") || !strings.Contains(strings.ToLower(err.Error()), want) {
				t.Fatalf("want source-located rejection, got %v", err)
			}
		})
	}
}

const manualDefaultAssertions = `package main
import("testing";"manualdefault.example/service/config")
func TestManualDefaultsAreOrderedAndIndependent(t *testing.T){
 defaultTrace=""
 value:=(ServiceOps{}).Initial()
 if defaultTrace!="SCSC"{t.Fatalf("field order / call count: %q",defaultTrace)}
 if value.First.Port!=8080||value.Second.Port!=8080||value.Login.Name!="service"||ModeName(value.Mode)!="service"{t.Fatalf("business defaults: %#v",value)}
 if value.First.Ready==nil||value.Second.Ready==nil||value.First.Ready==value.Second.Ready{t.Fatal("manual construction reused channel or returned Go zero")}
 value.First.Ready<-7
 select{case <-value.Second.Ready:t.Fatal("field defaults shared channel");default:}
 if <-value.First.Ready!=7{t.Fatal("channel did not retain business value")}
 defaultTrace=""
 direct:=(SettingsOps{}).Initial();if direct.Port!=8080||defaultTrace!="S"{t.Fatal("native Go cannot call manual constructor")}
}
func TestSelectedEnumPayloadAndPackageIdentity(t *testing.T){
 defaultTrace=""
 if ModeName((ModeOps{}).Initial())!="service"||defaultTrace!="C"{t.Fatal("selected enum payload did not call manual Default exactly once")}
 // This second Settings belongs to another package and has its own constructor.
 if got:=config.NewSettings();got.Port!=9090{t.Fatalf("package identity lost: %#v",got)}
 if defaultTrace!="C"{t.Fatal("other package called this package's implementation")}
}
func TestManualProtocolDoesNotChangeGoZero(t *testing.T){
 defaultTrace=""
 var zero Service
 values:=[]Service{zero,make([]Service,1)[0],(map[string]Service{})["missing"]}
 for _,value:=range values{if value.First.Port!=0||value.First.Ready!=nil||value.Login.Name!=""||value.Mode!=nil{t.Fatalf("Go zero acquired Default: %#v",value)}}
 if defaultTrace!=""{t.Fatal("zero initialization called Default")}
}
`

func TestManualDefaultMayRecurse(t *testing.T) {
	result, err := testdeps.FormalSource(t, "recursive.gox", []byte(`package fixture; use std;
 type Settings struct{Depth int}
 var Remaining int
 impl std::Default for Settings {func r#default()Self{if Remaining==0{return Self{}};Remaining--;value:=std::default_value[Self]();value.Depth++;return value}}
 func Make(n int)Settings{Remaining=n;return std::default_value[Settings]()}
 `))
	if err != nil {
		t.Fatal(err)
	}
	runFormalSerdeRace(t, result, `package fixture
 import "testing"
 func TestRecursion(t *testing.T){if Make(3).Depth!=3{t.Fatal("ordinary Default recursion changed")}}
 `)
}
