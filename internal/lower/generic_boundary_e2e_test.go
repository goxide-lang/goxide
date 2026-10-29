package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestConditionalImplOrdinaryGoBoundary(t *testing.T) {
	sources := map[string]string{"boundary/library/library.gox": `package library
pub trait Copy { func copy(self &Self) Self }
impl Copy for int { func copy(self &Self) Self { return *self } }
pub struct Box[T any] { pub Value T }
impl[T Copy] Copy for []T {
    func copy(self &Self) Self {
        if *self == nil { return nil }
        out := make([]T, len(*self))
        for i := range *self { out[i] = Copy::copy(&(*self)[i]) }
        return out
    }
}
impl[T Copy] Copy for Box[T] {
    func copy(self &Self) Self { return Self{Value: Copy::copy(&self.Value)} }
}
pub func copyInts(value &[]int) []int { return Copy::copy(value) }
pub func copyBox(value &Box[[]int]) Box[[]int] { return Copy::copy(value) }
pub func identity[T any](value T) T { return value }
`}
	generated, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	again, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	module := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module boundary\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(module, "library"), 0700); err != nil {
		t.Fatal(err)
	}
	name := "boundary/library/library.gox"
	if !bytes.Equal(generated[name], again[name]) {
		t.Fatal("conditional output is not deterministic")
	}
	if !strings.Contains(string(generated[name]), "func Identity[T any]") {
		t.Fatal("ordinary Go generic function was not preserved")
	}
	if strings.Contains(string(generated[name]), "GoxideBody_") {
		t.Fatal("obsolete forwarding bridge in generated Go")
	}
	if err := os.WriteFile(filepath.Join(module, "library", "library.go"), generated[name], 0600); err != nil {
		t.Fatal(err)
	}
	consumer := `package main
import("fmt"; "boundary/library")
func main() {
    values := []int{1, 2}
    copied := library.CopyInts(&values)
    boxed := library.Box[[]int]{Value: values}
    copiedBox := library.CopyBox(&boxed)
    values[0] = 9
    unconstrained := library.Box[func()]{Value: func(){}}
    unconstrained.Value()
    fmt.Println(copied, copiedBox.Value, library.Identity("ordinary"))
}
`
	if err := os.WriteFile(filepath.Join(module, "main.go"), []byte(consumer), 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "[1 2] [1 2] ordinary\n" {
		t.Fatalf("ordinary Go consumer output = %q", got)
	}
}

func TestGenericGoEntryRecoverFrame(t *testing.T) {
	sources := map[string]string{"recoverfixture/library/library.gox": `package library
pub trait Mark {}
pub struct Token {}
impl Mark for Token {}
pub func catchPanic[T Mark](caught *bool) { *caught = recover() != nil }
`}
	generated, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	module := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module recoverfixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(module, "library"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "library", "library.go"), generated["recoverfixture/library/library.gox"], 0600); err != nil {
		t.Fatal(err)
	}
	consumer := `package main
import("fmt"; "recoverfixture/library")
func main() {
    caught := false
    func() { defer library.CatchPanic[library.Token](&caught); panic("caught") }()
    fmt.Println(caught)
}
`
	if err := os.WriteFile(filepath.Join(module, "main.go"), []byte(consumer), 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "true\n" {
		t.Fatalf("recover output = %q", got)
	}
}

func TestConditionalTemplatesRequireConcreteDemand(t *testing.T) {
	sources := map[string]string{"demandfixture/library/library.gox": `package library
pub trait Copy { func copy(self &Self) Self }
impl Copy for int { func copy(self &Self) Self { return *self } }
impl[T Copy] Copy for []T {
    func copy(self &Self) Self {
        out := make([]T, len(*self))
        for i := range *self { out[i] = Copy::copy(&(*self)[i]) }
        return out
    }
}
pub struct Ghost[T any] { pub Value T }
impl[T Copy] Copy for Ghost[T] {
    func copy(self &Self) Self { return Self{Value: Copy::copy(&self.Value)} }
}
pub func copyOnce[T Copy](value T) T { return Copy::copy(&value) }
pub func copyNested[T Copy](value []T) []T { return copyOnce[[]T](value) }
pub trait Probe { func probe(self Self) int }
pub struct Runner {}
impl Probe for Runner {
    func probe(self Self) int {
        value := []int{42}
        return Copy::copy(&value)[0]
    }
}
`}
	generated, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	data := generated["demandfixture/library/library.gox"]
	if strings.Contains(string(data), "GhostTCopy") || strings.Contains(string(data), "func Grow[") || strings.Contains(string(data), "GrowFunc") {
		t.Fatalf("unused conditional templates emitted executable bodies:\n%s", data)
	}
	module := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module demandfixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(module, "library"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "library", "library.go"), data, 0600); err != nil {
		t.Fatal(err)
	}
	consumer := `package main
import("fmt"; "demandfixture/library")
func main() { fmt.Println(library.Runner{}.AsProbe().Probe()) }
`
	if err := os.WriteFile(filepath.Join(module, "main.go"), []byte(consumer), 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "42\n" {
		t.Fatalf("concrete impl did not retain its conditional body requirements: %q", got)
	}
}
