package main

import (
	"fmt"
	"github.com/goxide-lang/std/sum"
)

func mark(name string, value int, trace *[]string) int { *trace = append(*trace, name); return value }
func load(name string, value int, ok bool, trace *[]string) sum.Result[int, string] {
	*trace = append(*trace, name)
	if !ok {
		return sum.NewErr[int]("stop")
	}
	return sum.NewOk[int, string](value)
}
func mutate(values *[2]int, ok bool, trace *[]string) sum.Result[int, string] {
	values[1] = 99
	return load("index", 1, ok, trace)
}
func readArray(ok bool, trace *[]string) sum.Result[int, string] {
	values := [2]int{10, 20}
	index := mutate(&values, ok, trace)
	sum.ValidateResult(index)
	if sum.TagResult(index) == 2 {
		return sum.NewErr[int](sum.ValueErr(index))
	}
	return sum.NewOk[int, string](values[sum.ValueOk(index)])
}
func sliceArray(ok bool, trace *[]string) sum.Result[[]int, string] {
	values := [3]int{1, 2, 3}
	low := load("low", 1, ok, trace)
	sum.ValidateResult(low)
	if sum.TagResult(low) == 2 {
		return sum.NewErr[[]int](sum.ValueErr(low))
	}
	part := values[sum.ValueOk(low):]
	part[0] = 9
	return sum.NewOk[[]int, string]([]int{values[1], part[0]})
}
func buildMap(keyOK, valueOK bool, trace *[]string) sum.Result[map[int]int, string] {
	k1 := mark("k1", 1, trace)
	v1 := mark("v1", 10, trace)
	k2 := load("k2", 2, keyOK, trace)
	sum.ValidateResult(k2)
	if sum.TagResult(k2) == 2 {
		return sum.NewErr[map[int]int](sum.ValueErr(k2))
	}
	v2 := load("v2", 20, valueOK, trace)
	sum.ValidateResult(v2)
	if sum.TagResult(v2) == 2 {
		return sum.NewErr[map[int]int](sum.ValueErr(v2))
	}
	k3 := mark("k3", 3, trace)
	v3 := mark("v3", 30, trace)
	return sum.NewOk[map[int]int, string](map[int]int{k1: v1, sum.ValueOk(k2): sum.ValueOk(v2), k3: v3})
}

type Record struct{ Value int }

func nilPlace(value *Record, ok bool, trace *[]string) sum.Result[int, string] {
	rhs := load("rhs", 7, ok, trace)
	sum.ValidateResult(rhs)
	if sum.TagResult(rhs) == 2 {
		return sum.NewErr[int](sum.ValueErr(rhs))
	}
	value.Value = sum.ValueOk(rhs)
	return sum.NewOk[int, string](value.Value)
}

func places() sum.Option[int] {
	values := [1]int{1}
	index := sum.NewSome(0)
	if sum.TagOption(index) == 0 {
		return sum.None[int]()
	}
	ptr := &values[sum.ValueSome(index)]
	*ptr = 2
	value := sum.NewSome(values[0] + 3)
	if sum.TagOption(value) == 0 {
		return sum.None[int]()
	}
	values[0] = sum.ValueSome(value)
	firstValue := sum.NewSome(values[0])
	if sum.TagOption(firstValue) == 0 {
		return sum.None[int]()
	}
	first := sum.ValueSome(firstValue)
	secondValue := sum.NewSome(first + 1)
	if sum.TagOption(secondValue) == 0 {
		return sum.None[int]()
	}
	second := sum.ValueSome(secondValue)
	return sum.NewSome(second)
}
func optionInt(value sum.Option[int]) int {
	switch sum.TagOption(value) {
	case 1:
		return sum.ValueSome(value)
	case 0:
		return -1
	}
	panic("goxide: invalid enum Option")
}
func text(value sum.Result[int, string]) string {
	switch sum.TagResult(value) {
	case 1:
		return fmt.Sprint(sum.ValueOk(value))
	case 2:
		return sum.ValueErr(value)
	}
	panic("goxide: invalid enum Result")
}
func sliceText(value sum.Result[[]int, string]) string {
	switch sum.TagResult(value) {
	case 1:
		return fmt.Sprint(sum.ValueOk(value))
	case 2:
		return sum.ValueErr(value)
	}
	panic("goxide: invalid enum Result")
}
func mapText(value sum.Result[map[int]int, string]) string {
	switch sum.TagResult(value) {
	case 1:
		return fmt.Sprint(len(sum.ValueOk(value)))
	case 2:
		return sum.ValueErr(value)
	}
	panic("goxide: invalid enum Result")
}
func main() {
	for _, ok := range []bool{true, false} {
		trace := []string{}
		value := readArray(ok, &trace)
		fmt.Println(text(value), trace)
	}
	for _, ok := range []bool{true, false} {
		trace := []string{}
		value := sliceArray(ok, &trace)
		fmt.Println(sliceText(value), trace)
	}
	for _, mode := range [][2]bool{{false, true}, {true, false}, {true, true}} {
		trace := []string{}
		value := buildMap(mode[0], mode[1], &trace)
		fmt.Println(mapText(value), trace)
	}
	for _, ok := range []bool{false, true} {
		trace := []string{}
		result := ""
		func() {
			defer func() {
				if recover() != nil {
					result = "panic"
				}
			}()
			result = text(nilPlace(nil, ok, &trace))
		}()
		fmt.Println(result, trace)
	}
	fmt.Println(optionInt(places()))
}
