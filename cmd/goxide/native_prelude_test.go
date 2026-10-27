package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestNativeAutomaticPrelude(t *testing.T) {
	const standard = "github.com/goxide-lang/std"
	for _, test := range []struct {
		name, source, output string
		imports              []string
		tools                bool
	}{
		{name: "unused", source: `func main(){}`},
		{name: "go-builtin", source: `func main(){println("builtin")}`, output: "builtin\n"},
		{name: "value", source: `func main(){if answer()!=41{panic("value")};println("value")}`, output: "value\n", imports: []string{standard + "/values"}},
		{name: "macro", source: `func main(){if forty_two!()!=42 || derive::answer!()!=42{panic("macro")};println("macro")}`, output: "macro\n", tools: true},
		{name: "trait-method", source: `func main(){value:=7;if value.mark()!=9{panic("trait")};println("trait")}`, output: "trait\n", imports: []string{standard + "/values"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			app := filepath.Join(root, "app")
			cfg := testdeps.Configuration()
			files := map[string]string{
				"std/go.mod":             "module " + standard + "\ngo 1.27\n",
				"std/goxide.toml":        "[go_mod]\nmodule='" + standard + "'\ngo='1.27'\n[lib]\nname='std'\npath='.'\n",
				"std/lib.gox":            "package std\npub mod prelude;pub mod values;pub mod macros;pub mod derive;\n",
				"std/lib_gox.go":         generatedHeader + "package std\n",
				"std/prelude/lib.gox":    "package prelude\npub use crate::values::answer;pub use crate::macros::forty_two;pub use crate::macros::println;pub use crate::derive;\n",
				"std/prelude/lib_gox.go": generatedHeader + "package prelude\n",
				"std/values/lib.gox":     "package values\npub func answer()int{return 41}\n",
				"std/values/lib_gox.go":  generatedHeader + "package values\nfunc Answer()int{return 41}\n",
				"std/macros/lib.gox":     "package macros\npub macro_rules! forty_two{()=>{42};}\npub macro_rules! println{()=>{17};}\n",
				"std/macros/lib_gox.go":  generatedHeader + "package macros\n",
				"std/derive/lib.gox":     "package derive\npub macro_rules! answer{()=>{42};}\n",
				"std/derive/lib_gox.go":  generatedHeader + "package derive\n",
				"app/go.mod":             "module example.org/app\ngo 1.27\nrequire " + standard + " v0.0.0\nreplace " + standard + " => ../std\n",
				"app/main.gox":           "package main\n" + test.source + "\n",
			}
			if test.name == "trait-method" {
				files["std/prelude/lib.gox"] += "pub use crate::values::Mark;\n"
				files["std/values/lib.gox"] += "pub trait Mark{func mark(self Self)int}\nimpl Mark for int{func mark(self Self)int{return self+2}}\n"
			}
			for name, source := range files {
				singleFileWrite(t, root, name, source)
			}
			if test.name == "trait-method" {
				if err := lower.CompileModules([]lower.Module{{Path: standard, Dir: filepath.Join(root, "std"), RootPackage: standard, Config: cfg, StableGo: true,
					Crates: []lower.Crate{{Root: standard, SourceDir: standard, Library: true}},
				}}); err != nil {
					t.Fatalf("publish trait fixture: %v", err)
				}
				// Published libraries retain source beside their stable Go API.
				for name, source := range files {
					if strings.HasPrefix(name, "std/") && filepath.Ext(name) == ".gox" {
						singleFileWrite(t, root, name, source)
					}
				}
			}
			before := singleFileSnapshot(t, root)
			var previous map[string][32]byte
			for attempt := 0; attempt < 2; attempt++ {
				var stdout, stderr bytes.Buffer
				if err := run([]string{"gen", app}, &stdout, &stderr); err != nil {
					t.Fatalf("gen: %v\n%s", err, &stderr)
				}
				current := singleFileSnapshot(t, root)
				for name, digest := range before {
					if current[name] != digest {
						t.Fatalf("generation changed authored file %s", name)
					}
				}
				if attempt != 0 && !reflect.DeepEqual(previous, current) {
					t.Fatal("same source produced different generated files")
				}
				previous = current
			}
			var runtimeImports, toolImports []string
			generated, err := filepath.Glob(filepath.Join(app, "*_gox.go"))
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range generated {
				file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
				if err != nil {
					t.Fatal(err)
				}
				for _, imported := range file.Imports {
					path, err := strconv.Unquote(imported.Path.Value)
					if err != nil {
						t.Fatal(err)
					}
					if filepath.Base(name) == "goxide_tools_gox.go" {
						if imported.Name == nil || imported.Name.Name != "_" {
							t.Fatalf("generation dependency is not a blank import: %s", name)
						}
						toolImports = append(toolImports, path)
					} else {
						runtimeImports = append(runtimeImports, path)
					}
				}
			}
			sort.Strings(runtimeImports)
			if !reflect.DeepEqual(runtimeImports, test.imports) {
				t.Fatalf("runtime imports=%v, want %v", runtimeImports, test.imports)
			}
			var wantTools []string
			if test.tools {
				wantTools = []string{standard}
			}
			if !reflect.DeepEqual(toolImports, wantTools) {
				t.Fatalf("generation-only imports=%v, want %v", toolImports, wantTools)
			}
			for _, arguments := range [][]string{{"build", "-o", filepath.Join(t.TempDir(), "app"), "."}, {"run", "."}} {
				command := exec.Command(cfg.Environment.GoTool, arguments...)
				command.Dir, command.Env = app, append(cfg.Environment.ExecEnv(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("go %s: %v\n%s", strings.Join(arguments, " "), err, output)
				}
				if arguments[0] == "run" && string(output) != test.output {
					t.Fatalf("output=%q, want %q", output, test.output)
				}
			}
			if !test.tools {
				if _, err := os.Stat(filepath.Join(app, "goxide_tools_gox.go")); !os.IsNotExist(err) {
					t.Fatalf("unused generation dependency file exists: %v", err)
				}
			}
		})
	}
}
