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
		if value == nil {
			panic("goxide: match on nil State at corefixture/main.gox:9")
		}
		switch gxVar1 := value.(type) {
		case *Ready:
			if gxVar1 == nil {
				panic("goxide: match on nil State at corefixture/main.gox:9")
			}
			*touched = true
		default:
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
