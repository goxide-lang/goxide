package frontend

import (
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func useWrite(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, src := range files {
		path := filepath.Join(root, name)
		if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			t.Fatal(e)
		}
		if e := testdeps.WriteFile(path, []byte(src), 0644); e != nil {
			t.Fatal(e)
		}
	}
}
func useGo(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-buildvcs=false")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("go %v: %v\n%s", args, e, b)
	}
	return string(b)
}
func TestUseStorageFunctionIdentityAndFacadeInit(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")
	useWrite(t, src, map[string]string{
		"go.mod": "module example.org/use\n\ngo 1.27\n",
		"origin/origin.go": `package origin
 var Count=1
 var Trace=[]string{"origin"}
 func Catch(){_=recover()}
 func Add(n int, rest ...int)(int,int){for _,v:=range rest{n+=v};return n,Count}
 func Identity[T any](x T)T{return x}
 type Box[T any] struct {Value T}
 func(b Box[T])Get()T{return b.Value}
 const Small=7
 `,
		"api/api.hgo": `package api
 import p "example.org/use/origin"
 pub use p::*;
 func init(){p.Trace=append(p.Trace,"facade")}
 `,
		"relay/relay.hgo": `package relay
 import a "example.org/use/api"
 pub use a::Count as Counter;
 pub use a::Catch;
 pub use a::Add;
 pub use a::Identity;
 pub use a::Box;
 pub use a::Small;
 `,
		"client/client.hgo": `package client
 import a "example.org/use/relay"
 import o "example.org/use/origin"
 use a::Counter;
 use a::Add as Sum;
 func Run()bool{
   Counter++; a.Counter=9
   if &Counter!=&o.Count{return false}
   x,y:=Sum(1,2,3)
   local:=func()bool{Counter:=42;return Counter==42}
   return x==6&&y==9&&local()&&a.Identity(8)==8&&len(o.Trace)==2&&o.Trace[1]=="facade"
 }
 func Recover()(escaped bool){defer func(){escaped=recover()!=nil}();defer a.Catch();panic("probe")}
 func Generic()int{b:=a.Box[int]{Value:7};return b.Get()+a.Small}
 `,
		"client/client_test.go": `package client
 import "testing"
 func TestRun(t *testing.T){if !Run()||Recover()||Generic()!=14{t.Fatal("storage/function/init/type identity")}}
 `,
	})
	if _, e := CompileModule(src, dst); e != nil {
		t.Fatal(e)
	}
	useGo(t, dst, "test", "-race", "./...")
	useGo(t, dst, "vet", "./...")
	b, e := os.ReadFile(filepath.Join(dst, "api", "api.go"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "var Count") || strings.Contains(string(b), "func Catch") {
		t.Fatal("copied storage or function wrapper")
	}
}

func TestUseExternalMetadataAndStaleness(t *testing.T) {
	source := t.TempDir()
	producer := filepath.Join(t.TempDir(), "producer")
	useWrite(t, source, map[string]string{
		"go.mod":        "module example.org/producer\n\ngo 1.27\n",
		"impl/value.go": "package impl\nvar Count=2\nfunc Catch(){_=recover()}\ntype T struct{N int}\ntype Box[T any] struct{Value T}\ntype hidden struct{N int}\nfunc Hidden()hidden{return hidden{N:9}}\n",
		"api.hgo":       "package producer\nimport p \"example.org/producer/impl\"\npub use p::*;\n",
	})
	if _, e := CompileModule(source, producer); e != nil {
		t.Fatal(e)
	}
	client := t.TempDir()
	useWrite(t, client, map[string]string{
		"go.mod": "module example.org/client\n\ngo 1.27\nrequire example.org/producer v0.0.0\nreplace example.org/producer => " + producer + "\n",
		"client.hgo": `package client
 import a "example.org/producer"
 use a::Count as Counter;
 func Run()int{Counter++;return a.Count}
 func Generic()int{return a.Box[int]{Value:42}.Value+a.Hidden().N}
 func Recover()(escaped bool){defer func(){escaped=recover()!=nil}();defer a.Catch();panic("probe")}
 `,
		"client_test.go": "package client\nimport \"testing\"\nfunc TestRun(t *testing.T){if Run()!=3||Recover()||Generic()!=51{t.Fatal(\"external binding\")}}\n",
	})
	output := filepath.Join(t.TempDir(), "client")
	if _, e := CompileModule(client, output); e != nil {
		t.Fatal(e)
	}
	useGo(t, output, "test", "-race", "./...")
	// Existing build-mode fingerprints must reject test-mode reuse.
	_, e := CompileModuleWithOptions(client, filepath.Join(t.TempDir(), "race"), ModuleOptions{Race: true})
	if e == nil || !strings.Contains(e.Error(), "fingerprint") {
		t.Fatalf("expected mode mismatch, got %v", e)
	}
	f := filepath.Join(producer, "impl", "value.go")
	b, e := os.ReadFile(f)
	if e != nil {
		t.Fatal(e)
	}
	if e = testdeps.WriteFile(f, append(b, []byte("\n// changed dependency closure\n")...), 0644); e != nil {
		t.Fatal(e)
	}
	_, e = CompileModule(client, filepath.Join(t.TempDir(), "stale"))
	if e == nil || !strings.Contains(e.Error(), "fingerprint") {
		t.Fatalf("expected stale dependency metadata, got %v", e)
	}
}

func TestUseDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, api, client, want string }{
		{"private", "use p::Count;", "func F()int{return a.Count}", "private use binding"},
		{"unknown", "pub use p::Missing;", "", "unknown, private, or unsupported"},
		{"conflict", "pub use p::Count as X; pub use q::Count as X;", "", "ambiguous use"},
		{"glob-conflict", "pub use p::*; pub use q::*;", "", "ambiguous use"},
		{"local-conflict", "use p::Count; var Count=4;", "", "conflicts with declaration"},
		{"malformed", "pub use p::Count as;", "", "expected [pub] use"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			useWrite(t, src, map[string]string{
				"go.mod":            "module example.org/negative\n\ngo 1.27\n",
				"p/p.go":            "package p\nvar Count=1\n",
				"q/q.go":            "package q\nvar Count=2\n",
				"api/api.hgo":       "package api\nimport p \"example.org/negative/p\"\nimport q \"example.org/negative/q\"\n" + tc.api + "\nvar _=q.Count\n",
				"client/client.hgo": "package client\nimport a \"example.org/negative/api\"\n" + tc.client + "\n",
			})
			_, e := CompileModule(src, filepath.Join(t.TempDir(), "out"))
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, e)
			}
		})
	}
}

func TestUseInternalTypeRouteAndFunctionRejection(t *testing.T) {
	source := t.TempDir()
	producer := filepath.Join(t.TempDir(), "producer")
	useWrite(t, source, map[string]string{
		"go.mod":                "module example.org/producer\n\ngo 1.27\n",
		"internal/impl/impl.go": "package impl\ntype T struct{N int}\nfunc F()int{return 1}\n",
		"api.hgo":               "package producer\nimport p \"example.org/producer/internal/impl\"\npub use p::T; pub use p::F;\n",
	})
	if _, e := CompileModule(source, producer); e != nil {
		t.Fatal(e)
	}
	client := t.TempDir()
	useWrite(t, client, map[string]string{
		"go.mod":     "module example.org/client\n\ngo 1.27\nrequire example.org/producer v0.0.0\nreplace example.org/producer => " + producer + "\n",
		"client.hgo": "package client\nimport a \"example.org/producer\"\nuse a::T;\nfunc Run()int{return T{N:7}.N}\n",
	})
	if _, e := CompileModule(client, filepath.Join(t.TempDir(), "ok")); e != nil {
		t.Fatal(e)
	}
	useWrite(t, client, map[string]string{"client.hgo": "package client\nimport a \"example.org/producer\"\nfunc Run()int{return a.F()}\n"})
	_, e := CompileModule(client, filepath.Join(t.TempDir(), "bad"))
	if e == nil || !strings.Contains(e.Error(), "internal package") {
		t.Fatalf("expected illegal internal route, got %v", e)
	}
}

func TestUseCycleAndMacroDiagnostic(t *testing.T) {
	t.Run("cycle", func(t *testing.T) {
		src := t.TempDir()
		useWrite(t, src, map[string]string{
			"go.mod":  "module example.org/cycle\n\ngo 1.27\n",
			"a/a.hgo": "package a\nimport b \"example.org/cycle/b\"\npub use b::*;\n",
			"b/b.hgo": "package b\nimport a \"example.org/cycle/a\"\npub use a::*;\n",
		})
		_, e := CompileModule(src, filepath.Join(t.TempDir(), "out"))
		if e == nil || !strings.Contains(e.Error(), "cycle") {
			t.Fatalf("expected cycle, got %v", e)
		}
	})

}

func TestUseRouteCannotCaptureLocalImportShadow(t *testing.T) {
	src := t.TempDir()
	useWrite(t, src, map[string]string{
		"go.mod":           "module example.org/shadow\n\ngo 1.27\n",
		"origin/origin.go": "package origin\nvar Count=7\nfunc F()int{return Count}\n",
		"client/client.hgo": `package client
 import p "example.org/shadow/origin"
 use p::Count;
 use p::F as Call;
 func Run(p struct{Count int;F func()int})int{return Count+Call()}
 `,
		"client/client_test.go": "package client\nimport \"testing\"\nfunc TestShadow(t *testing.T){if Run(struct{Count int;F func()int}{99,func()int{return 100}})!=14{t.Fatal(\"captured local import shadow\")}}\n",
	})
	dst := filepath.Join(t.TempDir(), "out")
	if _, e := CompileModule(src, dst); e != nil {
		t.Fatal(e)
	}
	useGo(t, dst, "test", "./...")
}

func TestUsePrivateDotImportAndTypeConflictRejected(t *testing.T) {
	for _, tc := range []struct{ name, api, client, want string }{
		{"dot-private", "use p::T as Secret;", "import . \"example.org/case/api\"\nfunc F() Secret{return Secret{}}", "dot imports"},
		{"type-conflict", "pub use p::Count; type Count int; func F()Count{return Count(1)}", "", "conflicts with type declaration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			useWrite(t, src, map[string]string{
				"go.mod":            "module example.org/case\n\ngo 1.27\n",
				"p/p.go":            "package p\ntype T struct{}\nvar Count=2\n",
				"api/api.hgo":       "package api\nimport p \"example.org/case/p\"\n" + tc.api + "\n",
				"client/client.hgo": "package client\n" + tc.client + "\n",
			})
			_, e := CompileModule(src, filepath.Join(t.TempDir(), "out"))
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, e)
			}
		})
	}
}

func TestUseEnumMetadataAndGeneratedRoundTrip(t *testing.T) {
	src := t.TempDir()
	first := filepath.Join(t.TempDir(), "first")
	useWrite(t, src, map[string]string{
		"go.mod":           "module example.org/enuse\n\ngo 1.27\n",
		"origin/event.hgo": "package origin\npub enum Event {Empty, Item(int)}\n",
		"api/api.hgo":      "package api\nimport p \"example.org/enuse/origin\"\npub use p::Event as Event;\n",
		"client/client.hgo": `package client
 import a "example.org/enuse/api"
 func Value()int{v:=a.Event::Item(9);return match v {a.Event::Empty=>0,a.Event::Item(x)=>x}}
 `,
		"client/client_test.go": "package client\nimport \"testing\"\nfunc TestValue(t *testing.T){if Value()!=9{t.Fatal(\"enum identity\")}}\n",
	})
	if _, e := CompileModule(src, first); e != nil {
		t.Fatal(e)
	}
	useGo(t, first, "test", "./...")
	second := filepath.Join(t.TempDir(), "second")
	if _, e := CompileModule(first, second); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(second, "api", useMetadataFilename)); e != nil {
		t.Fatal("lost use artifact", e)
	}
	useGo(t, second, "test", "./...")
}

func TestUseDistinctGenericInstancesConflict(t *testing.T) {
	src := t.TempDir()
	useWrite(t, src, map[string]string{
		"go.mod":            "module example.org/instances\n\ngo 1.27\n",
		"origin/origin.go":  "package origin\ntype Box[T any] struct{Value T}\n",
		"a/a.go":            "package a\nimport o \"example.org/instances/origin\"\ntype Value=o.Box[int]\n",
		"b/b.go":            "package b\nimport o \"example.org/instances/origin\"\ntype Value=o.Box[string]\n",
		"client/client.hgo": "package client\nimport a \"example.org/instances/a\"\nimport b \"example.org/instances/b\"\nuse a::Value; use b::Value;\n",
	})
	_, e := CompileModule(src, filepath.Join(t.TempDir(), "out"))
	if e == nil || !strings.Contains(e.Error(), "ambiguous use") {
		t.Fatalf("generic arguments lost: %v", e)
	}
}

func TestUsePrivateStructuralTypeArgumentsConflict(t *testing.T) {
	for _, arg := range []string{"struct{x int}", "interface{hidden()}"} {
		t.Run(arg, func(t *testing.T) {
			src := t.TempDir()
			useWrite(t, src, map[string]string{
				"go.mod":            "module example.org/structural\n\ngo 1.27\n",
				"origin/origin.go":  "package origin\ntype Box[T any] struct{Value T}\n",
				"a/a.go":            "package a\nimport o \"example.org/structural/origin\"\ntype Value=o.Box[" + arg + "]\n",
				"b/b.go":            "package b\nimport o \"example.org/structural/origin\"\ntype Value=o.Box[" + arg + "]\n",
				"client/client.hgo": "package client\nimport a \"example.org/structural/a\"\nimport b \"example.org/structural/b\"\nuse a::Value; use b::Value;\n",
			})
			_, e := CompileModule(src, filepath.Join(t.TempDir(), "out"))
			if e == nil || !strings.Contains(e.Error(), "distinct Go type identities") {
				t.Fatalf("private structural identity lost: %v", e)
			}
		})
	}
}

func TestUseDependentGenericConstraintRejectedAtProducer(t *testing.T) {
	src := t.TempDir()
	useWrite(t, src, map[string]string{
		"go.mod":           "module example.org/dependent\n\ngo 1.27\n",
		"origin/origin.go": "package origin\ntype Pair[T any,S ~[]T] struct{Value S}\n",
		"api/api.hgo":      "package api\nimport p \"example.org/dependent/origin\"\npub use p::Pair;\n",
	})
	_, e := CompileModule(src, filepath.Join(t.TempDir(), "out"))
	if e == nil || !strings.Contains(e.Error(), "constraints referring to type parameters") {
		t.Fatalf("expected producer diagnostic, got %v", e)
	}
}

func TestUseArtifactTrailingDataAndSingleFileGuard(t *testing.T) {
	source := t.TempDir()
	producer := filepath.Join(t.TempDir(), "producer")
	useWrite(t, source, map[string]string{
		"go.mod":       "module example.org/metadata\n\ngo 1.27\n",
		"impl/impl.go": "package impl\ntype T struct{}\nvar Count=1\n",
		"api.hgo":      "package metadata\nimport p \"example.org/metadata/impl\"\nuse p::T as Secret; pub use p::Count;\n",
	})
	if _, e := CompileModule(source, producer); e != nil {
		t.Fatal(e)
	}
	client := t.TempDir()
	src := "package client\nimport p \"example.org/metadata\"\nfunc F()p.Secret{return p.Secret{}}\n"
	useWrite(t, client, map[string]string{
		"go.mod":     "module example.org/client\n\ngo 1.27\nrequire example.org/metadata v0.0.0\nreplace example.org/metadata => " + producer + "\n",
		"client.hgo": src,
	})
	_, e := Compile(filepath.Join(client, "client.hgo"), []byte(src))
	if e == nil || !strings.Contains(e.Error(), "require directory/package") {
		t.Fatalf("single file bypass: %v", e)
	}
	artifact := filepath.Join(producer, useMetadataFilename)
	b, e := os.ReadFile(artifact)
	if e != nil {
		t.Fatal(e)
	}
	if e = testdeps.WriteFile(artifact, append(b, []byte("{}")...), 0644); e != nil {
		t.Fatal(e)
	}
	useWrite(t, client, map[string]string{"client.hgo": "package client\nimport p \"example.org/metadata\"\nfunc F()int{return p.Count}\n"})
	_, e = CompileModule(client, filepath.Join(t.TempDir(), "out"))
	if e == nil || !strings.Contains(e.Error(), "exactly one JSON object") {
		t.Fatalf("trailing metadata accepted: %v", e)
	}
}
