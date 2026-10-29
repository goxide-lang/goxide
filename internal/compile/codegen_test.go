package compile

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
)

func TestProjectGoSugarPoliciesAndStableLock(t *testing.T) {
	const appManifest = "[go_mod]\nmodule='example.org/sugar/app'\ngo='1.27'\n[[bin]]\nname='app'\npath='.'\n[dependencies]\ndep={path='../dep'}\n"
	const depManifest = "[go_mod]\nmodule='example.org/sugar/dep'\ngo='1.27'\n[lib]\nname='dep'\npath='.'\n"
	root := featureProject(t, map[string]string{
		"app/goxide.toml": appManifest,
		"app/main.gox":    "package main\nuse dep;\nfunc main(){if dep::Value()!=42 {panic(\"dependency\")}}\n",
		"dep/goxide.toml": depManifest,
		"dep/lib.gox":     "package dep\npub func Value()int{return 42}\n",
	})
	app := filepath.Join(root, "app")
	var lock []byte
	paths := map[string]string{}
	on, off := true, false
	for _, test := range []struct {
		name, rootPolicy, depPolicy string
		override                    *bool
		rootValue, depValue         bool
	}{
		{"default", "", "", nil, true, true},
		{"manifest off", "false", "", nil, false, true},
		{"CLI enables root", "false", "", &on, true, true},
		{"CLI disables root", "true", "", &off, false, true},
		{"dependency disables itself", "false", "false", &on, true, false},
		{"CLI does not enable dependency", "true", "false", &on, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			writePolicy := func(directory, manifest, value string) {
				if value != "" {
					manifest += "[codegen]\ngo_sugar=" + value + "\n"
				}
				writeProjectFile(t, root, directory+"/goxide.toml", manifest)
			}
			writePolicy("app", appManifest, test.rootPolicy)
			writePolicy("dep", depManifest, test.depPolicy)
			var snapshots []analysis.SourcePackage
			result, err := PrepareProject(app, Options{Offline: true, Locked: lock != nil, GoSugar: test.override, OnPreparedSources: func(sources PreparedSources) error {
				snapshots = sources.SourcePackages
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Config.GoSugarEnabled() != test.rootValue {
				t.Fatalf("root policy = %v, want %v", result.Config.GoSugarEnabled(), test.rootValue)
			}
			if len(snapshots) != 1 || snapshots[0].GoSugar == nil || *snapshots[0].GoSugar != test.depValue {
				t.Fatalf("dependency snapshot policy = %+v, want %v", snapshots, test.depValue)
			}
			identity := fmt.Sprintf("%t/%t", test.rootValue, test.depValue)
			if previous := paths[identity]; previous != "" && previous != result.Root {
				t.Fatal("same effective module policies changed artifact identity")
			}
			for other, previous := range paths {
				if other != identity && previous == result.Root {
					t.Fatal("different module policies shared artifact identity")
				}
			}
			paths[identity] = result.Root
			lock = unchangedConfigLock(t, app, lock)
			if test.name == "default" {
				configuredGo(t, result, "run", "-mod=readonly", result.Entry)
			}
		})
	}
}
