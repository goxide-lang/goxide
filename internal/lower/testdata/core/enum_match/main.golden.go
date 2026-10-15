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
			if goxidevariant3 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:157")
			}
			goxideMatch1 = "idle"
		case *Number:
			_ = goxidevariant3
			if goxidevariant3 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:157")
			}
			goxidePayload4 := goxidevariant3.Value
			if goxidePayload4 ==
				0 {
				goxideMatch1 = "zero"
			} else if n := goxidePayload4; n > 0 {
				goxideMatch1 = fmt.Sprintf("positive:%d", n)
			} else {
				n := goxidePayload4
				goxideMatch1 = fmt.Sprintf("number:%d", n)
			}
		case *Pair:
			_ = goxidevariant3
			if goxidevariant3 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:157")
			}
			n, s := goxidevariant3.V0, goxidevariant3.V1
			goxideMatch1 = fmt.Sprintf("pair:%d:%s", n, s)
		case *Record:
			_ = goxidevariant3
			if goxidevariant3 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:157")
			}
			if n := goxidevariant3.Y; goxidevariant3.X == 1 {
				goxideMatch1 = fmt.Sprintf("record:%d", n)
			} else {
				goxideMatch1 = "record"
			}
		case *Text:
			_ = goxidevariant3
			if goxidevariant3 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:157")
			}
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
		var goxidematch5 State = next()
		if goxidematch5 == nil {
			panic("goxide: match on nil State at corefixture/main.gox:656")
		}
		switch goxidevariant6 := goxidematch5.(type) {
		case *Number:
			_ = goxidevariant6
			if goxidevariant6 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:656")
			}
			goxidePayload7 := goxidevariant6.Value
			if n := goxidePayload7; mark(1, false) {
				return n
			} else if mark(2, false) {
				return -2
			} else if n := goxidePayload7; mark(3, true) {
				return n
			} else {
				n := goxidePayload7
				return n + 100
			}
		case *Idle:
			_ = goxidevariant6
			if goxidevariant6 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:656")
			}
			if mark(2, false) {
				return -2
			} else {

				return -1
			}
		case *Pair:
			_ = goxidevariant6
			if goxidevariant6 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:656")
			}
			if mark(2, false) {
				return -2
			} else {

				return -1
			}
		case *Record:
			_ = goxidevariant6
			if goxidevariant6 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:656")
			}
			if mark(2, false) {
				return -2
			} else {

				return -1
			}
		case *Text:
			_ = goxidevariant6
			if goxidevariant6 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:656")
			}
			if mark(2, false) {
				return -2
			} else {

				return -1
			}
		default:
			_ = goxidevariant6
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
		var goxidematch8 State = x
		if goxidematch8 == nil {
			panic("goxide: match on nil State at corefixture/main.gox:922")
		}
		switch goxidevariant9 := goxidematch8.(type) {
		case *Number:
			_ = goxidevariant9
			if goxidevariant9 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:922")
			}
			goxidePayload10 := goxidevariant9.Value
			if n := goxidePayload10; func() bool { n = 99; return false }() {
				return n
			} else {
				n := goxidePayload10
				return n
			}
		case *Idle:
			_ = goxidevariant9
			if goxidevariant9 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:922")
			}
			return -1
		case *Pair:
			_ = goxidevariant9
			if goxidevariant9 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:922")
			}
			return -1
		case *Record:
			_ = goxidevariant9
			if goxidevariant9 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:922")
			}
			return -1
		case *Text:
			_ = goxidevariant9
			if goxidevariant9 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:922")
			}
			return -1
		default:
			_ = goxidevariant9
			return -1
		}
	}

}

func nested(x State) int {
	{
		var goxidematch11 State = x
		if goxidematch11 == nil {
			panic("goxide: match on nil State at corefixture/main.gox:1102")
		}
		switch goxidevariant12 := goxidematch11.(type) {
		case *Number:
			_ = goxidevariant12
			if goxidevariant12 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:1102")
			}
			n := goxidevariant12.Value
			{
				var goxidematch13 State = &Pair{V0: n, V1: "ok"}
				if goxidematch13 == nil {
					panic("goxide: match on nil State at corefixture/main.gox:1139")
				}
				switch goxidevariant14 := goxidematch13.(type) {
				case *Pair:
					_ = goxidevariant14
					if goxidevariant14 == nil {
						panic("goxide: match on nil State at corefixture/main.gox:1139")
					}
					value := goxidevariant14.V0
					return value
				case *Idle:
					_ = goxidevariant14
					if goxidevariant14 == nil {
						panic("goxide: match on nil State at corefixture/main.gox:1139")
					}
					return -1
				case *Number:
					_ = goxidevariant14
					if goxidevariant14 == nil {
						panic("goxide: match on nil State at corefixture/main.gox:1139")
					}
					return -1
				case *Record:
					_ = goxidevariant14
					if goxidevariant14 == nil {
						panic("goxide: match on nil State at corefixture/main.gox:1139")
					}
					return -1
				case *Text:
					_ = goxidevariant14
					if goxidevariant14 == nil {
						panic("goxide: match on nil State at corefixture/main.gox:1139")
					}
					return -1
				default:
					_ = goxidevariant14
					return -1
				}
			}
		case *Idle:
			_ = goxidevariant12
			if goxidevariant12 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:1102")
			}

			return -1
		case *Pair:
			_ = goxidevariant12
			if goxidevariant12 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:1102")
			}
			return -1
		case *Record:
			_ = goxidevariant12
			if goxidevariant12 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:1102")
			}
			return -1
		case *Text:
			_ = goxidevariant12
			if goxidevariant12 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:1102")
			}
			return -1
		default:
			_ = goxidevariant12
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
