package buildscript

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/project"
)

const generator = `package main
import("encoding/json";"os";"path/filepath")
func main(){var r struct{SourceDir string ` + "`json:\"source_dir\"`" + `;OutDir string ` + "`json:\"out_dir\"`" + `;Env map[string]string ` + "`json:\"env\"`" + `};b,e:=os.ReadFile(os.Args[1]);if e!=nil{panic(e)};if e=json.Unmarshal(b,&r);e!=nil{panic(e)};b,e=os.ReadFile(filepath.Join(r.SourceDir,"schema"));if e!=nil{panic(e)};if e=os.WriteFile(filepath.Join(r.OutDir,"generated.go"),append(b,[]byte(r.Env["SCHEMA_SUFFIX"])...),0600);e!=nil{panic(e)}}`

func fixture(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	put(t, root, "scripts/build.go", generator)
	put(t, root, "schema", "package main\nconst Generated=42\n")
	return Options{ProjectRoot: root, PackageRoot: root, Module: "example.test/app", GoTool: filepath.Join(runtime.GOROOT(), "bin", "go"), CompilerID: "test", Root: true, Config: Config{Entry: "scripts/build.go", Inputs: []string{"schema"}, Outputs: []string{"generated.go"}, Env: []string{"SCHEMA_SUFFIX"}}}
}
func put(t *testing.T, root, n, s string) {
	t.Helper()
	p := filepath.Join(root, n)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
}
func prepare(t *testing.T, o Options) *Plan {
	t.Helper()
	p, e := Prepare(o)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func run(t *testing.T, p *Plan) Artifact {
	t.Helper()
	a, e := p.Execute(context.Background(), func() error { return nil }, nil)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestGenerationCacheAndAuthorization(t *testing.T) {
	o := fixture(t)
	p := prepare(t, o)
	a := run(t, p)
	if a.CacheHit || !strings.Contains(string(a.Files["generated.go"]), "42") {
		t.Fatal(a)
	}
	if !run(t, prepare(t, o)).CacheHit {
		t.Fatal("cache missed")
	}
	denied := errors.New("revoked")
	if _, e := p.Execute(context.Background(), func() error { return denied }, nil); !errors.Is(e, denied) {
		t.Fatal(e)
	}
	put(t, o.PackageRoot, "schema", "package main\nconst Generated=43\n")
	next := prepare(t, o)
	if next.ID == p.ID || next.RootIdentity != p.RootIdentity {
		t.Fatal("input key/root identity")
	}
	if run(t, next).CacheHit {
		t.Fatal("input cache reused")
	}
	t.Setenv("SCHEMA_SUFFIX", "// env")
	if prepare(t, o).ID == next.ID {
		t.Fatal("env not fingerprinted")
	}
	o.Features = []string{"extra"}
	if prepare(t, o).ID == next.ID {
		t.Fatal("features not fingerprinted")
	}
}
func TestPrepareFreezesBytes(t *testing.T) {
	o := fixture(t)
	p := prepare(t, o)
	put(t, o.PackageRoot, "scripts/build.go", "broken")
	put(t, o.PackageRoot, "schema", "changed")
	if !strings.Contains(string(run(t, p).Files["generated.go"]), "42") {
		t.Fatal("mutable prepared inputs")
	}
}
func TestLocalHelperGoAndHgo(t *testing.T) {
	for _, ext := range []string{"go", "hgo"} {
		t.Run(ext, func(t *testing.T) {
			o := fixture(t)
			o.Config.Entry = "scripts/build." + ext
			os.Remove(filepath.Join(o.PackageRoot, "scripts/build.go"))
			put(t, o.PackageRoot, o.Config.Entry, strings.Replace(generator, "func main(){", "func main(){_ = helper.Value;", 1))
			b, _ := os.ReadFile(filepath.Join(o.PackageRoot, o.Config.Entry))
			put(t, o.PackageRoot, o.Config.Entry, strings.Replace(string(b), "import(", "import(\"example.test/helper\";", 1))
			helper := t.TempDir()
			put(t, helper, "go.mod", "module example.test/helper\n\ngo 1.27\n")
			put(t, helper, "value."+ext, "package helper\nconst Value=1\n")
			o.Config.Dependencies = map[string]string{"example.test/helper": helper}
			p := prepare(t, o)
			if len(p.Subjects) != 1 {
				t.Fatal(p.Subjects)
			}
			run(t, p)
		})
	}
}
func TestRejectedPathsAndConfig(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Options)
	}{{"escape", func(o *Options) { o.Config.Outputs = []string{"../escape"} }}, {"source", func(o *Options) { o.Config.Outputs = []string{"schema"} }}, {"case", func(o *Options) { o.Config.Outputs = []string{"a.go", "A.go"} }}, {"env", func(o *Options) { o.Config.Env = []string{"PATH"} }}, {"rootentry", func(o *Options) { o.Config.Entry = "build.go" }}, {"alias", func(o *Options) { o.Config.Tools = map[string]string{"..": "schema"} }}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := fixture(t)
			c.change(&o)
			if _, e := Prepare(o); e == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}
func TestSymlinkAndHardlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix links")
	}
	for _, hard := range []bool{false, true} {
		o := fixture(t)
		path := filepath.Join(o.PackageRoot, "scripts/link.go")
		var e error
		if hard {
			e = os.Link(filepath.Join(o.PackageRoot, "schema"), path)
		} else {
			e = os.Symlink(filepath.Join(o.PackageRoot, "schema"), path)
		}
		if e != nil {
			t.Fatal(e)
		}
		if _, e = Prepare(o); e == nil {
			t.Fatal("link accepted")
		}
	}
}
func TestMissingExtraFailureNoPublication(t *testing.T) {
	for _, body := range []string{`func main(){}`, `func main(){panic("failure")}`, `import("os";"path/filepath");func main(){os.WriteFile(filepath.Join(os.Getenv("OUT_DIR"),"extra"),[]byte("x"),0600)}`} {
		o := fixture(t)
		put(t, o.PackageRoot, "scripts/build.go", "package main\n"+body)
		p := prepare(t, o)
		if _, e := p.Execute(context.Background(), func() error { return nil }, nil); e == nil {
			t.Fatal("invalid output accepted")
		}
		if _, e := os.Stat(filepath.Join(p.options.ArtifactRoot, "entries", p.ID)); !os.IsNotExist(e) {
			t.Fatal("partial cache published")
		}
	}
}
func TestDirectoryAndToolFingerprint(t *testing.T) {
	o := fixture(t)
	os.Mkdir(filepath.Join(o.PackageRoot, "assets"), 0700)
	o.Config.Inputs = append(o.Config.Inputs, "assets")
	p := prepare(t, o)
	os.Mkdir(filepath.Join(o.PackageRoot, "assets", "empty"), 0700)
	if p.ID == prepare(t, o).ID {
		t.Fatal("empty directory key missing")
	}
	put(t, o.PackageRoot, "tool", "first")
	o.Config.Tools = map[string]string{"local": "tool"}
	p = prepare(t, o)
	put(t, o.PackageRoot, "tool", "second")
	q := prepare(t, o)
	if p.ID == q.ID || p.Subjects[0].ID == q.Subjects[0].ID {
		t.Fatal("tool not fingerprinted")
	}
}
func TestCleanOwnershipAndBusy(t *testing.T) {
	o := fixture(t)
	p := prepare(t, o)
	run(t, p)
	if e := os.Mkdir(filepath.Join(p.options.ArtifactRoot, "lock"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := Clean(o.ProjectRoot); e == nil {
		t.Fatal("clean while busy")
	}
	os.Remove(filepath.Join(p.options.ArtifactRoot, "lock"))
	if e := Clean(o.ProjectRoot); e != nil {
		t.Fatal(e)
	}
	put(t, o.ProjectRoot, ".hgo/build-scripts/owner", "other")
	if e := Clean(o.ProjectRoot); e == nil {
		t.Fatal("unowned clean")
	}
}
func TestCrossTargetBeforeAuthorization(t *testing.T) {
	o := fixture(t)
	target := "arm64"
	if runtime.GOARCH == target {
		target = "amd64"
	}
	t.Setenv("GOARCH", target)
	if _, e := Prepare(o); e == nil || !strings.Contains(e.Error(), "cross compilation") {
		t.Fatal(e)
	}
}

func TestTimeoutAndParallelLock(t *testing.T) {
	o := fixture(t)
	o.Config.TimeoutSeconds = 1
	put(t, o.PackageRoot, "scripts/build.go", "package main\nimport \"time\"\nfunc main(){time.Sleep(3*time.Second)}")
	p := prepare(t, o)
	if _, e := p.Execute(context.Background(), func() error { return nil }, nil); e == nil {
		t.Fatal("timeout accepted")
	}
	if _, e := os.Stat(filepath.Join(p.options.ArtifactRoot, "entries", p.ID)); !os.IsNotExist(e) {
		t.Fatal("timeout published")
	}
	if e := os.Mkdir(filepath.Join(p.options.ArtifactRoot, "lock"), 0700); e != nil {
		t.Fatal(e)
	}
	defer os.Remove(filepath.Join(p.options.ArtifactRoot, "lock"))
	if _, e := p.Execute(context.Background(), func() error { return nil }, nil); e == nil || !strings.Contains(e.Error(), "busy") {
		t.Fatal(e)
	}
}
func TestConfigFrozenAndDependencySubject(t *testing.T) {
	o := fixture(t)
	o.Root = false
	p := prepare(t, o)
	o.Config.Outputs[0] = "another.go"
	q := prepare(t, o)
	if p.Subject == q.Subject {
		t.Fatal("subject omitted output contract")
	}
	if _, ok := run(t, p).Files["generated.go"]; !ok {
		t.Fatal("config not frozen")
	}
}
func TestUnauthorizedNeverExecutes(t *testing.T) {
	o := fixture(t)
	p := prepare(t, o)
	if _, e := p.Execute(context.Background(), func() error { return errors.New("untrusted") }, nil); e == nil {
		t.Fatal("untrusted accepted")
	}
	if _, e := os.Stat(p.options.ArtifactRoot); !os.IsNotExist(e) {
		t.Fatal("untrusted execution created cache")
	}
}
func TestHelperManifestRejectsBootstrap(t *testing.T) {
	for _, manifest := range []string{"module example.test/helper\nrequire example.test/missing v1.0.0\n", "module wrong\n", "module example.test/helper\ntoolchain go9.1\n", "module example.test/helper\nreplace example.test/missing => /tmp\n"} {
		o := fixture(t)
		helper := t.TempDir()
		put(t, helper, "go.mod", manifest)
		o.Config.Dependencies = map[string]string{"example.test/helper": helper}
		if _, e := Prepare(o); e == nil {
			t.Fatal("unsafe helper manifest accepted")
		}
	}
}

func TestHelperCommentsDoNotCreateCycle(t *testing.T) {
	o := fixture(t)
	o.Config.Dependencies = map[string]string{}
	for _, name := range []string{"a", "b"} {
		root := t.TempDir()
		mod := "example.test/" + name
		other := "example.test/b"
		if name == "b" {
			other = "example.test/a"
		}
		put(t, root, "go.mod", "module "+mod+"\n\ngo 1.27\n")
		put(t, root, "value.hgo", "package "+name+"\n// import \""+other+"\"\nconst Value=\""+other+"\"\n")
		o.Config.Dependencies[mod] = root
	}
	run(t, prepare(t, o))
}
func TestHostImportsSelectRealImports(t *testing.T) {
	opposite := "windows"
	if runtime.GOOS == "windows" {
		opposite = "linux"
	}
	files := map[string][]byte{
		"main.hgo":                  []byte("package x\nimport alias `example.test/real/sub`\n// import \"example.test/comment\"\nconst s=\"example.test/string\""),
		"main_test.go":              []byte("package x\nimport _ \"example.test/test\""),
		"other_" + opposite + ".go": []byte("package x\nimport _ \"example.test/platform\""),
		"tagged.hgo":                []byte("//go:build nonexistent_host_tag\n\npackage x\nimport _ \"example.test/tagged\""),
		"testdata/ignored.go":       []byte("package x\nimport _ \"example.test/testdata\""),
		"cgo.go":                    []byte("package x\nimport \"C\"\nimport _ \"example.test/cgo\""),
	}
	got, e := hostImports(files)
	if e != nil {
		t.Fatal(e)
	}
	if len(got) != 1 || got[0] != "example.test/real/sub" {
		t.Fatalf("imports = %q", got)
	}
}

func TestOutputSourceMapReserved(t *testing.T) {
	o := fixture(t)
	o.Config.Outputs = []string{"generated.hgo", "generated.go.map.json"}
	if _, e := Prepare(o); e == nil || !strings.Contains(e.Error(), "reserved output") {
		t.Fatal(e)
	}
}
func TestHostUseRetainsImportEdge(t *testing.T) {
	imports, e := hostImports(map[string][]byte{"use.hgo": []byte("package helper\nimport other \"example.test/other\"\npub use other::*;\n")})
	if e != nil {
		t.Fatal(e)
	}
	if len(imports) != 1 || imports[0] != "example.test/other" {
		t.Fatal(imports)
	}
}

func TestLongestHelperModule(t *testing.T) {
	mods := []string{"example.test/a", "example.test/a/sub", "example.test/b"}
	for path, want := range map[string]string{"example.test/a/sub": "example.test/a/sub", "example.test/a/sub/pkg": "example.test/a/sub", "example.test/a/pkg": "example.test/a", "example.test/ax": ""} {
		if got := importedHelper(path, mods); got != want {
			t.Fatalf("%s: got %s want %s", path, got, want)
		}
	}
}

func TestOverlappingHelperModulesNoFalseCycle(t *testing.T) {
	o := fixture(t)
	o.Config.Dependencies = map[string]string{}
	sources := map[string]string{
		"example.test/a":     "package a\nimport \"example.test/b\"\nconst Value=b.Value\n",
		"example.test/b":     "package b\nimport \"example.test/a/sub\"\nconst Value=sub.Value\n",
		"example.test/a/sub": "package sub\nconst Value=7\n",
	}
	for mod, source := range sources {
		root := t.TempDir()
		put(t, root, "go.mod", "module "+mod+"\n\ngo 1.27\n")
		put(t, root, "value.hgo", source)
		o.Config.Dependencies[mod] = root
	}
	run(t, prepare(t, o))
}

func TestManagedLayoutsExcludedFromSourceSnapshots(t *testing.T) {
	o := fixture(t)
	o.Config.Inputs = append(o.Config.Inputs, "scripts")
	helper := t.TempDir()
	put(t, helper, "go.mod", "module example.test/helper\n\ngo 1.27\n")
	put(t, helper, "value.go", "package helper\nconst Value=1\n")
	o.Config.Dependencies = map[string]string{"example.test/helper": helper}
	before := prepare(t, o)
	for _, spec := range []struct{ root, module, out string }{{o.PackageRoot, o.Module, "scripts/artifacts"}, {helper, "example.test/helper", "target"}} {
		layout, e := project.OpenLayout(spec.root, spec.module, spec.out)
		if e != nil {
			t.Fatal(e)
		}
		unit, e := layout.Begin(project.UnitSpec{Host: "linux-amd64", Target: "linux-amd64", Kind: "bin", Name: "app", Config: "test"})
		if e != nil {
			t.Fatal(e)
		}
		put(t, unit.Intermediate, "bundle/go.mod", "module generated\n")
		if e = layout.Close(); e != nil {
			t.Fatal(e)
		}
	}
	after := prepare(t, o)
	if before.ID != after.ID || before.Subject != after.Subject {
		t.Fatal("managed outputs polluted source/input/helper snapshot")
	}
	run(t, after)
	for _, input := range []string{"scripts/artifacts", "scripts/artifacts/.hgo-layout.json"} {
		copy := o
		copy.Config.Inputs = []string{input}
		if _, e := Prepare(copy); e == nil || !strings.Contains(e.Error(), "managed") {
			t.Fatalf("explicit managed input accepted: %s: %v", input, e)
		}
	}
	// An invalid or copied marker must produce a diagnostic, not an exclusion.
	put(t, o.PackageRoot, "scripts/foreign/.hgo-layout.json", "{}")
	if _, e := Prepare(o); e == nil {
		t.Fatal("foreign layout silently skipped")
	}
}
func TestOutputSnapshotNeverSkipsManagedMarkers(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".hgo-layout.json", "{}")
	files, e := snapshotMode(root, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := files[".hgo-layout.json"]; !ok {
		t.Fatal("output marker omitted")
	}
}

func TestTOMLManifestReservedAndHelperBootstrap(t *testing.T) {
	for _, name := range []string{"goxide.toml", "hgo.json"} {
		t.Run(name, func(t *testing.T) {
			o := fixture(t)
			o.Config.Outputs = []string{name}
			if _, e := Prepare(o); e == nil {
				t.Fatal("generator could publish a manifest", name)
			}
		})
	}
	o := fixture(t)
	helper := t.TempDir()
	put(t, helper, "go.mod", "module example.test/helper\n\ngo 1.27\n")
	put(t, helper, "goxide.toml", "[package]\nformat=1\n[build]\nentry='tools/build.go'\n")
	o.Config.Dependencies = map[string]string{"example.test/helper": helper}
	if _, e := Prepare(o); e == nil || !strings.Contains(e.Error(), "bootstrap scripts unsupported") {
		t.Fatal(e)
	}
}
