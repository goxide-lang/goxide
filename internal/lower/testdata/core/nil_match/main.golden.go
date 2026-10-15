package main

import fmt "fmt"
import strings "strings"

type State interface {
	isState()
}
type Ready struct {
}

func (*Ready) isState() {
}

func observe(value State, touched *bool) {
	{
		var goxidematch1 State = value
		if goxidematch1 == nil {
			panic("goxide: match on nil State at corefixture/main.gox:107")
		}
		switch goxidevariant2 := goxidematch1.(type) {
		case *Ready:
			_ = goxidevariant2
			if goxidevariant2 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:107")
			}
			*touched = true
		default:
			_ = goxidevariant2
			*touched = true
		}
	}
}

func main() {
	var value State
	touched := false
	message := ""
	func() {
		defer func() { message = fmt.Sprint(recover()) }()
		observe(value, &touched)
	}()
	fmt.Println(strings.Contains(message, "match on nil State"), touched)
}
