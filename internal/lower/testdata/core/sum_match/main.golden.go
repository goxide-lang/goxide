package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"
import strings "strings"

func option(x sum.Option[int]) int {
	{
		var goxideMatch1 int
		var goxidematch2 sum.Option[int] = x
		switch goxidetag4 := sum.TagOption(goxidematch2); goxidetag4 {
		case 1:
			if sum.ValueSome(goxidematch2) ==

				0 {
				goxideMatch1 = 0
			} else if n := sum.ValueSome(goxidematch2); n > 0 {
				goxideMatch1 = n + 1
			} else {
				n := sum.ValueSome(goxidematch2)
				goxideMatch1 = n
			}
		case 0:
			goxideMatch1 = -1
		default:
			panic("goxide: invalid enum Option")
		}
		return goxideMatch1
	}

}

func result(x sum.Result[int, string]) string {
	{
		var goxideMatch5 string
		var goxidematch6 sum.Result[int, string] = x
		switch goxidetag8 := sum.TagResult(goxidematch6); goxidetag8 {
		case 1:
			if sum.ValueOk(goxidematch6) ==

				0 {
				goxideMatch5 = "0"
			} else {
				n := sum.ValueOk(goxidematch6)
				goxideMatch5 = fmt.Sprint(n)
			}
		case 2:
			e := sum.ValueErr(goxidematch6)
			goxideMatch5 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch5
	}

}

func invalid(x sum.Result[int, string], touched *bool) {
	{
		var goxidematch9 sum.Result[int, string] = x
		switch goxidetag11 := sum.TagResult(goxidematch9); goxidetag11 {
		default:
			if goxidetag11 != 1 && goxidetag11 != 2 {
				panic("goxide: invalid enum Result")
			}

			*touched = true
		}
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
