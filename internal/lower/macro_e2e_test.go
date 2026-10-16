package lower_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
	testdeps "github.com/hxsf/goxide/tests/testutil"
)

func TestMacroPositionsAndHygiene(t *testing.T) {
	runMacroFixture(t, "positions", "85 2 3 41 45 9 9 -1000 21 local\n")
}

func TestMacroSourcePackages(t *testing.T) {
	runMacroFixture(t, "packages", "18 8 6 5 2 -1\n")
}

func runMacroFixture(t *testing.T, fixture, want string) {
	t.Helper()
	source := filepath.Join("testdata", "macros", fixture)
	generated, err := lower.PrepareModule([]string{source}, "macrofixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(generated) })
	repeated, err := lower.PrepareModule([]string{source}, "macrofixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(repeated) })
	err = filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(name) != ".gox" {
			return err
		}
		relative, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		outputName := strings.TrimSuffix(relative, ".gox") + ".go"
		output, err := os.ReadFile(filepath.Join(generated, outputName))
		if err != nil {
			return err
		}
		again, err := os.ReadFile(filepath.Join(repeated, outputName))
		if err != nil {
			return err
		}
		if !bytes.Equal(output, again) {
			t.Fatalf("%s: identical source produced different bytes", relative)
		}
		goldenName := strings.TrimSuffix(name, ".gox") + ".golden.go"
		golden, err := os.ReadFile(goldenName)
		if err != nil {
			return err
		}
		if !bytes.Equal(output, golden) {
			t.Fatalf("%s: generated code differs from golden:\n%s", relative, output)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runGo(t, generated, "build", "./...")
	if got := runGo(t, generated, "run", "."); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestMacroSourceVisibility(t *testing.T) {
	for _, test := range []struct{ name, declaration, call, want string }{
		{"private-macro", "macro_rules! hidden { () => { 1 }; }", "macros::hidden!()", "macro hidden is private"},
		{"private-helper", "func hidden() int { return 1 }; pub macro_rules! public { () => { hidden() }; }", "macros::public!()", "私有成员 hidden"},
		{"missing-source", "", "missing::work!()", "requires source module missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := t.TempDir()
			if err := os.Mkdir(filepath.Join(source, "kit"), 0700); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				"main.gox":       "package main\nuse macrofixture::kit as macros;\nfunc main() { _ = " + test.call + " }\n",
				"kit/macros.gox": "package kit\n" + test.declaration + "\n",
			}
			for name, contents := range files {
				if err := os.WriteFile(filepath.Join(source, name), []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			generated, err := lower.PrepareModule([]string{source}, "macrofixture", testConfig(t))
			if generated != "" {
				os.RemoveAll(generated)
				t.Fatalf("rejected macro produced output %s", generated)
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if test.name == "private-helper" {
				var diagnostic *lower.Diagnostic
				if !errors.As(err, &diagnostic) || diagnostic.Origin.Expn == 0 || len(diagnostic.Origin.Stack) != 1 || diagnostic.Definition.File == nil || diagnostic.Call.File == nil || diagnostic.Definition.File.Package.Name != "kit" || diagnostic.Call.File.Package.Name != "main" {
					t.Fatalf("private macro reference lost definition/call origins: %#v", diagnostic)
				}
			}
		})
	}
}

func TestMacroDeriveOwnerHygiene(t *testing.T) {
	source := t.TempDir()
	files := map[string]string{
		"go.mod":            "module macroderive\n\ngo 1.27\n",
		"goxide.roots.toml": "[roots.serde]\nmodule = \"github.com/goxide-lang/serde\"\npackage = \"github.com/goxide-lang/serde\"\n[roots.serde.namespaces]\njson = \"github.com/goxide-lang/serde/json\"\n",
		"macros.gox": `package main
use serde as owner;
macro_rules! named { ($owner:ident, $name:ident) => { #[derive($owner::Serialize)] type $name struct { Value int } }; }
macro_rules! relay { ($owner:ident, $name:ident) => { named!($owner, $name) }; }
macro_rules! defined { () => { #[derive(owner::Serialize)] type Defined struct { Value int } }; }
macro_rules! caller { () => { #[derive($caller::s::Serialize)] type Caller struct { Value int } }; }
`,
		"main.gox": `package main
use fmt as owner;
use serde as s;
use serde::json as json;
named!(s, Captured)
relay!(s, Forwarded)
defined!()
caller!()
func main() {
    captured := Captured{Value:1}
    forwarded := Forwarded{Value:2}
    defined := Defined{Value:3}
    caller := Caller{Value:4}
    a, _ := json::Marshal(&captured, SerializeCaptured)
    b, _ := json::Marshal(&forwarded, SerializeForwarded)
    c, _ := json::Marshal(&defined, SerializeDefined)
    d, _ := json::Marshal(&caller, SerializeCaller)
    owner::Println(string(a), string(b), string(c), string(d))
}
`,
	}
	for name, text := range files {
		if err := testdeps.WriteFile(filepath.Join(source, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var generated []string
	for range 2 {
		module, err := lower.PrepareModule([]string{source}, "macroderive", testConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(module) })
		generated = append(generated, module)
	}
	for _, name := range []string{"main.go", "macros.go"} {
		first, err := os.ReadFile(filepath.Join(generated[0], name))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(generated[1], name))
		if err != nil || !bytes.Equal(first, second) {
			t.Fatalf("%s: repeated generation differs: %v", name, err)
		}
	}
	runGo(t, generated[0], "build", "./...")
	if got := runGo(t, generated[0], "run", "."); got != "{\"Value\":1} {\"Value\":2} {\"Value\":3} {\"Value\":4}\n" {
		t.Fatalf("derived macro output = %q", got)
	}
	// A caller alias cannot acquire the definition file's same-spelled owner.
	invalid := "package main\nuse fmt as owner;\nrelay!(owner, Rejected)\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(source, "main.gox"), []byte(invalid), 0600); err != nil {
		t.Fatal(err)
	}
	module, err := lower.PrepareModule([]string{source}, "macroderive", testConfig(t))
	if module != "" {
		os.RemoveAll(module)
		t.Fatal("captured non-canonical owner produced Go output")
	}
	if err == nil || !strings.Contains(err.Error(), "derive") {
		t.Fatalf("non-canonical captured owner error = %v", err)
	}
}
