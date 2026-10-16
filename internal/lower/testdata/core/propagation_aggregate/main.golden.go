package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"

func mark(name string, value int, trace *[]string) int {
	*trace = append(*trace, name)
	return value
}

func load(name string, value int, ok bool, trace *[]string) sum.Result[int, string] {
	*trace = append(*trace, name)
	if !ok {
		return sum.NewErr[int, string]("stop")
	}
	return sum.NewOk[int, string](value)
}

func mutate(values *[2]int, ok bool, trace *[]string) sum.Result[int, string] {
	values[1] = 99
	return load("index", 1, ok, trace)
}

func readArray(ok bool, trace *[]string) sum.Result[int, string] {
	values := [2]int{10, 20}
	{
		var gxArray1 *[2]int = &values
		var gxTry2 sum.Result[int, string] = mutate(&values, ok, trace)
		var gxOk3 int
		switch sum.TagResult(gxTry2) {
		case 1:
			gxOk3 = sum.ValueOk(gxTry2)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(gxTry2))
		default:
			panic("goxide: invalid Result at ?")
		}

		return sum.NewOk[int, string](gxArray1[gxOk3])
	}
}

func sliceArray(ok bool, trace *[]string) sum.Result[[]int, string] {
	values := [3]int{1, 2, 3}
	var gxArray4 *[3]int = &values
	var gxTry5 sum.Result[int, string] = load("low", 1, ok, trace)
	var gxOk6 int
	switch sum.TagResult(gxTry5) {
	case 1:
		gxOk6 = sum.ValueOk(gxTry5)
	case 2:
		return sum.NewErr[[]int, string](sum.ValueErr(gxTry5))
	default:
		panic("goxide: invalid Result at ?")
	}

	part := gxArray4[gxOk6:]
	part[0] = 9
	return sum.NewOk[[]int, string]([]int{values[1], part[0]})
}

func buildMap(keyOK, valueOK bool, trace *[]string) sum.Result[map[int]int, string] {
	var gxValue7 int = mark("k1", 1, trace)
	var gxValue8 int = mark("v1", 10, trace)
	var gxTry9 sum.Result[int, string] = load("k2", 2, keyOK, trace)
	var gxOk10 int
	switch sum.TagResult(gxTry9) {
	case 1:
		gxOk10 = sum.ValueOk(gxTry9)
	case 2:
		return sum.NewErr[map[int]int, string](sum.ValueErr(gxTry9))
	default:
		panic("goxide: invalid Result at ?")
	}
	var gxValue11 int = gxOk10
	var gxTry12 sum.Result[int, string] = load("v2", 20, valueOK, trace)
	var gxOk13 int
	switch sum.TagResult(gxTry12) {
	case 1:
		gxOk13 = sum.ValueOk(gxTry12)
	case 2:
		return sum.NewErr[map[int]int, string](sum.ValueErr(gxTry12))
	default:
		panic("goxide: invalid Result at ?")
	}

	value := map[int]int{gxValue7: gxValue8, gxValue11: gxOk13, mark("k3", 3, trace): mark("v3", 30, trace)}
	return sum.NewOk[map[int]int, string](value)
}

type Record struct{ Value int }

func nilPlace(value *Record, ok bool, trace *[]string) sum.Result[int, string] {
	{
		var gxValue14 *Record = value
		var gxTry15 sum.Result[int, string] = load("rhs", 7, ok, trace)
		var gxOk16 int
		switch sum.TagResult(gxTry15) {
		case 1:
			gxOk16 = sum.ValueOk(gxTry15)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(gxTry15))
		default:
			panic("goxide: invalid Result at ?")
		}
		gxValue14.
			Value = gxOk16
	}
	return sum.NewOk[int, string](value.Value)
}

func places() sum.Option[int] {
	values := [1]int{1}
	var gxTry17 sum.Option[int] = sum.NewSome[int](0)
	var gxOk18 int
	switch sum.TagOption(gxTry17) {
	case 1:
		gxOk18 = sum.ValueSome(gxTry17)
	case 0:
		return sum.None[int]()
	default:
		panic("goxide: invalid Option at ?")
	}
	var gxValue19 int = gxOk18

	ptr := &values[gxValue19]
	*ptr = 2
	{
		var gxTry20 sum.Option[int] = sum.NewSome[int](values[0] + 3)
		var gxOk21 int
		switch sum.TagOption(gxTry20) {
		case 1:
			gxOk21 = sum.ValueSome(gxTry20)
		case 0:
			return sum.None[int]()
		default:
			panic("goxide: invalid Option at ?")
		}

		values[0] = gxOk21
	}
	var gxTry22 sum.Option[int] = sum.NewSome[int](values[0])
	var gxOk23 int
	switch sum.TagOption(gxTry22) {
	case 1:
		gxOk23 = sum.ValueSome(gxTry22)
	case 0:
		return sum.None[int]()
	default:
		panic("goxide: invalid Option at ?")
	}
	var first int = gxOk23
	var gxTry24 sum.Option[int] = sum.NewSome[int](first + 1)
	var gxOk25 int
	switch sum.TagOption(gxTry24) {
	case 1:
		gxOk25 = sum.ValueSome(gxTry24)
	case 0:
		return sum.None[int]()
	default:
		panic("goxide: invalid Option at ?")
	}
	var second int = gxOk25

	return sum.NewSome[int](second)
}

func optionInt(value sum.Option[int]) int {
	{
		var gxMatch26 int
		switch gxTag28 := sum.TagOption(value); gxTag28 {
		case 1:
			n := sum.ValueSome(value)
			gxMatch26 = n
		case 0:
			gxMatch26 = -1
		default:
			panic("goxide: invalid enum Option")
		}
		return gxMatch26
	}
}

func text(value sum.Result[int, string]) string {
	{
		var gxMatch29 string
		switch gxTag31 := sum.TagResult(value); gxTag31 {
		case 1:
			n := sum.ValueOk(value)
			gxMatch29 = fmt.Sprint(n)
		case 2:
			e := sum.ValueErr(value)
			gxMatch29 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch29
	}
}

func sliceText(value sum.Result[[]int, string]) string {
	{
		var gxMatch32 string
		switch gxTag34 := sum.TagResult(value); gxTag34 {
		case 1:
			items := sum.ValueOk(value)
			gxMatch32 = fmt.Sprint(items)
		case 2:
			e := sum.ValueErr(value)
			gxMatch32 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch32
	}
}

func mapText(value sum.Result[map[int]int, string]) string {
	{
		var gxMatch35 string
		switch gxTag37 := sum.TagResult(value); gxTag37 {
		case 1:
			items := sum.ValueOk(value)
			gxMatch35 = fmt.Sprint(len(items))
		case 2:
			e := sum.ValueErr(value)
			gxMatch35 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch35
	}
}

func main() {
	for _, ok := range []bool{true, false} {
		trace := []string{}
		var value sum.Result[int, string] = readArray(ok, &trace)
		fmt.Println(text(value), trace)
	}
	for _, ok := range []bool{true, false} {
		trace := []string{}
		var value sum.Result[[]int, string] = sliceArray(ok, &trace)
		fmt.Println(sliceText(value), trace)
	}
	for _, mode := range [][2]bool{[2]bool{false, true}, [2]bool{true, false}, [2]bool{true, true}} {
		trace := []string{}
		var value sum.Result[map[int]int, string] = buildMap(mode[0], mode[1], &trace)
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
