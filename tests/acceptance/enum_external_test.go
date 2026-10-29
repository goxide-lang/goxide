package acceptance_test

import (
	"fmt"

	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCrossModuleEnumArtifacts(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "run", "./scripts/build.go", "-o", cli)
	src := formalDependencyDir(t)
	writeStdFormalManifest(t, src, "enum.example/library", "library", nil)

	packagesWrite(t, src, "lib.gox", "package library; use std; use std::PartialEq;\npub mod model;\npub mod relay;\npub mod other;\npub mod api;\npub mod internal;\n")
	packagesWrite(t, src, "model/model.gox", `package model; use std; use std::PartialEq; use serde;
#[derive(std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize)]
type hidden struct{N int}
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize)]
pub enum E {
 #[default]
 A,
 Count(int),
 Batch([]int),
 Hidden(hidden),
}
func View(r &int)&int{return r}
func Secret()E{return E::Hidden(hidden{N:9})}

impl hidden {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl E {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
`)
	packagesWrite(t, src, "model/native.go", "//go:build !extra\n\npackage model\nfunc Native()int{return 1}\n")
	packagesWrite(t, src, "model/native_extra.go", "//go:build extra\n\npackage model\nfunc Native()int{return 2}\n")
	packagesWrite(t, src, "relay/relay.go", `package relay
import m "enum.example/library/model"
type Alias=m.E
type New m.E
func Get()Alias{return &m.Count{Value: 6}}
`)

	packagesWrite(t, src, "internal/lib.gox", "package internal; pub mod hidden;\n")
	packagesWrite(t, src, "internal/hidden/hidden.gox", `package hidden; use std; use std::PartialEq;
pub enum E { A(int), }
`)
	packagesWrite(t, src, "api/api.go", `package api
import h "enum.example/library/internal/hidden"
type Public=h.E
func Get()Public{return &h.A{Value: 8}}
func Read(e Public)int{return e.(*h.A).Value}
`)
	packagesWrite(t, src, "other/other.gox", `package other; use std; use std::PartialEq;
pub enum E { A, Count(int), }
`)
	library := filepath.Join(t.TempDir(), "library")
	library = strings.TrimSpace(packagesCommand(t, root, cli, true, "transpile", "--lib", "-o", library, src))
	compare := func(a, b string) {
		t.Helper()
		e := filepath.WalkDir(a, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(a, path)
			x, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			y, e := os.ReadFile(filepath.Join(b, rel))
			if e != nil {
				return e
			}
			if string(x) != string(y) {
				return fmt.Errorf("nondeterministic %s", rel)
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	again := filepath.Join(t.TempDir(), "library")
	again = strings.TrimSpace(packagesCommand(t, root, cli, true, "transpile", "--lib", "-o", again, src))
	compare(library, again)
	appFor := func(body string) string {
		dir := t.TempDir()
		writeStdFormalManifest(t, dir, "enum.example/app", "app", map[string]string{"library": src})

		packagesWrite(t, dir, "app.gox", body)
		return dir
	}
	app := appFor(`package app; use std; use std::PartialEq;
use library::relay as r; use library::model as m; use serde::json as serdejson;
func Score(e r::Alias)int{return match e{r::Alias::A=>0,r::Alias::Count(n)=>n,r::Alias::Batch(xs)=>len(xs),r::Alias::Hidden(v)=>v.N,}}
func Run()int{return Score(r::Get())+Score(r::Alias::Count(7))+Score(m::Secret())}
func Rebuild(e r::Alias)r::Alias{return match e{r::Alias::Hidden(v)=>r::Alias::Hidden(v),_=>e,}}
func Decode()(int,error){e,err:=serdejson::Unmarshal([]byte("{\"Count\":4}"),m::E::decode);if err!=nil{return 0,err};return Score(e),nil}
func Ordered(e r::Alias,seen *bool)int{return match e{r::Alias::Count(n) if mark(seen)=>n,_=>0,}}
func mark(seen *bool)bool{*seen=true;return true}
func Read()int{n:=3;return *m::View(&n)}
func Cross(e r::Alias)int{return match e{m::E::A=>0,r::Alias::Count(n)=>n,m::E::Batch(xs)=>len(xs),r::Alias::Hidden(v)=>v.N,}}
`)
	packagesWrite(t, app, "other.gox", `package app; use std; use std::PartialEq;
use library::model as renamed; use library::api as a;
func Facade()int{return a::Read(a::Get())}
func Other()int{return match renamed::E::Count(2){renamed::E::Count(n)=>n,_=>0,}}
`)
	out := filepath.Join(t.TempDir(), "app")
	compileStdFormalGraph(t, true, app, out)
	out2 := filepath.Join(t.TempDir(), "app")
	compileStdFormalGraph(t, true, app, out2)
	compare(out, out2)
	packagesWrite(t, out, "app_test.go", `package app
import("testing";m "enum.example/library/model";r "enum.example/library/relay")
func TestBusiness(t *testing.T){if Run()!=22||Other()!=2||Read()!=3||Facade()!=8||Cross(&m.Count{Value: 4})!=4||Score(Rebuild(m.Secret()))!=9{t.Fatal("business")};if n,e:=Decode();e!=nil||n!=4{t.Fatal(n,e)}}
func TestInvalidBeforeGuard(t *testing.T){for _,e:=range []r.Alias{nil}{func(){seen:=false;defer func(){if recover()==nil||seen{t.Fatal("invalid before guard")}}();Ordered(e,&seen)}()}}
`)
	packagesWrite(t, library, "model/model_test.go", `package model
import "testing"
func TestProducer(t *testing.T){e:=(EOps{}).Initial();if (EOps{}).Describe(&e)!="E::A"{t.Fatal((EOps{}).Describe(&e))};_=(EOps{}).Copy(&e)}
`)
	plain := t.TempDir()
	writeStdNativeConsumerManifest(t, plain, "enum.example/plain", library)
	packagesWrite(t, plain, "plain_test.go", `package plain
import("testing";m "enum.example/library/model";r "enum.example/library/relay")
func TestGo(t *testing.T){var e r.Alias=&m.Count{Value: 5};if e.(*m.Count).Value!=5{t.Fatal(e)}}
`)
	for _, dir := range []string{library, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-mod=mod", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	// The unexported marker prevents another package from implementing E.
	assertNativeGoRejected(t, out, tool, map[string]nativeGoRejection{
		"enum-forged": {"package app\nimport o \"enum.example/library/other\"\ntype forged struct{}\nfunc (*forged) isE() {}\nvar _ o.E=(*forged)(nil)\n", "unexported method isE"},
	})
	for name, body := range map[string]string{
		"non-exhaustive":  `func Bad(e r::Alias)int{return match e{r::Alias::A=>0,}}`,
		"unknown-variant": `func Bad()r::Alias{return r::Alias::Missing}`,
		"readonly":        `func Bad(r &int){m::View(r);*r=1}`,
	} {
		t.Run(name, func(t *testing.T) {
			src := appFor("package app; use std; use std::PartialEq;\nuse library::relay as r; use library::model as m;\nvar _=m::Secret\n" + body)
			msg := compileStdFormalGraph(t, false, src, filepath.Join(t.TempDir(), "out"))
			if !strings.Contains(msg, ".gox:") {
				t.Fatal(msg)
			}
		})
	}
	for name, body := range map[string]string{
		"new-defined-not-alias": `use library::relay as r;
func Bad()r::New{return r::New::Count(1)}`,
		"distinct-package-identity": `use library::relay as r; use library::other as o;
func Bad(e r::Alias)int{return match e{r::Alias::A=>0,o::E::Count(n)=>n,_=>0,}}`,
		"internal-origin-pattern": `use library::api as a;
func Bad()int{return match a::Get(){a::Public::A(n)=>n,}}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := appFor("package app; use std; use std::PartialEq;\n" + body)
			msg := compileStdFormalGraph(t, false, source, filepath.Join(t.TempDir(), "out"))
			if !strings.Contains(msg, ".gox:") {
				t.Fatal(msg)
			}
			if name == "internal-origin-pattern" && !strings.Contains(msg, "internal-origin alias") {
				t.Fatal(msg)
			}
		})
	}

}
