package main

import "fmt"

type State interface{ isState() }
type Idle struct{}
type Number struct{ Value int }

func (*Idle) isState()   {}
func (*Number) isState() {}

func loopValue() int {
	total := 0
outer:
	for i := 0; i < 5; i++ {
		var x State = &Number{i}
		if x == nil {
			panic("goxide: match on nil State")
		}
		switch v := x.(type) {
		case *Number:
			if v.Value == 1 {
				continue
			}
			if v.Value == 3 {
				break outer
			}
			total++
		default:
			total++
		}
		total += 10
	}
	return total
}
func deferred(done *int) int {
	var x State = &Number{4}
	if x == nil {
		panic("goxide: match on nil State")
	}
	switch v := x.(type) {
	case *Number:
		n := v.Value
		defer func() { *done = n }()
		return n
	default:
		return 0
	}
}
func main() { done := 0; value := deferred(&done); fmt.Println(loopValue(), value, done) }
