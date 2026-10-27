package lower_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/typecheck"
)

func TestImportedGoTypesAndFunctionsShadowSumPrelude(t *testing.T) {
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module example.org/preludeshadow\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "foreign/foreign.go", []byte(`package foreign
type Option[T any] struct { Value T }
type Result[T any] struct { Value T }
func Some(value int) Option[int] { return Option[int]{Value: value} }
func None() Option[int] { return Option[int]{Value: 22} }
func Ok(value int) Result[int] { return Result[int]{Value: value} }
func Err() Result[int] { return Result[int]{Value: 24} }
`))
	const source = `package main
use foreign::Option;
use foreign::Result;
use foreign::Some;
use foreign::None;
use foreign::Ok;
use foreign::Err;
func readOption(value Option[int]) int { return value.Value }
func readResult(value Result[int]) int { return value.Value }
func main() { println(readOption(Some(21)), readOption(None()), readResult(Ok(23)), readResult(Err())) }
`
	name := filepath.Join(directory, "main.gox")
	configuration := testConfig(t)
	imports := typecheck.NewImporter(name, typecheck.ImporterOptions{Config: configuration})
	roots := paths.Roots{"foreign": {PackagePath: "example.org/preludeshadow/foreign", ModulePath: "example.org/preludeshadow", Source: "module"}}
	generate := func() []byte {
		t.Helper()
		output, err := lower.CompilePackage(map[string][]byte{name: []byte(source)}, "example.org/preludeshadow", imports, roots, lower.PackageOptions{Config: configuration})
		if err != nil {
			t.Fatal(err)
		}
		return output[name]
	}
	output := generate()
	if !bytes.Equal(output, generate()) {
		t.Fatal("shadowed prelude generation is not stable")
	}
	writeDiagnosticModuleFile(t, directory, "main_gox.go", output)
	runGo(t, directory, "build", "./...")
	if got := runGo(t, directory, "run", "."); got != "21 22 23 24\n" {
		t.Fatalf("shadowed prelude output = %q", got)
	}
}
