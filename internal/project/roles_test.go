package project

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func roleManifest(module string, macro bool) string {
	return fmt.Sprintf("[go_mod]\nmodule=%q\ngo='1.27'\n[lib]\nname='library'\npath='.'\nproc_macro=%t\n[features]\nleft=[]\nright=[]\n", module, macro)
}

func loadRoles(t *testing.T, root string, locked bool) *BuildPlans {
	t.Helper()
	plans, err := LoadBuildPlans(root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, false, true, locked, "", "linux/amd64", false)
	if err != nil {
		t.Fatal(err)
	}
	return plans
}

func TestRolePlansSeparateFeaturesAndTopologicalMacros(t *testing.T) {
	root := sourceTempDir(t)
	wsWrite(t, root, ManifestName, roleManifest("example.org/app", false)+"[dependencies]\ncommon={path='common',features=['left']}\nmacros={path='macros',features=['left']}\nouter={path='outer'}\n")
	wsWrite(t, root, "common/"+ManifestName, roleManifest("example.org/common", false))
	wsWrite(t, root, "macros/"+ManifestName, roleManifest("example.org/macros", true)+"[dependencies]\ncommon={path='../common',features=['right']}\n")
	wsWrite(t, root, "outer/"+ManifestName, roleManifest("example.org/outer", true)+"[dependencies]\nupstream={path='../macros',features=['right']}\n")
	plans := loadRoles(t, root, false)
	if len(plans.Target.Nodes) != 2 || len(plans.Target.Plan) != 1 || len(plans.Target.ProcEdges) != 2 || len(plans.Macros) != 3 {
		t.Fatalf("target nodes=%d plan=%v edges=%v macros=%d", len(plans.Target.Nodes), plans.Target.Plan, plans.Target.ProcEdges, len(plans.Macros))
	}
	common := plans.Target.Root.Children["common"]
	if !common.Features["left"] || common.Features["right"] {
		t.Fatalf("macro features leaked into target: %v", common.Features)
	}
	seen := map[*Graph]bool{}
	for _, graph := range plans.Macros {
		if graph.Role != "proc-macro" || graph.Platform != "wasip1/wasm" || !strings.Contains(strings.Join(graph.Environment, "\n"), "CGO_ENABLED=0") {
			t.Fatalf("macro configuration: %+v", graph)
		}
		for _, edge := range graph.ProcEdges {
			if !seen[edge.Plan] {
				t.Fatal("macro prerequisite is not ordered first")
			}
		}
		seen[graph] = true
		if graph.Root.Module == "example.org/macros" {
			if child := graph.Root.Children["common"]; child.Features["left"] || !child.Features["right"] {
				t.Fatalf("target features leaked into macro: %v", child.Features)
			}
		}
	}
	left := plans.Target.Root.Children["macros"]
	right := plans.Target.Root.Children["outer"].Children["upstream"]
	if left == right || !left.Features["left"] || left.Features["right"] || right.Features["left"] || !right.Features["right"] {
		t.Fatalf("independent macro feature plans were merged: left=%v right=%v", left.Features, right.Features)
	}
}

func TestRolePlansLockReplayAndContentInvalidation(t *testing.T) {
	root := sourceTempDir(t)
	wsWrite(t, root, ManifestName, roleManifest("example.org/app", false)+"[dependencies.macros]\npath='macros'\n")
	wsWrite(t, root, "macros/"+ManifestName, roleManifest("example.org/macros", true))
	wsWrite(t, root, "macros/helper.gox", "package macros\nfunc Value() int { return 1 }\n")
	plans := loadRoles(t, root, false)
	if err := plans.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	again := loadRoles(t, root, true)
	if plans.Target.PlanID != again.Target.PlanID || plans.Macros[0].PlanID != again.Macros[0].PlanID {
		t.Fatal("unchanged lock replay changed artifact identity")
	}
	wsWrite(t, root, "macros/helper.gox", "package macros\nfunc Value() int { return 2 }\n")
	changed := loadRoles(t, root, true)
	if changed.Target.PlanID == plans.Target.PlanID || changed.Macros[0].PlanID == plans.Macros[0].PlanID {
		t.Fatal("macro source edit did not invalidate macro and consumer artifacts")
	}
	if changed.Target.DependencyPlanID != plans.Target.DependencyPlanID || changed.Macros[0].DependencyPlanID != plans.Macros[0].DependencyPlanID {
		t.Fatal("local source body changed dependency lock identity")
	}
	if err := changed.Save(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("locked replay changed lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "macros/goxide.lock")); !os.IsNotExist(err) {
		t.Fatal("macro producer received a separate lock")
	}
	wsWrite(t, root, "macros/"+ManifestName, roleManifest("example.org/macros", true)+"[dependencies.extra]\npath='../extra'\n")
	wsWrite(t, root, "extra/"+ManifestName, roleManifest("example.org/extra", false))
	_, err = LoadBuildPlans(root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, false, true, true, "", "linux/amd64", false)
	if err == nil || !strings.Contains(err.Error(), "--locked") {
		t.Fatalf("changed macro dependency accepted by --locked: %v", err)
	}
}

func TestRolePlansMacroCycle(t *testing.T) {
	root := sourceTempDir(t)
	wsWrite(t, root, ManifestName, roleManifest("example.org/app", false)+"[dependencies.a]\npath='a'\n")
	wsWrite(t, root, "a/"+ManifestName, roleManifest("example.org/a", true)+"[dependencies.b]\npath='../b'\n")
	wsWrite(t, root, "b/"+ManifestName, roleManifest("example.org/b", true)+"[dependencies.a]\npath='../a'\n")
	_, err := LoadBuildPlans(root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, false, true, false, "", "linux/amd64", false)
	if err == nil || !strings.Contains(err.Error(), "procedural macro build cycle") || !strings.Contains(err.Error(), "example.org/a") || !strings.Contains(err.Error(), "example.org/b") {
		t.Fatalf("macro cycle = %v", err)
	}
}

func TestRolePlansOwnMacroLibrary(t *testing.T) {
	root := sourceTempDir(t)
	wsWrite(t, root, ManifestName, roleManifest("example.org/macros", true))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, procRoot := range []bool{false, true} {
		plans, err := LoadBuildPlans(root, tool, nil, false, true, false, "", "linux/amd64", procRoot)
		if err != nil {
			t.Fatal(err)
		}
		if procRoot {
			if plans.Target.Role != "proc-macro" || plans.Target.Platform != "wasip1/wasm" || len(plans.Macros) != 0 || len(plans.Target.ProcEdges) != 0 {
				t.Fatal("proc root gained a self dependency")
			}
		} else if plans.Target.Role != "target" || plans.Target.Platform != "linux/amd64" || len(plans.Macros) != 1 || len(plans.Target.ProcEdges) != 1 || plans.Target.ProcEdges[0].Alias != "library" || plans.Target.Root.Children["library"] != plans.Macros[0].Root {
			t.Fatal("ordinary target did not bind its own separate macro library")
		}
	}
}

func roleProxyModule(t *testing.T, proxy, module, version, manifest, metadata string) {
	t.Helper()
	base := filepath.Join(proxy, filepath.FromSlash(module), "@v")
	wsWrite(t, base, version+".mod", metadata)
	wsWrite(t, base, version+".info", fmt.Sprintf("{\"Version\":%q,\"Time\":\"2026-01-01T00:00:00Z\"}", version))
	file, err := os.Create(filepath.Join(base, version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for _, input := range []struct{ name, body string }{{"go.mod", metadata}, {ManifestName, manifest}, {"lib.go", "package library\n"}} {
		if input.body == "" {
			continue
		}
		entry, err := archive.Create(module + "@" + version + "/" + input.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(input.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func roleProxyEnvironment(t *testing.T) string {
	t.Helper()
	proxy, cache := sourceTempDir(t), sourceTempDir(t)
	t.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxy))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", cache)
	t.Cleanup(func() {
		if err := filepath.WalkDir(cache, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				return os.Chmod(path, 0755)
			}
			return err
		}); err != nil {
			t.Error(err)
		}
	})
	return proxy
}

func TestRolePlansVersionedMacroDoesNotRaiseTargetSelection(t *testing.T) {
	root, proxy := sourceTempDir(t), roleProxyEnvironment(t)
	wsWrite(t, root, ManifestName, roleManifest("example.org/app", false)+"[dependencies]\ncommon={go='example.org/common',version='v1.0.0'}\nmacros={go='example.org/macros',version='v1.0.0'}\n")
	for _, version := range []string{"v1.0.0", "v1.1.0"} {
		roleProxyModule(t, proxy, "example.org/common", version, "", "module example.org/common\n\ngo 1.27\n")
	}
	macro := roleManifest("example.org/macros", true) + "[[go.require]]\nmodule='example.org/common'\nversion='v1.1.0'\n"
	roleProxyModule(t, proxy, "example.org/macros", "v1.0.0", macro, "module example.org/macros\n\ngo 1.27\n\nrequire example.org/common v1.1.0\n")
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	plans, err := LoadBuildPlans(root, tool, nil, false, false, false, "", "linux/amd64", false)
	if err != nil {
		t.Fatal(err)
	}
	if plans.Target.Plan["example.org/common"].Version != "v1.0.0" || len(plans.Target.Plan) != 1 || len(plans.Macros) != 1 || plans.Macros[0].Plan["example.org/common"].Version != "v1.1.0" {
		t.Fatalf("version isolation failed: target=%v macros=%v", plans.Target.Plan, plans.Macros)
	}
	if err := plans.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	var lock Lock
	if err := json.Unmarshal(before, &lock); err != nil || len(lock.Roles) != 2 {
		t.Fatalf("role lock records=%d error=%v", len(lock.Roles), err)
	}
	replay := loadRoles(t, root, true)
	if replay.Target.PlanID != plans.Target.PlanID || replay.Macros[0].PlanID != plans.Macros[0].PlanID {
		t.Fatal("offline locked replay changed versioned role identity")
	}
}

func TestRolePlansSameModuleMacroVersions(t *testing.T) {
	for _, version := range []string{"v1.0.0", "v1.1.0"} {
		t.Run(version, func(t *testing.T) {
			root, proxy := sourceTempDir(t), roleProxyEnvironment(t)
			wsWrite(t, root, ManifestName, roleManifest("example.org/app", false)+"[dependencies.macros]\ngo='example.org/macros'\nversion='v1.0.0'\n")
			macro := roleManifest("example.org/macros", true) + fmt.Sprintf("[dependencies.upstream]\ngo='example.org/macros'\nversion=%q\n", version)
			metadata := "module example.org/macros\n\ngo 1.27\n"
			roleProxyModule(t, proxy, "example.org/macros", "v1.0.0", macro, metadata)
			roleProxyModule(t, proxy, "example.org/macros", "v1.1.0", roleManifest("example.org/macros", true), metadata)
			plans, err := LoadBuildPlans(root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, false, false, false, "", "linux/amd64", false)
			if version == "v1.0.0" {
				if err == nil || !strings.Contains(err.Error(), "procedural macro build cycle") {
					t.Fatalf("same actual macro unit cycle: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(plans.Macros) != 2 || plans.Macros[0].Root.SelectedVersion != "v1.1.0" || plans.Macros[1].Root.SelectedVersion != "v1.0.0" {
				t.Fatalf("same logical module versions not independently ordered: %+v", plans.Macros)
			}
			if len(plans.Macros[1].ProcEdges) != 1 || plans.Macros[1].ProcEdges[0].Plan != plans.Macros[0] || len(plans.Target.Plan) != 0 || len(plans.Macros[1].Plan) != 0 {
				t.Fatal("upstream macro entered an ordinary Go build plan")
			}
			if err := plans.Save(); err != nil {
				t.Fatal(err)
			}
			replay := loadRoles(t, root, true)
			if replay.Target.PlanID != plans.Target.PlanID || replay.Macros[1].PlanID != plans.Macros[1].PlanID {
				t.Fatal("same-module role lock replay changed identity")
			}
		})
	}
}

func TestRolePlansAcquireOnlySelectedSourceVersion(t *testing.T) {
	for _, excluded := range []bool{false, true} {
		t.Run(fmt.Sprint(excluded), func(t *testing.T) {
			root, proxy := sourceTempDir(t), roleProxyEnvironment(t)
			// The unselected manifest minimum is deliberately unavailable.
			roleProxyModule(t, proxy, "example.org/common", "v1.1.0", "", "module example.org/common\n\ngo 1.27\n")
			manifest := roleManifest("example.org/app", false) + "[dependencies.common]\ngo='example.org/common'\nversion='v1.0.0'\n[[go.require]]\nmodule='example.org/common'\nversion='v1.1.0'\n"
			if excluded {
				manifest += "[[go.exclude]]\nmodule='example.org/common'\nversion='v1.0.0'\n"
			}
			wsWrite(t, root, ManifestName, manifest)
			plans, err := LoadBuildPlans(root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, false, false, false, "", "linux/amd64", false)
			if err != nil {
				t.Fatal(err)
			}
			if selected := plans.Target.Plan["example.org/common"].Version; selected != "v1.1.0" {
				t.Fatalf("selected version = %s", selected)
			}
		})
	}
}

func TestRolePlansRejectMacroMetadataSourceMismatch(t *testing.T) {
	root, proxy := sourceTempDir(t), roleProxyEnvironment(t)
	roleProxyModule(t, proxy, "example.org/macros", "v1.0.0", roleManifest("example.org/macros", true), "module example.org/macros\n\ngo 1.27\n")
	wsWrite(t, proxy, "example.org/macros/@v/v1.0.0.mod", "module example.org/macros\n\ngo 1.26\n")
	wsWrite(t, root, ManifestName, roleManifest("example.org/app", false)+"[dependencies.macros]\ngo='example.org/macros'\nversion='v1.0.0'\n")
	_, err := LoadBuildPlans(root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, false, false, false, "", "linux/amd64", false)
	if err == nil || !strings.Contains(err.Error(), "differs between selected metadata and verified source") {
		t.Fatalf("macro metadata/source mismatch = %v", err)
	}
}

func TestRolePlansIndependentGoAndReplacementPolicies(t *testing.T) {
	root := sourceTempDir(t)
	wsWrite(t, root, ManifestName, strings.Replace(roleManifest("example.org/app", false), "1.27", "1.26", 1)+"[dependencies]\ncommon={go='example.org/common',version='v1.0.0'}\nmacros={path='macros'}\n[[go.replace]]\nmodule='example.org/common'\npath='./target-common'\n")
	wsWrite(t, root, "target-common/go.mod", "module example.org/common\n\ngo 1.26\n")
	wsWrite(t, root, "macro-common/go.mod", "module example.org/common\n\ngo 1.27\n")
	wsWrite(t, root, "macros/"+ManifestName, roleManifest("example.org/macros", true)+"[dependencies.common]\ngo='example.org/common'\nversion='v1.0.0'\n[[go.replace]]\nmodule='example.org/common'\npath='../macro-common'\n")
	plans := loadRoles(t, root, false)
	if plans.Target.RootMod.Go != "1.26" || plans.Macros[0].RootMod.Go != "1.27" {
		t.Fatal("Go versions merged between roles")
	}
	if !strings.HasSuffix(plans.Target.Plan["example.org/common"].Replace.Path, "target-common") || !strings.HasSuffix(plans.Macros[0].Plan["example.org/common"].Replace.Path, "macro-common") {
		t.Fatal("replacement policies merged between roles")
	}
}
