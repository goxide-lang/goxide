package main

import (
	"fmt"
	"github.com/goxide-lang/std/sum"
)

func load(name string, value int, ok bool, trace *[]string) sum.Result[int, string] {
	*trace = append(*trace, name)
	if !ok {
		return sum.NewErr[int]("stop")
	}
	return sum.NewOk[int, string](value)
}
func less(i int, trace *[]string) sum.Result[bool, string] {
	*trace = append(*trace, fmt.Sprintf("cond%d", i))
	return sum.NewOk[bool, string](i < 3)
}
func next(i int, stop bool, trace *[]string) sum.Result[int, string] {
	return load(fmt.Sprintf("post%d", i), i+1, !(stop && i == 1), trace)
}
func count(stop bool, trace *[]string) sum.Result[int, string] {
	total := 0
	for i := 0; ; {
		condition := less(i, trace)
		sum.ValidateResult(condition)
		if sum.TagResult(condition) == 2 {
			return sum.NewErr[int](sum.ValueErr(condition))
		}
		if !sum.ValueOk(condition) {
			break
		}
		*trace = append(*trace, fmt.Sprintf("body%d", i))
		if i != 1 {
			total += i
		}
		post := next(i, stop, trace)
		sum.ValidateResult(post)
		if sum.TagResult(post) == 2 {
			return sum.NewErr[int](sum.ValueErr(post))
		}
		i = sum.ValueOk(post)
	}
	return sum.NewOk[int, string](total)
}
func selected(ok bool, trace *[]string) sum.Result[int, string] {
	tag := load("tag", 2, true, trace)
	sum.ValidateResult(tag)
	if sum.TagResult(tag) == 2 {
		return sum.NewErr[int](sum.ValueErr(tag))
	}
	first := load("case1", 1, true, trace)
	sum.ValidateResult(first)
	if sum.TagResult(first) == 2 {
		return sum.NewErr[int](sum.ValueErr(first))
	}
	if sum.ValueOk(tag) == sum.ValueOk(first) {
		return sum.NewOk[int, string](10)
	}
	second := load("case2", 2, ok, trace)
	sum.ValidateResult(second)
	if sum.TagResult(second) == 2 {
		return sum.NewErr[int](sum.ValueErr(second))
	}
	if sum.ValueOk(tag) == sum.ValueOk(second) {
		*trace = append(*trace, "selected")
		return sum.NewOk[int, string](20)
	}
	third := load("case3", 3, true, trace)
	sum.ValidateResult(third)
	if sum.TagResult(third) == 2 {
		return sum.NewErr[int](sum.ValueErr(third))
	}
	if sum.ValueOk(tag) == sum.ValueOk(third) {
		return sum.NewOk[int, string](20)
	}
	return sum.NewOk[int, string](0)
}
func index(trace *[]string) int { *trace = append(*trace, "lhs"); return 0 }
func assignment(ok bool, values []int, out *int, trace *[]string) sum.Result[int, string] {
	place := &values[index(trace)]
	value := load("rhs", 7, ok, trace)
	sum.ValidateResult(value)
	if sum.TagResult(value) == 2 {
		return sum.NewErr[int](sum.ValueErr(value))
	}
	*place, *out = 9, sum.ValueOk(value)
	return sum.NewOk[int, string](*out)
}

type Wide uint64

func consume(value Wide, extra int) Wide { return value + Wide(extra) }
func wideValue(trace *[]string) sum.Result[Wide, string] {
	value := load("wide", 7, true, trace)
	sum.ValidateResult(value)
	if sum.TagResult(value) == 2 {
		return sum.NewErr[Wide](sum.ValueErr(value))
	}
	return sum.NewOk[Wide, string](consume(1<<63, sum.ValueOk(value)))
}

func bare(ok bool, trace *[]string) sum.Result[int, string] {
	value := load("bare", 1, ok, trace)
	sum.ValidateResult(value)
	if sum.TagResult(value) == 2 {
		return sum.NewErr[int](sum.ValueErr(value))
	}
	return sum.NewOk[int, string](1)
}
func forward(skip bool, trace *[]string) sum.Result[int, string] {
	if skip {
		goto done
	}
	{
		value := load("goto", 1, true, trace)
		sum.ValidateResult(value)
		if sum.TagResult(value) == 2 {
			return sum.NewErr[int](sum.ValueErr(value))
		}
	}
done:
	return sum.NewOk[int, string](7)
}
func noContinue(trace *[]string) sum.Result[int, string] {
	total := 0
	for i := 0; i < 2; {
		total += i
		value := next(i, false, trace)
		sum.ValidateResult(value)
		if sum.TagResult(value) == 2 {
			return sum.NewErr[int](sum.ValueErr(value))
		}
		i = sum.ValueOk(value)
	}
	return sum.NewOk[int, string](total)
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
func wideText(value sum.Result[Wide, string]) string {
	switch sum.TagResult(value) {
	case 1:
		return fmt.Sprint(uint64(sum.ValueOk(value)))
	case 2:
		return sum.ValueErr(value)
	}
	panic("goxide: invalid enum Result")
}
func main() {
	for _, stop := range []bool{false, true} {
		trace := []string{}
		result := count(stop, &trace)
		fmt.Println(text(result), trace)
	}
	for _, ok := range []bool{false, true} {
		trace := []string{}
		result := selected(ok, &trace)
		fmt.Println(text(result), trace)
	}
	for _, ok := range []bool{false, true} {
		trace := []string{}
		values := []int{10}
		out := 100
		result := assignment(ok, values, &out, &trace)
		fmt.Println(text(result), values, out, trace)
	}
	trace := []string{}
	value := wideValue(&trace)
	fmt.Println(wideText(value), trace)
	for _, ok := range []bool{false, true} {
		trace := []string{}
		value := bare(ok, &trace)
		fmt.Println(text(value), trace)
	}
	for _, skip := range []bool{true, false} {
		trace := []string{}
		value := forward(skip, &trace)
		fmt.Println(text(value), trace)
	}
	trace = []string{}
	result := noContinue(&trace)
	fmt.Println(text(result), trace)
}
