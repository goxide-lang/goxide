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
	var gxValue1 func(int, int, int) int = callee(trace)
	var gxValue2 int = mark("first", trace)
	var gxTry3 sum.Result[int, string] = load(ok, trace)
	var gxOk4 int
	switch sum.TagResult(gxTry3) {
	case 1:
		gxOk4 = sum.ValueOk(gxTry3)
	case 2:
		return sum.NewErr[string, string](sum.ValueErr(gxTry3))
	default:
		panic("goxide: invalid Result at ?")
	}

	n := gxValue1(gxValue2, gxOk4, mark("last", trace))
	defer func() { *trace = append(*trace, "late") }()
	return sum.NewOk[string, string](fmt.Sprint(n))
}

func text(value sum.Result[string, string]) string {
	{
		var gxMatch5 string
		switch gxTag7 := sum.TagResult(value); gxTag7 {
		case 1:
			s := sum.ValueOk(value)
			gxMatch5 = s
		case 2:
			e := sum.ValueErr(value)
			gxMatch5 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch5
	}
}

func main() {
	for _, ok := range []bool{false, true} {
		trace := []string{}
		var result sum.Result[string, string] = sequence(ok, &trace)
		fmt.Println(text(result), trace)
	}
}
