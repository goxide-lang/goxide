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

func loopValue() int {
	total := 0
gxBreak1:
	for i := 0; i < 5; i++ {
		{
			var gxMatch2 State = &Number{Value: i}
			if gxMatch2 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:10")
			}
			switch gxVar3 := gxMatch2.(type) {
			case *Number:
				if gxVar3 == nil {
					panic("goxide: match on nil State at corefixture/main.gox:10")
				}
				gxPayload4 := gxVar3.Value
				if n := gxPayload4; n == 1 {
					continue
				} else if n := gxPayload4; n == 3 {
					break gxBreak1
				} else {
					total++
				}
			case *Idle:
				if gxVar3 == nil {
					panic("goxide: match on nil State at corefixture/main.gox:10")
				}
				total++
			default:
				total++
			}
		}

		total += 10
	}
	return total
}

func deferred(done *int) int {
	{
		var gxMatch5 State = &Number{Value: 4}
		if gxMatch5 == nil {
			panic("goxide: match on nil State at corefixture/main.gox:21")
		}
		switch gxVar6 := gxMatch5.(type) {
		case *Number:
			if gxVar6 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:21")
			}
			n := gxVar6.Value
			defer func() { *done = n }()
			return n
		case *Idle:
			if gxVar6 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:21")
			}
			return 0
		default:
			return 0
		}
	}

}

func main() {
	done := 0
	value := deferred(&done)
	fmt.Println(loopValue(), value, done)
}
