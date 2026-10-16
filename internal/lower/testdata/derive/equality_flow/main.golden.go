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
		var gxValue1 Value = left()
		var gxTry2 sum.Option[Value] = right(ok)
		var gxOk3 Value
		switch sum.TagOption(gxTry2) {
		case 1:
			gxOk3 = sum.ValueSome(gxTry2)
		case 0:
			return sum.None[bool]()
		default:
			panic("goxide: invalid Option at ?")
		}

		return sum.NewSome[bool](EqualsValue(gxValue1, gxOk3))
	}

}
func result(value sum.Option[bool]) bool {
	{
		var gxMatch4 bool
		switch gxTag6 := sum.TagOption(value); gxTag6 {
		case 1:
			v := sum.ValueSome(value)
			gxMatch4 = v
		case 0:
			gxMatch4 = false
		default:
			panic("goxide: invalid enum Option")
		}
		return gxMatch4
	}
}
func typed(value any) (bool, bool) { v, ok := value.(bool); return v, ok }
func main() {
	first := result(compare(true))
	firstOrder := order
	order = nil
	second := result(compare(false))
	bool2 := 7
	defer func(gxLeft7, gxRight8 int) bool {
		return gxLeft7 == gxRight8
	}((1), 1)
	var scalar any = func(gxLeft9, gxRight10 int) bool {
		return gxLeft9 == gxRight10
	}((1), 1)
	value, ok := typed(scalar)
	fmt.Println(first, firstOrder, second, order, value, ok, bool2)
}
func EqualsValue(gxValue11, gxOther12 Value) bool {
	return (gxValue11 == gxOther12)
}
