package main

import fmt "fmt"

type State interface {
	isState()
}
type Idle struct {
}

func (*Idle) isState() {
}

type Number struct {
	Value int
}

func (*Number) isState() {
}

type Pair struct {
	V0 int
	V1 string
}

func (*Pair) isState() {
}

type Record struct {
	X int
	Y int
}

func (*Record) isState() {
}

type Text struct {
	Value string
}

func (*Text) isState() {
}

func describe(x State) string {
	{
		var goxideMatch1 string
		var goxidematch2 State = x
		if goxidematch2 == nil {
			panic("goxide: match on nil State at corefixture/main.gox:157")
		}
		switch goxidevariant3 := goxidematch2.(type) {
		case *Idle:
			_ = goxidevariant3
			goxideMatch1 = "idle"
		case *Number:
			_ = goxidevariant3
			if goxidevariant3.Value ==
				0 {
				goxideMatch1 = "zero"
			} else if n := goxidevariant3.Value; n > 0 {
				goxideMatch1 = fmt.Sprintf("positive:%d", n)
			} else {
				n := goxidevariant3.Value
				goxideMatch1 = fmt.Sprintf("number:%d", n)
			}
		case *Pair:
			_ = goxidevariant3
			n, s := goxidevariant3.V0, goxidevariant3.V1
			goxideMatch1 = fmt.Sprintf("pair:%d:%s", n, s)
		case *Record:
			_ = goxidevariant3
			if n := goxidevariant3.Y; goxidevariant3.X == 1 {
				goxideMatch1 = fmt.Sprintf("record:%d", n)
			} else {
				goxideMatch1 = "record"
			}
		case *Text:
			_ = goxidevariant3
			s := goxidevariant3.Value
			goxideMatch1 = "text:" + s
		default:
			_ = goxidevariant3
			panic("goxide: invalid enum State")
		}
		return goxideMatch1
	}

}

func ordered(next func() State, seen *[]int) int {
	mark := func(n int, value bool) bool { *seen = append(*seen, n); return value }
	{
		var goxidematch4 State = next()
		if goxidematch4 == nil {
			panic("goxide: match on nil State at corefixture/main.gox:656")
		}
		switch goxidevariant5 := goxidematch4.(type) {
		case *Number:
			_ = goxidevariant5
			if n := goxidevariant5.Value; mark(1, false) {
				return n
			} else if mark(2, false) {
				return -2
			} else if n := goxidevariant5.Value; mark(3, true) {
				return n
			} else {
				n := goxidevariant5.Value
				return n + 100
			}
		default:
			_ = goxidevariant5
			if mark(2, false) {
				return -2
			} else {

				return -1
			}
		}
	}

}

func freshBinding(x State) int {
	{
		var goxidematch6 State = x
		if goxidematch6 == nil {
			panic("goxide: match on nil State at corefixture/main.gox:922")
		}
		switch goxidevariant7 := goxidematch6.(type) {
		case *Number:
			_ = goxidevariant7
			if n := goxidevariant7.Value; func() bool { n = 99; return false }() {
				return n
			} else {
				n := goxidevariant7.Value
				return n
			}
		default:
			_ = goxidevariant7
			return -1
		}
	}

}

func nested(x State) int {
	{
		var goxidematch8 State = x
		if goxidematch8 == nil {
			panic("goxide: match on nil State at corefixture/main.gox:1102")
		}
		switch goxidevariant9 := goxidematch8.(type) {
		case *Number:
			_ = goxidevariant9
			n := goxidevariant9.Value
			{
				var goxidematch10 State = &Pair{V0: n, V1: "ok"}
				if goxidematch10 == nil {
					panic("goxide: match on nil State at corefixture/main.gox:1139")
				}
				switch goxidevariant11 := goxidematch10.(type) {
				case *Pair:
					_ = goxidevariant11
					value := goxidevariant11.V0
					return value
				default:
					_ = goxidevariant11
					return -1
				}
			}
		default:
			_ = goxidevariant9

			return -1
		}
	}

}

func main() {
	fmt.Println(describe(&Idle{}), describe(&Number{Value: 0}), describe(&Number{Value: 3}), describe(&Number{Value: -2}), describe(&Pair{V0: 4, V1: "x"}), describe(&Record{X: 1, Y: 7}), describe(&Text{Value: "y"}))
	calls := 0
	seen := []int{}
	n := ordered(func() State { calls++; return &Number{Value: 7} }, &seen)
	fmt.Println(n, calls, seen, nested(&Number{Value: freshBinding(&Number{Value: 9})}))
}
