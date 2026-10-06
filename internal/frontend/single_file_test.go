package frontend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
	"github.com/hxsf/goxide/internal/testdeps"
	"github.com/hxsf/goxide/internal/typecheck"
)

func TestSingleFileRegisteredImports(t *testing.T) {
	write := func(root, name, source string) {
		t.Helper()
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := testdeps.WriteFile(file, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	producer, library, consumer := t.TempDir(), filepath.Join(t.TempDir(), "lib"), t.TempDir()
	write(producer, "go.mod", "module file.example/lib\ngo 1.27\n")
	write(producer, "model.gox", `package actualmodel
#[derive(Default,Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
type Item struct{N int}
pub enum Event{Idle,Count(int),}
type O=Option[int]
func Some()O{return Option[int]::Some(7)}
func View(v &int)&int{return v}
`)
	write(producer, "relay/relay.go", `package differentname
import p "file.example/lib"
type Item=p.Item
type Event=p.Event
type O=p.O
type New p.Item
`)
	if _, err := CompileModule(producer, library); err != nil {
		t.Fatal(err)
	}
	write(consumer, "go.mod", fmt.Sprintf("module file.example/app\ngo 1.27\nrequire file.example/lib v0.0.0\nreplace file.example/lib => %s\n", filepath.ToSlash(library)))
	file := filepath.Join(consumer, "input.gox")
	compileFile := func(source string) (Result, error) {
		return CompileWithOptions(file, []byte(source), FileOptions{PackagePath: "file.example/app", Importer: typecheck.NewImporter(file)})
	}
	for name, source := range map[string]string{
		"registered-protocol-alias": `package app
import p "file.example/lib/relay"
type Local=p.Item
type Level=Local
#[derive(Default,Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
type Envelope struct{Item Level}
`,
		"enum-and-sum": `package app
import p "file.example/lib/relay"
func Count(e p.Event)int{return match e{p.Event::Idle=>0,p.Event::Count(n)=>n}}
func Make()p.Event{return p.Event::Count(3)}
func Some()p.O{return p.O::Some(4)}
func Read(o p.O)p.O{n:=o?;return p.O::Some(n)}
`,
	} {
		for _, inputName := range []string{"input.gox", "input.go", "input"} {
			t.Run(name+"/"+inputName, func(t *testing.T) {
				inputFile := filepath.Join(consumer, inputName)
				result, err := CompileWithOptions(inputFile, []byte(source), FileOptions{PackagePath: "file.example/app", Importer: typecheck.NewImporter(inputFile)})
				if err != nil {
					t.Fatal(err)
				}
				if err := typecheck.CheckPackage("file.example/app", map[string][]byte{inputFile: result.Go}, typecheck.NewImporter(inputFile)); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(result.Go), "const "+enumMetadataMarker) {
					t.Fatal("single file published enum artifact marker")
				}
			})
		}
	}
	t.Run("new-defined-does-not-inherit", func(t *testing.T) {
		_, err := compileFile("package app\nimport p \"file.example/lib/relay\"\n#[derive(Default)]\ntype Bad struct{V p.New}\n")
		if err == nil || !strings.Contains(err.Error(), "Default") {
			t.Fatal(err)
		}
	})
	t.Run("readonly-not-erased", func(t *testing.T) {
		_, err := compileFile("package app\nimport p \"file.example/lib\"\nfunc Bad(){n:=1;v:=p.View(&n);*v=2}\n")
		if err == nil || !strings.Contains(err.Error(), "readonly") {
			t.Fatal(err)
		}
	})
	t.Run("ordinary-api-still-verifies-marker", func(t *testing.T) {
		artifact := filepath.Join(library, enumMetadataFilename)
		saved, err := os.ReadFile(artifact)
		if err != nil {
			t.Fatal(err)
		}
		defer os.WriteFile(artifact, saved, 0600)
		if err := os.WriteFile(artifact, []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err = compileFile("package app\nimport \"file.example/lib\"\nfunc Read()actualmodel.Item{return actualmodel.DefaultItem()}\n")
		if err == nil || !strings.Contains(err.Error(), ":2:8:") || !strings.Contains(err.Error(), "metadata") {
			t.Fatal(err)
		}
	})
}

func TestSingleFileModePolicies(t *testing.T) {
	file := filepath.Join(t.TempDir(), "input.gox")
	imp := typecheck.NewImporter(file)
	options := FileOptions{PackagePath: "file.example/app", Importer: imp}
	for name, source := range map[string]string{
		"non-pub-local-enum":           "package app\nenum Event{A,B,}\nfunc F()Event{return Event::A}\n",
		"conditional-native-typecheck": "package app\nfunc F()int{return missingSibling()}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(filepath.Dir(file), "bad_sibling.go"), []byte("invalid sibling"), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := CompileWithOptions(file, []byte(source), options)
			if err != nil || len(result.Go) == 0 {
				t.Fatal(err)
			}
			if strings.Contains(string(result.Go), "const "+enumMetadataMarker) {
				t.Fatal("single-file enum publication changed")
			}
		})
	}
	t.Run("filename-does-not-select-directory-mode", func(t *testing.T) {
		for _, name := range []string{"input.go", "input"} {
			result, err := CompileWithOptions(filepath.Join(filepath.Dir(file), name), []byte("package app\nenum Event{A,B,}\nfunc F()Event{return Event::A}\n"), options)
			if err != nil || !strings.Contains(string(result.Go), "func F() Event") {
				t.Errorf("%s: language lowering bypassed: %v\n%s", name, err, result.Go)
			}
		}
	})
	t.Run("configured-owner-requires-importer", func(t *testing.T) {
		_, err := CompileWithOptions(file, []byte("package app"), FileOptions{PackagePath: "file.example/app"})
		if err == nil || !strings.Contains(err.Error(), "module importer") {
			t.Fatal(err)
		}
	})
	t.Run("use-remains-directory-only", func(t *testing.T) {
		_, err := CompileWithOptions(file, []byte("package app\nuse p::Value;\n"), options)
		if err == nil || !strings.Contains(err.Error(), "use/pub use requires") {
			t.Fatal(err)
		}
	})
	t.Run("imported-use-remains-directory-only", func(t *testing.T) {
		path := "file.example/uses"
		pkg := enumTestPackage(t, path, "package uses;const GoxideUseMetadata=\"marker\";const Value=1", nil)
		_, err := CompileWithOptions(file, []byte("package app\nimport p \""+path+"\"\nfunc F()int{return p.Value}"), FileOptions{PackagePath: options.PackagePath, Importer: enumTestImporter{path: pkg}})
		if err == nil || !strings.Contains(err.Error(), "use metadata require") {
			t.Fatal(err)
		}
	})
	t.Run("actual-internal-owner", func(t *testing.T) {
		path := "file.example/lib/internal/native"
		pkg := enumTestPackage(t, path, "package native;const Value=1", nil)
		source := []byte("package app\nimport p \"" + path + "\"\nfunc F()int{return p.Value}")
		for _, owner := range []string{"file.example/lib/app", "file.example/other"} {
			_, err := CompileWithOptions(file, source, FileOptions{PackagePath: owner, Importer: enumTestImporter{path: pkg}})
			if owner == "file.example/lib/app" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), ":2:8: internal package") {
				t.Fatal(err)
			}
		}
	})
}

func TestSingleFileKnownOwnerMacroOrigins(t *testing.T) {
	dir := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module file.example/app\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "origins.gox")
	for _, newline := range []string{"\n", "\r\n"} {
		for _, expression := range []string{"len($x?)", "($x?).Missing"} {
			t.Run(fmt.Sprintf("%q/%s", newline, expression), func(t *testing.T) {
				source := strings.ReplaceAll("package p\nmacro_rules! inner {($x:expr)=>{"+expression+"};}\nmacro_rules! outer {($x:expr)=>{inner!($x)};}\nfunc value()Option[int]{return Option[int]::Some(1)}\nfunc f()Option[int]{_=outer!(value());return Option[int]::None}\n", "\n", newline)
				_, err := CompileWithOptions(file, []byte(source), FileOptions{PackagePath: "file.example/app", Importer: typecheck.NewImporter(file)})
				var d *sourcemap.Error
				if !errors.As(err, &d) || len(d.Stack) != 2 {
					t.Fatalf("known-owner mode lost complete macro frames: %v", err)
				}
				if expression == "len($x?)" {
					if d.Origin != nil {
						t.Fatalf("synthetic argument acquired a source point: %+v", d.Origin)
					}
				} else if d.Origin == nil || d.Origin.File != file || source[d.Origin.Start:d.Origin.End] != "Missing" {
					t.Fatalf("exact selector token lost source identity: %v", err)
				}
				for i, name := range []string{"outer", "inner"} {
					if d.Stack[i].Macro != name || d.Stack[i].Definition.File != file || d.Stack[i].Callsite.File != file {
						t.Fatalf("wrong macro owner: %+v", d.Stack)
					}
					checkMacroFrame(t, d.Stack[i].Definition, source, "macro_rules! "+name)
					checkMacroFrame(t, d.Stack[i].Callsite, source, name+"!(")
				}
			})
		}
	}
}
