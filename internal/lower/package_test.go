package lower_test

import (
	"bytes"
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/typecheck"
)

func TestCompilePackageNativeDeclarationsAndStableLines(t *testing.T) {
	const native = `package fixture
type Native struct { Value int }
func FromNative(value Native) Generated { return Generated{Value: value.Value + 1} }
`
	const source = `package fixture
// Generated is also used by handwritten Go.
type Generated struct { Value int }
func Make(value Native) Generated {
    return FromNative(value)
}
`
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		directory := t.TempDir()
		name := filepath.Join(directory, "types.gox")
		files := map[string][]byte{name: []byte(source), filepath.Join(directory, "native.go"): []byte(native)}
		output, err := lower.CompilePackage(files, "fixture", nil, nil, lower.PackageOptions{LineDirectives: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(output) != 1 || output[name] == nil {
			t.Fatalf("output must contain only the original .gox key: %v", output)
		}
		if string(files[name]) != source || string(files[filepath.Join(directory, "native.go")]) != native {
			t.Fatal("generation modified source bytes")
		}
		if !bytes.Contains(output[name], []byte("//line types.gox:")) || bytes.Contains(output[name], []byte(directory)) {
			t.Fatalf("line directives must use a relocatable basename:\n%s", output[name])
		}
		if attempt != 0 && !bytes.Equal(previous, output[name]) {
			t.Fatalf("generation differs across source directories:\n%s\n%s", previous, output[name])
		}
		previous = output[name]
		withoutLines, err := lower.CompilePackage(files, "fixture", nil, nil, lower.PackageOptions{})
		if err != nil || bytes.Contains(withoutLines[name], []byte("//line ")) {
			t.Fatalf("line directives must be opt-in: %v\n%s", err, withoutLines[name])
		}
		if attempt == 0 {
			writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, directory, "native.go", []byte(native))
			writeDiagnosticModuleFile(t, directory, "types_gox.go", output[name])
			writeDiagnosticModuleFile(t, directory, "types_test.go", []byte(`package fixture
import "testing"
func TestMake(t *testing.T) {
    if got := Make(Native{Value: 41}).Value; got != 42 { t.Fatal(got) }
}
`))
			runGo(t, directory, "test", "./...")
		}
	}
}

func TestCompilePackageGoDiagnosticLines(t *testing.T) {
	for _, test := range []struct {
		name, source, definitions string
		line                      int
	}{
		{"ordinary", "package fixture\n\n// Keep this comment.\nfunc Broken() {\n    unused := 1\n}\n", "", 5},
		{"macro-definition", "package fixture\nmacro_rules! unused {\n    () => {\n        unused := 1\n    };\n}\nfunc Broken() { unused!() }\n", "", 4},
		{"macro-capture", "package fixture\nmacro_rules! emit { ($body:expr) => { $body }; }\nfunc Broken() {\n    emit!(func() { unused := 1 }())\n}\n", "", 4},
		{"macro-other-file", "package fixture\nfunc Broken() {\n    unused!()\n}\n", "package fixture\nmacro_rules! unused {\n    () => {\n        unused := 1\n    };\n}\n", 3},
		{"nested-macro-other-file", "package fixture\nmacro_rules! relay { () => { unused!() }; }\nfunc Broken() {\n    relay!()\n}\n", "package fixture\nmacro_rules! unused {\n    () => {\n        unused := 1\n    };\n}\n", 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			name := filepath.Join(directory, "broken.gox")
			sources := map[string][]byte{name: []byte(test.source)}
			if test.definitions != "" {
				sources[filepath.Join(directory, "macros.gox")] = []byte(test.definitions)
			}
			output, err := lower.CompilePackage(sources, "fixture", nil, nil, lower.PackageOptions{LineDirectives: true})
			if err != nil {
				t.Fatal(err)
			}
			writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
			for source, data := range output {
				target := strings.TrimSuffix(filepath.Base(source), ".gox") + "_gox.go"
				writeDiagnosticModuleFile(t, directory, target, data)
			}
			command := exec.Command("go", "build", ".")
			command.Dir = directory
			command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
			diagnostic, err := command.CombinedOutput()
			location := fmt.Sprintf("broken.gox:%d:", test.line)
			if err == nil || !bytes.Contains(diagnostic, []byte(location)) || !bytes.Contains(diagnostic, []byte("declared and not used")) {
				t.Fatalf("expected unused declaration at %s: %v\n%s\ngenerated:\n%s", location, err, diagnostic, output[name])
			}
			if regexp.MustCompile(`broken\.gox:\d+:\d+:`).Match(diagnostic) {
				t.Fatalf("line-only mapping unexpectedly includes a column: %s", diagnostic)
			}
		})
	}
}

func TestCompilePackageGeneratedConstructLines(t *testing.T) {
	const source = `package fixture
enum State {
    Empty,
    Ready(Order),
}

func Summary(state State) string {
    return match state {
        State::Ready(order) => DescribeOrder(order),
        State::Empty => "empty",
    }
}
`
	output, err := lower.CompilePackage(map[string][]byte{
		"order.gox": []byte(source),
		"native.go": []byte("package fixture\ntype Order struct { ID int }\nfunc DescribeOrder(Order) string { return \"ready\" }\n"),
	}, "fixture", nil, nil, lower.PackageOptions{LineDirectives: true})
	if err != nil {
		t.Fatal(err)
	}
	set := gotoken.NewFileSet()
	file, err := goparser.ParseFile(set, "order_gox.go", output["order.gox"], 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"_State": 2, "_Empty": 3, "_Ready": 4}
	seenEmptyArm := false
	goast.Inspect(file, func(node goast.Node) bool {
		switch node := node.(type) {
		case *goast.TypeSpec:
			if line, ok := want[node.Name.Name]; ok {
				if got := set.Position(node.Pos()).Line; got != line {
					t.Errorf("%s declaration line = %d, want %d", node.Name.Name, got, line)
				}
				delete(want, node.Name.Name)
			}
		case *goast.AssignStmt:
			if len(node.Rhs) == 1 {
				if literal, ok := node.Rhs[0].(*goast.BasicLit); ok && literal.Value == `"empty"` {
					seenEmptyArm = true
					if got := set.Position(node.Pos()).Line; got != 10 {
						t.Errorf("Empty arm assignment line = %d, want 10", got)
					}
				}
			}
		}
		return true
	})
	if len(want) != 0 || !seenEmptyArm {
		t.Fatalf("missing generated declarations or match arm: %v, arm=%v\n%s", want, seenEmptyArm, output["order.gox"])
	}
}

func TestCompilePackageVetDiagnosticLine(t *testing.T) {
	for _, test := range []struct {
		name, source string
		line         int
	}{
		{"ordinary", "package fixture\nuse fmt;\n\nfunc Broken() {\n    fmt::Printf(\"%d\", \"text\")\n}\n", 5},
		{"match-arm", "package fixture\nuse fmt;\nenum State { Empty, Ready }\n\nfunc Broken(state State) string {\n    return match state {\n        State::Empty => \"empty\",\n        State::Ready => fmt::Sprintf(\"%d\", \"text\"),\n    }\n}\n", 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			name := filepath.Join(directory, "format.gox")
			writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
			output, err := lower.CompilePackage(map[string][]byte{name: []byte(test.source)}, "fixture", typecheck.NewImporter(name, typecheck.ImporterOptions{Config: testConfig(t)}), paths.Roots{"fmt": {PackagePath: "fmt", Source: "toolchain"}}, lower.PackageOptions{LineDirectives: true})
			if err != nil {
				t.Fatal(err)
			}
			writeDiagnosticModuleFile(t, directory, "format_gox.go", output[name])
			command := exec.Command("go", "vet", ".")
			command.Dir = directory
			command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
			diagnostic, err := command.CombinedOutput()
			location := fmt.Sprintf("format.gox:%d:", test.line)
			if err == nil || !bytes.Contains(diagnostic, []byte(location)) || !bytes.Contains(diagnostic, []byte("wrong type string")) {
				t.Fatalf("expected printf vet diagnostic at %s: %v\n%s\ngenerated:\n%s", location, err, diagnostic, output[name])
			}
		})
	}
}
