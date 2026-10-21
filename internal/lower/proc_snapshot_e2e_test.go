//go:build !js && !race

package lower_test

import (
	"context"
	"encoding/json"
	"fmt"
	"go/importer"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// Run actual WASI guests without race instrumentation so validation/startup uses
// the unchanged five-second production budget. Snapshot admission is race-tested
// independently in proc_snapshot_test.go.
func TestProcSnapshotPreparedRoleLifetime(t *testing.T) {
	sdk := strings.TrimSpace(runGo(t, ".", "list", "-m", "-f", "{{.Dir}}", "github.com/goxide-lang/syntax"))
	identity, err := procmacro.ReadSDKIdentity(context.Background(), filepath.Join(runtime.GOROOT(), "bin", "go"), ".", project.GoEnv(true))
	if err != nil {
		t.Fatal(err)
	}
	previous := procmacro.CompilerSDKIdentity
	procmacro.CompilerSDKIdentity = identity
	t.Cleanup(func() { procmacro.CompilerSDKIdentity = previous })
	directory := t.TempDir()
	upstream, producer, app := filepath.Join(directory, "upstream"), filepath.Join(directory, "producer"), filepath.Join(directory, "app")
	writeManifest := func(dir, module, dependencies string) {
		t.Helper()
		writeDiagnosticModuleFile(t, dir, "goxide.toml", []byte(fmt.Sprintf(`[go_mod]
module=%q
go="1.27"
[lib]
name="macros"
path="."
proc_macro=true
[dependencies]
syntax={path=%q}
%s
`, module, filepath.ToSlash(sdk), dependencies)))
	}
	writeManifest(upstream, "example.org/upstream", "")
	writeDiagnosticModuleFile(t, upstream, "lib.gox", []byte(`package upstream
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro]
pub func identity(ctx pm::Context, input token::TokenStream) pm::Output { return pm::Success(input) }
`))
	writeManifest(producer, "example.org/macros", `upstream={path="../upstream"}`)
	writeDiagnosticModuleFile(t, producer, "lib.gox", []byte(`package macros
use syntax::token;
use syntax::proc_macro as pm;
use upstream;
#[proc_macro]
pub func identity(ctx pm::Context, input token::TokenStream) pm::Output {
 return upstream::identity!(pm::Success(input))
}
`))
	writeDiagnosticModuleFile(t, app, "goxide.toml", []byte(`[go_mod]
module="example.org/consumer"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies]
macros={path="../producer"}
`))
	source := []byte("package main\nuse macros;\nfunc main(){println(macros::identity!(42))}\n")
	writeDiagnosticModuleFile(t, app, "main.gox", source)
	host, err := procmacro.NewWASIHost(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	type artifact struct {
		Snapshot lower.ProcLibrarySnapshot
		WASM     []byte
	}
	var snapshots []artifact
	var sources compile.PreparedSources
	_, err = compile.PrepareProject(app, compile.Options{Offline: true, RunProcMacro: host.Run, OnPreparedSources: func(prepared compile.PreparedSources) error {
		sources = prepared
		for _, macro := range prepared.Macros {
			snapshot, err := lower.SnapshotProcLibrary(macro.Library)
			if err != nil {
				return err
			}
			// The upstream procedural role is a source capability, never a Go
			// module tree in its consumer producer's selected runtime graph.
			for _, tree := range snapshot.GoTrees {
				if tree.Module == "example.org/upstream" {
					return fmt.Errorf("procedural dependency was indexed as runtime Go")
				}
			}
			snapshots = append(snapshots, artifact{snapshot, append([]byte(nil), macro.WASM...)})
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || len(sources.Macros) != 2 {
		t.Fatalf("missing upstream macro role: snapshots=%d macros=%d", len(snapshots), len(sources.Macros))
	}
	// PrepareProject has removed its staging directories. Only the frozen JSON
	// and matching executables remain available to this in-memory consumer.
	encoded, err := json.Marshal(snapshots)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), directory) || strings.Contains(string(encoded), ".generation-") {
		t.Fatal("snapshot retained a staging path")
	}
	snapshots = nil
	if err := json.Unmarshal(encoded, &snapshots); err != nil {
		t.Fatal(err)
	}
	consumerImports := importer.Default()
	var libraries []*lower.ProcLibrary
	for _, macro := range snapshots {
		library, err := lower.LoadProcLibrarySnapshot(macro.Snapshot, consumerImports, func(request pm.Request) ([]byte, error) {
			return host.Run(macro.WASM, request)
		})
		if err != nil {
			t.Fatal(err)
		}
		libraries = append(libraries, library)
	}
	options := lower.PackageOptions{Config: testConfig(t), ProcLibraries: libraries, SourcePackages: sources.SourcePackages, RuntimeRoots: sources.RuntimeRoots}
	output, err := lower.CompilePackage(map[string][]byte{"main.gox": source}, "example.org/consumer", consumerImports, sources.Roots, options)
	if err != nil {
		t.Fatal(err)
	}
	again, err := lower.CompilePackage(map[string][]byte{"main.gox": source}, "example.org/consumer", consumerImports, sources.Roots, options)
	if err != nil || !reflect.DeepEqual(output, again) || len(output) != 1 {
		t.Fatalf("loaded role snapshots changed generated bytes: %v", err)
	}
	generated := filepath.Join(directory, "generated")
	writeDiagnosticModuleFile(t, generated, "go.mod", []byte("module example.org/consumer\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, generated, "main.go", output["main.gox"])
	if got := runGo(t, generated, "run", "."); got != "42\n" {
		t.Fatalf("loaded producer with upstream dependency = %q", got)
	}
}
