package lower_test

import (
	"encoding/json"
	"fmt"
	"go/importer"
	"go/types"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

type noSnapshotImports struct{ calls int }

func (imports *noSnapshotImports) Import(name string) (*types.Package, error) {
	imports.calls++
	return nil, fmt.Errorf("snapshot must not import %s", name)
}

func TestProcLibrarySnapshotRoundTrip(t *testing.T) {
	_, producer, _ := compileProcFixture(t, procFixtureSource)
	snapshot, err := analysis.SnapshotProcLibrary(producer)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var restored analysis.ProcLibrarySnapshot
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(restored)
	if err != nil || string(second) != string(encoded) {
		t.Fatalf("snapshot JSON is unstable: %v", err)
	}
	imports := &noSnapshotImports{}
	host := procFixtureHost(pm.Entry{Name: "identity", Function: func(_ pm.Context, input public.TokenStream) pm.Output { return pm.Success(input) }})
	library, err := analysis.LoadProcLibrarySnapshot(restored, imports, host)
	if err != nil || imports.calls != 0 || library.Run == nil || !reflect.DeepEqual(library.GoNames, producer.GoNames) {
		t.Fatalf("load required SDK exports or lost registration: library=%v imports=%d error=%v", library, imports.calls, err)
	}
	// Loaded snapshots own their source bytes, maps and helper lists. A caller
	// changing the JSON DTO cannot affect later expansion or serialization.
	restored.Sources["macros.gox"][0] = '!'
	restored.Roots["sdkpm"] = paths.Namespace{Unavailable: "mutated"}
	restored.Entries[0].Helpers[0] = "mutated"
	again, err := analysis.SnapshotProcLibrary(library)
	if err != nil || !reflect.DeepEqual(snapshot, again) {
		t.Fatalf("snapshot aliases loader input: %v", err)
	}
	consumerImports := procFixtureImporter{packages: map[string]*types.Package{}, native: importer.Default()}
	library, err = analysis.LoadProcLibrarySnapshot(snapshot, consumerImports, host)
	if err != nil {
		t.Fatal(err)
	}
	options := lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}}
	output, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte("package main\nuse macros;\nfunc main(){println(macros::identity!(42))}\n")}, "consumer", consumerImports,
		paths.Roots{"macros": {PackagePath: library.PackagePath, ModulePath: library.PackagePath}}, options)
	if err != nil || len(output) != 1 || strings.Contains(string(output["main.gox"]), "github.com/goxide-lang/syntax") || strings.Contains(string(output["main.gox"]), "example.org/macros") {
		t.Fatalf("loaded macro did not expand without the SDK in consumer: %v\n%s", err, output["main.gox"])
	}
}

func TestProcLibrarySnapshotFreezesGoSugar(t *testing.T) {
	enabled := false
	cfg := testConfig(t)
	cfg.GoSugar = &enabled
	_, library, err := lower.CompileProcLibrary(map[string][]byte{"macros.gox": []byte(procFixtureSource)}, procFixturePackage, procFixtureSDK(t), procFixtureRoots(), lower.PackageOptions{Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	enabled = true
	snapshot, err := analysis.SnapshotProcLibrary(library)
	if err != nil || snapshot.GoSugar == nil || *snapshot.GoSugar {
		t.Fatalf("producer configuration was not frozen: %v, %v", snapshot.GoSugar, err)
	}
	loaded, err := analysis.LoadProcLibrarySnapshot(snapshot, &noSnapshotImports{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	*snapshot.GoSugar = true
	again, err := analysis.SnapshotProcLibrary(loaded)
	if err != nil || again.GoSugar == nil || *again.GoSugar {
		t.Fatalf("loaded configuration aliases snapshot: %v, %v", again.GoSugar, err)
	}
}

func TestProcLibrarySnapshotRejectsInconsistentMetadata(t *testing.T) {
	_, producer, _ := compileProcFixture(t, procFixtureSource)
	tests := map[string]func(*analysis.ProcLibrarySnapshot){
		"registration name":      func(s *analysis.ProcLibrarySnapshot) { s.Entries[0].Name = "Unknown" },
		"registration kind":      func(s *analysis.ProcLibrarySnapshot) { s.Entries[0].Kind = pm.Function },
		"registration helper":    func(s *analysis.ProcLibrarySnapshot) { s.Entries[0].Helpers = []string{"wrong"} },
		"registration function":  func(s *analysis.ProcLibrarySnapshot) { s.Entries[0].Function = "wrong" },
		"registration file":      func(s *analysis.ProcLibrarySnapshot) { s.Entries[0].File = "wrong.gox" },
		"registration count":     func(s *analysis.ProcLibrarySnapshot) { s.Entries = s.Entries[:1] },
		"duplicate registration": func(s *analysis.ProcLibrarySnapshot) { s.Entries[1] = s.Entries[0] },
		"private Go export":      func(s *analysis.ProcLibrarySnapshot) { s.Entries[0].GoName = "private" },
		"source registration": func(s *analysis.ProcLibrarySnapshot) {
			s.Sources["macros.gox"] = []byte(strings.Replace(procFixtureSource, "attributes(describe)", "attributes(changed)", 1))
		},
		"source syntax": func(s *analysis.ProcLibrarySnapshot) { s.Sources["macros.gox"] = []byte("package broken\nfunc (") },
		"source path":   func(s *analysis.ProcLibrarySnapshot) { s.Sources["../escape.gox"] = []byte("package macros") },
		"source limit": func(s *analysis.ProcLibrarySnapshot) {
			s.Sources["large.gox"] = []byte(strings.Repeat("x", 8*1024*1024))
		},
		"package path": func(s *analysis.ProcLibrarySnapshot) { s.PackagePath = "/host/producer" },
		"target":       func(s *analysis.ProcLibrarySnapshot) { s.Target.PointerWidth = 17 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot, err := analysis.SnapshotProcLibrary(producer)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&snapshot)
			imports := &noSnapshotImports{}
			if library, err := analysis.LoadProcLibrarySnapshot(snapshot, imports, nil); err == nil || library != nil || imports.calls != 0 {
				t.Fatalf("invalid snapshot accepted or imported SDK: library=%v imports=%d error=%v", library, imports.calls, err)
			}
		})
	}
}

func TestProcLibrarySnapshotRequiresExplicitEnvironmentIndependentSelection(t *testing.T) {
	configuration := testConfig(t)
	environment, err := config.NewEnvironment(configuration.Environment.Target, []string{"SNAPSHOT_CHOICE=secret"})
	if err != nil {
		t.Fatal(err)
	}
	configuration.Environment = environment
	source := strings.Replace(procFixtureSource, "#[proc_macro]", `#[cfg(env("SNAPSHOT_CHOICE"))]`+"\n#[proc_macro]", 1)
	_, producer, err := lower.CompileProcLibrary(map[string][]byte{"macros.gox": []byte(source)}, procFixturePackage, procFixtureSDK(t), procFixtureRoots(), lower.PackageOptions{Config: configuration})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := analysis.SnapshotProcLibrary(producer); err == nil || !strings.Contains(err.Error(), "environment variables") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("environment-dependent snapshot accepted or disclosed environment: %v", err)
	}
	if _, err := analysis.LoadProcLibrarySnapshot(analysis.ProcLibrarySnapshot{}, nil, nil); err == nil {
		t.Fatal("missing fixed importer accepted")
	}
	if _, err := analysis.SnapshotProcLibrary(&analysis.ProcLibrary{}); err == nil {
		t.Fatal("unchecked producer accepted")
	}
}
