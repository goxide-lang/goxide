package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoSugarFlagsAreMutuallyExclusive(t *testing.T) {
	for _, command := range []string{"gen", "transpile", "build", "run", "test"} {
		var output bytes.Buffer
		err := run([]string{command, "--go-sugar", "--no-go-sugar", "missing.gox"}, &output, &output)
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("%s accepted conflicting sugar options: %v", command, err)
		}
	}
}

func TestNativeCodegenConfigurationKeepsInPlaceGeneration(t *testing.T) {
	root := t.TempDir()
	singleFileWrite(t, root, "go.mod", "module example.org/codegen\ngo 1.27\n")
	singleFileWrite(t, root, "goxide.toml", "[codegen]\ngo_sugar=false\n")
	singleFileWrite(t, root, "value.gox", "package main\npub func Value()int{return 42}\n")
	singleFileWrite(t, root, "main.go", "package main\nfunc main(){if Value()!=42 {panic(\"value\")}}\n")
	singleFileWrite(t, root, "main_test.go", "package main\nimport \"testing\"\nfunc TestValue(t *testing.T){if Value()!=42 {t.Fatal(\"value\")}}\n")
	before := singleFileSnapshot(t, root)
	var first []byte
	for _, args := range [][]string{{"gen", root}, {"transpile", "--no-go-sugar", root}, {"gen", "--go-sugar", root}} {
		var output bytes.Buffer
		if err := run(args, &output, &output); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, &output)
		}
		generated, err := os.ReadFile(filepath.Join(root, "value_gox.go"))
		if err != nil || !bytes.HasPrefix(generated, []byte(generatedHeader)) {
			t.Fatalf("native generation did not write its owned file: %v\n%s", err, generated)
		}
		if first == nil {
			first = generated
		} else if !bytes.Equal(first, generated) {
			t.Fatal("sugar policy changed a package with no trait implementation")
		}
	}
	for _, args := range [][]string{
		{"build", "--no-go-sugar", "-o", filepath.Join(t.TempDir(), "app"), root},
		{"run", "--go-sugar", root},
		{"test", "--no-go-sugar", root},
	} {
		var output bytes.Buffer
		if err := run(args, &output, &output); err != nil {
			t.Fatalf("native configuration %v: %v\n%s", args, err, &output)
		}
	}
	after := singleFileSnapshot(t, root)
	for name, contents := range before {
		if after[name] != contents {
			t.Fatalf("native generation changed authored %s", name)
		}
	}
	if len(after) != len(before)+1 {
		t.Fatalf("native codegen settings created project artifacts: %v", after)
	}
}
