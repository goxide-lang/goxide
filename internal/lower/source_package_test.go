package lower_test

import (
	"go/importer"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
)

func TestSourcePackageSnapshotKeepsPublicBoundaries(t *testing.T) {
	cfg := testConfig(t)
	root := paths.Namespace{PackagePath: "example.org/lib", ModulePath: "example.org/lib"}
	makePackage := func(pkg, source string) lower.SourcePackage {
		return lower.SourcePackage{PackagePath: pkg, ModulePath: root.ModulePath, RootPackage: root.PackagePath, Target: cfg.Environment.Target, Roots: paths.Roots{"crate": root}, Sources: map[string][]byte{"lib.gox": []byte(source)}}
	}
	packages := []lower.SourcePackage{
		makePackage(root.PackagePath, "package lib\nmod hidden;\npub func Value()int{return hidden::Value()}\n"),
		makePackage(root.PackagePath+"/hidden", "package hidden\npub func Value()int{return 42}\n"),
	}
	for _, test := range []struct{ name, source, native, want string }{
		{"public", "package main\nuse api;\nfunc main(){_=api::Value()}\n", "", ""},
		{"private source", "package main\nuse api;\nfunc main(){_=api::hidden::Value()}\n", "", "unresolved"},
		{"private native", "package main\nfunc main(){}\n", "package main\nimport hidden \"example.org/lib/hidden\"\nvar _=hidden.Value\n", "not publicly reachable"},
		{"private blank import", "package main\nfunc main(){}\n", "package main\nimport _ \"example.org/lib/hidden\"\n", "not publicly reachable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources := map[string][]byte{"main.gox": []byte(test.source)}
			if test.native != "" {
				sources["native.go"] = []byte(test.native)
			}
			expanded := map[string][]byte{}
			output, err := lower.CompilePackage(sources, "consumer", importer.Default(), paths.Roots{"api": root}, lower.PackageOptions{Config: cfg, SourcePackages: packages, Expanded: expanded})
			if test.want != "" {
				if err == nil || output != nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("expected %q, output=%v error=%v", test.want, output, err)
				}
				return
			}
			if err != nil || len(output) != 1 || len(expanded) != 1 || output["main.gox"] == nil || expanded["main.gox"] == nil {
				t.Fatalf("output=%v expanded=%v error=%v", output, expanded, err)
			}
		})
	}
}

func TestSourceSnapshotMarksGeneratedEnvironmentSelection(t *testing.T) {
	cfg := testConfig(t)
	environment, err := config.NewEnvironment(cfg.Environment.Target, []string{"SOURCE_SNAPSHOT_ON=yes"})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Environment = environment
	root := t.TempDir()
	app, dependency := filepath.Join(root, "app"), filepath.Join(root, "dep")
	consumer := []byte("package main\nuse dep;\nfunc main(){_=dep::Value()}\n")
	writeDiagnosticModuleFile(t, app, "main.gox", consumer)
	writeDiagnosticModuleFile(t, dependency, "lib.gox", []byte("package dep\nmacro_rules! exported { () => { #[cfg(env(\"SOURCE_SNAPSHOT_ON\"))] pub func Value()int{return 42} }; }\nexported!()\n"))
	roots := paths.Roots{"dep": {PackagePath: "example.org/dep", ModulePath: "example.org/dep"}}
	var snapshots []lower.SourcePackage
	err = lower.CompileModules([]lower.Module{{Path: "consumer", Dir: app, Roots: roots, Config: cfg, OnSourcePackages: func(sources []lower.SourcePackage) error { snapshots = sources; return nil }}, {Path: "example.org/dep", Dir: dependency, RootPackage: "example.org/dep", Config: cfg}})
	if err != nil {
		t.Fatalf("live compilation lost captured environment: %v", err)
	}
	if len(snapshots) != 1 || !snapshots[0].EnvironmentDependent {
		t.Fatalf("snapshot did not record environment dependence: %+v", snapshots)
	}
	output, err := lower.CompilePackage(map[string][]byte{"main.gox": consumer}, "consumer", importer.Default(), roots, lower.PackageOptions{Config: cfg, SourcePackages: snapshots})
	if err == nil || output != nil || !strings.Contains(err.Error(), "depends on environment variables") {
		t.Fatalf("snapshot silently changed configuration: %v, output=%v", err, output)
	}
}
