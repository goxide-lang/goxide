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
goxideBreak1:
	for i := 0; i < 5; i++ {
		{
			var goxidematch2 State = &Number{Value: i}
			if goxidematch2 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:125")
			}
			switch goxidevariant3 := goxidematch2.(type) {
			case *Number:
				_ = goxidevariant3
				goxidePayload4 := goxidevariant3.Value
				if n := goxidePayload4; n == 1 {
					continue
				} else if n := goxidePayload4; n == 3 {
					break goxideBreak1
				} else {
					total++
				}
			default:
				_ = goxidevariant3
				total++
			}
		}

		total += 10
	}
	return total
}

func deferred(done *int) int {
	{
		var goxidematch5 State = &Number{Value: 4}
		if goxidematch5 == nil {
			panic("goxide: match on nil State at corefixture/main.gox:332")
		}
		switch goxidevariant6 := goxidematch5.(type) {
		case *Number:
			_ = goxidevariant6
			n := goxidevariant6.Value
			defer func() { *done = n }()
			return n
		default:
			_ = goxidevariant6
			return 0
		}
	}

}

func main() {
	done := 0
	value := deferred(&done)
	fmt.Println(loopValue(), value, done)
}
