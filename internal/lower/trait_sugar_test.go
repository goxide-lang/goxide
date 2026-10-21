package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

const sugarCell = `package model
pub struct Cell { pub N int }
pub trait Observe { func inspect(self &Self) int }
impl Observe for Cell { func inspect(self &Self) int { return self.N } }
pub trait Change { func add(self &mut Self, n int) int }
impl Change for Cell { func add(self &mut Self, n int) int { self.N += n; return self.N } }
pub trait Shift { func shifted(self Self, n int) Self }
impl Shift for Cell { func shifted(self Self, n int) Self { self.N += n; return self } }
pub trait Count { func count(self &Self, values ...int) int }
impl Count for Cell { func count(self &Self, values ...int) int { return self.N + len(values) } }
pub trait Construct { func make() Self }
impl Construct for Cell { func make() Self { return Cell{N: 8} } }
pub trait Extra { func twice(self Self) int }
macro_rules! extra_impl { () => {
    impl Extra for Cell { func twice(self Self) int { return self.N * 2 } }
}; }
extra_impl!()
impl Cell { pub func own(self Self) int { return self.N + 100 } }
`

func TestGoSugarReceiverAndNativeInterop(t *testing.T) {
	const native = `package model
func RequireInspectable(value interface{ Inspect() int }) int { return value.Inspect() }
func CallGeneric[T interface{ Inspect() int }](value T) int { return value.Inspect() }
`
	const throughGo = `
pub func ThroughGo(value *Cell) int { return RequireInspectable(value) + CallGeneric(value) }
`
	const consumer = `package main
import (
    "fmt"
    "sugarfixture/model"
)
func main() {
    value := model.Cell{N: 3}
    inspect := value.Inspect
    fmt.Println(value.Add(4))
    fmt.Println(value.N, value.Inspect(), inspect())
    shifted := value.Shifted(5)
    fmt.Println(shifted.N, value.N, value.Count(20, 30), value.Twice())
    fmt.Println(model.ThroughGo(&value), value.Own())
    _, marked := any(&value).(model.Observe)
    fmt.Println(marked, value.AsObserve().Inspect())
}
`
	runSugarConsumer(t, sugarCell+throughGo, native, consumer, nil, "7\n7 7 7\n12 7 9 14\n14 107\nfalse 7\n")
}

func TestGoSugarPreservesReadonlyInterfaceBoundary(t *testing.T) {
	sources := map[string][]byte{
		"model.gox": []byte(sugarCell + `
pub func ThroughGo(value &Cell) int { return RequireInspectable(value) }
`),
		"native.go": []byte(`package model
func RequireInspectable(value interface{ Inspect() int }) int { return value.Inspect() }
`),
	}
	output, err := lower.CompilePackage(sources, "sugarfixture/model", nil, nil, lower.PackageOptions{Config: testConfig(t)})
	if err == nil || len(output) != 0 || !strings.Contains(err.Error(), "cannot erase or increase read-only reference permissions") {
		t.Fatalf("Go sugar erased the readonly interface boundary: %v", err)
	}
}

func TestGoSugarDisabledRetainsViewsAndStaticCarrier(t *testing.T) {
	const consumer = `package main
import (
    "fmt"
    "github.com/goxide-lang/std/typeinfo"
    "sugarfixture/model"
)
func main() {
    value := model.Cell{N: 3}
    fmt.Println(value.AsChange().Add(4))
    _, inspectable := any(&value).(interface{ Inspect() int })
    fmt.Println(inspectable, value.AsObserve().Inspect(), value.N, value.Own())
    made := typeinfo.Of[model.Cell]().Make()
    fmt.Println(made.N)
}
`
	disabled := false
	runSugarConsumer(t, sugarCell, "", consumer, &disabled, "7\nfalse 7 7 107\n8\n")
}

func TestGoSugarUnconditionalGenericReceiver(t *testing.T) {
	const source = `package model
pub struct Box[T any] { pub Value T }
pub trait Store {
    type Item
    func get(self Self) Self::Item
    func set(self &mut Self, value Self::Item)
}
impl[T any] Store for Box[T] {
    type Item = T
    func get(self Self) Self::Item { return self.Value }
    func set(self &mut Self, value Self::Item) { self.Value = value }
}
`
	const consumer = `package main
import (
    "fmt"
    "sugarfixture/model"
)
func main() {
    value := model.Box[int]{Value: 3}
    value.Set(9)
    fmt.Println(value.Get(), value.Value)
    text := model.Box[string]{Value: "before"}
    text.Set("after")
    fmt.Println(text.Get())
}
`
	runSugarConsumer(t, source, "", consumer, nil, "9 9\nafter\n")
}

func TestGoSugarExplicitMethodPrecedence(t *testing.T) {
	const source = `package model
pub struct Cell {}
pub trait First { func value(self Self) int }
pub trait Second { func value(self Self) int }
impl First for Cell { func value(self Self) int { return 1 } }
impl Second for Cell { func value(self Self) int { return 2 } }
`
	const consumer = `package main
import (
    "fmt"
    "sugarfixture/model"
)
func main() {
    value := model.Cell{}
    fmt.Println(value.Value(), value.AsFirst().Value(), value.AsSecond().Value())
}
`
	for _, test := range []struct {
		name, source, native, want string
	}{
		{"inherent", source + `impl Cell { pub func value(self Self) int { return First::value(self) } }`, "", "1 1 2\n"},
		{"native", source, "package model\nfunc (Cell) Value() int { return 3 }\n", "3 1 2\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runSugarConsumer(t, test.source, test.native, consumer, nil, test.want)
		})
	}
}

func TestGoSugarRejectsFinalNameConflicts(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []string
	}{
		{"traits", `package model
pub struct Cell {}
pub trait First { func value(self Self) int }
pub trait Second { func value(self Self) int }
impl First for Cell { func value(self Self) int { return 1 } }
impl Second for Cell { func value(self Self) int { return 2 } }
`, []string{"Value", "First", "Second"}},
		{"field", `package model
pub struct Cell { pub Value int }
pub trait First { func value(self Self) int }
impl First for Cell { func value(self Self) int { return 1 } }
`, []string{"Value"}},
		{"fixed_helper", `package model
pub struct Cell {}
pub trait First { func asFirst(self Self) int }
impl First for Cell { func asFirst(self Self) int { return 1 } }
`, []string{"AsFirst"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := lower.CompilePackage(map[string][]byte{"model.gox": []byte(test.source)}, "sugarfixture/model", nil, nil, lower.PackageOptions{Config: testConfig(t)})
			if err == nil || len(output) != 0 {
				t.Fatalf("conflicting sugar generated output: %v", err)
			}
			for _, name := range test.want {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("diagnostic %q does not identify %s", err, name)
				}
			}
		})
	}
}

func runSugarConsumer(t *testing.T, source, native, consumer string, sugar *bool, want string) {
	t.Helper()
	configuration := testConfig(t)
	configuration.GoSugar = sugar
	sources := map[string][]byte{"model.gox": []byte(source)}
	if native != "" {
		sources["native.go"] = []byte(native)
	}
	options := lower.PackageOptions{Config: configuration}
	output, err := lower.CompilePackage(sources, "sugarfixture/model", nil, nil, options)
	if err != nil {
		t.Fatal(err)
	}
	again, err := lower.CompilePackage(sources, "sugarfixture/model", nil, nil, options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output["model.gox"], again["model.gox"]) {
		t.Fatal("identical sugar inputs produced different Go bytes")
	}
	module := t.TempDir()
	if err := os.Mkdir(filepath.Join(module, "model"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module sugarfixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{
		"model/model_gox.go": output["model.gox"],
		"main.go":            []byte(consumer),
	} {
		if err := os.WriteFile(filepath.Join(module, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if native != "" {
		if err := os.WriteFile(filepath.Join(module, "model/native.go"), []byte(native), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("Go sugar output = %q, want %q", got, want)
	}
}
