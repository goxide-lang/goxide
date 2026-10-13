package main

import (
	"fmt"
	"github.com/goxide-lang/std/sum"
)

func load(ok bool, trace *[]string) sum.Result[int, string] {
	*trace = append(*trace, "load")
	if !ok {
		return sum.NewErr[int]("failed")
	}
	return sum.NewOk[int, string](4)
}

func mark(name string, trace *[]string) int { *trace = append(*trace, name); return 1 }
func callee(trace *[]string) func(int, int, int) int {
	*trace = append(*trace, "callee")
	return func(a, b, c int) int { *trace = append(*trace, "body"); return a + b + c }
}

func sequence(ok bool, trace *[]string) sum.Result[string, string] {
	defer func() { *trace = append(*trace, "cleanup") }()
	call := callee(trace)
	first := mark("first", trace)
	value := load(ok, trace)
	sum.ValidateResult(value)
	if sum.TagResult(value) == 2 {
		return sum.NewErr[string](sum.ValueErr(value))
	}
	n := call(first, sum.ValueOk(value), mark("last", trace))
	defer func() { *trace = append(*trace, "late") }()
	return sum.NewOk[string, string](fmt.Sprint(n))
}

func text(value sum.Result[string, string]) string {
	switch sum.TagResult(value) {
	case 1:
		return sum.ValueOk(value)
	case 2:
		return sum.ValueErr(value)
	}
	panic("goxide: invalid enum Result")
}

func main() {
	for _, ok := range []bool{false, true} {
		trace := []string{}
		result := sequence(ok, &trace)
		fmt.Println(text(result), trace)
	}
}
