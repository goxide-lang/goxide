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
	var gxTry1 sum.Result[int, string] = load(false, trace)
	var gxOk2 int
	switch sum.TagResult(gxTry1) {
	case 1:
		gxOk2 = sum.ValueOk(gxTry1)
	case 2:
		return sum.NewErr[bool, string](sum.ValueErr(gxTry1))
	default:
		panic("goxide: invalid Result at ?")
	}

	n := gxOk2
	return sum.NewOk[bool, string](n > 0)
}

func shortCircuit(left bool, trace *[]string) sum.Result[string, string] {
	var gxBool3 bool = left
	if gxBool3 {
		var gxTry4 sum.Result[bool, string] = truth(trace)
		var gxOk5 bool
		switch sum.TagResult(gxTry4) {
		case 1:
			gxOk5 = sum.ValueOk(gxTry4)
		case 2:
			return sum.NewErr[string, string](sum.ValueErr(gxTry4))
		default:
			panic("goxide: invalid Result at ?")
		}
		gxBool3 = gxOk5
	}

	value := gxBool3
	return sum.NewOk[string, string](fmt.Sprint(value))
}

func closure(trace *[]string) sum.Result[string, string] {
	inner := func() sum.Result[int, string] {
		defer func() { *trace = append(*trace, "inner-cleanup") }()
		{
			var gxTry6 sum.Result[int, string] = load(false, trace)
			var gxOk7 int
			switch sum.TagResult(gxTry6) {
			case 1:
				gxOk7 = sum.ValueOk(gxTry6)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(gxTry6))
			default:
				panic("goxide: invalid Result at ?")
			}

			return sum.NewOk[int, string](gxOk7)
		}

	}
	_ = inner()
	*trace = append(*trace, "outer")
	return sum.NewOk[string, string]("outer")
}

func deferred(ok bool, trace *[]string) sum.Result[string, string] {
	defer func() { *trace = append(*trace, "cleanup") }()
	{
		var gxValue8 func(int) = func(n int) { *trace = append(*trace, "deferred") }
		var gxTry9 sum.Result[int, string] = load(ok, trace)
		var gxOk10 int
		switch sum.TagResult(gxTry9) {
		case 1:
			gxOk10 = sum.ValueOk(gxTry9)
		case 2:
			return sum.NewErr[string, string](sum.ValueErr(gxTry9))
		default:
			panic("goxide: invalid Result at ?")
		}

		defer gxValue8(gxOk10)
	}
	return sum.NewOk[string, string]("1")
}

func guarded(ok bool, trace *[]string) sum.Result[string, string] {
	defer func() { *trace = append(*trace, "guard-cleanup") }()
	{
		var gxMatch11 Gate = &Enter{Value: 1}
		if gxMatch11 == nil {
			panic("goxide: match on nil Gate at corefixture/main.gox:1197")
		}
		switch gxVar12 := gxMatch11.(type) {
		case *Enter:
			if gxVar12 == nil {
				panic("goxide: match on nil Gate at corefixture/main.gox:1197")
			}
			gxPayload13 := gxVar12.Value
			if gxPayload13 ==
				0 {
				var gxTry16 sum.Result[bool, string] = truth(trace)
				var gxOk17 bool
				switch sum.TagResult(gxTry16) {
				case 1:
					gxOk17 = sum.ValueOk(gxTry16)
				case 2:
					return sum.NewErr[string, string](sum.ValueErr(gxTry16))
				default:
					panic("goxide: invalid Result at ?")
				}
				if gxOk17 {

					return sum.NewOk[string, string]("incorrect")
				} else {
					{
						n := gxPayload13
						var gxTry14 sum.Result[int, string] = load(ok, trace)
						var gxOk15 int
						switch sum.TagResult(gxTry14) {
						case 1:
							gxOk15 = sum.ValueOk(gxTry14)
						case 2:
							return sum.NewErr[string, string](sum.ValueErr(gxTry14))
						default:
							panic("goxide: invalid Result at ?")
						}
						if gxOk15 ==
							n+3 {
							return sum.NewOk[string, string]("guard")
						} else {
							return sum.NewOk[string, string]("other")
						}
					}
				}
			} else {
				{
					n := gxPayload13
					var gxTry14 sum.Result[int, string] = load(ok, trace)
					var gxOk15 int
					switch sum.TagResult(gxTry14) {
					case 1:
						gxOk15 = sum.ValueOk(gxTry14)
					case 2:
						return sum.NewErr[string, string](sum.ValueErr(gxTry14))
					default:
						panic("goxide: invalid Result at ?")
					}
					if gxOk15 ==
						n+3 {
						return sum.NewOk[string, string]("guard")
					} else {
						return sum.NewOk[string, string]("other")
					}
				}
			}
		case *Shut:
			if gxVar12 == nil {
				panic("goxide: match on nil Gate at corefixture/main.gox:1197")
			}
			return sum.NewOk[string, string]("other")
		default:
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
	var gxTry18 sum.Option[string] = optional(ok)
	var gxOk19 string
	switch sum.TagOption(gxTry18) {
	case 1:
		gxOk19 = sum.ValueSome(gxTry18)
	case 0:
		return sum.None[int]()
	default:
		panic("goxide: invalid Option at ?")
	}

	s := gxOk19
	return sum.NewSome[int](len(s))
}

func optionInt(value sum.Option[int]) int {
	{
		var gxMatch20 int
		switch gxTag22 := sum.TagOption(value); gxTag22 {
		case 1:
			n := sum.ValueSome(value)
			gxMatch20 = n
		case 0:
			gxMatch20 = -1
		default:
			panic("goxide: invalid enum Option")
		}
		return gxMatch20
	}
}

func text(value sum.Result[string, string]) string {
	{
		var gxMatch23 string
		switch gxTag25 := sum.TagResult(value); gxTag25 {
		case 1:
			s := sum.ValueOk(value)
			gxMatch23 = s
		case 2:
			e := sum.ValueErr(value)
			gxMatch23 = e
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch23
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
