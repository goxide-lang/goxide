package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"

type Value int

var order []string

func left() Value { order = append(order, "left"); return Value(4) }
func right(ok bool) sum.Option[Value] {
	order = append(order, "right")
	if !ok {
		return sum.None[Value]()

	}
	return sum.NewSome[Value](Value(4))
}
func compare(ok bool) sum.Option[bool] {
	{
		var goxideValue1 Value = left()
		var goxideTry2 sum.Option[Value] = right(ok)
		var goxideOk3 Value
		switch sum.TagOption(goxideTry2) {
		case 1:
			goxideOk3 = sum.ValueSome(goxideTry2)
		case 0:
			return sum.None[bool]()
		default:
			panic("goxide: invalid Option at ?")
		}

		return sum.NewSome[bool](EqualsValue(goxideValue1, goxideOk3))
	}

}
func result(value sum.Option[bool]) bool {
	{
		var goxideMatch4 bool
		var goxidematch5 sum.Option[bool] = value
		switch goxidetag7 := sum.TagOption(goxidematch5); goxidetag7 {
		case 1:

			v := sum.ValueSome(goxidematch5)
			goxideMatch4 = v
		case 0:
			goxideMatch4 = false
		default:
			panic("goxide: invalid enum Option")
		}
		return goxideMatch4
	}
}
func typed(value any) (bool, bool) { v, ok := value.(bool); return v, ok }
func main() {
	first := result(compare(true))
	firstOrder := order
	order = nil
	second := result(compare(false))
	bool2 := 7
	defer func(goxideLeft8, goxideRight9 int) bool {
		return goxideLeft8 == goxideRight9
	}((1), 1)
	var scalar any = func(goxideLeft10, goxideRight11 int) bool {
		return goxideLeft10 == goxideRight11
	}((1), 1)
	value, ok := typed(scalar)
	fmt.Println(first, firstOrder, second, order, value, ok, bool2)
}
func EqualsValue(goxideValue12, goxideOther13 Value) bool {
	return (goxideValue12 == goxideOther13)
}
