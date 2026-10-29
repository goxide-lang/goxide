package acceptance_test

import (
	"fmt"

	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExternalDefaultModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := acceptanceCLI(t)
	src := formalDependencyDir(t)
	writeStdFormalManifest(t, src, "defaults.example/lib", "library", nil)

	packagesWrite(t, src, "lib.gox", "package library; use std; use std::PartialEq;\npub mod model;\npub mod other;\npub mod relay;\npub mod fake;\n")
	packagesWrite(t, src, "model/types.gox", `package model; use std; use std::PartialEq;
type Manual struct{value int}
var Calls int
func Value(v Manual)int{return v.value}
#[derive(std::derive::Default)]
pub enum E {
 #[default]
 Ready(Manual),
 Other([]int),
}
func Read(v E)int{return match v {E::Ready(n)=>Value(n),E::Other(_)=>-1,}}

impl E {
 pub func initial() Self { return std::default_value[Self]() }
}
`)
	packagesWrite(t, src, "model/impl.gox", `package model; use std; use std::PartialEq;
impl std::Default for Manual {func Default()Manual{Calls++;return Manual{value:7}}}

impl Manual {
 pub func initial() Self { return std::default_value[Self]() }
}
`)
	packagesWrite(t, src, "other/model.gox", `package other; use std; use std::PartialEq;
#[derive(std::derive::Default)]
type Manual struct{N int}

impl Manual {
 pub func initial() Self { return std::default_value[Self]() }
}
`)
	packagesWrite(t, src, "relay/relay.go", `package relay
import m "defaults.example/lib/model"
type E=m.E
type Manual=m.Manual
type New m.Manual
`)
	packagesWrite(t, src, "fake/fake.go", `package fake
type Value struct{N int}
func DefaultValue()Value{return Value{N:99}}
`)
	lib := filepath.Join(t.TempDir(), "lib")
	lib = strings.TrimSpace(packagesCommand(t, root, cli, true, "transpile", "--lib", "-o", lib, src))
	appsrc := t.TempDir()
	writeStdFormalManifest(t, appsrc, "defaults.example/app", "app", map[string]string{"library": src})

	packagesWrite(t, appsrc, "app.gox", `package app; use std; use std::PartialEq;
use library::relay as out; use library::model as m;
#[derive(std::derive::Default)]
type Wrap struct{V out::E;M m::Manual}


impl Wrap {
 pub func initial() Self { return std::default_value[Self]() }
}
`)
	packagesWrite(t, appsrc, "other.gox", `package app; use std; use std::PartialEq;
use library::other as m;
#[derive(std::derive::Default)]
type Other struct{V m::Manual}

impl Other {
 pub func initial() Self { return std::default_value[Self]() }
}
`)
	packagesWrite(t, appsrc, "ops/macros.gox", `package ops; use std; use std::PartialEq;
pub macro_rules! size {($x:expr)=>{len($x)};}
`)
	packagesWrite(t, appsrc, "builtin.gox", `package app; use std; use std::PartialEq;
use library::model as len;
mod ops;
#[derive(std::derive::Default)]
type Both struct{M len::Manual}
func Count()int{return ops::size!("abc")}

impl Both {
 pub func initial() Self { return std::default_value[Self]() }
}
`)
	packagesWrite(t, appsrc, "skip.gox", `package app; use std; use std::PartialEq; use serde;
use library::model as m;
#[derive(serde::derive::Deserialize)]
type Skipped struct {
 #[serde(skip)]
 V m::Manual
}

impl Skipped {
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
`)
	out := filepath.Join(t.TempDir(), "out")
	compileStdFormalGraph(t, true, appsrc, out)
	repeat := filepath.Join(t.TempDir(), "repeat")
	compileStdFormalGraph(t, true, appsrc, repeat)
	err := filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(out, path)
		a, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(filepath.Join(repeat, rel))
		if e != nil {
			return e
		}
		if string(a) != string(b) {
			return fmt.Errorf("nondeterministic %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, out, "app_test.go", `package app
import("testing";m "defaults.example/lib/model";"github.com/goxide-lang/serde/json")
func TestExternalDefaults(t *testing.T){m.Calls=0;v:=(WrapOps{}).Initial();if m.Read(v.V)!=7||m.Value(v.M)!=7||m.Calls!=2{t.Fatal(v,m.Calls)};if (OtherOps{}).Initial().V.N!=0{t.Fatal("cross-file alias bound to wrong package")};s,e:=serdejson.Unmarshal([]byte("{}"),(SkippedOps{}).Decode);if e!=nil||m.Value(s.V)!=7||m.Calls!=3{t.Fatal(s,e,m.Calls)};b:=(BothOps{}).Initial();if m.Value(b.M)!=7||Count()!=3||m.Calls!=4{t.Fatal(b,m.Calls)}}
`)
	plain := t.TempDir()
	writeStdNativeConsumerManifest(t, plain, "defaults.example/plain", out)
	packagesWrite(t, plain, "consumer_test.go", `package plain
import("testing";a "defaults.example/app";m "defaults.example/lib/model")
func TestGoConsumer(t *testing.T){v:=(a.WrapOps{}).Initial();if m.Read(v.V)!=7||m.Value(v.M)!=7{t.Fatal(v)}}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-mod=mod", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	for _, tc := range []struct{ path, typ string }{{"fake", "Value"}, {"relay", "New"}} {
		bad := t.TempDir()
		writeStdFormalManifest(t, bad, "defaults.example/bad", "bad", map[string]string{"library": src})

		packagesWrite(t, bad, "bad.gox", fmt.Sprintf("package bad; use std; use std::PartialEq;\nuse library::%s as m;\n#[derive(std::derive::Default)]\ntype Wrap struct{V m::%s}\n\nimpl Wrap {\n pub func initial() Self { return std::default_value[Self]() }\n}\n", tc.path, tc.typ))
		log := compileStdFormalGraph(t, false, bad, filepath.Join(t.TempDir(), "bad"))
		if !strings.Contains(log, "Default") {
			t.Fatal(log)
		}
	}
}
