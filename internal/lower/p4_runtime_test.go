package lower_test

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestP4RuntimeViewsAndQualifiedMethods(t *testing.T) {
	module := prepareP4RuntimeFixture(t, "runtime", "traitruntime")
	runGo(t, module, "build", "./...")
	const want = "-1 102 trait 102 204 8 borrow\n6 101 102 103 104 105 106\n103 15 8\n104 208 6 10 7 7 -1\n9 9\nfalse false true\n"
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("runtime views and method selection = %q, want %q", got, want)
	}
	runGo(t, module, "vet", "./...")
}

func TestP4ExplicitWrappersShareNativeStateAndRestrictMethodSet(t *testing.T) {
	module := prepareP4RuntimeFixture(t, "wrappers", "traitwrappers")
	runGo(t, module, "build", "./...")
	const want = "true false false 6\n6 source <nil> 0\n2 pa <nil> 2\n2 ir <nil> 0\nother <nil>\nagain <nil>\nview <nil> true true\n"
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("explicit wrapper = %q, want %q", got, want)
	}
	runGo(t, module, "vet", "./...")
}

func prepareP4RuntimeFixture(t *testing.T, fixtureName, modulePath string) string {
	t.Helper()
	fixture := filepath.Join("testdata", "traits", fixtureName)
	input := t.TempDir()
	if err := os.CopyFS(input, os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	if err := testdeps.WriteFile(filepath.Join(input, "go.mod"), []byte("module "+modulePath+"\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture = input
	configuration := testConfig(t)
	if fixtureName == "runtime" {
		// This fixture deliberately gives several traits the same Read method.
		// It exercises qualified views, with short Go member sugar disabled.
		disabled := false
		configuration.GoSugar = &disabled
	}
	module, err := lower.PrepareModule([]string{fixture}, modulePath, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(module) })
	again, err := lower.PrepareModule([]string{fixture}, modulePath, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(again) })
	if err := filepath.WalkDir(fixture, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(name) != ".gox" {
			return nil
		}
		relative, err := filepath.Rel(fixture, name)
		if err != nil {
			return err
		}
		generated := strings.TrimSuffix(relative, ".gox") + ".go"
		first, err := os.ReadFile(filepath.Join(module, generated))
		if err != nil {
			return err
		}
		second, err := os.ReadFile(filepath.Join(again, generated))
		if err != nil {
			return err
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("identical %s inputs produced different Go bytes", fixtureName)
		}
		formatted, err := format.Source(first)
		if err != nil {
			return err
		}
		if !bytes.Equal(first, formatted) {
			t.Fatalf("%s output is not gofmt formatted", generated)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	pinP4Dependencies(t, module)
	consumer, err := os.ReadFile(filepath.Join(fixture, "consumer.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "main.go"), consumer, 0600); err != nil {
		t.Fatal(err)
	}
	return module
}
