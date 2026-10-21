package check_test

import (
	"go/importer"
	"go/types"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/typecheck"
)

func TestArrayLayoutConstantsUseTargetSizes(t *testing.T) {
	for _, architecture := range []string{"386", "amd64", "s390x", "wasm"} {
		t.Run(architecture, func(t *testing.T) {
			files, resolved := parse(t, `package p
use r#unsafe;
use time;
const Message = "你" + "a"
var StringBytes [len(Message)]byte
var ImportedStringBytes [len(time::RFC3339)]byte
var ArrayLength [len([2]int{int(1)})]byte
var ArrayCapacity [cap([3]int{})]byte
var PointerLength [len((*[4]int)(nil))]byte
var PointerCapacity [cap((*[5]int)(nil))]byte
const Bytes = r#unsafe::Sizeof(uintptr(0))
type Record struct { head byte; word uintptr }
var Width [2 * Bytes]byte
var Alignment [r#unsafe::Alignof(Record{})]byte
var RecordBytes [r#unsafe::Sizeof(Record{})]byte
func SideEffect() uintptr { panic("must not run") }
var Unevaluated [r#unsafe::Sizeof(SideEffect())]byte
func Unused[const N int]() int { return N }
func Fixed[T any](value []T) { var FixedBytes [r#unsafe::Sizeof(value)]byte; _ = FixedBytes }
`)
			resolved = resolve.Resolve(files, typecheck.NewNamespaceImporter(importer.Default(), nil))
			if len(resolved.Diagnostics) != 0 {
				t.Fatal(resolved.Diagnostics)
			}
			sizes := types.SizesFor("gc", architecture)
			info, diagnostics := check.CheckWithImporter(files, resolved, nil, sizes)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			word := sizes.Sizeof(types.Typ[types.Uintptr])
			want := map[string]int64{"Width": 2 * word, "Alignment": word, "RecordBytes": 2 * word, "Unevaluated": word, "FixedBytes": 3 * word, "StringBytes": 4, "ImportedStringBytes": int64(len(time.RFC3339)), "ArrayLength": 2, "ArrayCapacity": 3, "PointerLength": 4, "PointerCapacity": 5}
			for symbol, typ := range info.Symbols {
				if length, ok := want[symbol.Name.Name]; ok {
					array, ok := typ.(*types.Array)
					if !ok || array.Len() != length {
						t.Fatalf("%s = %v, want length %d", symbol.Name.Name, typ, length)
					}
					delete(want, symbol.Name.Name)
				}
			}
			if len(want) != 0 {
				t.Fatalf("missing checked arrays: %v", want)
			}
			if len(info.Demands.Functions) != 0 {
				t.Fatalf("layout created const function demands: %+v", info.Demands.Functions)
			}
		})
	}
}

func TestArrayLayoutRejectsVariableSizeAndNonConstants(t *testing.T) {
	for name, source := range map[string]string{
		"type-parameter":        `func F[T any](value T) { var data [r#unsafe::Sizeof(value)]byte; _ = data }`,
		"generic-struct":        `type Box[T any] struct { value T }; func F[T any](value Box[T]) { var data [r#unsafe::Alignof(value)]byte; _ = data }`,
		"array-of-parameter":    `func F[T any](value [2]T) { var data [r#unsafe::Sizeof(value)]byte; _ = data }`,
		"runtime-call":          `func Size() int { return 3 }; var data [Size()]byte`,
		"runtime-variable":      `var Size = 3; var data [Size]byte`,
		"const-argument":        `func F[const N int]() int { return N }; var value = F[r#unsafe::Sizeof(uintptr(0))]()`,
		"shadowed-len":          `func F() { len := func(value [2]int) int { return 2 }; var data [len([2]int{})]byte; _ = data }`,
		"array-call":            `func Values() [2]int { return [2]int{} }; var data [len(Values())]byte`,
		"element-call":          `func Value() int { return 1 }; var data [len([2]int{Value()})]byte`,
		"channel-receive":       `var values chan [2]int; var data [len(<-values)]byte`,
		"slice-length":          `var data [len([]int{1,2})]byte`,
		"slice-capacity":        `var data [cap([]int{1,2})]byte`,
		"variable-string":       `var message = "hi"; var data [len(message)]byte`,
		"string-capacity":       `var data [cap("hi")]byte`,
		"length-const-argument": `func F[const N int]() int { return N }; var value = F[len([2]int{})]()`,
		"untyped-nil":           `var data [r#unsafe::Sizeof(nil)]byte`,
	} {
		t.Run(name, func(t *testing.T) {
			files, resolved := parse(t, "package p;use r#unsafe;"+source)
			resolved = resolve.Resolve(files, typecheck.NewNamespaceImporter(importer.Default(), nil))
			if len(resolved.Diagnostics) != 0 {
				t.Fatal(resolved.Diagnostics)
			}
			info, diagnostics := check.Check(files, resolved)
			found := false
			for _, diagnostic := range diagnostics {
				found = found || diagnostic.Code == check.GenericConstraint
			}
			if !found {
				t.Fatalf("missing array length rejection: %v", diagnostics)
			}
			if info.Demands != nil {
				t.Fatal("invalid layout reached demand collection")
			}
		})
	}
}
