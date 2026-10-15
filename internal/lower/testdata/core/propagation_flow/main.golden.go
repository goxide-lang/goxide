package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"

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
		var goxideFirst4 bool = true
		for i := 0; ; {
			if goxideFirst4 {
				goxideFirst4 = false
			} else {
				{
					var goxideTry5 sum.Result[int, string] = next(i, stop, trace)
					var goxideOk6 int
					switch sum.TagResult(goxideTry5) {
					case 1:
						goxideOk6 = sum.ValueOk(goxideTry5)
					case 2:
						return sum.NewErr[int, string](sum.ValueErr(goxideTry5))
					default:
						panic("goxide: invalid Result at ?")
					}

					i = goxideOk6
				}
			}
			var goxideTry2 sum.Result[bool, string] = less(i, trace)
			var goxideOk3 bool
			switch sum.TagResult(goxideTry2) {
			case 1:
				goxideOk3 = sum.ValueOk(goxideTry2)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(goxideTry2))
			default:
				panic("goxide: invalid Result at ?")
			}
			if !goxideOk3 {
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
		var goxideTry8 sum.Result[int, string] = load("tag", 2, true, trace)
		var goxideOk9 int
		switch sum.TagResult(goxideTry8) {
		case 1:
			goxideOk9 = sum.ValueOk(goxideTry8)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(goxideTry8))
		default:
			panic("goxide: invalid Result at ?")
		}
		var goxideSwitch10 int = goxideOk9
		var goxideCase11 int = -1
		if goxideCase11 < 0 {
			var goxideTry12 sum.Result[int, string] = load("case1", 1, true, trace)
			var goxideOk13 int
			switch sum.TagResult(goxideTry12) {
			case 1:
				goxideOk13 = sum.ValueOk(goxideTry12)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(goxideTry12))
			default:
				panic("goxide: invalid Result at ?")
			}
			if goxideSwitch10 == goxideOk13 {
				goxideCase11 = 0
			}
		}
		if goxideCase11 < 0 {
			var goxideTry14 sum.Result[int, string] = load("case2", 2, ok, trace)
			var goxideOk15 int
			switch sum.TagResult(goxideTry14) {
			case 1:
				goxideOk15 = sum.ValueOk(goxideTry14)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(goxideTry14))
			default:
				panic("goxide: invalid Result at ?")
			}
			if goxideSwitch10 == goxideOk15 {
				goxideCase11 = 1
			}
		}
		if goxideCase11 < 0 {
			var goxideTry16 sum.Result[int, string] = load("case3", 3, true, trace)
			var goxideOk17 int
			switch sum.TagResult(goxideTry16) {
			case 1:
				goxideOk17 = sum.ValueOk(goxideTry16)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(goxideTry16))
			default:
				panic("goxide: invalid Result at ?")
			}
			if goxideSwitch10 == goxideOk17 {
				goxideCase11 = 2
			}
		}
		switch goxideCase11 {
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
		var goxideValue18 []int = values
		var goxideValue19 int = index(trace)
		var goxideValue20 *int = out
		var goxideTry21 sum.Result[int, string] = load("rhs", 7, ok, trace)
		var goxideOk22 int
		switch sum.TagResult(goxideTry21) {
		case 1:
			goxideOk22 = sum.ValueOk(goxideTry21)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(goxideTry21))
		default:
			panic("goxide: invalid Result at ?")
		}
		goxideValue18[goxideValue19], *goxideValue20 = 9, goxideOk22
	}
	return sum.NewOk[int, string](*out)
}

type Wide uint64

func consume(value Wide, extra int) Wide { return value + Wide(extra) }
func wideValue(trace *[]string) sum.Result[Wide, string] {
	{
		var goxideTry23 sum.Result[int, string] = load("wide", 7, true, trace)
		var goxideOk24 int
		switch sum.TagResult(goxideTry23) {
		case 1:
			goxideOk24 = sum.ValueOk(goxideTry23)
		case 2:
			return sum.NewErr[Wide, string](sum.ValueErr(goxideTry23))
		default:
			panic("goxide: invalid Result at ?")
		}

		return sum.NewOk[Wide, string](consume(1<<63, goxideOk24))
	}
}

func bare(ok bool, trace *[]string) sum.Result[int, string] {
	{
		var goxideTry25 sum.Result[int, string] = load("bare", 1, ok, trace)
		var goxideOk26 int
		switch sum.TagResult(goxideTry25) {
		case 1:
			goxideOk26 = sum.ValueOk(goxideTry25)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(goxideTry25))
		default:
			panic("goxide: invalid Result at ?")
		}
		_ = goxideOk26
	}

	return sum.NewOk[int, string](1)
}

func forward(skip bool, trace *[]string) sum.Result[int, string] {
	if skip {
		goto done
	}
	{
		var goxideTry27 sum.Result[int, string] = load("goto", 1, true, trace)
		var goxideOk28 int
		switch sum.TagResult(goxideTry27) {
		case 1:
			goxideOk28 = sum.ValueOk(goxideTry27)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(goxideTry27))
		default:
			panic("goxide: invalid Result at ?")
		}
		_ = goxideOk28
	}

done:
	return sum.NewOk[int, string](7)
}

func noContinue(trace *[]string) sum.Result[int, string] {
	total := 0
	{
		var goxideFirst30 bool = true
		for i := 0; ; {
			if goxideFirst30 {
				goxideFirst30 = false
			} else {
				{
					var goxideTry31 sum.Result[int, string] = next(i, false, trace)
					var goxideOk32 int
					switch sum.TagResult(goxideTry31) {
					case 1:
						goxideOk32 = sum.ValueOk(goxideTry31)
					case 2:
						return sum.NewErr[int, string](sum.ValueErr(goxideTry31))
					default:
						panic("goxide: invalid Result at ?")
					}

					i = goxideOk32
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
		var goxideMatch33 string
		var goxidematch34 sum.Result[int, string] = value
		switch goxidetag36 := sum.TagResult(goxidematch34); goxidetag36 {
		case 1:

			n := sum.ValueOk(goxidematch34)
			goxideMatch33 = fmt.Sprint(n)
		case 2:
			e := sum.ValueErr(goxidematch34)
			goxideMatch33 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch33
	}
}
func wideText(value sum.Result[Wide, string]) string {
	{
		var goxideMatch37 string
		var goxidematch38 sum.Result[Wide, string] = value
		switch goxidetag40 := sum.TagResult(goxidematch38); goxidetag40 {
		case 1:

			n := sum.ValueOk(goxidematch38)
			goxideMatch37 = fmt.Sprint(uint64(n))
		case 2:
			e := sum.ValueErr(goxidematch38)
			goxideMatch37 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch37
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
