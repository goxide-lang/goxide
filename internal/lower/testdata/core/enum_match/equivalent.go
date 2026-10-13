package main

import "fmt"

type State interface{ isState() }
type Idle struct{}
type Number struct{ Value int }
type Pair struct {
	V0 int
	V1 string
}
type Record struct{ X, Y int }
type Text struct{ Value string }

func (*Idle) isState()   {}
func (*Number) isState() {}
func (*Pair) isState()   {}
func (*Record) isState() {}
func (*Text) isState()   {}

func describe(x State) string {
	if x == nil {
		panic("goxide: match on nil State")
	}
	switch v := x.(type) {
	case *Idle:
		return "idle"
	case *Number:
		if v.Value == 0 {
			return "zero"
		}
		if v.Value > 0 {
			return fmt.Sprintf("positive:%d", v.Value)
		}
		return fmt.Sprintf("number:%d", v.Value)
	case *Pair:
		return fmt.Sprintf("pair:%d:%s", v.V0, v.V1)
	case *Record:
		if v.X == 1 {
			return fmt.Sprintf("record:%d", v.Y)
		}
		return "record"
	case *Text:
		return "text:" + v.Value
	}
	panic("unreachable")
}

func ordered(next func() State, seen *[]int) int {
	x := next()
	if x == nil {
		panic("goxide: match on nil State")
	}
	switch v := x.(type) {
	case *Number:
		*seen = append(*seen, 1)
		*seen = append(*seen, 2)
		*seen = append(*seen, 3)
		return v.Value
	default:
		return -1
	}
}

func freshBinding(x State) int {
	if x == nil {
		panic("goxide: match on nil State")
	}
	switch v := x.(type) {
	case *Number:
		n := v.Value
		if func() bool { n = 99; return false }() {
			return n
		}
		return v.Value
	default:
		return -1
	}
}

func nested(x State) int {
	if x == nil {
		panic("goxide: match on nil State")
	}
	switch v := x.(type) {
	case *Number:
		var inner State = &Pair{V0: v.Value, V1: "ok"}
		switch item := inner.(type) {
		case *Pair:
			return item.V0
		default:
			return -1
		}
	default:
		return -1
	}
}

func main() {
	fmt.Println(describe(&Idle{}), describe(&Number{0}), describe(&Number{3}), describe(&Number{-2}), describe(&Pair{4, "x"}), describe(&Record{1, 7}), describe(&Text{"y"}))
	calls := 0
	seen := []int{}
	n := ordered(func() State { calls++; return &Number{7} }, &seen)
	fmt.Println(n, calls, seen, nested(&Number{freshBinding(&Number{9})}))
}
