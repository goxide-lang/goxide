package refs

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"testing"
)

func checkedPathPackage(t *testing.T, path, source string) *types.Package {
	t.Helper()
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "dependency.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (&types.Config{}).Check(path, fs, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestFinishPackagePathSeparatesImportedPrivateFields(t *testing.T) {
	// The dependency happens to occupy the legacy synthetic path. Its private
	// field must not acquire the consumer's identity during permission checks.
	dep := checkedPathPackage(t, "goxide.local/refs", `package dep
func Take(v struct{ hidden int }) int { return v.hidden }
`)
	ir, err := Prepare("consumer.gox", []byte(`package consumer
import dep "goxide.local/refs"
func f(r &int) int { return dep.Take(struct{ hidden int }{*r}) }
`))
	if err != nil {
		t.Fatal(err)
	}
	imp := metadataImporter{"goxide.local/refs": dep}
	for _, mode := range []string{"single", "package"} {
		t.Run(mode, func(t *testing.T) {
			var err error
			if mode == "single" {
				_, err = FinishWithPackagePath("example/consumer", "consumer.gox", ir.Source, imp)
			} else {
				_, err = FinishPackageWithPath("example/consumer", map[string][]byte{"consumer.gox": ir.Source}, imp)
			}
			if err == nil || !strings.Contains(err.Error(), "cannot use") {
				t.Fatalf("private field identities were merged: %v", err)
			}
		})
	}
}

func checkPathOutput(t *testing.T, path string, sources map[string][]byte, imp types.Importer) *types.Package {
	t.Helper()
	fs := token.NewFileSet()
	var files []*ast.File
	for _, name := range sourceNames(sources) {
		if bytes.Contains(sources[name], []byte(prefix)) {
			t.Fatalf("reference IR leaked in %s:\n%s", name, sources[name])
		}
		file, err := parser.ParseFile(fs, name, sources[name], 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	pkg, err := (&types.Config{Importer: imp}).Check(path, fs, files, nil)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestFinishPackagePathLocalAndImportedTypes(t *testing.T) {
	dep := checkedPathPackage(t, "example/model", `package model; type Node struct{ N int }`)
	imp := metadataImporter{"example/model": dep}
	ir, err := Prepare("consumer.gox", []byte(`package consumer
import model "example/model"
type Node struct { N int }
type Alias = Node
func read(r &Alias) int { return r.N }
func local() int { n:=Node{}; w:=&mut n; w.N=2; return read(&*w) }
func imported(r &model.Node) int { return r.N }
func external() int { n:=model.Node{}; w:=&mut n; w.N=3; return imported(&*w) }
`))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := Finish("consumer.gox", ir.Source, imp)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "goxide.local/refs", "example/consumer"} {
		t.Run(path, func(t *testing.T) {
			single, err := FinishWithPackagePath(path, "consumer.gox", ir.Source, imp)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(single.Source, legacy.Source) {
				t.Fatalf("path altered local/imported type spelling:\n%s", single.Source)
			}
			checkPathOutput(t, "example/consumer", map[string][]byte{"consumer.go": single.Source}, imp)
			group, err := FinishPackageWithPath(path, map[string][]byte{"consumer.gox": ir.Source}, imp)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(group["consumer.gox"].Source, single.Source) {
				t.Fatal("single and package finishing disagree")
			}
		})
	}
}

func TestFinishPackagePathAcrossFiles(t *testing.T) {
	files := map[string][]byte{}
	for name, source := range map[string]string{
		"a.gox": "package consumer\ntype Node struct{N int}\nfunc read(r &Node)int{return r.N}\n",
		"b.gox": "package consumer\nfunc f()int{n:=Node{};w:=&mut n;w.N=5;return read(&*w)}\n",
	} {
		ir, err := Prepare(name, []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = ir.Source
	}
	legacy, err := FinishPackage(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "example/consumer"} {
		result, err := FinishPackageWithPath(path, files, nil)
		if err != nil {
			t.Fatal(err)
		}
		output := map[string][]byte{}
		for name, r := range result {
			if !bytes.Equal(r.Source, legacy[name].Source) {
				t.Fatalf("path altered %s", name)
			}
			output[name] = r.Source
		}
		checkPathOutput(t, "example/consumer", output, nil)
	}
}

func TestBindImportsWithPackagePathKeepsNominalPermissions(t *testing.T) {
	meta := Metadata{Version: MetadataVersion, Path: "example/api", Functions: map[string]Signature{
		"View": {
			Params:  []Slot{{Type: "*example/api.Node", ElementAlias: "Node", Permission: Read}},
			Results: []Slot{{Type: "*example/api.Node", ElementAlias: "Node", Permission: Read}},
		},
	}}
	dep := checkedPathPackage(t, meta.Path, fmt.Sprintf(`package api
type Node struct { N int }
const %s = %q
func View(p *Node) *Node { return p }
`, MetadataMarker, meta.Marker()))
	imp := metadataImporter{meta.Path: dep}
	metadata := map[string]Metadata{meta.Path: meta}
	node := dep.Scope().Lookup("Node").Type()
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"read", "v:=api.Node{N:2}; p:=api.View(&v); return p.N", true},
		{"write", "v:=api.Node{N:2}; p:=api.View(&v); p.N++; return p.N", false},
		{"erase", "v:=api.Node{N:2}; var p *api.Node=api.View(&v); return p.N", false},
		{"nominal", "type Node struct{N int}; v:=Node{N:2}; p:=api.View(&v); return p.N", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string][]byte{"consumer.go": []byte("package consumer\nimport api \"example/api\"\nfunc f()int{" + tc.body + "}\n")}
			bound, bindings, err := BindImportsWithPath("example/consumer", files, imp, metadata)
			if err != nil {
				t.Fatal(err)
			}
			legacy, legacyBindings, err := BindImports(files, imp, metadata)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"", "example/consumer"} {
				out, gotBindings, err := BindImportsWithPath(path, files, imp, metadata)
				if err != nil || !reflect.DeepEqual(out, legacy) || !reflect.DeepEqual(gotBindings, legacyBindings) {
					t.Fatalf("binding defaults changed: %v", err)
				}
			}
			result, err := FinishWithPackagePath("example/consumer", "consumer.go", bound["consumer.go"], imp)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected permission result: %v", err)
			}
			if tc.valid {
				erased, err := bindings.Restore("consumer.go", result.Source)
				if err != nil {
					t.Fatal(err)
				}
				consumer := checkPathOutput(t, "example/consumer", map[string][]byte{"consumer.go": erased}, imp)
				if len(consumer.Imports()) != 1 || consumer.Imports()[0] != dep || dep.Scope().Lookup("Node").Type() != node {
					t.Fatal("imported package/type identity changed")
				}
			}
		})
	}
}
