package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"

type Gate interface {
	isGate()
}
type Enter struct {
	Value int
}

func (*Enter) isGate() {
}

type Shut struct {
}

func (*Shut) isGate() {
}

func load(ok bool, trace *[]string) sum.Result[int, string] {
	*trace = append(*trace, "load")
	if !ok {
		return sum.NewErr[int, string]("failed")
	}
	return sum.NewOk[int, string](4)
}

func truth(trace *[]string) sum.Result[bool, string] {
	var goxideTry1 sum.Result[int, string] = load(false, trace)
	var goxideOk2 int
	switch sum.TagResult(goxideTry1) {
	case 1:
		goxideOk2 = sum.ValueOk(goxideTry1)
	case 2:
		return sum.NewErr[bool, string](sum.ValueErr(goxideTry1))
	default:
		panic("goxide: invalid Result at ?")
	}

	n := goxideOk2
	return sum.NewOk[bool, string](n > 0)
}

func shortCircuit(left bool, trace *[]string) sum.Result[string, string] {
	var goxideBool3 bool = left
	if goxideBool3 {
		var goxideTry4 sum.Result[bool, string] = truth(trace)
		var goxideOk5 bool
		switch sum.TagResult(goxideTry4) {
		case 1:
			goxideOk5 = sum.ValueOk(goxideTry4)
		case 2:
			return sum.NewErr[string, string](sum.ValueErr(goxideTry4))
		default:
			panic("goxide: invalid Result at ?")
		}
		goxideBool3 = goxideOk5
	}

	value := goxideBool3
	return sum.NewOk[string, string](fmt.Sprint(value))
}

func closure(trace *[]string) sum.Result[string, string] {
	inner := func() sum.Result[int, string] {
		defer func() { *trace = append(*trace, "inner-cleanup") }()
		{
			var goxideTry6 sum.Result[int, string] = load(false, trace)
			var goxideOk7 int
			switch sum.TagResult(goxideTry6) {
			case 1:
				goxideOk7 = sum.ValueOk(goxideTry6)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(goxideTry6))
			default:
				panic("goxide: invalid Result at ?")
			}

			return sum.NewOk[int, string](goxideOk7)
		}

	}
	_ = inner()
	*trace = append(*trace, "outer")
	return sum.NewOk[string, string]("outer")
}

func deferred(ok bool, trace *[]string) sum.Result[string, string] {
	defer func() { *trace = append(*trace, "cleanup") }()
	{
		var goxideValue8 func(n int) = func(n int) { *trace = append(*trace, "deferred") }
		var goxideTry9 sum.Result[int, string] = load(ok, trace)
		var goxideOk10 int
		switch sum.TagResult(goxideTry9) {
		case 1:
			goxideOk10 = sum.ValueOk(goxideTry9)
		case 2:
			return sum.NewErr[string, string](sum.ValueErr(goxideTry9))
		default:
			panic("goxide: invalid Result at ?")
		}

		defer goxideValue8(goxideOk10)
	}
	return sum.NewOk[string, string]("1")
}

func guarded(ok bool, trace *[]string) sum.Result[string, string] {
	defer func() { *trace = append(*trace, "guard-cleanup") }()
	{
		var goxidematch11 Gate = &Enter{Value: 1}
		if goxidematch11 == nil {
			panic("goxide: match on nil Gate at corefixture/main.gox:1197")
		}
		switch goxidevariant12 := goxidematch11.(type) {
		case *Enter:
			_ = goxidevariant12
			goxidePayload13 := goxidevariant12.Value
			if goxidePayload13 ==
				0 {
				var goxideTry16 sum.Result[bool, string] = truth(trace)
				var goxideOk17 bool
				switch sum.TagResult(goxideTry16) {
				case 1:
					goxideOk17 = sum.ValueOk(goxideTry16)
				case 2:
					return sum.NewErr[string, string](sum.ValueErr(goxideTry16))
				default:
					panic("goxide: invalid Result at ?")
				}
				if goxideOk17 {

					return sum.NewOk[string, string]("incorrect")
				} else {
					{
						n := goxidePayload13
						var goxideTry14 sum.Result[int, string] = load(ok, trace)
						var goxideOk15 int
						switch sum.TagResult(goxideTry14) {
						case 1:
							goxideOk15 = sum.ValueOk(goxideTry14)
						case 2:
							return sum.NewErr[string, string](sum.ValueErr(goxideTry14))
						default:
							panic("goxide: invalid Result at ?")
						}
						if goxideOk15 ==
							n+3 {
							return sum.NewOk[string, string]("guard")
						} else {
							return sum.NewOk[string, string]("other")
						}
					}
				}
			} else {
				{
					n := goxidePayload13
					var goxideTry14 sum.Result[int, string] = load(ok, trace)
					var goxideOk15 int
					switch sum.TagResult(goxideTry14) {
					case 1:
						goxideOk15 = sum.ValueOk(goxideTry14)
					case 2:
						return sum.NewErr[string, string](sum.ValueErr(goxideTry14))
					default:
						panic("goxide: invalid Result at ?")
					}
					if goxideOk15 ==
						n+3 {
						return sum.NewOk[string, string]("guard")
					} else {
						return sum.NewOk[string, string]("other")
					}
				}
			}
		default:
			_ = goxidevariant12
			return sum.NewOk[string, string]("other")
		}
	}

}

func optional(ok bool) sum.Option[string] {
	if !ok {
		return sum.None[string]()

	}
	return sum.NewSome[string]("abc")
}

func length(ok bool) sum.Option[int] {
	var goxideTry18 sum.Option[string] = optional(ok)
	var goxideOk19 string
	switch sum.TagOption(goxideTry18) {
	case 1:
		goxideOk19 = sum.ValueSome(goxideTry18)
	case 0:
		return sum.None[int]()
	default:
		panic("goxide: invalid Option at ?")
	}

	s := goxideOk19
	return sum.NewSome[int](len(s))
}

func optionInt(value sum.Option[int]) int {
	{
		var goxideMatch20 int
		var goxidematch21 sum.Option[int] = value
		switch goxidetag23 := sum.TagOption(goxidematch21); goxidetag23 {
		case 1:

			n := sum.ValueSome(goxidematch21)
			goxideMatch20 = n
		case 0:
			goxideMatch20 = -1
		default:
			panic("goxide: invalid enum Option")
		}
		return goxideMatch20
	}
}

func text(value sum.Result[string, string]) string {
	{
		var goxideMatch24 string
		var goxidematch25 sum.Result[string, string] = value
		switch goxidetag27 := sum.TagResult(goxidematch25); goxidetag27 {
		case 1:

			s := sum.ValueOk(goxidematch25)
			goxideMatch24 = s
		case 2:
			e := sum.ValueErr(goxidematch25)
			goxideMatch24 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch24
	}
}

func main() {
	for _, left := range []bool{false, true} {
		trace := []string{}
		var result sum.Result[string, string] = shortCircuit(left, &trace)
		fmt.Println(text(result), trace)
	}
	trace := []string{}
	var result sum.Result[string, string] = closure(&trace)
	fmt.Println(text(result), trace)
	for _, ok := range []bool{false, true} {
		trace := []string{}
		var result sum.Result[string, string] = deferred(ok, &trace)
		fmt.Println(text(result), trace)
	}
	fmt.Println(optionInt(length(true)), optionInt(length(false)))
	for _, ok := range []bool{false, true} {
		trace := []string{}
		var result sum.Result[string, string] = guarded(ok, &trace)
		fmt.Println(text(result), trace)
	}
}
