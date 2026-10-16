package expand

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/config"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

func environmentConfig(t *testing.T, target string, values ...string) config.Config {
	t.Helper()
	selected, err := config.ParseTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := config.NewEnvironment(selected, values)
	if err != nil {
		t.Fatal(err)
	}
	return config.Config{Environment: environment, Reads: &config.Reads{}}
}

func TestTargetAndEnvironmentPredicates(t *testing.T) {
	configuration := environmentConfig(t, "linux/386", "GOOS=darwin", "EMPTY=", "VALUE=0", "\xff=\xfe")
	for _, test := range []struct {
		predicate string
		want      bool
	}{
		{`target_os("linux")`, true}, {`target_os("android")`, false},
		{`target_arch("386")`, true}, {`target_arch("amd64")`, false},
		{`target_platform("unix")`, true}, {`target_platform("wasm")`, false},
		{`target_endian("little")`, true}, {`target_endian("big")`, false},
		{`target_pointer_width("32")`, true}, {`target_pointer_width("64")`, false},
		{`env("GOOS", "darwin")`, true}, {`env("GOOS", "linux")`, false},
		{`env("MISSING")`, false}, {`env("MISSING", "")`, false},
		{`env("EMPTY")`, true}, {`env("EMPTY", "")`, true},
		{`env("VALUE")`, true}, {`env("VALUE", "false")`, false},
		{`env("\xff", "\xfe")`, true},
	} {
		t.Run(test.predicate, func(t *testing.T) {
			file := expandFile(t, "env.gox", "var value = cfg!("+test.predicate+")")
			result, err := Expand([]*ast.File{file}, nil, map[*ast.File]config.Config{file: configuration})
			if err != nil {
				t.Fatal(err)
			}
			if got := expandValue(t, result.Files[0], "value").(*ast.BoolLit).Value; got != test.want {
				t.Fatalf("query = %v, want %v", got, test.want)
			}
		})
	}
}

func TestEnvironmentReadsFollowConfigurationSelection(t *testing.T) {
	configuration := environmentConfig(t, "linux/amd64", "EMPTY=", "ACTIVE=x", "UNUSED=secret")
	file := expandFile(t, "env.gox", `
#[cfg(env("MISSING"))] func gone() { var ignored = cfg!(env("HIDDEN")) }
#[cfg_attr(any(), cfg(env("UNSELECTED")))] func kept() {}
var eager = cfg!(any(all(), env("ACTIVE")))
func branches() { if cfg!(env("EMPTY", "")) { var a = cfg!(env("LEFT")) } else { var b = cfg!(env("RIGHT")) } }
`)
	if _, err := Expand([]*ast.File{file}, nil, map[*ast.File]config.Config{file: configuration}); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, input := range configuration.Reads.Inputs() {
		keys = append(keys, input.Key)
	}
	if !reflect.DeepEqual(keys, []string{"ACTIVE", "EMPTY", "LEFT", "MISSING", "RIGHT"}) {
		t.Fatalf("wrong actual environment inputs: %v", keys)
	}
}

func TestMacroEnvironmentAndTargetUseDestination(t *testing.T) {
	definition := expandFile(t, "definition.gox", `
macro_rules! query { () => { cfg!(all(feature("local"), target_os("linux"), env("GOOS", "darwin"))) }; }
macro_rules! unused { () => { cfg!(target_os("windows")) }; }
macro_rules! item { () => { #[cfg(target_arch("amd64"))] func made() {} }; }
`)
	caller := expandFile(t, "caller.gox", `var enabled = query!(); item!()`)
	configuration := environmentConfig(t, "linux/amd64", "GOOS=darwin")
	definitionConfig, callerConfig := configuration, configuration
	definitionConfig.Features = config.Features{Enabled: map[string]bool{"local": true}, Declared: map[string]bool{"local": true}}
	callerConfig.Features = config.Features{Declared: map[string]bool{"local": true}}
	result, err := Expand([]*ast.File{definition, caller}, nil, map[*ast.File]config.Config{definition: definitionConfig, caller: callerConfig})
	if err != nil {
		t.Fatal(err)
	}
	if !expandValue(t, result.Files[1], "enabled").(*ast.BoolLit).Value {
		t.Fatal("macro query lost definition feature or shared environment")
	}
	if !result.TargetFiles[caller] || result.TargetFiles[definition] {
		t.Fatalf("target constraints assigned to wrong destination: %+v", result.TargetFiles)
	}
	if files := configuration.Reads.TargetFiles(); len(files) != 1 || files[0] != caller {
		t.Fatalf("target read records assigned to wrong destination: %v", files)
	}
}

func TestTargetUseSurvivesEarlyRemoval(t *testing.T) {
	file := expandFile(t, "env.gox", `#[cfg(target_os("windows"))] func gone() { missing!() }`)
	configuration := environmentConfig(t, "linux/amd64")
	if _, err := Configure(file, configuration); err != nil {
		t.Fatal(err)
	}
	if len(file.Items) != 0 {
		t.Fatal("opposite target item survived")
	}
	result, err := Expand([]*ast.File{file}, nil, map[*ast.File]config.Config{file: configuration})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TargetFiles[file] {
		t.Fatal("empty file lost its target constraint after early Configure")
	}
}

func TestTargetAndEnvironmentDiagnostics(t *testing.T) {
	configuration := environmentConfig(t, "linux/amd64")
	for _, test := range []struct{ predicate, want string }{
		{`target_os("Linux")`, "unknown target_os value"},
		{`target_arch("x86_64")`, "unknown target_arch value"},
		{`target_platform("posix")`, "unknown target_platform value"},
		{`target_endian("middle")`, "unknown target_endian value"},
		{`target_pointer_width(64)`, "string literal"},
		{`target_os("linux", "windows")`, "exactly one string literal"},
		{`env()`, "one or two string literals"},
		{`env("A", "B", "C")`, "one or two string literals"},
		{`env(name)`, "string literal"},
		{`env("A=B")`, "environment key"},
		{`any(all(), target_os("typo"))`, "unknown target_os value"},
		{`compiler("gc")`, "unknown configuration predicate"},
		{`cgo`, "not supported"},
		{`race`, "unknown configuration predicate"},
		{`test`, "not supported"},
	} {
		file := expandFile(t, "bad.gox", "var value = cfg!("+test.predicate+")")
		_, err := Expand([]*ast.File{file}, nil, map[*ast.File]config.Config{file: configuration})
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error %v, want %s", test.predicate, err, test.want)
		}
	}
	for _, predicate := range []string{`env("A")`, `target_os("linux")`} {
		file := expandFile(t, "bad.gox", "var value = cfg!("+predicate+")")
		if _, err := Expand([]*ast.File{file}, nil); err == nil || !strings.Contains(err.Error(), "prepared environment") {
			t.Errorf("%s silently accepted without context: %v", predicate, err)
		}
	}
}
