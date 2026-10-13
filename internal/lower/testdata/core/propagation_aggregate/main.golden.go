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
		var goxideArray1 *[2]int = &values
		var goxideTry2 sum.Result[int, string] = mutate(&values, ok, trace)
		var goxideOk3 int
		switch sum.TagResult(goxideTry2) {
		case 1:
			goxideOk3 = sum.ValueOk(goxideTry2)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(goxideTry2))
		default:
			panic("goxide: invalid Result at ?")
		}

		return sum.NewOk[int, string](goxideArray1[goxideOk3])
	}
}

func sliceArray(ok bool, trace *[]string) sum.Result[[]int, string] {
	values := [3]int{1, 2, 3}
	var goxideArray4 *[3]int = &values
	var goxideTry5 sum.Result[int, string] = load("low", 1, ok, trace)
	var goxideOk6 int
	switch sum.TagResult(goxideTry5) {
	case 1:
		goxideOk6 = sum.ValueOk(goxideTry5)
	case 2:
		return sum.NewErr[[]int, string](sum.ValueErr(goxideTry5))
	default:
		panic("goxide: invalid Result at ?")
	}

	part := goxideArray4[goxideOk6:]
	part[0] = 9
	return sum.NewOk[[]int, string]([]int{values[1], part[0]})
}

func buildMap(keyOK, valueOK bool, trace *[]string) sum.Result[map[int]int, string] {
	var goxideValue7 int = mark("k1", 1, trace)
	var goxideValue8 int = mark("v1", 10, trace)
	var goxideTry9 sum.Result[int, string] = load("k2", 2, keyOK, trace)
	var goxideOk10 int
	switch sum.TagResult(goxideTry9) {
	case 1:
		goxideOk10 = sum.ValueOk(goxideTry9)
	case 2:
		return sum.NewErr[map[int]int, string](sum.ValueErr(goxideTry9))
	default:
		panic("goxide: invalid Result at ?")
	}
	var goxideValue11 int = goxideOk10
	var goxideTry12 sum.Result[int, string] = load("v2", 20, valueOK, trace)
	var goxideOk13 int
	switch sum.TagResult(goxideTry12) {
	case 1:
		goxideOk13 = sum.ValueOk(goxideTry12)
	case 2:
		return sum.NewErr[map[int]int, string](sum.ValueErr(goxideTry12))
	default:
		panic("goxide: invalid Result at ?")
	}

	value := map[int]int{goxideValue7: goxideValue8, goxideValue11: goxideOk13, mark("k3", 3, trace): mark("v3", 30, trace)}
	return sum.NewOk[map[int]int, string](value)
}

type Record struct{ Value int }

func nilPlace(value *Record, ok bool, trace *[]string) sum.Result[int, string] {
	{
		var goxideValue14 *Record = value
		var goxideTry15 sum.Result[int, string] = load("rhs", 7, ok, trace)
		var goxideOk16 int
		switch sum.TagResult(goxideTry15) {
		case 1:
			goxideOk16 = sum.ValueOk(goxideTry15)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(goxideTry15))
		default:
			panic("goxide: invalid Result at ?")
		}
		goxideValue14.
			Value = goxideOk16
	}
	return sum.NewOk[int, string](value.Value)
}

func places() sum.Option[int] {
	values := [1]int{1}
	var goxideArray17 *[1]int = &values
	var goxideTry18 sum.Option[int] = sum.NewSome[int](0)
	var goxideOk19 int
	switch sum.TagOption(goxideTry18) {
	case 1:
		goxideOk19 = sum.ValueSome(goxideTry18)
	case 0:
		return sum.None[int]()
	default:
		panic("goxide: invalid Option at ?")
	}
	var goxideValue20 int = goxideOk19

	ptr := &goxideArray17[goxideValue20]
	*ptr = 2
	{
		var goxideArray21 *[1]int = &values
		var goxideTry22 sum.Option[int] = sum.NewSome[int](values[0] + 3)
		var goxideOk23 int
		switch sum.TagOption(goxideTry22) {
		case 1:
			goxideOk23 = sum.ValueSome(goxideTry22)
		case 0:
			return sum.None[int]()
		default:
			panic("goxide: invalid Option at ?")
		}
		goxideArray21[0] = goxideOk23
	}
	var goxideTry24 sum.Option[int] = sum.NewSome[int](values[0])
	var goxideOk25 int
	switch sum.TagOption(goxideTry24) {
	case 1:
		goxideOk25 = sum.ValueSome(goxideTry24)
	case 0:
		return sum.None[int]()
	default:
		panic("goxide: invalid Option at ?")
	}
	var first int = goxideOk25
	var goxideTry26 sum.Option[int] = sum.NewSome[int](first + 1)
	var goxideOk27 int
	switch sum.TagOption(goxideTry26) {
	case 1:
		goxideOk27 = sum.ValueSome(goxideTry26)
	case 0:
		return sum.None[int]()
	default:
		panic("goxide: invalid Option at ?")
	}
	var second int = goxideOk27

	return sum.NewSome[int](second)
}

func optionInt(value sum.Option[int]) int {
	{
		var goxideMatch28 int
		var goxidematch29 sum.Option[int] = value
		switch goxidetag31 := sum.TagOption(goxidematch29); goxidetag31 {
		case 1:

			n := sum.ValueSome(goxidematch29)
			goxideMatch28 = n
		case 0:
			goxideMatch28 = -1
		default:
			panic("goxide: invalid enum Option")
		}
		return goxideMatch28
	}
}

func text(value sum.Result[int, string]) string {
	{
		var goxideMatch32 string
		var goxidematch33 sum.Result[int, string] = value
		switch goxidetag35 := sum.TagResult(goxidematch33); goxidetag35 {
		case 1:

			n := sum.ValueOk(goxidematch33)
			goxideMatch32 = fmt.Sprint(n)
		case 2:
			e := sum.ValueErr(goxidematch33)
			goxideMatch32 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch32
	}
}

func sliceText(value sum.Result[[]int, string]) string {
	{
		var goxideMatch36 string
		var goxidematch37 sum.Result[[]int, string] = value
		switch goxidetag39 := sum.TagResult(goxidematch37); goxidetag39 {
		case 1:

			items := sum.ValueOk(goxidematch37)
			goxideMatch36 = fmt.Sprint(items)
		case 2:
			e := sum.ValueErr(goxidematch37)
			goxideMatch36 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch36
	}
}

func mapText(value sum.Result[map[int]int, string]) string {
	{
		var goxideMatch40 string
		var goxidematch41 sum.Result[map[int]int, string] = value
		switch goxidetag43 := sum.TagResult(goxidematch41); goxidetag43 {
		case 1:

			items := sum.ValueOk(goxidematch41)
			goxideMatch40 = fmt.Sprint(len(items))
		case 2:
			e := sum.ValueErr(goxidematch41)
			goxideMatch40 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch40
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
	for _, mode := range [][2]bool{{false, true}, {true, false}, {true, true}} {
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
