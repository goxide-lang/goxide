package check_test

import (
	"go/types"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestMinMaxPreserveNamedAndParameterTypes(t *testing.T) {
	files, resolved := parse(t, `package p
type Number int
type Label string
func F() {
	minimum := min(Number(3), 1)
	maximum := max(2, Number(4))
	label := max(Label("a"), "b")
	runeValue := max(1, 'b')
	floatValue := min(2, 1.5)
	_, _, _, _, _ = minimum, maximum, label, runeValue, floatValue
}
func G[T interface{~int}](a, b T) T { selected := min(a, b); return selected }
`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	names := map[string]types.Type{}
	for symbol, typ := range info.Symbols {
		names[symbol.Name.Name] = typ
	}
	for name, want := range map[string]types.Type{
		"minimum": names["Number"], "maximum": names["Number"], "label": names["Label"],
		"runeValue": types.Typ[types.Rune], "floatValue": types.Typ[types.Float64], "selected": names["T"],
	} {
		if want == nil || names[name] == nil || !types.Identical(names[name], want) {
			t.Errorf("%s has type %v, want %v", name, names[name], want)
		}
	}
}

func TestRangePreservesIteratorAndPointerArrayElements(t *testing.T) {
	files, resolved := parse(t, `package p
type Item struct{ Value int }
type Items [2]Item
type Yield func(Item) bool
type Sequence func(Yield)
func iterator(yield Yield) { yield(Item{Value: 7}) }
func pairs(yield func(Item, *Item) bool) {}
func F(values *Items) {
	for fromIterator := range iterator { _ = fromIterator.Value }
	for fromNamed := range Sequence(iterator) { _ = fromNamed.Value }
	for first, second := range pairs { _, _ = first.Value, second.Value }
	for _, fromArray := range values { _ = fromArray.Value }
}
`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	names := map[string]types.Type{}
	for symbol, typ := range info.Symbols {
		names[symbol.Name.Name] = typ
	}
	item := names["Item"]
	if item == nil {
		t.Fatal("missing Item type")
	}
	for name, want := range map[string]types.Type{
		"fromIterator": item, "fromNamed": item, "first": item, "second": types.NewPointer(item), "fromArray": item,
	} {
		if names[name] == nil || !types.Identical(names[name], want) {
			t.Errorf("%s has type %v, want %v", name, names[name], want)
		}
	}
}
