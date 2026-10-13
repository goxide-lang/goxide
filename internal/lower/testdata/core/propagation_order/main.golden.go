package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"

func load(ok bool, trace *[]string) sum.Result[int, string] {
	*trace = append(*trace, "load")
	if !ok {
		return sum.NewErr[int, string]("failed")
	}
	return sum.NewOk[int, string](4)
}

func mark(name string, trace *[]string) int {
	*trace = append(*trace, name)
	return 1
}

func callee(trace *[]string) func(int, int, int) int {
	*trace = append(*trace, "callee")
	return func(a, b, c int) int { *trace = append(*trace, "body"); return a + b + c }
}

func sequence(ok bool, trace *[]string) sum.Result[string, string] {
	defer func() { *trace = append(*trace, "cleanup") }()
	var goxideValue1 func(int, int, int) int = callee(trace)
	var goxideValue2 int = mark("first", trace)
	var goxideTry3 sum.Result[int, string] = load(ok, trace)
	var goxideOk4 int
	switch sum.TagResult(goxideTry3) {
	case 1:
		goxideOk4 = sum.ValueOk(goxideTry3)
	case 2:
		return sum.NewErr[string, string](sum.ValueErr(goxideTry3))
	default:
		panic("goxide: invalid Result at ?")
	}

	n := goxideValue1(goxideValue2, goxideOk4, mark("last", trace))
	defer func() { *trace = append(*trace, "late") }()
	return sum.NewOk[string, string](fmt.Sprint(n))
}

func text(value sum.Result[string, string]) string {
	{
		var goxideMatch5 string
		var goxidematch6 sum.Result[string, string] = value
		switch goxidetag8 := sum.TagResult(goxidematch6); goxidetag8 {
		case 1:

			s := sum.ValueOk(goxidematch6)
			goxideMatch5 = s
		case 2:
			e := sum.ValueErr(goxidematch6)
			goxideMatch5 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch5
	}
}

func main() {
	for _, ok := range []bool{false, true} {
		trace := []string{}
		var result sum.Result[string, string] = sequence(ok, &trace)
		fmt.Println(text(result), trace)
	}
}
