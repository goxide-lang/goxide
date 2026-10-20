package lower_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestRawDefaultMemberMatchesBareDefault(t *testing.T) {
	const source = `package main
use fmt;

type Entry struct { Label string; Value int }
var calls int
impl Default for Entry {
    func %s() Entry {
        calls++
        return Entry{Label: "manual", Value: 40 + calls}
    }
}
#[derive(Default)]
type Pair struct { First Entry; Second Entry }

func main() {
    pair := DefaultPair()
    direct := DefaultEntry()
    fmt::Println(pair.First.Label, pair.First.Value, pair.Second.Label, pair.Second.Value, direct.Label, direct.Value, calls)
}
`
	const want = "manual 41 manual 42 manual 43 3\n"
	outputs := make(map[string]string)
	for _, spelling := range []string{"default", "r#default"} {
		t.Run(spelling, func(t *testing.T) {
			sources := map[string]string{"protocolnames/main.gox": fmt.Sprintf(source, spelling)}
			generated, err := lowerDeriveSources(sources)
			if err != nil {
				t.Fatal(err)
			}
			again, err := lowerDeriveSources(sources)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(generated, again) {
				t.Fatal("same sources produced different Go bytes")
			}
			module := t.TempDir()
			if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module protocolnames\n\ngo 1.27\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(module, "main.go"), generated["protocolnames/main.gox"], 0600); err != nil {
				t.Fatal(err)
			}
			runGo(t, module, "build", "./...")
			got := runGo(t, module, "run", ".")
			if got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
			outputs[spelling] = got
		})
	}
	if outputs["default"] != outputs["r#default"] {
		t.Fatalf("default output = %q, r#default output = %q", outputs["default"], outputs["r#default"])
	}
}

func TestCapTraitMemberAndBuiltin(t *testing.T) {
	const source = `package main
use fmt;

trait Capacity { func cap(self Self) int }
type Buffer struct { Value int }
impl Capacity for Buffer {
    func cap(self Self) int { return self.Value + 40 }
}

func main() {
    value := Buffer{Value: 1}
    items := make([]int, 2, 5)
    fmt::Println(value.cap(), cap(items))
}
`
	sources := map[string]string{"protocolnames/main.gox": source}
	generated, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	again, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(generated, again) {
		t.Fatal("same sources produced different Go bytes")
	}
	module := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module protocolnames\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "main.go"), generated["protocolnames/main.gox"], 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", "./...")
	const want = "41 5\n"
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
