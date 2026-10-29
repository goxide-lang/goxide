package acceptance_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// A single input has one module owner. Exercise recovery and actual CLI execution
// separately: compiling emitted Go in a test module cannot prove build/run kept
// the input's requirements, replacements, package identity, or file selection.
func TestSingleFileExternalRecoveryCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "run", "./scripts/build.go", "-o", cli)
	outside := t.TempDir()
	base := formalDependencyDir(t)
	producer := filepath.Join(base, "producer")
	library := filepath.Join(base, "library")
	module := func(name, replacement string) string {
		s := fmt.Sprintf("module %s\ngo 1.27\n", name) + stdFixtureGoRequirements(t)
		if replacement != "" {
			s += "require a06.example/lib v0.0.0\nreplace a06.example/lib => " + strconvQuote(replacement) + "\n"
		}
		return s
	}
	writeStdFormalManifest(t, producer, "a06.example/lib", "library", nil)
	packagesWrite(t, producer, "lib.gox", "package library; use std; use std::PartialEq;\npub mod model;\npub mod relay;\npub mod other;\npub mod fake;\npub mod api;\npub mod uses;\npub mod internal;\n")
	packagesWrite(t, producer, "internal/lib.gox", "package internal; use std; use std::PartialEq;\npub mod hidden;\n")
	packagesWrite(t, producer, "model/model.gox", singleExternalProducer)
	packagesWrite(t, producer, "relay/relay.go", `package relay
import __borrow_std "github.com/goxide-lang/std/sum"

import m "a06.example/lib/model"
type Item=m.Item
type E=m.E
type New m.Item
type Maybe=__borrow_std.Option[int]
type Outcome=__borrow_std.Result[int,error]
func Get()E{return (m.EOps{}).Initial()}
`)
	packagesWrite(t, producer, "other/model.gox", "package other; use std; use std::PartialEq;\npub enum E{Empty,Item(int),}\n")
	packagesWrite(t, producer, "internal/hidden/model.gox", "package hidden; use std; use std::PartialEq;\npub enum E{Value(int),}\nfunc Get()E{return E::Value(8)}\nfunc Read(v E)int{return match v{E::Value(n)=>n}}\n")
	packagesWrite(t, producer, "api/api.go", `package api
import h "a06.example/lib/internal/hidden"
type Public=h.E
func Get()Public{return h.Get()}
func Read(v Public)int{return h.Read(v)}
`)
	packagesWrite(t, producer, "fake/fake.go", `package fake
import "github.com/goxide-lang/serde"
type Item struct{N int;Name string}
func DefaultItem()Item{return Item{N:99}}
func CloneItem(v *Item)Item{return *v}
func DebugItem(v *Item)string{return "fake"}
func EqualsItem(a,b Item)bool{return true}
func SerializeItem(s serde.Serializer,v *Item)error{return nil}
func DeserializeItem(d serde.Deserializer)(Item,error){return Item{},nil}
`)
	packagesWrite(t, producer, "uses/api.gox", "package uses; use std; use std::PartialEq;\nuse crate::model as p;\npub use p::Plain;\n")
	library = strings.TrimSpace(deliveryCommand(t, outside, cli, true, nil, "transpile", "--lib", "-o", library, producer))
	beforeProducer := singleExternalSnapshot(t, producer)
	again := filepath.Join(t.TempDir(), "library")
	again = strings.TrimSpace(deliveryCommand(t, outside, cli, true, nil, "transpile", "--lib", "-o", again, producer))
	singleExternalSameTree(t, library, again)
	singleExternalUnchanged(t, producer, beforeProducer)
	defer singleExternalUnchanged(t, producer, beforeProducer)
	packagesWrite(t, library, "model/producer_test.go", singleExternalProducerAssertions)
	beforeLibrary := singleExternalSnapshot(t, library)
	defer singleExternalUnchanged(t, library, beforeLibrary)
	consumer := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", module("a06.example/app", producer))
		writeSingleExternalRoots(t, dir)
		packagesWrite(t, dir, "app.gox", body)
		return dir
	}
	execute := func(t *testing.T, cwd, input, want string, extra []string) {
		t.Helper()
		owner := filepath.Dir(input)
		for {
			if _, err := os.Stat(filepath.Join(owner, "go.mod")); err == nil {
				break
			}
			parent := filepath.Dir(owner)
			if parent == owner {
				t.Fatal("missing fixture owner")
			}
			owner = parent
		}
		before := singleExternalSnapshot(t, owner)
		defer singleExternalUnchanged(t, owner, before)
		binary := filepath.Join(t.TempDir(), "app")
		deliveryCommand(t, cwd, cli, true, extra, "build", "--go", tool, "-o", binary, input)
		if got := deliveryCommand(t, cwd, binary, true, nil); got != want {
			t.Fatalf("binary output %q; want %q", got, want)
		}
		if got := deliveryCommand(t, cwd, cli, true, extra, "run", "--runtime", root, "--go", tool, input); got != want {
			t.Fatalf("run output %q; want %q", got, want)
		}
	}
	t.Run("registered-protocols-from-source", func(t *testing.T) {
		src := consumer(t, singleExternalConsumer)
		before := singleExternalSnapshot(t, src)
		defer singleExternalUnchanged(t, src, before)
		out, repeat := filepath.Join(t.TempDir(), "out"), filepath.Join(t.TempDir(), "repeat")
		deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", out, src)
		deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", repeat, src)
		singleExternalSameTree(t, out, repeat)
		packagesWrite(t, out, "app_test.go", singleExternalAssertions)
		deliveryCommand(t, out, tool, true, nil, "test", "-mod=readonly", "-race", "-count=1", "-v", "./...")
		deliveryCommand(t, out, tool, true, nil, "vet", "-mod=readonly", "./...")
		if got := deliveryCommand(t, out, tool, true, nil, "run", "-mod=readonly", "-race", "."); got != singleExternalOutput {
			t.Fatalf("business output %q; want %q", got, singleExternalOutput)
		}
	})
	t.Run("known-module-build-run", func(t *testing.T) {
		src := consumer(t, "package main; use std; use std::PartialEq;\nuse fmt; use library::model as m;\nfunc main(){v:=m::Item::initial();fmt::Println(v.N,v.Name)}\n")
		execute(t, outside, filepath.Join(src, "app.gox"), "7 producer\n", nil)
	})
	t.Run("ordinary-api-keeps-consumer-dependencies", func(t *testing.T) {
		src := consumer(t, singleExternalPlain)
		execute(t, outside, filepath.Join(src, "app.gox"), "7\n", nil)
	})
	t.Run("producer-and-native-consumer", func(t *testing.T) {
		deliveryCommand(t, library, tool, true, nil, "test", "-mod=readonly", "-race", "-count=1", "./...")
		deliveryCommand(t, library, tool, true, nil, "vet", "-mod=readonly", "./...")
		plain := t.TempDir()
		writeStdNativeConsumerManifest(t, plain, "a06.example/plain", library)
		packagesWrite(t, plain, "plain_test.go", singleExternalNativeAssertions)
		deliveryCommand(t, plain, tool, true, nil, "mod", "tidy")
		before := singleExternalSnapshot(t, plain)
		deliveryCommand(t, plain, tool, true, nil, "test", "-mod=readonly", "-race", "-count=1", "-v", "./...")
		deliveryCommand(t, plain, tool, true, nil, "vet", "-mod=readonly", "./...")
		singleExternalUnchanged(t, plain, before)
	})
	t.Run("owner-cwd-nested-symlink-and-move", func(t *testing.T) {
		parent := t.TempDir()
		fake := filepath.Join(parent, "fake")
		packagesWrite(t, fake, "go.mod", "module a06.example/lib\ngo 1.27\n")
		packagesWrite(t, fake, "model/model.go", "package model\nfunc Plain()int{return 99}\n")
		packagesWrite(t, parent, "go.mod", module("a06.example/wrong", "./fake"))
		packagesWrite(t, parent, "go.work", "go 1.27\nuse .\n")
		nested := filepath.Join(parent, "nested")
		packagesWrite(t, nested, "go.mod", module("a06.example/nested", producer))
		writeSingleExternalRoots(t, nested)
		packagesWrite(t, nested, "app.gox", singleExternalPlain)
		extra := []string{"GOWORK=" + filepath.Join(parent, "go.work")}
		execute(t, parent, filepath.Join(nested, "app.gox"), "7\n", extra)
		alias := filepath.Join(parent, "linked.gox")
		if err := os.Symlink(filepath.Join(nested, "app.gox"), alias); err != nil {
			t.Fatal(err)
		}
		// Logical link location has the conflicting module, while the real input owns dependencies.
		binary := filepath.Join(t.TempDir(), "linked")
		deliveryCommand(t, parent, cli, true, extra, "build", "--runtime", root, "--go", tool, "-o", binary, alias)
		if got := deliveryCommand(t, outside, binary, true, nil); got != "7\n" {
			t.Fatal(got)
		}
		original := filepath.Join(t.TempDir(), "original")
		singleExternalCopyTree(t, producer, filepath.Join(original, "library"), "target")
		app := filepath.Join(original, "consumer")
		packagesWrite(t, app, "go.mod", module("a06.example/movable", "../library"))
		writeSingleExternalRoots(t, app)
		packagesWrite(t, app, "app.gox", singleExternalPlain)
		execute(t, parent, filepath.Join(app, "app.gox"), "7\n", extra)
		moved := filepath.Join(t.TempDir(), "moved")
		if err := os.Rename(original, moved); err != nil {
			t.Fatal(err)
		}
		before := singleExternalSnapshot(t, moved)
		execute(t, parent, filepath.Join(moved, "consumer/app.gox"), "7\n", extra)
		singleExternalUnchanged(t, moved, before)
	})
	t.Run("selected-file-only", func(t *testing.T) {
		src := consumer(t, "package main; use std; use std::PartialEq;\nuse fmt;\nfunc main(){fmt::Println(fromSibling())}\n")
		packagesWrite(t, src, "sibling.go", "package main\nfunc fromSibling()int{return 12}\n")
		before := singleExternalSnapshot(t, src)
		msg := deliveryCommand(t, outside, cli, false, nil, "build", "--runtime", root, "--go", tool, "-o", filepath.Join(t.TempDir(), "bad"), filepath.Join(src, "app.gox"))
		singleExternalContains(t, msg, "app.gox:3:26:", "unresolved name")
		binary := filepath.Join(t.TempDir(), "directory")
		deliveryCommand(t, outside, cli, true, nil, "build", "--go", tool, "-o", binary, src)
		if got := deliveryCommand(t, outside, binary, true, nil); got != "12\n" {
			t.Fatal(got)
		}
		singleExternalUnchanged(t, src, before)
		safe := consumer(t, singleExternalPlain)
		packagesWrite(t, safe, "poison.gox", "package main; use std; use std::PartialEq;\nthis is invalid source\n")
		execute(t, outside, filepath.Join(safe, "app.gox"), "7\n", nil)
		msg = deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "out"), safe)
		singleExternalContains(t, msg, "poison.gox:2:1:", "expected declaration")
	})
	t.Run("internal-and-public-facade", func(t *testing.T) {
		lib := filepath.Join(t.TempDir(), "library")
		singleExternalCopyTree(t, filepath.Dir(library), lib)
		lib = filepath.Join(lib, "module")
		writeSingleExternalRoots(t, lib)
		input := filepath.Join(lib, "tools/main.gox")
		packagesWrite(t, lib, "tools/main.gox", `package main;
use fmt; use library::internal::hidden as h;
func main(){fmt::Println(h::Read(h::Get()))}
`)
		execute(t, outside, input, "8\n", nil)
		bad := consumer(t, "package main; use std; use std::PartialEq;\nuse library::internal::hidden as h;\nfunc main(){_=h::Get()}\n")
		rejected := func(path string) {
			msg := deliveryCommand(t, outside, cli, false, nil, "build", "--runtime", root, "--go", tool, "-o", filepath.Join(t.TempDir(), "bad"), path)
			singleExternalContains(t, msg, "app.gox:2:", "use of internal package a06.example/lib/internal/hidden is not allowed from a06.example/app")
		}
		rejected(filepath.Join(bad, "app.gox"))
		link := filepath.Join(lib, "tools/linked.gox")
		if err := os.Symlink(filepath.Join(bad, "app.gox"), link); err != nil {
			t.Fatal(err)
		}
		rejected(link)
		facade := consumer(t, "package main; use std; use std::PartialEq;\nuse fmt; use library::api as a;\nfunc main(){fmt::Println(a::Read(a::Get()))}\n")
		execute(t, outside, filepath.Join(facade, "app.gox"), "8\n", nil)
		enhanced := consumer(t, "package main; use std; use std::PartialEq;\nuse library::api as a;\nfunc main(){var n int=match a::Get(){a::Public::Value(n)=>n};_=n}\n")
		msg := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "bad"), enhanced)
		singleExternalContains(t, msg, "app.gox:3:23:", "internal-origin alias cannot expose enum variants outside its internal package boundary")
	})

	t.Run("identity-protocol-and-permission-rejections", func(t *testing.T) {
		reject := func(t *testing.T, source, location string, words ...string) {
			t.Helper()
			src := consumer(t, source)
			msg := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "out"), src)
			singleExternalContains(t, msg, location)
			singleExternalContains(t, msg, words...)

		}
		for _, kind := range []string{"Default", "Clone", "Debug", "Serialize", "Deserialize", "PartialEq", "PartialEq,Eq"} {
			t.Run("fake-"+kind, func(t *testing.T) {
				derive, ownerUse := "std::derive::"+strings.ReplaceAll(kind, ",", ",std::derive::"), ""
				if kind == "Serialize" || kind == "Deserialize" {
					derive, ownerUse = "serde::derive::"+kind, "use serde;"
				}
				location := "app.gox:4:1:"
				want := "missing trait implementation: GoxideTrait" + strings.Split(kind, ",")[0]
				if kind == "Serialize" || kind == "Deserialize" {
					want = "does not explicitly implement " + kind
				}
				reject(t, "package main; use std; use std::PartialEq; "+ownerUse+"\nuse library::fake as f;\ntype Local=f::Item\n#[derive("+derive+")]\ntype Wrap struct{V Local}\nfunc main(){}\n", location, want)
			})
		}
		for _, tc := range []struct {
			name, source string
			words        []string
		}{
			{"new-defined", "use library::model as m;\ntype New m::Item\ntype Local=New\n#[derive(std::derive::Clone)]\ntype Wrap struct{V Local}\n", []string{"app.gox:5:1:", "missing trait implementation", "Clone"}},
			{"wrong-serde-direction", "use library::model as m; use serde;\n#[derive(serde::derive::Deserialize)]\ntype Wrap struct{V m::Output}\n", []string{"app.gox:3:1:", "does not explicitly implement Deserialize"}},
			{"different-enum", "use library::model as m; use library::other as o;\nfunc Bad(v m::E)int{return match v{m::E::Empty=>0,o::E::Item(n)=>n,_=>0}}\n", []string{"app.gox:3:51:", "pattern variant does not belong to the matched enum instance"}},
			{"readonly", "use library::model as m;\nfunc Bad(){n:=1;p:=m::View(&n);*p=2}\n", []string{"app.gox:3:32:", "cannot write through a read-only reference"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				reject(t, "package main; use std; use std::PartialEq;\n"+tc.source+"func main(){}\n", "app.gox", tc.words...)
			})
		}
	})
	t.Run("private-type-rejection", func(t *testing.T) {
		src := consumer(t, "package main; use std; use std::PartialEq;\nuse library::model as m;\nfunc main(){var x m::hidden;_=x}\n")
		for _, input := range []string{filepath.Join(src, "app.gox"), src} {
			msg := deliveryCommand(t, outside, cli, false, nil, "build", "--runtime", root, "--go", tool, "-o", filepath.Join(t.TempDir(), "bad"), input)
			singleExternalContains(t, msg, "unresolved name", "app.gox:3:22:")
		}
	})
	t.Run("runtime-does-not-lend-dependencies", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module a06.example/missing\ngo 1.27\n"), 0600); err != nil {
			t.Fatal(err)
		}
		packagesWrite(t, dir, "goxide.roots.toml", `[roots.std]
module = "github.com/goxide-lang/std"
`)
		packagesWrite(t, dir, "app.gox", "package main; use std; use std::PartialEq;\nuse std::sum as s;\nfunc main(){var v s::Option[int];_=v}\n")
		before := singleExternalSnapshot(t, dir)
		output := filepath.Join(t.TempDir(), "bad")
		msg := deliveryCommand(t, outside, cli, false, nil, "build", "--runtime", root, "--go", tool, "-o", output, filepath.Join(dir, "app.gox"))
		singleExternalContains(t, msg, `native source module github.com/goxide-lang/std is not in the selected Go module graph`)
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("failed dependency selection wrote output: %v", err)
		}
		singleExternalUnchanged(t, dir, before)
	})
	t.Run("existing-file-boundaries", func(t *testing.T) {
		src := consumer(t, "package main; use std; use std::PartialEq;\nuse fmt;\nenum Local{A,}\nfunc main(){fmt::Println(match Local::A{Local::A=>3})}\n")
		execute(t, outside, filepath.Join(src, "app.gox"), "3\n", nil)
		for _, tc := range []struct{ name, source string }{
			{"local-use", "package main; use std; use std::PartialEq;\nuse library::model::Plain;\nfunc main(){_=Plain()}\n"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				input := consumer(t, tc.source)
				deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", filepath.Join(t.TempDir(), "out.go"), filepath.Join(input, "app.gox"))
				deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", filepath.Join(t.TempDir(), "out"), input)
			})
		}
		t.Run("imported-use-from-source", func(t *testing.T) {
			input := consumer(t, "package main; use std; use std::PartialEq;\nuse fmt; use library::uses as p;\nfunc main(){fmt::Println(p::Plain())}\n")
			out, repeat := filepath.Join(t.TempDir(), "out"), filepath.Join(t.TempDir(), "repeat")
			deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", out, input)
			deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", repeat, input)
			singleExternalSameTree(t, out, repeat)
			if got := deliveryCommand(t, out, tool, true, nil, "run", "-mod=readonly", "."); got != "7\n" {
				t.Fatal(got)
			}
		})
		t.Run("pub-use-single-file-and-directory", func(t *testing.T) {
			input := consumer(t, "package main; use std; use std::PartialEq;\nuse fmt; pub use library::model::Plain;\nfunc main(){fmt::Println(Plain())}\n")
			deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", filepath.Join(t.TempDir(), "out.go"), filepath.Join(input, "app.gox"))
			deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", filepath.Join(t.TempDir(), "out"), input)
			execute(t, outside, filepath.Join(input, "app.gox"), "7\n", nil)
		})

		for _, args := range [][]string{{"transpile", "--tags", "custom"}, {"test"}} {
			argv := append(append([]string(nil), args...), filepath.Join(src, "app.gox"))
			msg := deliveryCommand(t, outside, cli, false, nil, argv...)
			singleExternalContains(t, msg, "directory")
		}
		if got := deliveryCommand(t, outside, cli, true, nil, "run", "--features", "extra", filepath.Join(src, "app.gox")); got != "3\n" {
			t.Fatalf("single-file feature selection changed program behavior: %q", got)
		}
		msg := deliveryCommand(t, outside, cli, false, nil, "run", "--workspace", filepath.Join(src, "app.gox"))
		singleExternalContains(t, msg, "workspace selection is deferred until MVP")
	})
}

func writeSingleExternalRoots(t *testing.T, dir string) {
	t.Helper()
	packagesWrite(t, dir, "goxide.roots.toml", `[roots.serde]
module = "github.com/goxide-lang/serde"
package = "github.com/goxide-lang/serde"
[roots.library]
module = "a06.example/lib"
package = "a06.example/lib"
[roots.fmt]
source = "toolchain"
package = "fmt"
[roots.errors]
source = "toolchain"
package = "errors"
[roots.std]
module = "github.com/goxide-lang/std"
`)
}

func singleExternalSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[rel] = fmt.Sprintf("%x", sha256.Sum256(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func singleExternalUnchanged(t *testing.T, root string, before map[string]string) {
	t.Helper()
	if after := singleExternalSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Errorf("input tree changed: %s\nbefore=%v\nafter=%v", root, before, after)
	}
}
func singleExternalSameTree(t *testing.T, a, b string) {
	t.Helper()
	if !reflect.DeepEqual(singleExternalSnapshot(t, a), singleExternalSnapshot(t, b)) {
		t.Fatalf("nondeterministic output: %s and %s", a, b)
	}
}
func singleExternalCopyTree(t *testing.T, src, dst string, excluded ...string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		for _, skip := range excluded {
			if e.IsDir() && rel == skip {
				return filepath.SkipDir
			}
		}
		target := filepath.Join(dst, rel)
		if e.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
}
func singleExternalContains(t *testing.T, text string, words ...string) {
	t.Helper()
	for _, word := range words {
		if !strings.Contains(text, word) {
			t.Fatalf("missing %q in diagnostic:\n%s", word, text)
		}
	}
}

const singleExternalPlain = `package main; use std; use std::PartialEq;
use fmt; use library::model as m;
func main(){fmt::Println(m::Plain())}
`

const singleExternalProducer = `package model; use std; use std::PartialEq; use serde;
use errors;
var DefaultCalls int
#[derive(std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Item struct{N int;Name string}
impl std::Default for Item{func Default()Item{DefaultCalls++;return Item{N:7,Name:"producer"}}}
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
pub enum E{
 #[default]
 Entry(Item),
 Empty,
}
func Plain()int{return 7}
func View(r &int)&int{return r}
type hidden struct{N int}
#[derive(serde::derive::Serialize)]
type Output struct{N int}
var Sentinel=errors::New("external failure")
type Fault struct{}
func(*Fault)Error()string{return "typed nil failure"}
func Pick(ok bool)Option[int]{if ok{return Option[int]::Some(5)};return Option[int]::None}
func Load(kind int)Result[int,error]{if kind==1{return Result[int,error]::Err(Sentinel)};if kind==2{var e *Fault;return Result[int,error]::Err(e)};return Result[int,error]::Ok(4)}

impl Item {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl E {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl Output {
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
}
`

const singleExternalConsumer = `package main; use std; use std::PartialEq; use serde;
use fmt; use library::model as m; use library::relay as r; use serde::json as serdejson;
type Local=r::Item
type Level=Local
type Event=r::E
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Wrap struct{Direct m::Item;Value Level;Event Event}
#[derive(std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Bag struct{Items []Level;Index map[string]Local}
#[derive(serde::derive::Deserialize)]
type Skipped struct{
 #[serde(skip)]
 Value Level
}
#[derive(std::derive::Error)]
#[error("external input")]
type Failure struct{
 #[from]
 Cause error
}
func Equal(a,b Wrap)bool{return a.equals(b)}
func SameDirect(a,b m::Item)bool{return a.equals(b)}
func Read(e Event)int{return match e{r::E::Entry(v)=>v.N,r::E::Empty=>0}}
func Mark(hit *bool)bool{*hit=true;return true}
func Guard(e Event,hit *bool)int{return match e{r::E::Entry(v) if Mark(hit)=>v.N,r::E::Empty if Mark(hit)=>0,_=>-1}}
func Decode(input []byte)(Wrap,error){v,e:=serdejson::Unmarshal(input,Wrap::decode);if e!=nil{return Wrap{},Failure::wrap(e)};return v,nil}
func Business(input []byte)(string,string,string,error){
 v,e:=serdejson::Unmarshal(input,Bag::decode);if e!=nil{return "","","",e}
 c:=Bag::copy(&v);c.Items[0].N=9;c.Index["x"]=Local{N:8,Name:"copy"}
 before:=Bag::describe(&v);after:=Bag::describe(&c)
 data,e:=serdejson::Marshal(&c,Bag::encode);return before,after,string(data),e
}
var Trace []string
func record(s string){Trace=append(Trace,s)}
func mark(s string,n int)int{record(s);return n}
func OptionFlow(ok bool)r::Maybe{defer record("option-defer");record("option-before");n:=m::Pick(ok)?;record("option-after");return r::Maybe::Some(n+1)}
func ResultFlow(kind int)r::Outcome{defer record("result-defer");record("result-before");n:=m::Load(kind)?;record("result-after");return r::Outcome::Ok(n+1)}
func finish(a,b int)r::Outcome{return r::Outcome::Ok(a+b)}
func CallFlow(kind int)r::Outcome{return finish(m::Load(kind)?,mark("last-argument",2))}
func Nearest(kind int)int{f:=func()r::Outcome{defer record("inner-defer");n:=m::Load(kind)?;record("inner-after");return r::Outcome::Ok(n)};_=f();return mark("outer",42)}
func main(){
 v:=Wrap::initial();fmt::Println(Read(v.Event),v.Direct.N,v.Value.Name)
 _,after,wire,e:=Business([]byte("{\"Items\":[{\"N\":7,\"Name\":\"producer\"}],\"Index\":{\"x\":{\"N\":7,\"Name\":\"producer\"}}}"));if e!=nil{panic(e)}
 fmt::Println(after);fmt::Println(wire)
}

impl Wrap {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl Bag {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl Skipped {
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl Failure {
 pub func wrap(cause error) Self { return Failure::from::<std::From[error]>(cause) }
}
`

const singleExternalOutput = "7 7 producer\n" +
	"Bag { Items: [Item { N: 9, Name: \"producer\" }], Index: {\"x\": Item { N: 8, Name: \"copy\" }} }\n" +
	"{\"Items\":[{\"N\":9,\"Name\":\"producer\"}],\"Index\":{\"x\":{\"N\":8,\"Name\":\"copy\"}}}\n"

const singleExternalAssertions = `package main
import __borrow_std "github.com/goxide-lang/std/sum"

import (
 "errors"
 "reflect"
 "strconv"
 "testing"
 m "a06.example/lib/model"
 "github.com/goxide-lang/serde"
 "github.com/goxide-lang/serde/json"
)
const wrapDebug="Wrap { Direct: Item { N: 7, Name: \"producer\" }, Value: Item { N: 7, Name: \"producer\" }, Event: E::Entry(Item { N: 7, Name: \"producer\" }) }"
const wrapJSON="{\"Direct\":{\"N\":7,\"Name\":\"producer\"},\"Value\":{\"N\":7,\"Name\":\"producer\"},\"Event\":{\"Entry\":{\"N\":7,\"Name\":\"producer\"}}}"
func TestAllExternalProtocols(t *testing.T){
 m.DefaultCalls=0;v:=(WrapOps{}).Initial()
 if m.DefaultCalls!=3||v.Direct.N!=7||v.Value.Name!="producer"||Read(v.Event)!=7{t.Fatal(v,m.DefaultCalls)}
 if reflect.TypeOf(v.Value)!=reflect.TypeOf(m.Item{}){t.Fatal("transparent alias identity lost")}
 c:=(WrapOps{}).Copy(&v);if !Equal(v,c)||!(WrapOps{}).Equal(v,c)||!SameDirect(v.Direct,c.Direct)||(WrapOps{}).Describe(&v)!=wrapDebug||m.DefaultCalls!=3{t.Fatal("registered protocols",(WrapOps{}).Describe(&v),m.DefaultCalls)}
 c.Value.N=9;if Equal(v,c)||v.Value.N!=7{t.Fatal("equality or clone lost")}
 data,e:=serdejson.Marshal(&v,(WrapOps{}).Encode);if e!=nil||string(data)!=wrapJSON{t.Fatal(string(data),e)}
 d,e:=Decode(data);if e!=nil||!Equal(v,d)||m.DefaultCalls!=3{t.Fatal(d,e,m.DefaultCalls)}
 skipped,e:=serdejson.Unmarshal([]byte("{}"),(SkippedOps{}).Decode);if e!=nil||skipped.Value.N!=7||m.DefaultCalls!=4{t.Fatal(skipped,e,m.DefaultCalls)}
}
func TestCloneContainersNilAndEmpty(t *testing.T){
 item:=m.Item{N:7,Name:"producer"};v:=Bag{Items:[]Level{item},Index:map[string]Local{"x":item}}
 ordinary:=v;ordinary.Items[0].N=6;if v.Items[0].N!=6{t.Fatal("ordinary assignment changed")};v.Items[0].N=7
 c:=(BagOps{}).Copy(&v);if !(BagOps{}).Equal(v,c){t.Fatal("clone equality")};c.Items[0].N=9;c.Index["x"]=Local{N:8}
 if v.Items[0].N!=7||v.Index["x"].N!=7||(BagOps{}).Equal(v,c){t.Fatal("mutable containers shared")}
 for _,tc:=range []struct{value Bag;wantNil bool;debug string}{
  {Bag{},true,"Bag { Items: nil, Index: nil }"},
  {Bag{Items:[]Level{},Index:map[string]Local{}},false,"Bag { Items: [], Index: {} }"},
 }{c:=(BagOps{}).Copy(&tc.value);if (c.Items==nil)!=tc.wantNil||(c.Index==nil)!=tc.wantNil||(BagOps{}).Describe(&c)!=tc.debug{t.Fatal(c,(BagOps{}).Describe(&c))};data,e:=serdejson.Marshal(&c,(BagOps{}).Encode);if e!=nil||string(data)!="{\"Items\":[],\"Index\":{}}"{t.Fatal(string(data),e)};d,e:=serdejson.Unmarshal(data,(BagOps{}).Decode);if e!=nil||d.Items==nil||d.Index==nil{t.Fatal("serde empty projection",d,e)}}
 before,after,wire,e:=Business([]byte("{\"Items\":[{\"N\":7,\"Name\":\"producer\"}],\"Index\":{\"x\":{\"N\":7,\"Name\":\"producer\"}}}"))
 if e!=nil||before!="Bag { Items: [Item { N: 7, Name: \"producer\" }], Index: {\"x\": Item { N: 7, Name: \"producer\" }} }"||after!="Bag { Items: [Item { N: 9, Name: \"producer\" }], Index: {\"x\": Item { N: 8, Name: \"copy\" }} }"||wire!="{\"Items\":[{\"N\":9,\"Name\":\"producer\"}],\"Index\":{\"x\":{\"N\":8,\"Name\":\"copy\"}}}"{t.Fatal(before,after,wire,e)}
}
func TestSerdeCauseAndPath(t *testing.T){
 input:="{\"Direct\":{\"N\":1,\"Name\":\"direct\"},\"Value\":{\"N\":2,\"Name\":\"alias\"},\"Event\":{\"Entry\":{\"N\":9223372036854775808,\"Name\":\"bad\"}}}"
 _,e:=Decode([]byte(input));var detail *serde.Error;var numeric *strconv.NumError
 want:=[]serde.PathSegment{{Kind:"Field",Name:"Event"},{Kind:"Variant",Name:"Entry"},{Kind:"Field",Name:"N"}}
 if e==nil||e.Error()!="external input"||!errors.As(e,&detail)||detail.Kind!=serde.Range||!reflect.DeepEqual(detail.Path,want)||!errors.As(e,&numeric)||!errors.Is(e,strconv.ErrRange)||errors.Unwrap(e)!=error(detail){t.Fatalf("cause/path=%#v %v",detail,e)}
}
func TestInvalidEnumBeforeGuard(t *testing.T){
 for _,v:=range []m.E{nil}{
  func(){hit:=false;defer func(){if recover()==nil||hit{t.Errorf("invalid %T reached guard or did not panic",v)}}();Guard(v,&hit)}()
 }
 hit:=false;if Guard(&m.Entry{Value: m.Item{N:9}},&hit)!=9||!hit{t.Fatal("valid guard")}
}
func wantTrace(t *testing.T,want []string){t.Helper();if !reflect.DeepEqual(Trace,want){t.Fatalf("trace=%v want=%v",Trace,want)};Trace=nil}
func TestExternalSumsPropagationAndFrames(t *testing.T){
 Trace=nil;if __borrow_std.ValueSome(OptionFlow(true))!=6{t.Fatal("some")};wantTrace(t,[]string{"option-before","option-after","option-defer"})
 if __borrow_std.TagOption(OptionFlow(false))!=0{t.Fatal("none")};wantTrace(t,[]string{"option-before","option-defer"})
 if __borrow_std.ValueOk(ResultFlow(0))!=5{t.Fatal("ok")};wantTrace(t,[]string{"result-before","result-after","result-defer"})
 if e:=__borrow_std.ValueErr(ResultFlow(1));!errors.Is(e,m.Sentinel){t.Fatal("sentinel",e)};wantTrace(t,[]string{"result-before","result-defer"})
 e:=__borrow_std.ValueErr(ResultFlow(2));var typed *m.Fault
 if e==nil||!errors.As(e,&typed)||typed!=nil||e!=error((*m.Fault)(nil)){t.Fatal("typed nil changed",e)};wantTrace(t,[]string{"result-before","result-defer"})
 if e:=__borrow_std.ValueErr(CallFlow(1));!errors.Is(e,m.Sentinel){t.Fatal(e)};wantTrace(t,nil)
 if __borrow_std.ValueOk(CallFlow(0))!=6{t.Fatal("call result")};wantTrace(t,[]string{"last-argument"})
 if Nearest(1)!=42{t.Fatal("nearest failure frame")};wantTrace(t,[]string{"inner-defer","outer"})
 if Nearest(0)!=42{t.Fatal("nearest success frame")};wantTrace(t,[]string{"inner-after","inner-defer","outer"})
}
`

const singleExternalProducerAssertions = `package model
import "testing"
func TestProducer(t *testing.T){DefaultCalls=0;v:=(EOps{}).Initial();if (EOps{}).Describe(&v)!="E::Entry(Item { N: 7, Name: \"producer\" })"||DefaultCalls!=1{t.Fatal(v,DefaultCalls)};c:=(EOps{}).Copy(&v);if !(EOps{}).Equal(v,c){t.Fatal("producer equality")}}
`
const singleExternalNativeAssertions = `package plain
import __borrow_std "github.com/goxide-lang/std/sum"

import("testing";"github.com/goxide-lang/serde/json";m "a06.example/lib/model";r "a06.example/lib/relay";a "a06.example/lib/api")
func TestNativeWithoutMetadata(t *testing.T){var v r.Item=(m.ItemOps{}).Initial();c:=(m.ItemOps{}).Copy(&v);if !(m.ItemOps{}).Equal(v,c)||(m.ItemOps{}).Describe(&v)!="Item { N: 7, Name: \"producer\" }"{t.Fatal(v,c)};b,e:=serdejson.Marshal(&v,(m.ItemOps{}).Encode);if e!=nil{t.Fatal(e)};d,e:=serdejson.Unmarshal(b,(m.ItemOps{}).Decode);if e!=nil||!(m.ItemOps{}).Equal(v,d){t.Fatal(d,e)};event:=&m.Entry{Value: v};if event.Value.N!=7||a.Read(a.Get())!=8{t.Fatal(event)};if __borrow_std.ValueOk(m.Load(0))!=4{t.Fatal("sum")}}
`
