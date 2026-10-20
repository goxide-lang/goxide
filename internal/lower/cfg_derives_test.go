package lower_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/config"
	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/paths"
	testdeps "github.com/hxsf/goxide/tests/testutil"
)

func TestCFGDerivedProtocols(t *testing.T) {
	const source = `package main
#[cfg(feature("derive"))]
use serde;
#[cfg(feature("derive"))]
use serde::json as json;
#[cfg_attr(feature("derive"), derive(Default, Debug, serde::Serialize, serde::Deserialize))]
type Record struct { Value int }
#[cfg(feature("derive"))]
func Exercise() {
    value := DefaultRecord()
    println!("{}", value.Value)
    value.Value = 7
    println!("{:?}", value)
    data, err := json::Marshal(&value, SerializeRecord)
    println!("{} {}", string(data), err == nil)
    decoded, err := json::Unmarshal(data, DeserializeRecord)
    println!("{} {}", decoded.Value, err == nil)
}
#[cfg(not(feature("derive")))]
func Exercise() { println!("plain") }
func main() { Exercise() }
`
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			var previous []byte
			for attempt := 0; attempt < 2; attempt++ {
				directory, err := compileCFGSupport(t, source, enabled)
				if err != nil {
					t.Fatal(err)
				}
				output, err := os.ReadFile(filepath.Join(directory, "main.go"))
				if err != nil {
					t.Fatal(err)
				}
				for _, helper := range []string{"DefaultRecord", "DebugRecord", "SerializeRecord", "DeserializeRecord"} {
					if present := strings.Contains(string(output), "func "+helper+"("); present != enabled {
						t.Fatalf("%s presence = %t, enabled = %t", helper, present, enabled)
					}
				}
				if attempt != 0 && !bytes.Equal(previous, output) {
					t.Fatal("same conditional derives changed output")
				}
				previous = output
				if attempt == 0 {
					runGo(t, directory, "build", "./...")
					want := "plain\n"
					if enabled {
						want = "0\nRecord { Value: 7 }\n{\"Value\":7} true\n7 true\n"
					}
					if got := runGo(t, directory, "run", "."); got != want {
						t.Fatalf("conditional derive output = %q, want %q", got, want)
					}
				}
			}
		})
	}
}

func TestCFGDeriveConflicts(t *testing.T) {
	for _, test := range []struct{ name, declaration string }{
		{"manual-default", `#[cfg_attr(feature("derive"), derive(Default))]
type Record struct { Value int }
impl Default for Record { func default() Record { return Record{Value:9} } }`},
		{"duplicate-debug", `#[derive(Debug)]
#[cfg_attr(feature("derive"), derive(Debug))]
type Record struct { Value int }`},
		{"duplicate-serde", `use serde;
#[derive(serde::Serialize)]
#[cfg_attr(feature("derive"), derive(serde::Serialize))]
type Record struct { Value int }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "package main\n" + test.declaration + "\nfunc main(){}\n"
			directory, err := compileCFGSupport(t, source, false)
			if err != nil {
				t.Fatalf("disabled derive produced conflict: %v", err)
			}
			runGo(t, directory, "build", "./...")
			_, err = compileCFGSupport(t, source, true)
			if err == nil || !strings.Contains(err.Error(), "duplicate") {
				t.Fatalf("enabled derive conflict was not diagnosed: %v", err)
			}
		})
	}
}

func compileCFGSupport(t *testing.T, source string, enabled bool) (string, error) {
	t.Helper()
	directory := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(directory, "go.mod"), []byte("module cfgsupport\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writeDiagnosticModuleFile(t, directory, "main.gox", []byte(source))
	roots := paths.Roots{"serde": {
		ModulePath: "github.com/goxide-lang/serde", Source: "module", GoPrefix: "github.com/goxide-lang/serde",
	}}
	features := config.Features{Enabled: map[string]bool{"derive": enabled}, Declared: map[string]bool{"derive": true}}
	err := lower.CompileModules([]lower.Module{{Path: "cfgsupport", Dir: directory, Roots: roots, Config: testConfig(t, features)}})
	return directory, err
}
