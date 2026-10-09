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
	options := useTestOptions(map[string]string{"api": "example.org/mac/api", "left": "example.org/mac/left", "p": "example.org/mac/p", "right": "example.org/mac/right"})

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
 use p;
 pub use p::answer as value;
 func init(){p::Trace+=10}
 `,
		"right/right.gox": `package right
 use p;
 pub use p::answer as value;
 func init(){p::Trace+=100}
 `,
		"api/api.gox": `package api
 use left as l;
 use right as r;
 use p;
 pub use l::*;
 pub use r::*;
 pub use p::identity;
 func init(){p::Trace+=1000}
 `,
		"client/client.gox": `package client
 use api as a;
 use p;
 use a::value as local;
 func Helper(x int)int{return -100}
 func Run()bool{
   Helper:=func(x int)int{return -200}; _=Helper
   return a::value!(40)==41&&local!(41)==42&&a::identity!(Generic[int](7))==7&&p::Trace==1111
 }
 `,
		"client/generic.go": "package client\nfunc Generic[T any](x T)T{return x}\n",
		"client/other.gox": `package client
 use api::value as local;
 func Other()int{return local!(8)}
 `,
		"client/client_test.go": `package client
 import "testing"
 func TestRun(t *testing.T){if !Run()||Other()!=9{t.Fatal("macro definition/init/alias identity")}}
 `,
	})
	out := filepath.Join(t.TempDir(), "out")
	if _, e := CompileModuleWithOptions(src, out, options); e != nil {
		t.Fatal(e)
	}
	useGo(t, out, "test", "-race", "./...")
	useGo(t, out, "vet", "./...")
	repeat := filepath.Join(t.TempDir(), "out")
	if _, e := CompileModuleWithOptions(src, repeat, options); e != nil {
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
	options := useTestOptions(map[string]string{"a": "example.org/mac/a", "p": "example.org/mac/p"})

	for _, tc := range []struct{ name, origin, api, client, want string }{
		{"reserved-print", `pub macro_rules! answer { () => { 1 }; }`, "pub use p::answer as print;", "", "reserved builtin macro name"},
		{"reserved-result", `pub macro_rules! answer { () => { 1 }; }`, "pub use p::answer as go_result;", "", "reserved builtin macro name"},
		{"private-helper", `func hidden()int{return 1}
 pub macro_rules! answer { () => { hidden() }; }`, "pub use p::answer;", "func F()int{return a::answer!()}", "private runtime helper"},
		{"private-alias", `pub macro_rules! answer { () => { 1 }; }`, "use p::answer;", "func F()int{return a::answer!()}", "private"},
		{"macro-conflict", `pub macro_rules! answer { () => { 1 }; }`, "pub use p::answer;\npub macro_rules! answer { () => { 2 }; }", "", "conflicts with a package declaration"},
		{"string-path", `pub macro_rules! answer { () => { 1 }; }`, `pub use "example.org/mac/p";`, "", "expected [pub] use"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			useWrite(t, src, map[string]string{
				"go.mod":            "module example.org/mac\n\ngo 1.27\n",
				"p/p.gox":           "package p\n" + tc.origin + "\n",
				"a/a.gox":           "package a\nuse p;\n" + tc.api + "\n",
				"client/client.gox": "package client\nuse a;\n" + tc.client + "\n",
			})
			_, e := CompileModuleWithOptions(src, filepath.Join(t.TempDir(), "out"), options)
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, e)
			}
		})
	}
}

func TestUseMacroExternalSourceRegistryBoundary(t *testing.T) {
	options := useTestOptions(map[string]string{"mac": "example.org/mac", "p": "example.org/mac/p"})

	src := t.TempDir()
	producer := filepath.Join(t.TempDir(), "out")
	useWrite(t, src, map[string]string{
		"go.mod":  "module example.org/mac\n\ngo 1.27\n",
		"p/p.gox": "package p\npub macro_rules! answer { () => { 42 }; }\nconst C=7\n",
		"api.gox": "package api\nuse p;\npub use p::*;\n",
	})
	if _, e := CompileModuleWithOptions(src, producer, options); e != nil {
		t.Fatal(e)
	}
	for _, selection := range []string{"answer", "*"} {
		client := t.TempDir()
		useWrite(t, client, map[string]string{
			"go.mod":     "module example.org/client\n\ngo 1.27\nrequire example.org/mac v0.0.0\nreplace example.org/mac => " + producer + "\n",
			"client.gox": "package client\nuse mac as a;\npub use a::" + selection + ";\n",
		})
		_, e := CompileModuleWithOptions(client, filepath.Join(t.TempDir(), "out"), options)
		if e == nil || !strings.Contains(e.Error(), "compiled source registry") {
			t.Fatalf("want explicit source registry boundary, got %v", e)
		}
		useWrite(t, client, map[string]string{"client.gox": "package client\nuse mac as a;\npub use a::C;\n"})
		if _, e = CompileModuleWithOptions(client, filepath.Join(t.TempDir(), "ok"), options); e != nil {
			t.Fatal(e)
		}

		if selection == "answer" {
			_, e = CompileModuleWithOptions(client, filepath.Join(t.TempDir(), "race"), useTestRaceOptions(options))
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
			_, e = CompileModuleWithOptions(client, filepath.Join(t.TempDir(), "tampered"), options)
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
	options := useTestOptions(map[string]string{"p": "example.org/mac/p"})

	for _, selection := range []string{"C", "F", "*"} {
		src := t.TempDir()
		useWrite(t, src, map[string]string{
			"go.mod":  "module example.org/mac\n\ngo 1.27\n",
			"p/p.gox": "package p\npub macro_rules! F { () => { 42 }; }\nfunc F()int{return 3}\nconst C=7\n",
			"api.gox": "package api\nuse p;\npub use p::" + selection + ";\n",
		})
		_, e := CompileModuleWithOptions(src, filepath.Join(t.TempDir(), "out"), options)
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
	options := useTestOptions(map[string]string{"left": "example.org/mac/left", "mac": "example.org/mac", "p": "example.org/mac/p"})

	src := t.TempDir()
	useWrite(t, src, map[string]string{
		"go.mod": "module example.org/mac\n\ngo 1.27\n",
		"p/p.gox": `package p
 pub macro_rules! answer { () => { 42 }; }
 func Pick()Option[int]{return Option[int]::Some(3)}
 func ResultValue()Result[int,string]{return Result[int,string]::Ok(7)}
 `,
		"left/left.gox": `package left
 use p;
 pub use p::*;
 `,
		"api.gox": `package api
 use left as l;
 pub use l::Option as Maybe;
 pub use l::Result as Outcome;
 pub use l::answer;
 `,
		"client/client.gox": `package client
 use mac as a;
 func Run()int{
   x:=a::Maybe[int]::Some(a::answer!())
   y:=a::Outcome[int,string]::Ok(7)
   n:=match x {a::Maybe[int]::Some(v)=>v,a::Maybe[int]::None=>0,}
   return n+match y {a::Outcome[int,string]::Ok(v)=>v,a::Outcome[int,string]::Err(_)=>0,}
 }
 `,
		"client/client_test.go": "package client\nimport \"testing\"\nfunc TestRun(t *testing.T){if Run()!=49{t.Fatal(Run())}}\n",
	})
	out := filepath.Join(t.TempDir(), "out")
	if _, e := CompileModuleWithOptions(src, out, options); e != nil {
		t.Fatal(e)
	}
	useGo(t, out, "test", "-race", "./...")
	client := t.TempDir()
	useWrite(t, client, map[string]string{
		"go.mod": "module example.org/consumer\n\ngo 1.27\nrequire example.org/mac v0.0.0\nreplace example.org/mac => " + out + "\n",
		"client.gox": `package consumer
 use mac as a;
 use a::Maybe as Local;
 func F()int{
  var x Local[int]=a::Maybe[int]::Some(8)
  return match x {a::Maybe[int]::Some(v)=>v,a::Maybe[int]::None=>0,}
 }
 `,
		"client_test.go": "package consumer\nimport \"testing\"\nfunc TestF(t *testing.T){if F()!=8{t.Fatal(F())}}\n",
	})
	consumer := filepath.Join(t.TempDir(), "out")
	if _, e := CompileModuleWithOptions(client, consumer, options); e != nil {
		t.Fatal(e)
	}
	useGo(t, consumer, "test", "-race", "./...")

}

func TestUseMacroInternalRuntimeRoute(t *testing.T) {
	options := useTestOptions(map[string]string{"api": "example.org/mac/impl/api", "p": "example.org/mac/impl/internal/p", "q": "example.org/mac/impl/internal/q"})

	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"pure", "42", true}, {"runtime", "Helper()", false},
		{"imported-runtime", "q::Helper()", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			useWrite(t, src, map[string]string{
				"go.mod":                "module example.org/mac\n\ngo 1.27\n",
				"impl/internal/q/q.go":  "package q\nfunc Helper()int{return 42}\n",
				"impl/internal/p/p.gox": "package p\nuse q;\nvar _=q::Helper\nfunc Helper()int{return 42}\npub macro_rules! answer { () => { " + tc.body + " }; }\n",
				"impl/api/api.gox":      "package api\nuse p;\npub use p::answer;\n",
				"client/client.gox":     "package client\nuse api as a;\nfunc F()int{return a::answer!()}\n",
				"client/client_test.go": "package client\nimport \"testing\"\nfunc TestF(t *testing.T){if F()!=42{t.Fatal(F())}}\n",
			})
			out := filepath.Join(t.TempDir(), "out")
			_, e := CompileModuleWithOptions(src, out, options)
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

func TestUseMacroFunctionAliasDefinitionScope(t *testing.T) {
	source := t.TempDir()
	options := useTestOptions(map[string]string{"strings": "strings", "strconv": "strconv", "wrap": "example.org/function-use/wrap"})
	useWrite(t, source, map[string]string{
		"go.mod": "module example.org/function-use\n\ngo 1.27\n",
		"wrap/macros.gox": `package wrap
use strings::Repeat as selected;
pub macro_rules! twice {($x:expr)=>{selected($x,2)};}
`,
		"client/client.gox": `package client
use wrap;
use strconv::Itoa as selected;
func Run()string{selected:=func()string{return "wrong"};_=selected;return wrap::twice!("x")}
`,
		"client/client_test.go": "package client\nimport \"testing\"\nfunc TestRun(t *testing.T){if Run()!=\"xx\"{t.Fatal(Run())}}\n",
	})
	out := filepath.Join(t.TempDir(), "out")
	if _, err := CompileModuleWithOptions(source, out, options); err != nil {
		t.Fatal(err)
	}
	useGo(t, out, "test", "./...")
}

func TestUseMacroPrivateAliasDoesNotLeakFiles(t *testing.T) {
	source := t.TempDir()
	options := useTestOptions(map[string]string{"origin": "example.org/private-use/origin"})
	useWrite(t, source, map[string]string{
		"go.mod":           "module example.org/private-use\n\ngo 1.27\n",
		"origin/macro.gox": "package origin\npub macro_rules! answer {()=>{42};}\n",
		"client/one.gox":   "package client\nuse origin::answer as local;\nfunc One()int{return local!()}\n",
		"client/two.gox":   "package client\nfunc Two()int{return local!()}\n",
	})
	_, err := CompileModuleWithOptions(source, filepath.Join(t.TempDir(), "out"), options)
	if err == nil || !strings.Contains(err.Error(), "two.gox") || !strings.Contains(err.Error(), "unknown macro local") {
		t.Fatalf("private macro alias leaked across files: %v", err)
	}
}
