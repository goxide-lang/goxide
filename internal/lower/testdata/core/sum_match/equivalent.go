package main

import (
	"fmt"
	"github.com/goxide-lang/std/sum"
	"strings"
)

func option(x sum.Option[int]) int {
	switch sum.TagOption(x) {
	case 1:
		n := sum.ValueSome(x)
		if n == 0 {
			return 0
		}
		if n > 0 {
			return n + 1
		}
		return n
	case 0:
		return -1
	}
	panic("goxide: invalid enum Option")
}

func result(x sum.Result[int, string]) string {
	switch sum.TagResult(x) {
	case 1:
		n := sum.ValueOk(x)
		if n == 0 {
			return "0"
		}
		return fmt.Sprint(n)
	case 2:
		return sum.ValueErr(x)
	}
	panic("goxide: invalid enum Result")
}

func invalid(x sum.Result[int, string], touched *bool) {
	switch sum.TagResult(x) {
	case 1, 2:
		*touched = true
	default:
		panic("goxide: invalid enum Result")
	}
}

func main() {
	touched := false
	message := ""
	func() {
		defer func() { message = fmt.Sprint(recover()) }()
		var value sum.Result[int, string]
		invalid(value, &touched)
	}()
	fmt.Println(option(sum.None[int]()), option(sum.NewSome(0)), option(sum.NewSome(5)), option(sum.NewSome(-2)), result(sum.NewOk[int, string](0)), result(sum.NewOk[int, string](8)), result(sum.NewErr[int]("bad")), strings.Contains(message, "Result") && !touched)
}
