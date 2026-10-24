package compile

import (
	"bytes"
	"errors"
	"fmt"
	goast "go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/project"
)

// These are ordinary user protocols. Standard From identity for ? is tested
// separately against the compiler's published std dependency.
func TestParameterizedConversionModulesAndDependencyRecheck(t *testing.T) {
	compilerSDKForTest(t)
	run := testWASI(t, nil)
	root := t.TempDir()
	protocol, model := filepath.Join(root, "protocol"), filepath.Join(root, "model")
	business, consumer := filepath.Join(root, "business"), filepath.Join(root, "consumer")
	standard := pinnedStandardManifest(t)
	fixture := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", "conversion_protocol", name))
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		if filepath.Ext(name) == ".gox" {
			// Static lookup considers all reachable public traits. Keep this
			// author-owned method distinct from std::convert::From.from while
			// retaining unqualified input inference, including private sources.
			source = strings.NewReplacer("func from(", "func customFrom(", "::from", "::customFrom").Replace(source)
		}
		return source
	}
	writeProjectFile(t, protocol, project.ManifestName, `[go_mod]
module="example.org/conversion-protocol"
go="1.27"
[lib]
name="convert"
path="."
`+standard)
	writeProjectFile(t, protocol, "lib.gox", fixture("protocol.gox"))
	writeProjectFile(t, protocol, "facade/lib.gox", "package facade\npub use crate::From;\npub use crate::Into;\npub use crate::Convert;\n")
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
		command.Dir = protocol
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git fixture %v: %v\n%s", args, err, output)
		}
	}
	commit := func(tag string) {
		t.Helper()
		git("add", "goxide.toml", "lib.gox", "facade/lib.gox")
		git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "-qm", tag)
		git("tag", tag)
	}
	git("init", "-q")
	commit("v1.0.0")
	writeProjectFile(t, root, "native/go.mod", "module example.org/conversion-native\n\ngo 1.27\n")
	writeProjectFile(t, root, "native/native.go", fixture("native.go"))
	writeProjectFile(t, model, "lib.gox", fixture("model.gox"))
	writeProjectFile(t, business, "lib.gox", fixture("business.gox"))
	writeProjectFile(t, consumer, "main.go", fixture("consumer.go"))
	writeProjectFile(t, consumer, "main_test.go", fixture("consumer_test.go"))
	writeManifests := func(version string, reverse bool) {
		dependency := fmt.Sprintf("[dependencies.protocol]\ngit=%q\ntag=%q\n", protocol, version)
		for _, module := range []struct {
			directory, name string
			dependencies    []string
		}{
			{model, "model", []string{dependency, "[dependencies.native]\npath=\"../native\"\n", standard}},
			{business, "business", []string{dependency, "[dependencies.model]\npath=\"../model\"\n", standard}},
			{consumer, "consumer", []string{"[dependencies.model]\npath=\"../model\"\n", "[dependencies.business]\npath=\"../business\"\n", "[dependencies.native]\npath=\"../native\"\n"}},
		} {
			kind := "[lib]"
			if module.name == "consumer" {
				kind = "[[bin]]"
			}
			if reverse {
				slices.Reverse(module.dependencies)
			}
			manifest := fmt.Sprintf("[go_mod]\nmodule=\"example.org/conversion-%s\"\ngo=\"1.27\"\n%s\nname=%q\npath=\".\"\n", module.name, kind, module.name)
			writeProjectFile(t, module.directory, project.ManifestName, manifest+strings.Join(module.dependencies, ""))
		}
	}
	writeManifests("v1.0.0", false)
	// The Wrap[T] -> []T declaration must be checked for every T, including
	// unknown future types, before any downstream Ints or Texts demand exists.
	library, err := PrepareProject(model, Options{Lib: true, RunProcMacro: run})
	if err != nil {
		t.Fatalf("generic independent Into library: %v", err)
	}
	configuredGo(t, library, "build", "-mod=readonly", library.Entry)
	var symbols []string
	var generatedGo map[string][]byte
	for _, reversed := range []bool{false, true} {
		t.Run(fmt.Sprintf("reordered=%t", reversed), func(t *testing.T) {
			if reversed {
				writeProjectFile(t, protocol, "facade/lib.gox", "package facade\npub use crate::Convert;\npub use crate::Into;\npub use crate::From;\n")
				commit("v1.0.1")
				writeManifests("v1.0.1", true)
				for _, item := range []struct{ directory, name string }{{model, "model.gox"}, {business, "business.gox"}} {
					lines := strings.Split(fixture(item.name), "\n")
					end := 1
					for end < len(lines) && strings.HasPrefix(lines[end], "use ") {
						end++
					}
					slices.Reverse(lines[1:end])
					writeProjectFile(t, item.directory, "lib.gox", strings.Join(lines, "\n"))
				}
			}
			first, err := PrepareProject(consumer, Options{RunProcMacro: run})
			if err != nil {
				t.Fatal(err)
			}
			configuredGo(t, first, "build", "-mod=readonly", "-o", filepath.Join(root, "consumer-bin"), first.Entry)
			if output := configuredGo(t, first, "run", "-mod=readonly", first.Entry); output != "parameterized conversions, private types, views and call frames\n" {
				t.Fatalf("Go conversion consumer output = %q", output)
			}
			configuredGo(t, first, "test", "-mod=readonly", "-count=1", first.Entry)
			before := projectSnapshot(t, filepath.Dir(first.Root))
			currentGo := map[string][]byte{}
			for name, data := range before {
				if filepath.Ext(name) == ".go" {
					currentGo[name] = data
				}
			}
			if generatedGo == nil {
				generatedGo = currentGo
			} else {
				for name, data := range currentGo {
					if !bytes.Equal(generatedGo[name], data) {
						previous, current := bytes.Split(generatedGo[name], []byte("\n")), bytes.Split(data, []byte("\n"))
						for index := 0; index < min(len(previous), len(current)); index++ {
							if !bytes.Equal(previous[index], current[index]) {
								t.Fatalf("import, reexport or dependency ordering changed generated Go %s:%d:\nbefore: %s\nafter: %s", name, index+1, previous[index], current[index])
							}
						}
						t.Fatalf("reordering changed generated Go %s line count: %d -> %d", name, len(previous), len(current))
					}
				}
				if len(generatedGo) != len(currentGo) {
					t.Fatal("reordering changed generated Go file count")
				}
			}
			current := conversionDeclarationNames(t, before)
			if symbols == nil {
				symbols = current
			} else if !reflect.DeepEqual(symbols, current) {
				t.Fatalf("import, reexport or dependency ordering changed declaration identities:\nbefore=%v\nafter=%v", symbols, current)
			}
			lock := unchangedConfigLock(t, consumer, nil)
			second, err := PrepareProject(consumer, Options{Offline: true, Locked: true, RunProcMacro: run})
			if err != nil {
				t.Fatal(err)
			}
			if first.Root != second.Root || !reflect.DeepEqual(before, projectSnapshot(t, filepath.Dir(second.Root))) {
				t.Fatal("locked replay changed parameterized conversion artifacts")
			}
			unchangedConfigLock(t, consumer, lock)
		})
	}
	// This upstream blanket is a revalidation-requiring extension. It is
	// coherent in the protocol package, but overlaps model's independent Into.
	writeProjectFile(t, protocol, "lib.gox", fixture("protocol.gox")+`
impl[S any,T any] From[S] for []T where S:Elements[Item=T] {
    func customFrom(value S) Self { return value.elements() }
}
`)
	upgraded, err := PrepareProject(protocol, Options{Lib: true, Offline: true, RunProcMacro: run})
	if err != nil {
		t.Fatalf("upstream extension is not independently coherent: %v", err)
	}
	configuredGo(t, upgraded, "build", "-mod=readonly", upgraded.Entry)
	commit("v2.0.0")
	writeManifests("v2.0.0", true)
	rejected, err := PrepareProject(consumer, Options{RunProcMacro: run})
	var diagnostic check.Diagnostic
	if rejected != nil || !errors.As(err, &diagnostic) || diagnostic.Code != check.TraitInvalid || !strings.Contains(diagnostic.Message, "overlap") {
		t.Fatalf("dependency upgrade reused old negative evidence or changed implementation preference: %v, %v", rejected, err)
	}
}

func conversionDeclarationNames(t *testing.T, files map[string][]byte) []string {
	t.Helper()
	var names []string
	for name, data := range files {
		if filepath.Ext(name) != ".go" {
			continue
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, name, data, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			switch node := declaration.(type) {
			case *goast.FuncDecl:
				var receiver bytes.Buffer
				if node.Recv != nil {
					if err := format.Node(&receiver, set, node.Recv.List[0].Type); err != nil {
						t.Fatal(err)
					}
				}
				names = append(names, file.Name.Name+"."+receiver.String()+"."+node.Name.Name)
			case *goast.GenDecl:
				for _, spec := range node.Specs {
					if typ, ok := spec.(*goast.TypeSpec); ok {
						names = append(names, file.Name.Name+".type."+typ.Name.Name)
					}
				}
			}
		}
	}
	sort.Strings(names)
	return names
}
