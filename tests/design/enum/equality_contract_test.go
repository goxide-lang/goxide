package enumdemo_test

import (
	e "example.org/hgodesigncheck/enumdemo"
	"maps"
	"slices"
	"testing"
)

// Reference contract only: this does not compile hgo or implement container derives.
// Primitive element/key types deliberately avoid choosing custom map-key semantics.
func TestNilEmptyEqualityContract(t *testing.T) {
	sliceEqual := func(a, b []int) bool { return (a == nil) == (b == nil) && slices.Equal(a, b) }
	mapEqual := func(a, b map[string]int) bool { return (a == nil) == (b == nil) && maps.Equal(a, b) }
	for _, tc := range []struct {
		a, b []int
		want bool
	}{{nil, nil, true}, {nil, []int{}, false}, {[]int{}, nil, false}, {[]int{}, []int{}, true}, {[]int{1}, []int{1}, true}, {[]int{1}, []int{2}, false}} {
		if sliceEqual(tc.a, tc.b) != tc.want {
			t.Fatal("slice nil/content contract", tc)
		}
	}
	for _, tc := range []struct {
		a, b map[string]int
		want bool
	}{{nil, nil, true}, {nil, map[string]int{}, false}, {map[string]int{}, nil, false}, {map[string]int{}, map[string]int{}, true}, {map[string]int{"x": 1}, map[string]int{"x": 1}, true}, {map[string]int{"x": 1}, map[string]int{"x": 2}, false}} {
		if mapEqual(tc.a, tc.b) != tc.want {
			t.Fatal("map nil/content contract", tc)
		}
	}
	optionEqual := func(a, b e.Option[[]int]) bool {
		if a == nil || b == nil {
			return a == nil && b == nil
		}
		return sliceEqual(a.(e.Some[[]int]).Value, b.(e.Some[[]int]).Value)
	}
	values := []e.Option[[]int]{nil, e.Some[[]int]{Value: nil}, e.Some[[]int]{Value: []int{}}}
	for i, a := range values {
		for j, b := range values {
			if optionEqual(a, b) != (i == j) {
				t.Fatalf("Option states %d and %d", i, j)
			}
		}
	}
}
