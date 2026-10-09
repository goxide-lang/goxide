package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These generators and dependencies are authored here; no downloaded tool is executed.
func buildScriptFixture(t *testing.T, goxide bool, helper bool) string {
	t.Helper()
	t.Setenv("GOXIDE_TRUST_DIR", t.TempDir())
	t.Setenv("MODE", "alpha")
	root := filepath.Join(t.TempDir(), "root")
	entry := "tools/build/main.go"
	if goxide {
		entry = "tools/build/main.gox"
	}
	build := map[string]any{"entry": entry, "inputs": []string{"schema.txt", "templates"}, "env": []string{"MODE"}, "outputs": []string{"gen/schema.go", "gen/data.txt"}, "timeout_seconds": 60}
	extraImport, extraValue := "", `"local"`
	if helper {
		build["dependencies"] = map[string]string{"example.org/helper": "../helper"}
		extraImport, extraValue = `"example.org/helper"`, "helper.Value()"
		projectWrite(t, filepath.Join(filepath.Dir(root), "helper"), map[string]string{
			"go.mod": "module example.org/helper\n\ngo 1.27\n",
			"goxide.toml": `[lib]
name = 'helper'
path = 'lib.gox'

[package]
format = 1
`,
			"lib.gox": "package helper\nfunc Value()string{return \"helper-one\"}\n",
		})
	}
	manifest := map[string]any{"version": 1, "build": build, "features": map[string]any{"extra": []string{}}, "bins": []any{map[string]string{"name": "one", "path": "cmd/one"}, map[string]string{"name": "two", "path": "cmd/two"}}}
	generator := fmt.Sprintf(`package main
import("encoding/json";"fmt";"os";"path/filepath";"strings"; %s)
func main(){
 var q struct { Version int; SourceDir string `+"`json:\"source_dir\"`"+`; OutDir string `+"`json:\"out_dir\"`"+`; HostOS string `+"`json:\"host_os\"`"+`; HostArch string `+"`json:\"host_arch\"`"+`; TargetOS string `+"`json:\"target_os\"`"+`; TargetArch string `+"`json:\"target_arch\"`"+`; Features []string }
 b,e:=os.ReadFile(os.Args[1]);if e!=nil{panic(e)};if e=json.Unmarshal(b,&q);e!=nil{panic(e)}
 if q.Version!=1 || q.HostOS=="" || q.HostArch=="" || q.TargetOS=="" || q.TargetArch=="" {panic("invalid host/target request")}
 schema,e:=os.ReadFile(filepath.Join(q.SourceDir,"schema.txt"));if e!=nil{panic(e)}
 template,e:=os.ReadFile(filepath.Join(q.SourceDir,"templates","value.txt"));if e!=nil{panic(e)}
 value:=string(schema)+"/"+string(template)+"/"+os.Getenv("MODE")+"/"+strings.Join(q.Features,",")+"/"+%s
 if e=os.MkdirAll(filepath.Join(q.OutDir,"gen"),0755);e!=nil{panic(e)}
 code:=fmt.Sprintf("package gen\nimport _ \"embed\"\n//go:embed data.txt\nvar Resource string\nconst Value = %%q\n",value)
 if e=os.WriteFile(filepath.Join(q.OutDir,"gen","schema.go"),[]byte(code),0644);e!=nil{panic(e)}
 if e=os.WriteFile(filepath.Join(q.OutDir,"gen","data.txt"),[]byte("embedded-resource"),0644);e!=nil{panic(e)}
 fmt.Println("FIXTURE_GENERATOR_EXECUTED")
 fmt.Println("FIXTURE_HOST_TARGET",q.HostOS,q.HostArch,q.TargetOS,q.TargetArch)
}
`, extraImport, extraValue)
	if goxide {
		generator = strings.Replace(generator, `import("encoding/json";"fmt";"os";"path/filepath";"strings"; `+extraImport+`)`, "use json;\nuse fmt;\nuse os;\nuse filepath;\nuse strings;", 1)
		for _, name := range []string{"json", "fmt", "os", "filepath", "strings"} {
			generator = strings.ReplaceAll(generator, name+".", name+"::")
		}
		if helper {
			// Explicit native host bridge keeps the helper execution/permission
			// contract without granting target module roots to Goxide bootstrap.
			generator = strings.Replace(generator, "helper.Value()", "helperValue()", 1)
			projectWrite(t, root, map[string]string{"tools/build/helper.go": "package main\nimport \"example.org/helper\"\nfunc helperValue()string{return helper.Value()}\n"})
		}
	}
	projectWrite(t, root, map[string]string{
		"go.mod": "module example.org/generated\n\ngo 1.27\n", "goxide.toml": projectManifest(manifest), entry: generator,
		"schema.txt": "schema-one", "templates/value.txt": "template-one",
		"cmd/one/main.gox": "package main\nuse fmt;\nuse gen;\nfunc main(){fmt::Println(\"APP_ONE\",gen::Value,gen::Resource)}\n",
		"cmd/two/main.gox": "package main\nuse fmt;\nuse gen;\nfunc main(){fmt::Println(\"APP_TWO\",gen::Value,gen::Resource)}\n",
	})
	return root
}

func scriptAction(t *testing.T, root, action string, args ...string) string {
	t.Helper()
	argv := append([]string{"scripts", "--action", action}, args...)
	out, e := projectRun(t, root, argv...)
	if e != nil {
		t.Fatalf("scripts %s: %s: %v", action, out, e)
	}
	return out
}
func scriptPlan(t *testing.T, root string) struct {
	RootIdentity string `json:"root_identity"`
	RootTrusted  bool   `json:"root_trusted"`
	Subjects     []struct {
		ID      string `json:"id"`
		Trusted bool   `json:"trusted"`
	} `json:"subjects"`
} {
	t.Helper()
	var p struct {
		RootIdentity string `json:"root_identity"`
		RootTrusted  bool   `json:"root_trusted"`
		Subjects     []struct {
			ID      string `json:"id"`
			Trusted bool   `json:"trusted"`
		} `json:"subjects"`
	}
	if e := json.Unmarshal([]byte(scriptAction(t, root, "plan")), &p); e != nil {
		t.Fatal(e)
	}
	if p.RootIdentity == "" {
		t.Fatal("plan omitted root identity")
	}
	return p
}
func scriptRun(t *testing.T, root, bin, state, want string, args ...string) string {
	t.Helper()
	argv := append([]string{"run", "--offline", "--bin", bin}, args...)
	out, e := projectRun(t, root, argv...)
	if e != nil || !strings.Contains(out, want) || !strings.Contains(out, "build-script "+state) {
		t.Fatalf("%v: wanted %q and script %s; output=%s err=%v", argv, want, state, out, e)
	}
	return out
}

func TestBuildScriptGenerationAndIncrementalCLI(t *testing.T) {
	root := buildScriptFixture(t, false, false)
	p := scriptPlan(t, root)
	if p.RootTrusted {
		t.Fatal("fresh project unexpectedly trusted")
	}
	out, e := projectRun(t, root, "run", "--offline", "--bin", "one")
	if e == nil || strings.Contains(out, "FIXTURE_GENERATOR_EXECUTED") {
		t.Fatalf("untrusted script ran: %s %v", out, e)
	}
	scriptAction(t, root, "trust-project")
	if !scriptPlan(t, root).RootTrusted {
		t.Fatal("root trust not persisted")
	}
	out = scriptRun(t, root, "one", "run", "APP_ONE schema-one/template-one/alpha//local embedded-resource")
	if !strings.Contains(out, "unconfined") {
		t.Fatalf("missing execution boundary diagnostic: %s", out)
	}
	scriptRun(t, root, "one", "cache hit", "APP_ONE")
	scriptRun(t, root, "two", "cache hit", "APP_TWO")
	projectWrite(t, root, map[string]string{"schema.txt": "schema-two"})
	scriptRun(t, root, "one", "run", "schema-two/template-one/alpha//local")
	projectWrite(t, root, map[string]string{"templates/value.txt": "template-two"})
	scriptRun(t, root, "one", "run", "schema-two/template-two/alpha//local")
	t.Setenv("MODE", "beta")
	scriptRun(t, root, "one", "run", "schema-two/template-two/beta//local")
	scriptRun(t, root, "one", "run", "schema-two/template-two/beta/extra/local", "--features", "extra")
	scriptRun(t, root, "two", "cache hit", "APP_TWO", "--features", "extra")
	entry := filepath.Join(root, "tools/build/main.go")
	source, e := os.ReadFile(entry)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(entry, append(source, []byte("\n// root source edit remains authorized\n")...), 0644); e != nil {
		t.Fatal(e)
	}
	scriptRun(t, root, "one", "run", "APP_ONE", "--features", "extra")
	scriptAction(t, root, "forget-project")
	out, e = projectRun(t, root, "run", "--offline", "--bin", "one", "--features", "extra")
	if e == nil || strings.Contains(out, "APP_ONE") {
		t.Fatalf("forgotten trust reused artifact: %s %v", out, e)
	}
}

func TestBuildScriptGoxideHelperTrustCLI(t *testing.T) {
	root := buildScriptFixture(t, true, true)
	scriptAction(t, root, "trust-project")
	p := scriptPlan(t, root)
	if len(p.Subjects) == 0 {
		t.Fatal("helper execution closure missing from plan")
	}
	out, e := projectRun(t, root, "run", "--offline", "--bin", "one")
	if e == nil || strings.Contains(out, "FIXTURE_GENERATOR_EXECUTED") {
		t.Fatalf("helper ran before authorization: %s %v", out, e)
	}
	for _, s := range p.Subjects {
		scriptAction(t, root, "allow", "--subject", s.ID)
	}
	scriptRun(t, root, "one", "run", "/helper-one embedded-resource")
	scriptRun(t, root, "two", "cache hit", "/helper-one embedded-resource")
	scriptAction(t, root, "revoke", "--subject", p.Subjects[0].ID)
	out, e = projectRun(t, root, "run", "--offline", "--bin", "one")
	if e == nil || strings.Contains(out, "APP_ONE") {
		t.Fatalf("revoked closure reused cache: %s %v", out, e)
	}
	for _, s := range p.Subjects {
		scriptAction(t, root, "allow", "--subject", s.ID)
	}
	projectWrite(t, filepath.Join(filepath.Dir(root), "helper"), map[string]string{"lib.gox": "package helper\nfunc Value()string{return \"helper-two\"}\n"})
	changed := scriptPlan(t, root)
	fresh := false
	for _, s := range changed.Subjects {
		if !s.Trusted {
			fresh = true
		}
	}
	if !fresh {
		t.Fatal("helper source changed without invalidating exact permission")
	}
	out, e = projectRun(t, root, "run", "--offline", "--bin", "one")
	if e == nil || strings.Contains(out, "APP_ONE") {
		t.Fatalf("changed helper automatically authorized: %s %v", out, e)
	}
	for _, s := range changed.Subjects {
		scriptAction(t, root, "allow", "--subject", s.ID)
	}
	scriptRun(t, root, "one", "run", "/helper-two embedded-resource")
}

func TestBuildScriptCrossTargetRejectedBeforeExecutionCLI(t *testing.T) {
	root := buildScriptFixture(t, false, false)
	scriptAction(t, root, "trust-project")
	out := scriptRun(t, root, "one", "run", "APP_ONE")
	want := "FIXTURE_HOST_TARGET " + runtime.GOOS + " " + runtime.GOARCH + " " + runtime.GOOS + " " + runtime.GOARCH
	if !strings.Contains(out, want) {
		t.Fatalf("host request wanted %q: %s", want, out)
	}
	targetArch := "arm64"
	if runtime.GOARCH == targetArch {
		targetArch = "amd64"
	}
	t.Run("reject foreign target before scripts", func(t *testing.T) {
		t.Setenv("GOOS", runtime.GOOS)
		t.Setenv("GOARCH", targetArch)
		for _, args := range [][]string{{"scripts", "--action", "plan"}, {"build", "--offline", "--bin", "one", "-o", filepath.Join(t.TempDir(), "cross-app")}} {
			out, e := projectRun(t, root, args...)
			if e == nil || strings.Contains(out, "FIXTURE_GENERATOR_EXECUTED") || strings.Contains(out, "build-script run") {
				t.Fatalf("foreign target executed script or was accepted: %v: %s %v", args, out, e)
			}
			if !strings.Contains(strings.ToLower(e.Error()), "cross") && !strings.Contains(strings.ToLower(e.Error()), "host") {
				t.Fatalf("unhelpful foreign target rejection: %v", e)
			}
		}
	})
	scriptRun(t, root, "one", "cache hit", "APP_ONE")
}

func TestBuildScriptHostToolFingerprintCLI(t *testing.T) {
	root := buildScriptFixture(t, false, false)
	toolRoot := filepath.Join(filepath.Dir(root), "tool-source")
	toolBin := filepath.Join(filepath.Dir(root), "echoer")
	if runtime.GOOS == "windows" {
		toolBin += ".exe"
	}
	compile := func(value string) {
		t.Helper()
		projectWrite(t, toolRoot, map[string]string{"go.mod": "module example.org/fixturetool\n\ngo 1.27\n", "main.go": "package main\nimport \"fmt\"\nfunc main(){fmt.Println(" + fmt.Sprintf("%q", value) + ")}\n"})
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-buildvcs=false", "-o", toolBin, ".")
		cmd.Dir = toolRoot
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0", "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("build fixture tool: %s %v", out, e)
		}
	}
	compile("FIXTURE_TOOL_ONE")
	b, e := os.ReadFile(filepath.Join(root, "goxide.toml"))
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e = decodeManifestFixture(b, &m); e != nil {
		t.Fatal(e)
	}
	m["build"].(map[string]any)["tools"] = map[string]string{"echoer": toolBin}
	entry := filepath.Join(root, "tools/build/main.go")
	b, e = os.ReadFile(entry)
	if e != nil {
		t.Fatal(e)
	}
	source := strings.Replace(string(b), `"encoding/json";`, `"encoding/json";"os/exec";`, 1)
	source = strings.Replace(source, "Version int;", "Version int; Tools map[string]string;", 1)
	source = strings.Replace(source, `fmt.Println("FIXTURE_GENERATOR_EXECUTED")`, `b,e=exec.Command(q.Tools["echoer"]).CombinedOutput();if e!=nil{panic(e)};fmt.Print(string(b));fmt.Println("FIXTURE_GENERATOR_EXECUTED")`, 1)
	projectWrite(t, root, map[string]string{"goxide.toml": projectManifest(m), "tools/build/main.go": source})
	scriptAction(t, root, "trust-project")
	p := scriptPlan(t, root)
	if len(p.Subjects) == 0 {
		t.Fatal("external tool omitted from permission plan")
	}
	out, e := projectRun(t, root, "run", "--offline", "--bin", "one")
	if e == nil || strings.Contains(out, "FIXTURE_TOOL_ONE") {
		t.Fatalf("tool ran without permission: %s %v", out, e)
	}
	for _, s := range p.Subjects {
		scriptAction(t, root, "allow", "--subject", s.ID)
	}
	out = scriptRun(t, root, "one", "run", "APP_ONE")
	if !strings.Contains(out, "FIXTURE_TOOL_ONE") {
		t.Fatalf("tool was not invoked: %s", out)
	}
	scriptRun(t, root, "one", "cache hit", "APP_ONE")
	compile("FIXTURE_TOOL_TWO")
	p = scriptPlan(t, root)
	fresh := false
	for _, s := range p.Subjects {
		if !s.Trusted {
			fresh = true
		}
	}
	if !fresh {
		t.Fatal("tool replacement did not invalidate permission")
	}
	out, e = projectRun(t, root, "run", "--offline", "--bin", "one")
	if e == nil || strings.Contains(out, "FIXTURE_TOOL_TWO") {
		t.Fatalf("changed tool ran without permission: %s %v", out, e)
	}
	for _, s := range p.Subjects {
		scriptAction(t, root, "allow", "--subject", s.ID)
	}
	out = scriptRun(t, root, "one", "run", "APP_ONE")
	if !strings.Contains(out, "FIXTURE_TOOL_TWO") {
		t.Fatalf("new tool was not invoked: %s", out)
	}
}

func TestBuildScriptDependencyPackagePermissionCLI(t *testing.T) {
	dep := buildScriptFixture(t, false, false)
	b, e := os.ReadFile(filepath.Join(dep, "goxide.toml"))
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e = decodeManifestFixture(b, &m); e != nil {
		t.Fatal(e)
	}
	delete(m, "bins")
	m["lib"] = map[string]string{"name": "depscript", "path": "lib/lib.gox"}
	projectWrite(t, dep, map[string]string{
		"go.mod":      "module example.org/depscript\n\ngo 1.27\n",
		"goxide.toml": projectManifest(m),
		"lib/lib.gox": "package depscript\nuse gen;\nfunc Value()string{return gen::Value+\" \"+gen::Resource}\n",
	})
	root := filepath.Join(filepath.Dir(dep), "consumer")
	projectWrite(t, root, map[string]string{
		"go.mod": "module example.org/consumer\n\ngo 1.27\n",
		"goxide.toml": `[[bin]]
name = 'app'
path = 'main.gox'

[dependencies]
[dependencies.generator]
module = 'example.org/depscript'
path = '../root'

[package]
format = 1
`,
		"main.gox": "package main\nuse fmt;\nuse generator as d;\nfunc main(){fmt::Println(\"DEPENDENT_APP\",d::Value())}\n",
	})
	scriptAction(t, root, "trust-project")
	p := scriptPlan(t, root)
	if len(p.Subjects) != 1 {
		t.Fatalf("wanted dependency-script subject, got %+v", p.Subjects)
	}
	out, e := projectRun(t, root, "run", "--offline", "--bin", "app")
	if e == nil || strings.Contains(out, "FIXTURE_GENERATOR_EXECUTED") {
		t.Fatalf("dependency script ran without grant: %s %v", out, e)
	}
	scriptAction(t, root, "allow", "--subject", p.Subjects[0].ID)
	scriptRun(t, root, "app", "run", "DEPENDENT_APP schema-one")
	scriptRun(t, root, "app", "cache hit", "DEPENDENT_APP")
	scriptAction(t, root, "revoke", "--subject", p.Subjects[0].ID)
	out, e = projectRun(t, root, "run", "--offline", "--bin", "app")
	if e == nil || strings.Contains(out, "DEPENDENT_APP") {
		t.Fatalf("revoked dependency reused cache: %s %v", out, e)
	}
	scriptAction(t, root, "allow", "--subject", p.Subjects[0].ID)
	source, e := os.ReadFile(filepath.Join(dep, "tools/build/main.go"))
	if e != nil {
		t.Fatal(e)
	}
	projectWrite(t, dep, map[string]string{"tools/build/main.go": string(source) + "\n// Updated dependency script\n"})
	changed := scriptPlan(t, root)
	if len(changed.Subjects) != 1 || changed.Subjects[0].Trusted || changed.Subjects[0].ID == p.Subjects[0].ID {
		t.Fatalf("dependency update retained old grant: %+v", changed.Subjects)
	}
	out, e = projectRun(t, root, "run", "--offline", "--bin", "app")
	if e == nil || strings.Contains(out, "FIXTURE_GENERATOR_EXECUTED") {
		t.Fatalf("dependency update automatically authorized: %s %v", out, e)
	}
	scriptAction(t, root, "allow", "--subject", changed.Subjects[0].ID)
	scriptRun(t, root, "app", "run", "DEPENDENT_APP")
}

func TestBuildScriptTimeoutDoesNotPublishCLI(t *testing.T) {
	root := buildScriptFixture(t, false, false)
	entry := filepath.Join(root, "tools/build/main.go")
	b, e := os.ReadFile(entry)
	if e != nil {
		t.Fatal(e)
	}
	source := strings.Replace(string(b), `"encoding/json";`, `"encoding/json";"time";`, 1)
	source = strings.Replace(source, `fmt.Println("FIXTURE_GENERATOR_EXECUTED")`, `if os.Getenv("MODE")=="timeout" {fmt.Println("PARTIAL_READY");time.Sleep(10*time.Second)};fmt.Println("FIXTURE_GENERATOR_EXECUTED")`, 1)
	projectWrite(t, root, map[string]string{"tools/build/main.go": source})
	scriptAction(t, root, "trust-project")
	// Warm compilation of the exact generator before imposing a short deadline.
	scriptRun(t, root, "one", "run", "APP_ONE")
	scriptAction(t, root, "clean")
	b, e = os.ReadFile(filepath.Join(root, "goxide.toml"))
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e = decodeManifestFixture(b, &m); e != nil {
		t.Fatal(e)
	}
	m["build"].(map[string]any)["timeout_seconds"] = 1
	projectWrite(t, root, map[string]string{"goxide.toml": projectManifest(m)})
	t.Setenv("MODE", "timeout")
	for attempt := 0; attempt < 2; attempt++ {
		out, e := projectRun(t, root, "run", "--offline", "--bin", "one")
		if e == nil || !strings.Contains(out, "PARTIAL_READY") || strings.Contains(out, "APP_ONE") || strings.Contains(out, "cache hit") {
			t.Fatalf("partial outputs published or timeout did not reach script: %s %v", out, e)
		}
	}
	// A corrected script must run successfully, with neither partial target output
	// nor a cached success from the timed-out attempts.
	source = strings.Replace(source, "time.Sleep(10*time.Second)", "time.Sleep(0)", 1)
	projectWrite(t, root, map[string]string{"tools/build/main.go": source})
	scriptRun(t, root, "one", "run", "APP_ONE schema-one/template-one/timeout")
	scriptRun(t, root, "one", "cache hit", "APP_ONE")
}

func TestBuildScriptCleanPreservesTrustAndSourceCLI(t *testing.T) {
	root := buildScriptFixture(t, false, false)
	projectWrite(t, root, map[string]string{"source-sentinel.txt": "keep source", ".goxide/user-sentinel.txt": "keep unrelated cache"})
	scriptAction(t, root, "trust-project")
	scriptRun(t, root, "one", "run", "APP_ONE")
	scriptRun(t, root, "one", "cache hit", "APP_ONE")
	scriptAction(t, root, "clean")
	if !scriptPlan(t, root).RootTrusted {
		t.Fatal("clean deleted project trust")
	}
	for name, want := range map[string]string{"source-sentinel.txt": "keep source", ".goxide/user-sentinel.txt": "keep unrelated cache", "schema.txt": "schema-one"} {
		b, e := os.ReadFile(filepath.Join(root, name))
		if e != nil || string(b) != want {
			t.Fatalf("clean changed %s: %q %v", name, b, e)
		}
	}
	scriptRun(t, root, "one", "run", "APP_ONE")
	scriptRun(t, root, "one", "cache hit", "APP_ONE")
}
