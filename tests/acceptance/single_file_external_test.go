package acceptance_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
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
	deliveryCommand(t, root, tool, true, nil, "build", "-o", cli, "./cmd/goxide")
	outside := t.TempDir()
	base := t.TempDir()
	producer := filepath.Join(base, "producer")
	library := filepath.Join(base, "library")
	module := func(name, replacement string) string {
		s := fmt.Sprintf("module %s\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => %s\n", name, strconvQuote(root))
		if replacement != "" {
			s += "require a06.example/lib v0.0.0\nreplace a06.example/lib => " + strconvQuote(replacement) + "\n"
		}
		return s
	}
	packagesWrite(t, producer, "go.mod", module("a06.example/lib", ""))
	packagesWrite(t, producer, "model/model.gox", singleExternalProducer)
	packagesWrite(t, producer, "relay/relay.go", `package relay
import m "a06.example/lib/model"
type Item=m.Item
type E=m.E
type New m.Item
type Maybe=m.Option[int]
type Outcome=m.Result[int,error]
func Get()E{return m.DefaultE()}
`)
	packagesWrite(t, producer, "other/model.gox", "package other\npub enum E{Empty,Item(int),}\n")
	packagesWrite(t, producer, "internal/hidden/model.gox", "package hidden\npub enum E{Value(int),}\nfunc Get()E{return E::Value(8)}\nfunc Read(v E)int{return match v{E::Value(n)=>n}}\n")
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
	packagesWrite(t, producer, "uses/api.gox", "package uses\nimport p \"a06.example/lib/model\"\npub use p::Plain;\n")
	beforeProducer := singleExternalSnapshot(t, producer)
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", library, producer)
	again := filepath.Join(t.TempDir(), "library")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", again, producer)
	singleExternalSameTree(t, library, again)
	singleExternalUnchanged(t, producer, beforeProducer)
	defer singleExternalUnchanged(t, producer, beforeProducer)
	packagesWrite(t, library, "model/producer_test.go", singleExternalProducerAssertions)
	beforeLibrary := singleExternalSnapshot(t, library)
	defer singleExternalUnchanged(t, library, beforeLibrary)
	consumer := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", module("a06.example/app", library))
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
	t.Run("registered-protocols", func(t *testing.T) {
		src := consumer(t, singleExternalConsumer)
		before := singleExternalSnapshot(t, src)
		defer singleExternalUnchanged(t, src, before)
		outputs := []string{}
		for _, mode := range []string{"directory", "file"} {
			t.Run(mode, func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "out")
				repeat := filepath.Join(t.TempDir(), "out")
				if mode == "directory" {
					for _, dir := range []string{out, repeat} {
						deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", dir, src)
					}
				} else {
					for _, dir := range []string{out, repeat} {
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						deliveryCommand(t, outside, cli, true, nil, "transpile", "--source-map", filepath.Join(dir, "app.go.map.json"), "-o", filepath.Join(dir, "app.go"), filepath.Join(src, "app.gox"))
					}
				}
				singleExternalSameTree(t, out, repeat)
				if mode == "file" {
					// This generated-module check is additional to direct CLI build/run below.
					packagesWrite(t, out, "go.mod", module("a06.example/app", library))
					if _, err := os.Stat(filepath.Join(out, ".goxide.enums.json")); !os.IsNotExist(err) {
						t.Fatal("single-file mode published package metadata", err)
					}
				}
				packagesWrite(t, out, "app_test.go", singleExternalAssertions)
				deliveryCommand(t, out, tool, true, nil, "test", "-mod=readonly", "-race", "-count=1", "-v", "./...")
				deliveryCommand(t, out, tool, true, nil, "vet", "-mod=readonly", "./...")
				got := deliveryCommand(t, out, tool, true, nil, "run", "-mod=readonly", "-race", ".")
				if got != singleExternalOutput {
					t.Fatalf("business output %q; want %q", got, singleExternalOutput)
				}
				outputs = append(outputs, got)
			})
		}
		if len(outputs) == 2 && outputs[0] != outputs[1] {
			t.Fatal("entry modes differ")
		}
	})
	t.Run("known-module-build-run", func(t *testing.T) {
		src := consumer(t, singleExternalConsumer)
		execute(t, outside, filepath.Join(src, "app.gox"), singleExternalOutput, nil)
	})
	t.Run("ordinary-api-keeps-consumer-dependencies", func(t *testing.T) {
		src := consumer(t, singleExternalPlain)
		execute(t, outside, filepath.Join(src, "app.gox"), "7\n", nil)
	})
	t.Run("producer-and-native-consumer", func(t *testing.T) {
		deliveryCommand(t, library, tool, true, nil, "test", "-mod=readonly", "-race", "-count=1", "./...")
		deliveryCommand(t, library, tool, true, nil, "vet", "-mod=readonly", "./...")
		plain := t.TempDir()
		packagesWrite(t, plain, "go.mod", module("a06.example/plain", library))
		packagesWrite(t, plain, "plain_test.go", singleExternalNativeAssertions)
		before := singleExternalSnapshot(t, plain)
		// Native Go consumes exactly the public Go API; JSON metadata is not required.
		removed := map[string][]byte{}
		err := filepath.WalkDir(library, func(path string, e os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !e.IsDir() && e.Name() == ".goxide.enums.json" {
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				removed[path] = b
				return os.Remove(path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			for p, b := range removed {
				if err := os.WriteFile(p, b, 0600); err != nil {
					t.Error(err)
				}
			}
		}()
		if len(removed) == 0 {
			t.Fatal("no metadata removed")
		}
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
		packagesWrite(t, nested, "go.mod", module("a06.example/nested", library))
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
		singleExternalCopyTree(t, library, filepath.Join(original, "library"))
		app := filepath.Join(original, "consumer")
		packagesWrite(t, app, "go.mod", module("a06.example/movable", "../library"))
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
		src := consumer(t, "package main\nimport \"fmt\"\nfunc main(){fmt.Println(fromSibling())}\n")
		packagesWrite(t, src, "sibling.go", "package main\nfunc fromSibling()int{return 12}\n")
		before := singleExternalSnapshot(t, src)
		msg := deliveryCommand(t, outside, cli, false, nil, "build", "--runtime", root, "--go", tool, "-o", filepath.Join(t.TempDir(), "bad"), filepath.Join(src, "app.gox"))
		singleExternalContains(t, msg, "undefined: fromSibling")
		binary := filepath.Join(t.TempDir(), "directory")
		deliveryCommand(t, outside, cli, true, nil, "build", "--go", tool, "-o", binary, src)
		if got := deliveryCommand(t, outside, binary, true, nil); got != "12\n" {
			t.Fatal(got)
		}
		singleExternalUnchanged(t, src, before)
		safe := consumer(t, singleExternalPlain)
		packagesWrite(t, safe, "poison.gox", "package main\nthis is invalid source\n")
		execute(t, outside, filepath.Join(safe, "app.gox"), "7\n", nil)
		msg = deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "out"), safe)
		singleExternalContains(t, msg, "poison.gox:2:1:", "expected declaration, found this")
	})
	t.Run("internal-and-public-facade", func(t *testing.T) {
		lib := filepath.Join(t.TempDir(), "library")
		singleExternalCopyTree(t, library, lib)
		input := filepath.Join(lib, "tools/main.gox")
		packagesWrite(t, lib, "tools/main.gox", `package main
import("fmt";h "a06.example/lib/internal/hidden")
func main(){fmt.Println(match h.Get(){h.E::Value(n)=>n})}
`)
		execute(t, outside, input, "8\n", nil)
		bad := consumer(t, "package main\nimport h \"a06.example/lib/internal/hidden\"\nfunc main(){_=h.Get()}\n")
		rejected := func(path string) {
			msg := deliveryCommand(t, outside, cli, false, nil, "build", "--runtime", root, "--go", tool, "-o", filepath.Join(t.TempDir(), "bad"), path)
			singleExternalContains(t, msg, "app.gox:2:", "internal package is not accessible from a06.example/app: a06.example/lib/internal/hidden")
		}
		rejected(filepath.Join(bad, "app.gox"))
		link := filepath.Join(lib, "tools/linked.gox")
		if err := os.Symlink(filepath.Join(bad, "app.gox"), link); err != nil {
			t.Fatal(err)
		}
		rejected(link)
		facade := consumer(t, "package main\nimport(\"fmt\";a \"a06.example/lib/api\")\nfunc main(){fmt.Println(a.Read(a.Get()))}\n")
		execute(t, outside, filepath.Join(facade, "app.gox"), "8\n", nil)
		enhanced := consumer(t, "package main\nimport a \"a06.example/lib/api\"\nfunc main(){var n int=match a.Get(){a.Public::Value(n)=>n};_=n}\n")
		msg := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "bad.go"), filepath.Join(enhanced, "app.gox"))
		singleExternalContains(t, msg, "enum syntax through an internal-origin alias is not yet supported", "origin a06.example/lib/internal/hidden")
	})
	t.Run("metadata-validation", func(t *testing.T) {
		input := consumer(t, "package main\nimport m \"a06.example/lib/model\"\nfunc main(){_=m.Plain()}\n")
		path := filepath.Join(library, "model/.goxide.enums.json")
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		mutations := []struct {
			name, want string
			change     func(map[string]any)
		}{
			{"artifact-version", "unsupported metadata version", func(v map[string]any) { v["ArtifactVersion"] = 99 }},
			{"schema", "unsupported metadata version", func(v map[string]any) { v["Metadata"].(map[string]any)["Version"] = 99 }},
			{"package", "metadata package path mismatch", func(v map[string]any) { v["Metadata"].(map[string]any)["Path"] = "wrong.example/model" }},
			{"marker", "metadata marker digest mismatch", func(v map[string]any) {
				v["Metadata"].(map[string]any)["Enums"].([]any)[0].(map[string]any)["Validator"] = "PretendValidator"
			}},
			{"file-set", "selected Go source set mismatch", func(v map[string]any) { v["Files"] = map[string]any{} }},
			{"file-hash", "Go source content mismatch: model.go", func(v map[string]any) {
				for k := range v["Files"].(map[string]any) {
					v["Files"].(map[string]any)[k] = "bad"
					break
				}
			}},
			{"content", "package source closure or build configuration changed", func(v map[string]any) { v["Content"] = "stale" }},
			{"dependency-set", "dependency set mismatch", func(v map[string]any) { v["Dependencies"] = map[string]any{} }},
		}
		reject := func(t *testing.T, want string) {
			t.Helper()
			for _, mode := range []string{"file", "directory"} {
				t.Run(mode, func(t *testing.T) {
					arg := input
					dest := filepath.Join(t.TempDir(), "out")
					if mode == "file" {
						arg = filepath.Join(input, "app.gox")
						dest += ".go"
					}
					msg := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", dest, arg)
					singleExternalContains(t, msg, "enum metadata missing, stale or wrong package for a06.example/lib/model:", "app.gox:2:8:", want)
				})
			}
		}
		for _, tc := range mutations {
			t.Run(tc.name, func(t *testing.T) {
				var v map[string]any
				if err := json.Unmarshal(original, &v); err != nil {
					t.Fatal(err)
				}
				tc.change(v)
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
				defer os.WriteFile(path, original, 0600)
				reject(t, tc.want)
			})
		}
		t.Run("missing", func(t *testing.T) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			defer os.WriteFile(path, original, 0600)
			reject(t, "cannot read .goxide.enums.json:")
		})
		t.Run("stale-source", func(t *testing.T) {
			p := filepath.Join(library, "model/model.go")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append(append([]byte(nil), b...), []byte("\n// stale after publication\n")...), 0600); err != nil {
				t.Fatal(err)
			}
			defer os.WriteFile(p, b, 0600)
			reject(t, "Go source content mismatch: model.go")
		})
	})
	t.Run("canonical-source-rehashed", func(t *testing.T) {
		// Recompute the marker and file hash, so a checksum failure cannot stand
		// in for checking the canonical producer's explicit registration.
		path := filepath.Join(library, "relay/.goxide.enums.json")
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var artifact map[string]json.RawMessage
		if err := json.Unmarshal(original, &artifact); err != nil {
			t.Fatal(err)
		}
		var oldMetadata bytes.Buffer
		if err := json.Compact(&oldMetadata, artifact["Metadata"]); err != nil {
			t.Fatal(err)
		}
		changed := bytes.Replace(artifact["Metadata"], []byte(`"Source": "manual"`), []byte(`"Source": "derive"`), 1)
		if bytes.Equal(changed, artifact["Metadata"]) {
			t.Fatal("fixture has no manual Default registration")
		}
		var newMetadata bytes.Buffer
		if err := json.Compact(&newMetadata, changed); err != nil {
			t.Fatal(err)
		}
		oldMarker := fmt.Sprintf("%x", sha256.Sum256(oldMetadata.Bytes()))
		newMarker := fmt.Sprintf("%x", sha256.Sum256(newMetadata.Bytes()))
		var files map[string]string
		if err := json.Unmarshal(artifact["Files"], &files); err != nil {
			t.Fatal(err)
		}
		updated := false
		for name := range files {
			p := filepath.Join(library, "relay", name)
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(b, []byte(oldMarker)) {
				continue
			}
			next := bytes.ReplaceAll(b, []byte(oldMarker), []byte(newMarker))
			if err := os.WriteFile(p, next, 0600); err != nil {
				t.Fatal(err)
			}
			defer os.WriteFile(p, b, 0600)
			files[name] = fmt.Sprintf("%x", sha256.Sum256(next))
			updated = true
		}
		if !updated {
			t.Fatal("original marker did not match generated producer")
		}
		artifact["Metadata"] = changed
		artifact["Files"], err = json.Marshal(files)
		if err != nil {
			t.Fatal(err)
		}
		next, err := json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, next, 0600); err != nil {
			t.Fatal(err)
		}
		defer os.WriteFile(path, original, 0600)
		src := consumer(t, "package main\nimport r \"a06.example/lib/relay\"\nfunc main(){_=r.Get()}\n")
		for _, mode := range []string{"file", "directory"} {
			t.Run(mode, func(t *testing.T) {
				input := src
				dest := filepath.Join(t.TempDir(), "out")
				if mode == "file" {
					input = filepath.Join(src, "app.gox")
					dest += ".go"
				}
				msg := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", dest, input)
				singleExternalContains(t, msg, "app.gox:2:", "Default alias does not match")
			})
		}
	})
	t.Run("identity-protocol-and-permission-rejections", func(t *testing.T) {
		reject := func(t *testing.T, source, location string, words ...string) {
			t.Helper()
			src := consumer(t, source)
			for _, mode := range []string{"file", "directory"} {
				t.Run(mode, func(t *testing.T) {
					arg := src
					dest := filepath.Join(t.TempDir(), "out")
					if mode == "file" {
						arg = filepath.Join(src, "app.gox")
						dest += ".go"
					}
					msg := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", dest, arg)
					singleExternalContains(t, msg, location)
					singleExternalContains(t, msg, words...)
				})
			}
		}
		for _, kind := range []string{"Default", "Clone", "Debug", "Serialize", "Deserialize", "PartialEq", "PartialEq,Eq"} {
			t.Run("fake-"+kind, func(t *testing.T) {
				location := "app.gox:5:"
				want := "nested type requires " + kind + " derive or explicit implementation: Local"
				if kind == "Serialize" || kind == "Deserialize" {
					want = "unsupported field type Local"
				}
				if strings.HasPrefix(kind, "PartialEq") {
					// Existing equality preparation has no source span in either mode.
					location = "equality.Prepare [generated/unmapped; intermediate coordinates]"
					protocol := "PartialEq"
					if kind == "PartialEq,Eq" {
						protocol = "Eq"
					}
					want = "named type a06.example/lib/fake.Item requires an explicit " + protocol + " protocol"
				}
				reject(t, "package main\nimport f \"a06.example/lib/fake\"\ntype Local=f.Item\n#[derive("+kind+")]\ntype Wrap struct{V Local}\nfunc main(){}\n", location, want)
			})
		}
		for _, tc := range []struct {
			name, source string
			words        []string
		}{
			{"new-defined", "import m \"a06.example/lib/model\"\ntype New m.Item\ntype Local=New\n#[derive(Clone)]\ntype Wrap struct{V Local}\n", []string{"nested type requires Clone derive or explicit implementation: Local"}},
			{"wrong-serde-direction", "import m \"a06.example/lib/model\"\n#[derive(Deserialize)]\ntype Wrap struct{V m.Output}\n", []string{"nested type lacks required registered serde protocol: m.Output"}},
			{"different-enum", "import(m \"a06.example/lib/model\";o \"a06.example/lib/other\")\nfunc Bad(v m.E)int{return match v{m.E::Empty=>0,o.E::Item(n)=>n,_=>0}}\n", []string{"enum: mixed enum identities in match"}},
			{"readonly", "import m \"a06.example/lib/model\"\nfunc Bad(){n:=1;p:=m.View(&n);*p=2}\n", []string{"cannot write through a readonly projection"}},
		} {
			t.Run(tc.name, func(t *testing.T) { reject(t, "package main\n"+tc.source+"func main(){}\n", "app.gox", tc.words...) })
		}
	})
	t.Run("private-type-rejection", func(t *testing.T) {
		src := consumer(t, "package main\nimport m \"a06.example/lib/model\"\nfunc main(){var x m.hidden;_=x}\n")
		for _, input := range []string{filepath.Join(src, "app.gox"), src} {
			msg := deliveryCommand(t, outside, cli, false, nil, "build", "--runtime", root, "--go", tool, "-o", filepath.Join(t.TempDir(), "bad"), input)
			singleExternalContains(t, msg, "undefined: m.hidden", "app.gox:3:21:")
			if input == src {
				// Directory mode already performs full source typechecking.
				singleExternalContains(t, msg, "source span")
			} else {
				// Plain single-file Go keeps its existing conditional typecheck;
				// the real Go build rejects and maps the selected source token.
				singleExternalContains(t, msg, "[source]", "generated Go build")
			}
		}
	})
	t.Run("runtime-does-not-lend-dependencies", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module a06.example/missing\ngo 1.27\n"), 0600); err != nil {
			t.Fatal(err)
		}
		packagesWrite(t, dir, "app.gox", "package main\nimport s \"github.com/goxide-lang/std/sum\"\nfunc main(){var v s.Option[int];_=v}\n")
		before := singleExternalSnapshot(t, dir)
		msg := deliveryCommand(t, outside, cli, false, nil, "build", "--runtime", root, "--go", tool, "-o", filepath.Join(t.TempDir(), "bad"), filepath.Join(dir, "app.gox"))
		singleExternalContains(t, msg, "app.gox:2:8:", "cannot find module providing package github.com/goxide-lang/std/sum", "import lookup disabled by -mod=readonly")
		singleExternalUnchanged(t, dir, before)
	})
	t.Run("existing-file-boundaries", func(t *testing.T) {
		src := consumer(t, "package main\nimport \"fmt\"\nenum Local{A,}\nfunc main(){fmt.Println(match Local::A{Local::A=>3})}\n")
		execute(t, outside, filepath.Join(src, "app.gox"), "3\n", nil)
		for _, tc := range []struct{ name, source, want string }{
			{"local-use", "package main\nimport p \"a06.example/lib/model\"\nuse p::Plain;\nfunc main(){_=Plain()}\n", "use/pub use requires directory or package compilation"},
			{"imported-use", "package main\nimport p \"a06.example/lib/uses\"\nfunc main(){_=p.Plain()}\n", "imports with use metadata require directory/package compilation"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				input := consumer(t, tc.source)
				msg := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "out.go"), filepath.Join(input, "app.gox"))
				singleExternalContains(t, msg, "app.gox", tc.want)
				deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", filepath.Join(t.TempDir(), "out"), input)
			})
		}

		for _, args := range [][]string{{"transpile", "--tags", "custom"}, {"test"}, {"run", "--features", "extra"}, {"run", "--workspace"}} {
			argv := append(append([]string(nil), args...), filepath.Join(src, "app.gox"))
			msg := deliveryCommand(t, outside, cli, false, nil, argv...)
			singleExternalContains(t, msg, "directory")
		}
	})
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
func singleExternalCopyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
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

const singleExternalPlain = `package main
import("fmt";m "a06.example/lib/model")
func main(){fmt.Println(m.Plain())}
`

const singleExternalProducer = `package model
import "errors"
var DefaultCalls int
#[derive(Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
type Item struct{N int;Name string}
impl Default for Item{func default()Item{DefaultCalls++;return Item{N:7,Name:"producer"}}}
#[derive(Default,Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
pub enum E{
 #[default]
 Item(Item),
 Empty,
}
func Plain()int{return 7}
func View(r &int)&int{return r}
type hidden struct{N int}
#[derive(Serialize)]
type Output struct{N int}
var Sentinel=errors.New("external failure")
type Fault struct{}
func(*Fault)Error()string{return "typed nil failure"}
func Pick(ok bool)Option[int]{if ok{return Option[int]::Some(5)};return Option[int]::None}
func Load(kind int)Result[int,error]{if kind==1{return Result[int,error]::Err(Sentinel)};if kind==2{var e *Fault;return Result[int,error]::Err(e)};return Result[int,error]::Ok(4)}
`

const singleExternalConsumer = `package main
import (
 "fmt"
 m "a06.example/lib/model"
 r "a06.example/lib/relay"
 "github.com/goxide-lang/serde/json"
)
type Local=r.Item
type Level=Local
type Event=r.E
#[derive(Default,Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
type Wrap struct{Direct m.Item;Value Level;Event Event}
#[derive(Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
type Bag struct{Items []Level;Index map[string]Local}
#[derive(Deserialize)]
type Skipped struct{
 #[serde(skip)]
 Value Level
}
#[derive(Error)]
#[error("external input")]
type Failure struct{
 #[from]
 Cause error
}
func Equal(a,b Wrap)bool{return a.equals(b)}
func SameDirect(a,b m.Item)bool{return a.equals(b)}
func Read(e Event)int{return match e{r.E::Item(v)=>v.N,r.E::Empty=>0}}
func Mark(hit *bool)bool{*hit=true;return true}
func Guard(e Event,hit *bool)int{return match e{r.E::Item(v) if Mark(hit)=>v.N,r.E::Empty if Mark(hit)=>0,_=>-1}}
func Decode(input []byte)(Wrap,error){v,e:=serdejson.Unmarshal(input,DeserializeWrap);if e!=nil{return Wrap{},FromFailure(e)};return v,nil}
func Business(input []byte)(string,string,string,error){
 v,e:=serdejson.Unmarshal(input,DeserializeBag);if e!=nil{return "","","",e}
 c:=CloneBag(&v);c.Items[0].N=9;c.Index["x"]=Local{N:8,Name:"copy"}
 before:=DebugBag(&v);after:=DebugBag(&c)
 data,e:=serdejson.Marshal(&c,SerializeBag);return before,after,string(data),e
}
var Trace []string
func record(s string){Trace=append(Trace,s)}
func mark(s string,n int)int{record(s);return n}
func OptionFlow(ok bool)r.Maybe{defer record("option-defer");record("option-before");n:=m.Pick(ok)?;record("option-after");return r.Maybe::Some(n+1)}
func ResultFlow(kind int)r.Outcome{defer record("result-defer");record("result-before");n:=m.Load(kind)?;record("result-after");return r.Outcome::Ok(n+1)}
func finish(a,b int)r.Outcome{return r.Outcome::Ok(a+b)}
func CallFlow(kind int)r.Outcome{return finish(m.Load(kind)?,mark("last-argument",2))}
func Nearest(kind int)int{f:=func()r.Outcome{defer record("inner-defer");n:=m.Load(kind)?;record("inner-after");return r.Outcome::Ok(n)};_=f();return mark("outer",42)}
func main(){
 v:=DefaultWrap();fmt.Println(Read(v.Event),v.Direct.N,v.Value.Name)
 _,after,wire,e:=Business([]byte("{\"Items\":[{\"N\":7,\"Name\":\"producer\"}],\"Index\":{\"x\":{\"N\":7,\"Name\":\"producer\"}}}"));if e!=nil{panic(e)}
 fmt.Println(after);fmt.Println(wire)
}
`

const singleExternalOutput = "7 7 producer\n" +
	"Bag { Items: [Item { N: 9, Name: \"producer\" }], Index: {\"x\": Item { N: 8, Name: \"copy\" }} }\n" +
	"{\"Items\":[{\"N\":9,\"Name\":\"producer\"}],\"Index\":{\"x\":{\"N\":8,\"Name\":\"copy\"}}}\n"

const singleExternalAssertions = `package main
import (
 "errors"
 "reflect"
 "strconv"
 "testing"
 m "a06.example/lib/model"
 "github.com/goxide-lang/serde"
 "github.com/goxide-lang/serde/json"
)
const wrapDebug="Wrap { Direct: Item { N: 7, Name: \"producer\" }, Value: Item { N: 7, Name: \"producer\" }, Event: E::Item(Item { N: 7, Name: \"producer\" }) }"
const wrapJSON="{\"Direct\":{\"N\":7,\"Name\":\"producer\"},\"Value\":{\"N\":7,\"Name\":\"producer\"},\"Event\":{\"Item\":{\"N\":7,\"Name\":\"producer\"}}}"
func TestAllExternalProtocols(t *testing.T){
 m.DefaultCalls=0;v:=DefaultWrap()
 if m.DefaultCalls!=3||v.Direct.N!=7||v.Value.Name!="producer"||Read(v.Event)!=7{t.Fatal(v,m.DefaultCalls)}
 if reflect.TypeOf(v.Value)!=reflect.TypeOf(m.Item{}){t.Fatal("transparent alias identity lost")}
 c:=CloneWrap(&v);if !Equal(v,c)||!EqualsWrap(v,c)||!SameDirect(v.Direct,c.Direct)||DebugWrap(&v)!=wrapDebug||m.DefaultCalls!=3{t.Fatal("registered protocols",DebugWrap(&v),m.DefaultCalls)}
 c.Value.N=9;if Equal(v,c)||v.Value.N!=7{t.Fatal("equality or clone lost")}
 data,e:=serdejson.Marshal(&v,SerializeWrap);if e!=nil||string(data)!=wrapJSON{t.Fatal(string(data),e)}
 d,e:=Decode(data);if e!=nil||!Equal(v,d)||m.DefaultCalls!=3{t.Fatal(d,e,m.DefaultCalls)}
 skipped,e:=serdejson.Unmarshal([]byte("{}"),DeserializeSkipped);if e!=nil||skipped.Value.N!=7||m.DefaultCalls!=4{t.Fatal(skipped,e,m.DefaultCalls)}
}
func TestCloneContainersNilAndEmpty(t *testing.T){
 item:=m.Item{N:7,Name:"producer"};v:=Bag{Items:[]Level{item},Index:map[string]Local{"x":item}}
 ordinary:=v;ordinary.Items[0].N=6;if v.Items[0].N!=6{t.Fatal("ordinary assignment changed")};v.Items[0].N=7
 c:=CloneBag(&v);if !EqualsBag(v,c){t.Fatal("clone equality")};c.Items[0].N=9;c.Index["x"]=Local{N:8}
 if v.Items[0].N!=7||v.Index["x"].N!=7||EqualsBag(v,c){t.Fatal("mutable containers shared")}
 for _,tc:=range []struct{value Bag;wantNil bool;debug string}{
  {Bag{},true,"Bag { Items: nil, Index: nil }"},
  {Bag{Items:[]Level{},Index:map[string]Local{}},false,"Bag { Items: [], Index: {} }"},
 }{c:=CloneBag(&tc.value);if (c.Items==nil)!=tc.wantNil||(c.Index==nil)!=tc.wantNil||DebugBag(&c)!=tc.debug{t.Fatal(c,DebugBag(&c))};data,e:=serdejson.Marshal(&c,SerializeBag);if e!=nil||string(data)!="{\"Items\":[],\"Index\":{}}"{t.Fatal(string(data),e)};d,e:=serdejson.Unmarshal(data,DeserializeBag);if e!=nil||d.Items==nil||d.Index==nil{t.Fatal("serde empty projection",d,e)}}
 before,after,wire,e:=Business([]byte("{\"Items\":[{\"N\":7,\"Name\":\"producer\"}],\"Index\":{\"x\":{\"N\":7,\"Name\":\"producer\"}}}"))
 if e!=nil||before!="Bag { Items: [Item { N: 7, Name: \"producer\" }], Index: {\"x\": Item { N: 7, Name: \"producer\" }} }"||after!="Bag { Items: [Item { N: 9, Name: \"producer\" }], Index: {\"x\": Item { N: 8, Name: \"copy\" }} }"||wire!="{\"Items\":[{\"N\":9,\"Name\":\"producer\"}],\"Index\":{\"x\":{\"N\":8,\"Name\":\"copy\"}}}"{t.Fatal(before,after,wire,e)}
}
func TestSerdeCauseAndPath(t *testing.T){
 input:="{\"Direct\":{\"N\":1,\"Name\":\"direct\"},\"Value\":{\"N\":2,\"Name\":\"alias\"},\"Event\":{\"Item\":{\"N\":9223372036854775808,\"Name\":\"bad\"}}}"
 _,e:=Decode([]byte(input));var detail *serde.Error;var numeric *strconv.NumError
 want:=[]serde.PathSegment{{Kind:"Field",Name:"Event"},{Kind:"Variant",Name:"Item"},{Kind:"Field",Name:"N"}}
 if e==nil||e.Error()!="external input"||!errors.As(e,&detail)||detail.Kind!=serde.Range||!reflect.DeepEqual(detail.Path,want)||!errors.As(e,&numeric)||!errors.Is(e,strconv.ErrRange)||errors.Unwrap(e)!=error(detail){t.Fatalf("cause/path=%#v %v",detail,e)}
}
type forgedExternalEnum struct{m.E}
func TestInvalidEnumBeforeGuard(t *testing.T){
 var typed *m.GoxideCase1_E_Item
 for _,v:=range []m.E{nil,typed,&m.GoxideCase1_E_Item{Value:m.Item{N:1}},forgedExternalEnum{}}{
  func(){hit:=false;defer func(){if recover()==nil||hit{t.Errorf("invalid %T reached guard or did not panic",v)}}();Guard(v,&hit)}()
 }
 hit:=false;if Guard(m.NewEItem(m.Item{N:9}),&hit)!=9||!hit{t.Fatal("valid guard")}
}
func wantTrace(t *testing.T,want []string){t.Helper();if !reflect.DeepEqual(Trace,want){t.Fatalf("trace=%v want=%v",Trace,want)};Trace=nil}
func TestExternalSumsPropagationAndFrames(t *testing.T){
 Trace=nil;if OptionFlow(true).(m.GoxideOptionSome[int]).Value!=6{t.Fatal("some")};wantTrace(t,[]string{"option-before","option-after","option-defer"})
 if OptionFlow(false)!=nil{t.Fatal("none")};wantTrace(t,[]string{"option-before","option-defer"})
 if ResultFlow(0).(m.GoxideResultOk[int,error]).Value!=5{t.Fatal("ok")};wantTrace(t,[]string{"result-before","result-after","result-defer"})
 if e:=ResultFlow(1).(m.GoxideResultErr[int,error]).Value;!errors.Is(e,m.Sentinel){t.Fatal("sentinel",e)};wantTrace(t,[]string{"result-before","result-defer"})
 e:=ResultFlow(2).(m.GoxideResultErr[int,error]).Value;var typed *m.Fault
 if e==nil||!errors.As(e,&typed)||typed!=nil||e!=error((*m.Fault)(nil)){t.Fatal("typed nil changed",e)};wantTrace(t,[]string{"result-before","result-defer"})
 if e:=CallFlow(1).(m.GoxideResultErr[int,error]).Value;!errors.Is(e,m.Sentinel){t.Fatal(e)};wantTrace(t,nil)
 if CallFlow(0).(m.GoxideResultOk[int,error]).Value!=6{t.Fatal("call result")};wantTrace(t,[]string{"last-argument"})
 if Nearest(1)!=42{t.Fatal("nearest failure frame")};wantTrace(t,[]string{"inner-defer","outer"})
 if Nearest(0)!=42{t.Fatal("nearest success frame")};wantTrace(t,[]string{"inner-after","inner-defer","outer"})
}
`

const singleExternalProducerAssertions = `package model
import "testing"
func TestProducer(t *testing.T){DefaultCalls=0;v:=DefaultE();if DebugE(&v)!="E::Item(Item { N: 7, Name: \"producer\" })"||DefaultCalls!=1{t.Fatal(v,DefaultCalls)};c:=CloneE(&v);if !EqualsE(v,c){t.Fatal("producer equality")}}
`
const singleExternalNativeAssertions = `package plain
import("testing";"github.com/goxide-lang/serde/json";m "a06.example/lib/model";r "a06.example/lib/relay";a "a06.example/lib/api")
func TestNativeWithoutMetadata(t *testing.T){var v r.Item=m.DefaultItem();c:=m.CloneItem(&v);if !m.EqualsItem(v,c)||m.DebugItem(&v)!="Item { N: 7, Name: \"producer\" }"{t.Fatal(v,c)};b,e:=serdejson.Marshal(&v,m.SerializeItem);if e!=nil{t.Fatal(e)};d,e:=serdejson.Unmarshal(b,m.DeserializeItem);if e!=nil||!m.EqualsItem(v,d){t.Fatal(d,e)};event:=m.NewEItem(v);m.ValidateE(event);if event.(m.GoxideCase1_E_Item).Value.N!=7||a.Read(a.Get())!=8{t.Fatal(event)};if m.Load(0).(m.GoxideResultOk[int,error]).Value!=4{t.Fatal("sum")}}
`
