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

	ptr := &goxideArray17[goxideOk19]
	*ptr = 2
	{
		var goxideArray20 *[1]int = &values
		var goxideTry21 sum.Option[int] = sum.NewSome[int](values[0] + 3)
		var goxideOk22 int
		switch sum.TagOption(goxideTry21) {
		case 1:
			goxideOk22 = sum.ValueSome(goxideTry21)
		case 0:
			return sum.None[int]()
		default:
			panic("goxide: invalid Option at ?")
		}
		goxideArray20[0] = goxideOk22
	}
	var goxideTry23 sum.Option[int] = sum.NewSome[int](values[0])
	var goxideOk24 int
	switch sum.TagOption(goxideTry23) {
	case 1:
		goxideOk24 = sum.ValueSome(goxideTry23)
	case 0:
		return sum.None[int]()
	default:
		panic("goxide: invalid Option at ?")
	}
	var first int = goxideOk24
	var goxideTry25 sum.Option[int] = sum.NewSome[int](first + 1)
	var goxideOk26 int
	switch sum.TagOption(goxideTry25) {
	case 1:
		goxideOk26 = sum.ValueSome(goxideTry25)
	case 0:
		return sum.None[int]()
	default:
		panic("goxide: invalid Option at ?")
	}
	var second int = goxideOk26

	return sum.NewSome[int](second)
}

func optionInt(value sum.Option[int]) int {
	{
		var goxideMatch27 int
		var goxidematch28 sum.Option[int] = value
		switch goxidetag30 := sum.TagOption(goxidematch28); goxidetag30 {
		case 1:

			n := sum.ValueSome(goxidematch28)
			goxideMatch27 = n
		case 0:
			goxideMatch27 = -1
		default:
			panic("goxide: invalid enum Option")
		}
		return goxideMatch27
	}
}

func text(value sum.Result[int, string]) string {
	{
		var goxideMatch31 string
		var goxidematch32 sum.Result[int, string] = value
		switch goxidetag34 := sum.TagResult(goxidematch32); goxidetag34 {
		case 1:

			n := sum.ValueOk(goxidematch32)
			goxideMatch31 = fmt.Sprint(n)
		case 2:
			e := sum.ValueErr(goxidematch32)
			goxideMatch31 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch31
	}
}

func sliceText(value sum.Result[[]int, string]) string {
	{
		var goxideMatch35 string
		var goxidematch36 sum.Result[[]int, string] = value
		switch goxidetag38 := sum.TagResult(goxidematch36); goxidetag38 {
		case 1:

			items := sum.ValueOk(goxidematch36)
			goxideMatch35 = fmt.Sprint(items)
		case 2:
			e := sum.ValueErr(goxidematch36)
			goxideMatch35 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch35
	}
}

func mapText(value sum.Result[map[int]int, string]) string {
	{
		var goxideMatch39 string
		var goxidematch40 sum.Result[map[int]int, string] = value
		switch goxidetag42 := sum.TagResult(goxidematch40); goxidetag42 {
		case 1:

			items := sum.ValueOk(goxidematch40)
			goxideMatch39 = fmt.Sprint(len(items))
		case 2:
			e := sum.ValueErr(goxidematch40)
			goxideMatch39 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch39
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
