package main

import fmt "fmt"
import sum "github.com/goxide-lang/std/sum"

func load(name string, value int, ok bool, trace *[]string) sum.Result[int, string] {
	*trace = append(*trace, name)
	if !ok {
		return sum.NewErr[int, string]("stop")
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
	{
		var gxFirst4 bool = true
		for i := 0; ; {
			if gxFirst4 {
				gxFirst4 = false
			} else {
				{
					var gxTry5 sum.Result[int, string] = next(i, stop, trace)
					var gxOk6 int
					switch sum.TagResult(gxTry5) {
					case 1:
						gxOk6 = sum.ValueOk(gxTry5)
					case 2:
						return sum.NewErr[int, string](sum.ValueErr(gxTry5))
					default:
						panic("goxide: invalid Result at ?")
					}

					i = gxOk6
				}
			}
			var gxTry2 sum.Result[bool, string] = less(i, trace)
			var gxOk3 bool
			switch sum.TagResult(gxTry2) {
			case 1:
				gxOk3 = sum.ValueOk(gxTry2)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(gxTry2))
			default:
				panic("goxide: invalid Result at ?")
			}
			if !gxOk3 {
				break
			}

			*trace = append(*trace, fmt.Sprintf("body%d", i))
			if i == 1 {
				continue
			}
			total += i
		}
	}
	return sum.NewOk[int, string](total)
}

func selected(ok bool, trace *[]string) sum.Result[int, string] {
	{
		var gxTry8 sum.Result[int, string] = load("tag", 2, true, trace)
		var gxOk9 int
		switch sum.TagResult(gxTry8) {
		case 1:
			gxOk9 = sum.ValueOk(gxTry8)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(gxTry8))
		default:
			panic("goxide: invalid Result at ?")
		}
		var gxSwitch10 int = gxOk9
		var gxCase11 int = -1
		if gxCase11 < 0 {
			var gxTry12 sum.Result[int, string] = load("case1", 1, true, trace)
			var gxOk13 int
			switch sum.TagResult(gxTry12) {
			case 1:
				gxOk13 = sum.ValueOk(gxTry12)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(gxTry12))
			default:
				panic("goxide: invalid Result at ?")
			}
			if gxSwitch10 == gxOk13 {
				gxCase11 = 0
			}
		}
		if gxCase11 < 0 {
			var gxTry14 sum.Result[int, string] = load("case2", 2, ok, trace)
			var gxOk15 int
			switch sum.TagResult(gxTry14) {
			case 1:
				gxOk15 = sum.ValueOk(gxTry14)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(gxTry14))
			default:
				panic("goxide: invalid Result at ?")
			}
			if gxSwitch10 == gxOk15 {
				gxCase11 = 1
			}
		}
		if gxCase11 < 0 {
			var gxTry16 sum.Result[int, string] = load("case3", 3, true, trace)
			var gxOk17 int
			switch sum.TagResult(gxTry16) {
			case 1:
				gxOk17 = sum.ValueOk(gxTry16)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(gxTry16))
			default:
				panic("goxide: invalid Result at ?")
			}
			if gxSwitch10 == gxOk17 {
				gxCase11 = 2
			}
		}
		switch gxCase11 {
		case 0:

			return sum.NewOk[int, string](10)
		case 1:

			*trace = append(*trace, "selected")
			fallthrough
		case 2:

			return sum.NewOk[int, string](20)
		default:

			return sum.NewOk[int, string](0)
		}
	}

}

func index(trace *[]string) int { *trace = append(*trace, "lhs"); return 0 }

func assignment(ok bool, values []int, out *int, trace *[]string) sum.Result[int, string] {
	{
		var gxValue18 []int = values
		var gxValue19 int = index(trace)
		var gxValue20 *int = out
		var gxTry21 sum.Result[int, string] = load("rhs", 7, ok, trace)
		var gxOk22 int
		switch sum.TagResult(gxTry21) {
		case 1:
			gxOk22 = sum.ValueOk(gxTry21)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(gxTry21))
		default:
			panic("goxide: invalid Result at ?")
		}
		gxValue18[gxValue19], *gxValue20 = 9, gxOk22
	}
	return sum.NewOk[int, string](*out)
}

type Wide uint64

func consume(value Wide, extra int) Wide { return value + Wide(extra) }
func wideValue(trace *[]string) sum.Result[Wide, string] {
	{
		var gxTry23 sum.Result[int, string] = load("wide", 7, true, trace)
		var gxOk24 int
		switch sum.TagResult(gxTry23) {
		case 1:
			gxOk24 = sum.ValueOk(gxTry23)
		case 2:
			return sum.NewErr[Wide, string](sum.ValueErr(gxTry23))
		default:
			panic("goxide: invalid Result at ?")
		}

		return sum.NewOk[Wide, string](consume(1<<63, gxOk24))
	}
}

func bare(ok bool, trace *[]string) sum.Result[int, string] {
	{
		var gxTry25 sum.Result[int, string] = load("bare", 1, ok, trace)
		var gxOk26 int
		switch sum.TagResult(gxTry25) {
		case 1:
			gxOk26 = sum.ValueOk(gxTry25)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(gxTry25))
		default:
			panic("goxide: invalid Result at ?")
		}
		_ = gxOk26
	}

	return sum.NewOk[int, string](1)
}

func forward(skip bool, trace *[]string) sum.Result[int, string] {
	if skip {
		goto done
	}
	{
		var gxTry27 sum.Result[int, string] = load("goto", 1, true, trace)
		var gxOk28 int
		switch sum.TagResult(gxTry27) {
		case 1:
			gxOk28 = sum.ValueOk(gxTry27)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(gxTry27))
		default:
			panic("goxide: invalid Result at ?")
		}
		_ = gxOk28
	}

done:
	return sum.NewOk[int, string](7)
}

func noContinue(trace *[]string) sum.Result[int, string] {
	total := 0
	{
		var gxFirst30 bool = true
		for i := 0; ; {
			if gxFirst30 {
				gxFirst30 = false
			} else {
				{
					var gxTry31 sum.Result[int, string] = next(i, false, trace)
					var gxOk32 int
					switch sum.TagResult(gxTry31) {
					case 1:
						gxOk32 = sum.ValueOk(gxTry31)
					case 2:
						return sum.NewErr[int, string](sum.ValueErr(gxTry31))
					default:
						panic("goxide: invalid Result at ?")
					}

					i = gxOk32
				}
			}
			if !(i < 2) {
				break
			}
			total += i
		}
	}
	return sum.NewOk[int, string](total)
}

func text(value sum.Result[int, string]) string {
	{
		var gxMatch33 string
		switch gxTag35 := sum.TagResult(value); gxTag35 {
		case 1:
			n := sum.ValueOk(value)
			gxMatch33 = fmt.Sprint(n)
		case 2:
			e := sum.ValueErr(value)
			gxMatch33 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch33
	}
}
func wideText(value sum.Result[Wide, string]) string {
	{
		var gxMatch36 string
		switch gxTag38 := sum.TagResult(value); gxTag38 {
		case 1:
			n := sum.ValueOk(value)
			gxMatch36 = fmt.Sprint(uint64(n))
		case 2:
			e := sum.ValueErr(value)
			gxMatch36 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch36
	}
}

func main() {
	for _, stop := range []bool{false, true} {
		trace := []string{}
		var result sum.Result[int, string] = count(stop, &trace)
		fmt.Println(text(result), trace)
	}
	for _, ok := range []bool{false, true} {
		trace := []string{}
		var result sum.Result[int, string] = selected(ok, &trace)
		fmt.Println(text(result), trace)
	}
	for _, ok := range []bool{false, true} {
		trace := []string{}
		values := []int{10}
		out := 100
		var result sum.Result[int, string] = assignment(ok, values, &out, &trace)
		fmt.Println(text(result), values, out, trace)
	}
	trace := []string{}
	var value sum.Result[Wide, string] = wideValue(&trace)
	fmt.Println(wideText(value), trace)
	for _, ok := range []bool{false, true} {
		trace := []string{}
		var value sum.Result[int, string] = bare(ok, &trace)
		fmt.Println(text(value), trace)
	}
	for _, skip := range []bool{true, false} {
		trace := []string{}
		var value sum.Result[int, string] = forward(skip, &trace)
		fmt.Println(text(value), trace)
	}
	trace = []string{}
	var result sum.Result[int, string] = noContinue(&trace)
	fmt.Println(text(result), trace)
}
