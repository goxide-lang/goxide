package frontend

import (
	"bytes"
	"encoding/json"
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUseMacroSourceGraph(t *testing.T) {
	src := t.TempDir()
	useWrite(t, src, map[string]string{
		"go.mod": "module example.org/mac\n\ngo 1.27\n",
		"p/p.gox": `package p
 var Trace=1
 func Helper(x int)int{return x+1}
 macro_rules! inner { ($x:expr) => { Helper($x) }; }
 pub macro_rules! answer { ($x:expr) => { inner!($x) }; }
 pub macro_rules! identity { ($x:expr) => { $x }; }
 `,
		"left/left.gox": `package left
 import p "example.org/mac/p"
 pub use p::answer as value;
 func init(){p.Trace+=10}
 `,
		"right/right.gox": `package right
 import p "example.org/mac/p"
 pub use p::answer as value;
 func init(){p.Trace+=100}
 `,
		"api/api.gox": `package api
 import l "example.org/mac/left"
 import r "example.org/mac/right"
 import p "example.org/mac/p"
 pub use l::*;
 pub use r::*;
 pub use p::identity;
 func init(){p.Trace+=1000}
 `,
		"client/client.gox": `package client
 import a "example.org/mac/api"
 import p "example.org/mac/p"
 use a::value as local;
 func Helper(x int)int{return -100}
 func Run()bool{
   Helper:=func(x int)int{return -200}; _=Helper
   return a.value!(40)==41&&local!(41)==42&&a.identity!(Generic[int](7))==7&&p.Trace==1111
 }
 `,
		"client/generic.go": "package client\nfunc Generic[T any](x T)T{return x}\n",
		"client/other.gox": `package client
 func Other()int{return local!(8)}
 `,
		"client/client_test.go": `package client
 import "testing"
 func TestRun(t *testing.T){if !Run()||Other()!=9{t.Fatal("macro definition/init/alias identity")}}
 `,
	})
	out := filepath.Join(t.TempDir(), "out")
	if _, e := CompileModule(src, out); e != nil {
		t.Fatal(e)
	}
	useGo(t, out, "test", "-race", "./...")
	useGo(t, out, "vet", "./...")
	repeat := filepath.Join(t.TempDir(), "out")
	if _, e := CompileModule(src, repeat); e != nil {
		t.Fatal(e)
	}
	for _, file := range []string{"api/api.go", "api/.goxide.uses.json", "client/client.go"} {
		a, e := os.ReadFile(filepath.Join(out, file))
		if e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(filepath.Join(repeat, file))
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("nondeterministic artifact %s", file)
		}
	}
}

func TestUseMacroRejections(t *testing.T) {
	for _, tc := range []struct{ name, origin, api, client, want string }{
		{"reserved-print", `pub macro_rules! answer { () => { 1 }; }`, "pub use p::answer as print;", "", "reserved builtin macro name"},
		{"reserved-result", `pub macro_rules! answer { () => { 1 }; }`, "pub use p::answer as go_result;", "", "reserved builtin macro name"},
		{"private-helper", `func hidden()int{return 1}
 pub macro_rules! answer { () => { hidden() }; }`, "pub use p::answer;", "func F()int{return a.answer!()}", "private runtime helper"},
		{"private-alias", `pub macro_rules! answer { () => { 1 }; }`, "use p::answer;", "func F()int{return a.answer!()}", "is private"},
		{"macro-conflict", `pub macro_rules! answer { () => { 1 }; }`, "pub use p::answer;\npub macro_rules! answer { () => { 2 }; }", "", "conflicts with declaration"},
		{"string-path", `pub macro_rules! answer { () => { 1 }; }`, `pub use "example.org/mac/p";`, "", "expected [pub] use importAlias::Name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			useWrite(t, src, map[string]string{
				"go.mod":            "module example.org/mac\n\ngo 1.27\n",
				"p/p.gox":           "package p\n" + tc.origin + "\n",
				"a/a.gox":           "package a\nimport p \"example.org/mac/p\"\n" + tc.api + "\n",
				"client/client.gox": "package client\nimport a \"example.org/mac/a\"\n" + tc.client + "\n",
			})
			_, e := CompileModule(src, filepath.Join(t.TempDir(), "out"))
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, e)
			}
		})
	}
}

func TestUseMacroExternalSourceRegistryBoundary(t *testing.T) {
	src := t.TempDir()
	producer := filepath.Join(t.TempDir(), "out")
	useWrite(t, src, map[string]string{
		"go.mod":  "module example.org/mac\n\ngo 1.27\n",
		"p/p.gox": "package p\npub macro_rules! answer { () => { 42 }; }\nconst C=7\n",
		"api.gox": "package api\nimport p \"example.org/mac/p\"\npub use p::*;\n",
	})
	if _, e := CompileModule(src, producer); e != nil {
		t.Fatal(e)
	}
	for _, selection := range []string{"answer", "*"} {
		client := t.TempDir()
		useWrite(t, client, map[string]string{
			"go.mod":     "module example.org/client\n\ngo 1.27\nrequire example.org/mac v0.0.0\nreplace example.org/mac => " + producer + "\n",
			"client.gox": "package client\nimport a \"example.org/mac\"\npub use a::" + selection + ";\n",
		})
		_, e := CompileModule(client, filepath.Join(t.TempDir(), "out"))
		if e == nil || !strings.Contains(e.Error(), "compiled source registry") {
			t.Fatalf("want explicit source registry boundary, got %v", e)
		}
		useWrite(t, client, map[string]string{"client.gox": "package client\nimport a \"example.org/mac\"\npub use a::C;\n"})
		if _, e = CompileModule(client, filepath.Join(t.TempDir(), "ok")); e != nil {
			t.Fatal(e)
		}

		if selection == "answer" {
			_, e = CompileModuleWithOptions(client, filepath.Join(t.TempDir(), "race"), ModuleOptions{Race: true})
			if e == nil || !strings.Contains(e.Error(), "fingerprint") {
				t.Fatalf("want config mismatch, got %v", e)
			}
			file := filepath.Join(producer, useMetadataFilename)
			original, e := os.ReadFile(file)
			if e != nil {
				t.Fatal(e)
			}
			var artifact useArtifact
			if e = json.Unmarshal(original, &artifact); e != nil {
				t.Fatal(e)
			}
			artifact.Metadata.UnsupportedKinds["answer"] = "family"
			changed, e := json.Marshal(artifact)
			if e != nil {
				t.Fatal(e)
			}
			if e = testdeps.WriteFile(file, changed, 0644); e != nil {
				t.Fatal(e)
			}
			_, e = CompileModule(client, filepath.Join(t.TempDir(), "tampered"))
			if e == nil || !strings.Contains(e.Error(), "marker/schema mismatch") {
				t.Fatalf("want classification digest mismatch, got %v", e)
			}
			if e = testdeps.WriteFile(file, original, 0644); e != nil {
				t.Fatal(e)
			}
		}
	}
}

func TestUseMacroKindCollisionOnlyWhenSelected(t *testing.T) {
	for _, selection := range []string{"C", "F", "*"} {
		src := t.TempDir()
		useWrite(t, src, map[string]string{
			"go.mod":  "module example.org/mac\n\ngo 1.27\n",
			"p/p.gox": "package p\npub macro_rules! F { () => { 42 }; }\nfunc F()int{return 3}\nconst C=7\n",
			"api.gox": "package api\nimport p \"example.org/mac/p\"\npub use p::" + selection + ";\n",
		})
		_, e := CompileModule(src, filepath.Join(t.TempDir(), "out"))
		if selection == "C" {
			if e != nil {
				t.Fatal(e)
			}
		} else if e == nil || !strings.Contains(e.Error(), "macro and Go declaration") {
			t.Fatalf("want selected-kind ambiguity, got %v", e)
		}
	}
}

func TestUseMacroAndBuiltinFamilies(t *testing.T) {
	src := t.TempDir()
	useWrite(t, src, map[string]string{
		"go.mod": "module example.org/mac\n\ngo 1.27\n",
		"p/p.gox": `package p
 pub macro_rules! answer { () => { 42 }; }
 func Pick()Option[int]{return Option[int]::Some(3)}
 func ResultValue()Result[int,string]{return Result[int,string]::Ok(7)}
 `,
		"left/left.gox": `package left
 import p "example.org/mac/p"
 pub use p::*;
 `,
		"api.gox": `package api
 import l "example.org/mac/left"
 pub use l::Option as Maybe;
 pub use l::Result as Outcome;
 pub use l::answer;
 `,
		"client/client.gox": `package client
 import a "example.org/mac"
 func Run()int{
   x:=a.Maybe[int]::Some(a.answer!())
   y:=a.Outcome[int,string]::Ok(7)
   n:=match x {a.Maybe[int]::Some(v)=>v,a.Maybe[int]::None=>0,}
   return n+match y {a.Outcome[int,string]::Ok(v)=>v,a.Outcome[int,string]::Err(_)=>0,}
 }
 `,
		"client/client_test.go": "package client\nimport \"testing\"\nfunc TestRun(t *testing.T){if Run()!=49{t.Fatal(Run())}}\n",
	})
	out := filepath.Join(t.TempDir(), "out")
	if _, e := CompileModule(src, out); e != nil {
		t.Fatal(e)
	}
	useGo(t, out, "test", "-race", "./...")
	client := t.TempDir()
	useWrite(t, client, map[string]string{
		"go.mod": "module example.org/consumer\n\ngo 1.27\nrequire example.org/mac v0.0.0\nreplace example.org/mac => " + out + "\n",
		"client.gox": `package consumer
 import a "example.org/mac"
 use a::Maybe as Local;
 func F()int{
  var x Local[int]=a.Maybe[int]::Some(8)
  return match x {a.Maybe[int]::Some(v)=>v,a.Maybe[int]::None=>0,}
 }
 `,
		"client_test.go": "package consumer\nimport \"testing\"\nfunc TestF(t *testing.T){if F()!=8{t.Fatal(F())}}\n",
	})
	consumer := filepath.Join(t.TempDir(), "out")
	if _, e := CompileModule(client, consumer); e != nil {
		t.Fatal(e)
	}
	useGo(t, consumer, "test", "-race", "./...")

}

func TestUseMacroInternalRuntimeRoute(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"pure", "42", true}, {"runtime", "Helper()", false},
		{"imported-runtime", "q.Helper()", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			useWrite(t, src, map[string]string{
				"go.mod":                "module example.org/mac\n\ngo 1.27\n",
				"impl/internal/q/q.go":  "package q\nfunc Helper()int{return 42}\n",
				"impl/internal/p/p.gox": "package p\nimport q \"example.org/mac/impl/internal/q\"\nvar _=q.Helper\nfunc Helper()int{return 42}\npub macro_rules! answer { () => { " + tc.body + " }; }\n",
				"impl/api/api.gox":      "package api\nimport p \"example.org/mac/impl/internal/p\"\npub use p::answer;\n",
				"client/client.gox":     "package client\nimport a \"example.org/mac/impl/api\"\nfunc F()int{return a.answer!()}\n",
				"client/client_test.go": "package client\nimport \"testing\"\nfunc TestF(t *testing.T){if F()!=42{t.Fatal(F())}}\n",
			})
			out := filepath.Join(t.TempDir(), "out")
			_, e := CompileModule(src, out)
			if tc.allowed {
				if e != nil {
					t.Fatal(e)
				}
				useGo(t, out, "test", "./...")
			} else if e == nil || !strings.Contains(e.Error(), "unsupported macro runtime route") {
				t.Fatalf("want internal runtime route rejection, got %v", e)
			}
		})
	}
}
