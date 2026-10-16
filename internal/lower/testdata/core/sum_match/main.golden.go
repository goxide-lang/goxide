package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"
import strings "strings"

func option(x sum.Option[int]) int {
	{
		var gxMatch1 int
		var gxMatch2 sum.Option[int] = x
		switch gxTag4 := sum.TagOption(gxMatch2); gxTag4 {
		case 1:
			if sum.ValueSome(gxMatch2) ==

				0 {
				gxMatch1 = 0
			} else if n := sum.ValueSome(gxMatch2); n > 0 {
				gxMatch1 = n + 1
			} else {
				n := sum.ValueSome(gxMatch2)
				gxMatch1 = n
			}
		case 0:
			gxMatch1 = -1
		default:
			panic("goxide: invalid enum Option")
		}
		return gxMatch1
	}

}

func result(x sum.Result[int, string]) string {
	{
		var gxMatch5 string
		switch gxTag7 := sum.TagResult(x); gxTag7 {
		case 1:
			if sum.ValueOk(x) ==
				0 {
				gxMatch5 = "0"
			} else {
				n := sum.ValueOk(x)
				gxMatch5 = fmt.Sprint(n)
			}
		case 2:
			e := sum.ValueErr(x)
			gxMatch5 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch5
	}

}

func invalid(x sum.Result[int, string], touched *bool) {
	switch gxTag9 := sum.TagResult(x); gxTag9 {
	default:
		if gxTag9 != 1 && gxTag9 != 2 {
			panic("goxide: invalid enum Result")
		}
		*touched = true
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
	fmt.Println(option(sum.None[int]()), option(sum.NewSome[int](0)), option(sum.NewSome[int](5)), option(sum.NewSome[int](-2)), result(sum.NewOk[int, string](0)), result(sum.NewOk[int, string](8)), result(sum.NewErr[int, string]("bad")), strings.Contains(message, "Result") && !touched)
}
