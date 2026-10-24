package compile

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
)

func TestProjectGoSugarChangesNativeMethodSetsAndReplays(t *testing.T) {
	compilerSDKForTest(t)
	run := testWASI(t, nil)
	module, err := project.ReadMod(filepath.Join(runtime.GOROOT(), "bin", "go"), filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var stdVersion string
	for _, dependency := range module.Require {
		if dependency.Path == "github.com/goxide-lang/std" {
			stdVersion = dependency.Version
		}
	}
	if stdVersion == "" {
		t.Fatal("compiler go.mod must pin std")
	}
	standard := fmt.Sprintf("[dependencies.standard]\ngo='github.com/goxide-lang/std'\nversion=%q\n", stdVersion)
	appManifest := "[go_mod]\nmodule='example.org/sugar-behavior/app'\ngo='1.27'\n[[bin]]\nname='app'\npath='.'\n[dependencies.dep]\npath='../dep'\n" + standard
	depManifest := "[go_mod]\nmodule='example.org/sugar-behavior/dep'\ngo='1.27'\n[lib]\nname='dep'\npath='.'\n" + standard
	root := featureProject(t, map[string]string{
		"app/goxide.toml": appManifest,
		"dep/goxide.toml": depManifest,
		"app/model.gox": `package main
pub struct Local { pub N int }
pub trait Observe { func inspect(self &Self) int }
impl Observe for Local { func inspect(self &Self) int { return self.N } }
`,
		"dep/model.gox": `package dep
pub struct Remote { pub N int }
pub trait Observe { func inspect(self &Self) int }
impl Observe for Remote { func inspect(self &Self) int { return self.N } }
`,
		"app/main.go": `package main
import (
 "fmt"
 dep "example.org/sugar-behavior/dep"
)
type inspector interface { Inspect() int }
func inspect() (bool, bool, int, int) {
 local, remote := Local{N: 7}, dep.Remote{N: 9}
 localSugar, hasLocal := any(&local).(inspector)
 remoteSugar, hasRemote := any(&remote).(inspector)
 if hasLocal && localSugar.Inspect() != 7 { panic("root sugar used the wrong receiver") }
 if hasRemote && remoteSugar.Inspect() != 9 { panic("dependency sugar used the wrong receiver") }
 return hasLocal, hasRemote, local.AsObserve().Inspect(), remote.AsObserve().Inspect()
}
func main() { fmt.Println(inspect()) }
`,
		"app/main_test.go": `package main
import "testing"
func TestNativeProtocolViews(t *testing.T) {
 _, _, local, remote := inspect()
 if local != 7 || remote != 9 { t.Fatalf("protocol views = %d %d", local, remote) }
}
`,
	})
	app := filepath.Join(root, "app")
	on, off := true, false
	type generation struct {
		root  string
		files map[string][]byte
	}
	seen := map[string]generation{}
	var lock []byte
	for _, test := range []struct {
		name, rootPolicy, depPolicy string
		override                    *bool
		rootSugar, depSugar         bool
	}{
		{"default", "", "", nil, true, true},
		{"root manifest off", "false", "", nil, false, true},
		{"CLI enables only root", "false", "false", &on, true, false},
		{"CLI disables root", "true", "false", &off, false, false},
		{"replay root on dependency off", "false", "false", &on, true, false},
		{"replay default", "", "", nil, true, true},
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
			result, err := PrepareProject(app, Options{Offline: true, Locked: lock != nil, GoSugar: test.override, RunProcMacro: run})
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("%t %t 7 9\n", test.rootSugar, test.depSugar)
			configuredGo(t, result, "build", "-mod=readonly", "-o", filepath.Join(root, "consumer"), result.Entry)
			if got := configuredGo(t, result, "run", "-mod=readonly", result.Entry); got != want {
				t.Fatalf("native method sets and protocol views = %q, want %q", got, want)
			}
			configuredGo(t, result, "test", "-mod=readonly", "-count=1", result.Entry)
			identity := fmt.Sprintf("%t/%t", test.rootSugar, test.depSugar)
			files := projectSnapshot(t, filepath.Dir(result.Root))
			if prior, ok := seen[identity]; ok {
				if prior.root != result.Root || !reflect.DeepEqual(prior.files, files) {
					t.Fatal("replaying module sugar policies changed the artifact path or bytes")
				}
			} else {
				for other, prior := range seen {
					if prior.root == result.Root {
						t.Fatalf("different method sets %s and %s reused an artifact path", other, identity)
					}
				}
				seen[identity] = generation{result.Root, files}
			}
			lock = unchangedConfigLock(t, app, lock)
		})
	}
}
