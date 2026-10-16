package lower_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
	testdeps "github.com/hxsf/goxide/tests/testutil"
)

func TestReferenceValuesCopiesAndRebinding(t *testing.T) {
	runReferenceFixture(t, "values", "8 105 true\n2 3 5 7 10 20 31 41 61 10 10\n")
}

func TestReferenceEnumProjectionIdentityAndReplacement(t *testing.T) {
	runReferenceFixture(t, "identity", "5 5\n5\ntrue 10 7 7\n8 8\n20 8\n2 1 3 0 5\n4 3 0 9\n")
}

func TestReferenceMatchSnapshotEvaluationAndControl(t *testing.T) {
	runReferenceFixture(t, "evaluation", "1 9\n2 sig\n3 done\nstop done\n10 xx\nindex x\n")
}

func TestReferenceSourcePackagesGenericFieldsAndMacros(t *testing.T) {
	runReferenceFixture(t, "packages", "12 12\n7 changed\n")
}

func TestReferenceEnumBorrowExample(t *testing.T) {
	module := prepareP2Example(t, "enum_borrows", "referenceexample")
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "5 5\n5\n" {
		t.Fatalf("enum_borrows output = %q", got)
	}
}

func TestReferenceFlowRetainsOnlyCurrentSharedPermissions(t *testing.T) {
	runReferenceFixture(t, "flow", "0 2 2 11\n4\n")
}

func TestReferenceDerivedProtocolsReadAndCloneSharedStorage(t *testing.T) {
	runReferenceFixture(t, "protocols", "true true 1 2 9 8\nData { Items: [1], Counts: {\"one\": 2} }\ntrue true\nState::Pair(Data { Items: [1], Counts: {\"one\": 2} }) State::Pair(Data { Items: [7], Counts: {\"one\": 6} })\n")
}

func runReferenceFixture(t *testing.T, fixture, want string) {
	t.Helper()
	directory := filepath.Join("testdata", "references", fixture)
	generated, err := lower.PrepareModule([]string{directory}, "referencefixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(generated) })
	again, err := lower.PrepareModule([]string{directory}, "referencefixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(again) })
	err = filepath.WalkDir(directory, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Ext(name) != ".gox" {
			return walkErr
		}
		relative, err := filepath.Rel(directory, name)
		if err != nil {
			return err
		}
		outputName := strings.TrimSuffix(relative, ".gox") + ".go"
		output, err := os.ReadFile(filepath.Join(generated, outputName))
		if err != nil {
			return err
		}
		repeated, err := os.ReadFile(filepath.Join(again, outputName))
		if err != nil {
			return err
		}
		if !bytes.Equal(output, repeated) {
			t.Fatalf("%s: identical source produced different Go bytes", relative)
		}
		goldenName := strings.TrimSuffix(name, ".gox") + ".golden.go"
		if os.Getenv("GOXIDE_UPDATE_REFERENCE_GOLDEN") == "1" {
			if err := os.WriteFile(goldenName, output, 0600); err != nil {
				return err
			}
		}
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
	moduleName := filepath.Join(generated, "go.mod")
	module, err := os.ReadFile(moduleName)
	if err != nil {
		t.Fatal(err)
	}
	if err := testdeps.WriteFile(moduleName, module, 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, generated, "build", "./...")
	if got := runGo(t, generated, "run", "."); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if native, err := os.ReadFile(filepath.Join(directory, "consumer_test.go")); err == nil {
		if err := os.WriteFile(filepath.Join(generated, "consumer_test.go"), native, 0600); err != nil {
			t.Fatal(err)
		}
		runGo(t, generated, "vet", "./...")
		runGo(t, generated, "test", "-race", "./...")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
