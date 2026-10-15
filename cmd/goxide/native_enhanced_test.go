package main

import (
	"github.com/hxsf/goxide/internal/project"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeWorkspaceProgressiveFeatures(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI workspace features, vendor and build scripts deferred by P6")
	root := nativeWorkspaceFixture(t)
	dest := filepath.Join(t.TempDir(), "new")
	if out, e := importCall(t, root, "--workspace", "--out", dest, "--offline"); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	lib := filepath.Join(dest, "members/m1")
	if e := os.Remove(filepath.Join(lib, "lib.go")); e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, lib, "lib.gox", "package lib\npub enum E { A, B }\nfunc Value()string{if cfg!(feature(\"fancy\")){return \"enhanced\"};return \"plain\"}\n")
	b, e := os.ReadFile(filepath.Join(lib, "goxide.toml"))
	if e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, lib, "goxide.toml", string(b)+"\n[[go.require]]\nmodule='github.com/goxide-lang/std'\nversion='v0.0.0-20261009104839-c02a09d8c417'\n[features]\nfancy=[]\n")
	app := filepath.Join(dest, "members/m0")
	b, e = os.ReadFile(filepath.Join(app, "goxide.toml"))
	if e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, app, "goxide.toml", string(b)+"\n[features]\ndefault=[\"member1/fancy\"]\n")
	first, e := projectRun(t, dest, "build", "-p", "example.org/app", "--offline")
	if e != nil {
		t.Fatalf("%s %v", first, e)
	}
	configs, e := filepath.Glob(filepath.Join(dest, "target/units/*/*/bin/app/*/bundle/members/m1/goxide_project_config.go"))
	if e != nil || len(configs) != 1 {
		t.Fatalf("generated configurations: %v %v", configs, e)
	}
	before, e := os.Stat(configs[0])
	if e != nil {
		t.Fatal(e)
	}
	second, e := projectRun(t, dest, "build", "-p", "example.org/app", "--offline", "--locked")
	if e != nil || first != second {
		t.Fatalf("unstable native output: %s / %s %v", first, second, e)
	}
	after, e := os.Stat(configs[0])
	if e != nil || before.ModTime() != after.ModTime() {
		t.Fatalf("native configuration rewritten after checksum completion: %v", e)
	}
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "-p", "example.org/app", "--offline"}, "enhanced"},
		{[]string{"run", "-p", "example.org/app", "--no-default-features", "--locked", "--offline"}, "plain"},
		{[]string{"run", "-p", "example.org/app", "--locked", "--offline"}, "enhanced"},
	} {
		out, e := projectRun(t, dest, test.args...)
		if e != nil || !strings.Contains(out, test.want) {
			t.Fatalf("%v: %s %v", test.args, out, e)
		}
	}
	if out, e := projectRun(t, dest, "test", "--workspace", "--locked", "--offline"); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	if _, e := projectRun(t, dest, "build", "-p", "example.org/app", "--features", "unknown", "--offline"); e == nil {
		t.Fatal("unknown feature accepted")
	}
}

func TestNativeWorkspaceProgressiveVendor(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI workspace features, vendor and build scripts deferred by P6")
	root := nativeWorkspaceFixture(t)
	workspaceWrite(t, root, "dep/go.mod", "module example.org/dep\ngo 1.27\n")
	workspaceWrite(t, root, "dep/dep.go", "package dep\nfunc Value()string{return \"original\"}\n")
	workspaceWrite(t, root, "lib/go.mod", "module example.org/lib\ngo 1.27\nrequire example.org/dep v1.0.0\nreplace example.org/dep => ../dep\n")
	workspaceWrite(t, root, "lib/lib.go", "package lib\nimport \"example.org/dep\"\nfunc Value()string{return dep.Value()}\n")
	nativeFixtureCommand(t, root, "work", "vendor")
	workspaceWrite(t, root, "vendor/example.org/dep/dep.go", "package dep\nfunc Value()string{return \"modified\"}\n")
	os.RemoveAll(filepath.Join(root, "dep"))
	t.Setenv("GOMODCACHE", t.TempDir())
	dest := filepath.Join(t.TempDir(), "new")
	if out, e := importCall(t, root, "--workspace", "--out", dest, "--offline"); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	lib := filepath.Join(dest, "members/m1")
	os.Remove(filepath.Join(lib, "lib.go"))
	workspaceWrite(t, lib, "lib.gox", "package lib\nuse dep;\nmacro_rules! suffix { () => { \" enhanced\" }; }\nfunc Value()string{return dep::Value()+suffix!()}\n")
	manifest, err := os.ReadFile(filepath.Join(lib, "goxide.toml"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceWrite(t, lib, "goxide.toml", string(manifest)+"\n[dependencies.dep]\ngo = 'example.org/dep'\nversion = 'v1.0.0' \npackage='example.org/dep'\n")
	if out, e := projectRun(t, dest, "run", "-p", "example.org/app", "--offline"); e != nil {
		t.Fatalf("refresh explicit roots: %s %v", out, e)
	}
	out, e := projectRun(t, dest, "run", "-p", "example.org/app", "--offline", "--locked")
	if e != nil || !strings.Contains(out, "modified enhanced") {
		t.Fatalf("%s %v", out, e)
	}
	b, e := os.ReadFile(filepath.Join(dest, "local-modules", project.Digest("example.org/dep")[:24], "dep.go"))
	if e != nil || !strings.Contains(string(b), "modified") {
		t.Fatal("vendor modified by lowering", e)
	}
}

func TestNativeWorkspaceProgressiveScriptPermissions(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI workspace features, vendor and build scripts deferred by P6")
	t.Setenv("GOXIDE_TRUST_DIR", t.TempDir())
	root := nativeWorkspaceFixture(t)
	dest := filepath.Join(t.TempDir(), "new")
	if out, e := importCall(t, root, "--workspace", "--out", dest, "--offline"); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	lib := filepath.Join(dest, "members/m1")
	app := filepath.Join(dest, "members/m0")
	b, e := os.ReadFile(filepath.Join(lib, "goxide.toml"))
	if e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, lib, "goxide.toml", string(b)+"\n[package.namespaces]\ngen='example.org/lib/gen'\n[build]\nentry='tools/build/main.go'\noutputs=['gen/data.go']\n")
	workspaceWrite(t, lib, "tools/build/main.go", `package main
import("encoding/json";"fmt";"os";"path/filepath")
func main(){var q struct{OutDir string `+"`json:\"out_dir\"`"+`};b,e:=os.ReadFile(os.Args[1]);if e!=nil{panic(e)};if e=json.Unmarshal(b,&q);e!=nil{panic(e)};if e=os.MkdirAll(filepath.Join(q.OutDir,"gen"),0755);e!=nil{panic(e)};if e=os.WriteFile(filepath.Join(q.OutDir,"gen/data.go"),[]byte("package gen\nconst Value=\"script-output\"\n"),0644);e!=nil{panic(e)};fmt.Println("NATIVE_SCRIPT_EXECUTED")}
`)
	os.Remove(filepath.Join(lib, "lib.go"))
	workspaceWrite(t, lib, "lib.gox", "package lib\nuse gen;\nfunc Value()string{return gen::Value}\n")
	scriptAction(t, app, "trust-project")
	plan := scriptPlan(t, app)
	if len(plan.Subjects) != 1 {
		t.Fatalf("expected exact dependency grant: %+v", plan)
	}
	out, e := projectRun(t, dest, "run", "-p", "example.org/app", "--offline")
	if e == nil || strings.Contains(out, "NATIVE_SCRIPT_EXECUTED") {
		t.Fatalf("ungranted script executed: %s %v", out, e)
	}
	scriptAction(t, app, "allow", "--subject", plan.Subjects[0].ID)
	for i := 0; i < 2; i++ {
		out, e = projectRun(t, dest, "run", "-p", "example.org/app", "--offline", "--locked")
		if e != nil || !strings.Contains(out, "script-output") {
			t.Fatalf("%s %v", out, e)
		}
	}
	scriptAction(t, app, "revoke", "--subject", plan.Subjects[0].ID)
	out, e = projectRun(t, dest, "run", "-p", "example.org/app", "--offline", "--locked")
	if e == nil || strings.Contains(out, "script-output") {
		t.Fatalf("revoked script reused: %s %v", out, e)
	}
}

func TestNativeWorkspaceOptionalCfg(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI workspace features, vendor and build scripts deferred by P6")
	root := nativeWorkspaceFixture(t)
	dest := filepath.Join(t.TempDir(), "new")
	if out, e := importCall(t, root, "--workspace", "--out", dest, "--offline"); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	app := filepath.Join(dest, "members/m0")
	m, e := project.Read(app)
	if e != nil {
		t.Fatal(e)
	}
	// Materialize the existing template source before making this edge optional.
	w, e := project.DiscoverWorkspace(dest)
	if e != nil {
		t.Fatal(e)
	}
	m = w.Manifests[app]
	d := m.Dependencies["member1"]
	d.Optional = true
	d.Package = "example.org/lib"
	m.Dependencies["member1"] = d
	m.Dependencies["fmt"] = project.Dependency{Go: "fmt", Package: "fmt"}
	m.Features = map[string][]string{"default": {"use_lib"}, "use_lib": {"dep:member1"}, "never": {}}
	b, e := project.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, app, "goxide.toml", string(b))
	os.Remove(filepath.Join(app, "main.go"))
	workspaceWrite(t, app, "main.gox", "package main\nuse fmt;\nfunc main(){fmt::Println(value())}\n")
	workspaceWrite(t, app, "on.gox", "//goxide:cfg feature(\"use_lib\")\npackage main\nuse member1 as lib;\nfunc value()string{return lib::Value()}\n")
	workspaceWrite(t, app, "off.gox", "//goxide:cfg not(feature(\"use_lib\"))\npackage main\nfunc value()string{return \"inactive\"}\n")
	workspaceWrite(t, app, "never.gox", "//goxide:cfg feature(\"never\")\npackage main\nuse no;\nfunc Never()Unknown{return missing()}\n")
	out, e := projectRun(t, dest, "run", "-p", "example.org/app", "--offline")
	if e != nil || !strings.Contains(out, "work") {
		t.Fatalf("%s %v", out, e)
	}
	workspaceWrite(t, dest, "members/m1/lib.go", "invalid unused member source")
	out, e = projectRun(t, dest, "run", "-p", "example.org/app", "--no-default-features", "--locked", "--offline")
	if e != nil || !strings.Contains(out, "inactive") {
		t.Fatalf("inactive member parsed: %s %v", out, e)
	}
	workspaceWrite(t, app, "never.gox", "//goxide:cfg feature(\"never\")\npackage main\nfunc Broken(){x:=(}\n")
	if out, e = projectRun(t, dest, "build", "-p", "example.org/app", "--no-default-features", "--locked", "--offline"); e == nil {
		t.Fatalf("inactive unbalanced token tree accepted: %s", out)
	}
}

func TestNativeWorkspaceDependencyRequiredFeature(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI workspace features, vendor and build scripts deferred by P6")
	root := nativeWorkspaceFixture(t)
	dest := filepath.Join(t.TempDir(), "new")
	if out, e := importCall(t, root, "--workspace", "--out", dest, "--offline"); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	lib := filepath.Join(dest, "members/m1")
	m, e := project.Read(lib)
	if e != nil {
		t.Fatal(e)
	}
	m.Features = map[string][]string{"gate": {}}
	m.Lib.RequiredFeatures = []string{"gate"}
	b, e := project.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, lib, "goxide.toml", string(b))
	if out, e := projectRun(t, dest, "run", "-p", "example.org/app", "--offline"); e == nil || !strings.Contains(e.Error(), "requires feature gate") {
		t.Fatalf("unqualified dependency: %s %v", out, e)
	}
}
