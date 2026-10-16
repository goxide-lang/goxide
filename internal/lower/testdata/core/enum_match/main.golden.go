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
		var gxMatch1 string
		if x == nil {
			panic("goxide: match on nil State at corefixture/main.gox:8")
		}
		switch gxVar2 := x.(type) {
		case *Idle:
			if gxVar2 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:8")
			}
			gxMatch1 = "idle"
		case *Number:
			if gxVar2 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:8")
			}
			gxPayload3 := gxVar2.Value
			if gxPayload3 ==
				0 {
				gxMatch1 = "zero"
			} else if n := gxPayload3; n > 0 {
				gxMatch1 = fmt.Sprintf("positive:%d", n)
			} else {
				n := gxPayload3
				gxMatch1 = fmt.Sprintf("number:%d", n)
			}
		case *Pair:
			if gxVar2 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:8")
			}
			n, s := gxVar2.V0, gxVar2.V1
			gxMatch1 = fmt.Sprintf("pair:%d:%s", n, s)
		case *Record:
			if gxVar2 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:8")
			}
			if n := gxVar2.Y; gxVar2.X == 1 {
				gxMatch1 = fmt.Sprintf("record:%d", n)
			} else {
				gxMatch1 = "record"
			}
		case *Text:
			if gxVar2 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:8")
			}
			s := gxVar2.Value
			gxMatch1 = "text:" + s
		default:
			panic("goxide: invalid enum State")
		}
		return gxMatch1
	}

}

func ordered(next func() State, seen *[]int) int {
	mark := func(n int, value bool) bool { *seen = append(*seen, n); return value }
	{
		var gxMatch4 State = next()
		if gxMatch4 == nil {
			panic("goxide: match on nil State at corefixture/main.gox:22")
		}
		switch gxVar5 := gxMatch4.(type) {
		case *Number:
			if gxVar5 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:22")
			}
			gxPayload6 := gxVar5.Value
			if n := gxPayload6; mark(1, false) {
				return n
			} else if mark(2, false) {
				return -2
			} else if n := gxPayload6; mark(3, true) {
				return n
			} else {
				n := gxPayload6
				return n + 100
			}
		case *Idle:
			if gxVar5 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:22")
			}
			if mark(2, false) {
				return -2
			} else {

				return -1
			}
		case *Pair:
			if gxVar5 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:22")
			}
			if mark(2, false) {
				return -2
			} else {

				return -1
			}
		case *Record:
			if gxVar5 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:22")
			}
			if mark(2, false) {
				return -2
			} else {

				return -1
			}
		case *Text:
			if gxVar5 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:22")
			}
			if mark(2, false) {
				return -2
			} else {

				return -1
			}
		default:
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
		if x == nil {
			panic("goxide: match on nil State at corefixture/main.gox:32")
		}
		switch gxVar7 := x.(type) {
		case *Number:
			if gxVar7 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:32")
			}
			gxPayload8 := gxVar7.Value
			if n := gxPayload8; func() bool { n = 99; return false }() {
				return n
			} else {
				n := gxPayload8
				return n
			}
		case *Idle:
			if gxVar7 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:32")
			}
			return -1
		case *Pair:
			if gxVar7 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:32")
			}
			return -1
		case *Record:
			if gxVar7 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:32")
			}
			return -1
		case *Text:
			if gxVar7 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:32")
			}
			return -1
		default:
			return -1
		}
	}

}

func nested(x State) int {
	{
		if x == nil {
			panic("goxide: match on nil State at corefixture/main.gox:40")
		}
		switch gxVar9 := x.(type) {
		case *Number:
			if gxVar9 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:40")
			}
			n := gxVar9.Value
			{
				var gxMatch10 State = &Pair{V0: n, V1: "ok"}
				if gxMatch10 == nil {
					panic("goxide: match on nil State at corefixture/main.gox:42")
				}
				switch gxVar11 := gxMatch10.(type) {
				case *Pair:
					if gxVar11 == nil {
						panic("goxide: match on nil State at corefixture/main.gox:42")
					}
					value := gxVar11.V0
					return value
				case *Idle:
					if gxVar11 == nil {
						panic("goxide: match on nil State at corefixture/main.gox:42")
					}
					return -1
				case *Number:
					if gxVar11 == nil {
						panic("goxide: match on nil State at corefixture/main.gox:42")
					}
					return -1
				case *Record:
					if gxVar11 == nil {
						panic("goxide: match on nil State at corefixture/main.gox:42")
					}
					return -1
				case *Text:
					if gxVar11 == nil {
						panic("goxide: match on nil State at corefixture/main.gox:42")
					}
					return -1
				default:
					return -1
				}
			}
		case *Idle:
			if gxVar9 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:40")
			}

			return -1
		case *Pair:
			if gxVar9 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:40")
			}
			return -1
		case *Record:
			if gxVar9 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:40")
			}
			return -1
		case *Text:
			if gxVar9 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:40")
			}
			return -1
		default:
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
