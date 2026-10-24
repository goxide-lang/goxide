package compile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/project"
)

func TestRuntimeCapabilitiesDoNotPromoteNativeSubpackages(t *testing.T) {
	const module = "example.org/runtime"
	whole := paths.Namespace{ModulePath: module, PackagePath: module, GoPrefix: module, Source: "module"}
	child := paths.Namespace{ModulePath: module, PackagePath: module + "/assignment", GoPrefix: module + "/assignment", Source: "module"}
	library := paths.Namespace{ModulePath: module, PackagePath: module + "/src", Source: "module"}
	for _, tc := range []struct {
		name      string
		roots     paths.Roots
		requested string
		want      []paths.Namespace
	}{
		{"whole_and_subpackage", paths.Roots{"std": whole, "runtime": child}, "", []paths.Namespace{whole}},
		{"subpackage_only", paths.Roots{"runtime": child}, "", nil},
		{"package_only_subpackage", paths.Roots{"runtime": {ModulePath: module, PackagePath: module + "/assignment"}}, "", nil},
		{"package_only_whole", paths.Roots{"std": {ModulePath: module, PackagePath: module}}, "", []paths.Namespace{{ModulePath: module, PackagePath: module}}},
		{"go_tree_without_root_package", paths.Roots{"std": {ModulePath: module, GoPrefix: module}}, "", []paths.Namespace{{ModulePath: module, GoPrefix: module}}},
		{"declared_library_and_subpackage", paths.Roots{"api": library, "runtime": child}, "api", []paths.Namespace{library}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graph := &project.Graph{PlanID: "selected-runtime-content", Plan: map[string]project.Selection{module: {}}}
			node := &project.Node{Requests: map[string]project.Request{}}
			if tc.requested != "" {
				node.Requests[tc.requested] = project.Request{}
			}
			var namespaces []paths.Namespace
			for _, root := range runtimeRoots(graph, node, tc.roots, "") {
				if root.LogicalModule != module || root.BindingID == "" {
					t.Fatalf("runtime capability lost its selected module identity: %#v", root)
				}
				namespaces = append(namespaces, root.Namespace)
			}
			if !reflect.DeepEqual(namespaces, tc.want) {
				t.Fatalf("runtime capabilities = %#v, want %#v", namespaces, tc.want)
			}
		})
	}
}

func TestRuntimeCapabilityRetainsDeclaredLibrarySubdirectory(t *testing.T) {
	sdk := compilerSDKForTest(t)
	base := t.TempDir()
	app, macros, library := filepath.Join(base, "app"), filepath.Join(base, "macros"), filepath.Join(base, "runtime")
	writeProjectFile(t, macros, project.ManifestName, fmt.Sprintf(`[go_mod]
module="example.org/runtime-root-macros"
go="1.27"
[lib]
name="macros"
path="."
proc_macro=true
[dependencies]
syntax={path=%q}
`, filepath.ToSlash(sdk)))
	writeProjectFile(t, macros, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro]
pub func value(ctx pm::Context, input token::TokenStream) pm::Output {
 root,err:=ctx.RuntimeRoot("example.org/runtime-root-library")
 if err!=nil{return pm::Failure(err.Error(),ctx.CallSpan())}
 tail,err:=token::ParseText("::Value()",ctx.DefSite())
 if err!=nil{return pm::Failure(err.Error(),ctx.CallSpan())}
 return pm::Success(root.Append(tail.Tokens()...))
}
`)
	writeProjectFile(t, library, project.ManifestName, `[go_mod]
module="example.org/runtime-root-library"
go="1.27"
[lib]
name="library"
path="src"
`)
	writeProjectFile(t, library, "src/lib.gox", "package library\npub func Value()int{return 42}\n")
	writeProjectFile(t, app, project.ManifestName, `[go_mod]
module="example.org/runtime-root-app"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies]
api={path="../runtime"}
macros={path="../macros"}
fmt={go="fmt"}
`)
	writeProjectFile(t, app, "main.gox", "package main\nuse macros;use fmt;\nfunc main(){fmt::Println(macros::value!())}\n")
	writeProjectFile(t, app, "main_test.go", "package main\nimport \"testing\"\nfunc TestGenerated(t *testing.T){main()}\n")
	run := testWASI(t, nil)
	first, err := PrepareProject(app, Options{Offline: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(first.Root, "main.go"))
	if err != nil || !bytes.Contains(generated, []byte(`"example.org/runtime-root-library/src"`)) {
		t.Fatalf("declared library root did not retain its package: %v\n%s", err, generated)
	}
	configuredGo(t, first, "build", "-mod=readonly", "-o", filepath.Join(base, "program"), first.Entry)
	if got := configuredGo(t, first, "run", "-mod=readonly", first.Entry); got != "42\n" {
		t.Fatalf("declared library runtime output = %q", got)
	}
	configuredGo(t, first, "test", "-mod=readonly", "-count=1", first.Entry)
	before := projectSnapshot(t, filepath.Dir(first.Root))
	second, err := PrepareProject(app, Options{Offline: true, Locked: true, RunProcMacro: run})
	if err != nil || first.Root != second.Root || !reflect.DeepEqual(before, projectSnapshot(t, filepath.Dir(second.Root))) {
		t.Fatalf("locked replay changed declared library runtime output: %v", err)
	}
}
