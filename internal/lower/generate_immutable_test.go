package lower_test

import (
	"context"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/typecheck"
)

func TestGenerateRetainsCheckedTypeTables(t *testing.T) {
	const common = `package main
trait Read { func read(self Self) int }
impl Read for int { func read(self Self) int { return self } }
impl[T Read,const N int] Read for [N]T {
 func read(self Self) int { var total int; for _,v := range self { total += v.read() }; return total }
}
func Copy[T any,const N int](value [N]T) [N]T { return value }
func Total[T Read](value T) int { return value.read() }
enum Leaf { Live }
enum Box[T any] { Stored(T) }
`
	const arrays = `trait Mark {}
impl Mark for Leaf {}
func Check[T Mark,const N int](value Box[[N]T]) { __goxide_enum_assert(value) }
func Relay[T Mark,const N int](value Box[[N]T]) { Check[T,N](value) }
func main() {
 values := [2]int{20,22}
 copied := Copy(values)
 if copied[0]+copied[1] != 42 || values.read() != 42 { panic("wrong array total") }
 Relay[Leaf,2](Box[[2]Leaf]::Stored([2]Leaf{Leaf::Live,Leaf::Live}))
 Relay[Leaf,0](Box[[0]Leaf]::Stored([0]Leaf{}))
}
`
	const stable = `func Check(value Box[Leaf]) { __goxide_enum_assert(value) }
func main() {
 values := [2]int{20,22}
 copied := Copy(values)
 if copied[0]+copied[1] != 42 || values.read() != 42 { panic("wrong array total") }
 Check(Box[Leaf]::Stored(Leaf::Live))
}
`
	for _, fixture := range []struct {
		name, source string
		stableGo     bool
	}{
		{"array enum payload", common + arrays, false},
		{"stable templates", common + stable, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			configuration := testConfig(t)
			result, err := analysis.Analyze(context.Background(), analysis.Input{
				PackagePath: "typefixture", Sources: map[string][]byte{"main.gox": []byte(fixture.source)},
				GoImporter: typecheck.NewImporter("fixture.gox", typecheck.ImporterOptions{Config: configuration}),
				Options:    analysis.Options{PreserveSyntax: true, Config: configuration},
			})
			if err != nil {
				t.Fatal(err)
			}
			before := checkedTypeTables(result.Checked)
			if len(before) < 2 || len(result.Checked.ArrayPatterns) == 0 {
				t.Fatal("fixture did not produce symbolic arrays and checked body views")
			}
			// Start from a cold result so the race check also covers any first-use work.
			const workers = 4
			outputs := make([]map[string][]byte, workers)
			errors := make([]error, workers)
			options := lower.GenerationOptions{StableGo: fixture.stableGo}
			var group sync.WaitGroup
			for index := range workers {
				group.Add(1)
				go func() {
					defer group.Done()
					outputs[index], errors[index] = lower.Generate(context.Background(), result, options)
				}()
			}
			group.Wait()
			for index, err := range errors {
				if err != nil {
					t.Fatalf("generation %d: %v", index, err)
				}
				if !reflect.DeepEqual(outputs[index], outputs[0]) {
					t.Fatalf("independent generation %d changed output bytes", index)
				}
			}
			assertCheckedTypeTables(t, before, result.Checked)
			for index := range 2 {
				output, err := lower.Generate(context.Background(), result, options)
				if err != nil || !reflect.DeepEqual(output, outputs[0]) {
					t.Fatalf("repeated generation %d changed output: %v", index, err)
				}
				assertCheckedTypeTables(t, before, result.Checked)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module typefixture\n\ngo 1.27\n"), 0600); err != nil {
				t.Fatal(err)
			}
			for name, data := range outputs[0] {
				if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)+".go"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			pinP4Dependencies(t, dir)
			runGo(t, dir, "run", ".")
		})
	}
}

type checkedTables struct {
	collections map[string]any
	arrays      map[*types.TypeParam]check.ArrayPattern
}

func checkedTypeTables(info *check.Info) map[*check.Info]checkedTables {
	result := map[*check.Info]checkedTables{}
	var visit func(*check.Info)
	visit = func(info *check.Info) {
		if info == nil {
			return
		}
		if _, exists := result[info]; exists {
			return
		}
		snapshot := checkedTables{collections: map[string]any{}, arrays: map[*types.TypeParam]check.ArrayPattern{}}
		value := reflect.ValueOf(info).Elem()
		for index := range value.NumField() {
			field := value.Field(index)
			if field.CanInterface() && (field.Kind() == reflect.Map || field.Kind() == reflect.Slice) {
				snapshot.collections[value.Type().Field(index).Name] = copyCheckedCollection(field).Interface()
			}
		}
		for parameter, pattern := range info.ArrayPatterns {
			snapshot.arrays[parameter] = *pattern
		}
		result[info] = snapshot
		for _, cases := range info.BodyCases {
			for _, branch := range cases {
				visit(branch.Info)
			}
		}
		for _, cases := range info.ImplCases {
			for _, branch := range cases {
				visit(branch.Info)
			}
		}
		if plan := info.Demands; plan != nil {
			visit(plan.Info)
			for _, body := range plan.Roots {
				visit(body.Info)
			}
			for _, bodies := range plan.Bodies {
				for _, body := range bodies {
					visit(body.Info)
				}
			}
		}
	}
	visit(info)
	return result
}

// Copy collection membership without traversing shared semantic identities or
// go/types internals. Array patterns are copied separately by value above.
func copyCheckedCollection(value reflect.Value) reflect.Value {
	if value.IsNil() {
		return value
	}
	switch value.Kind() {
	case reflect.Map:
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		entries := value.MapRange()
		for entries.Next() {
			entry := entries.Value()
			if entry.Kind() == reflect.Map || entry.Kind() == reflect.Slice {
				entry = copyCheckedCollection(entry)
			}
			result.SetMapIndex(entries.Key(), entry)
		}
		return result
	case reflect.Slice:
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		reflect.Copy(result, value)
		return result
	}
	panic("expected checked collection")
}

func assertCheckedTypeTables(t *testing.T, before map[*check.Info]checkedTables, info *check.Info) {
	t.Helper()
	after := checkedTypeTables(info)
	if len(before) != len(after) {
		t.Fatalf("generation changed checked views: %d -> %d", len(before), len(after))
	}
	for view, old := range before {
		current := after[view]
		for name, collection := range old.collections {
			if !reflect.DeepEqual(collection, current.collections[name]) {
				t.Fatalf("generation changed checked %s collection", name)
			}
		}
		if !reflect.DeepEqual(old.arrays, current.arrays) {
			t.Fatal("generation changed checked array patterns")
		}
	}
}
