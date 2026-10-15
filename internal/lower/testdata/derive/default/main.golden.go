package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"

type Label struct{ Text string }
type Step struct{ Number int }

var calls []string

type Record struct {
	First    Label
	Second   Step
	Optional sum.Option[chan int]
}
type _Unusable interface {
	isUnusable()
}
type _Busy struct {
	Value chan int
}

func (*_Busy) isUnusable() {
}

type State interface {
	isState()
}
type Ready struct {
	First  Label
	Second Step
}

func (*Ready) isState() {
}

type Unavailable struct {
	Value _Unusable
}

func (*Unavailable) isState() {
}

func main() {
	zero := Record{}
	value := DefaultRecord()
	var state State = DefaultState()
	fmt.Println(zero.First.Text == "", value.First.Text, value.Second.Number, calls)
	{
		var goxideMatch1 any
		var goxidematch2 sum.Option[chan int] = value.Optional
		switch goxidetag4 := sum.TagOption(goxidematch2); goxidetag4 {
		case 0:
			goxideMatch1 = "none"
		case 1:
			goxideMatch1 = "some"
		default:
			panic("goxide: invalid enum Option")
		}
		fmt.Println(goxideMatch1)
	}
	{
		var goxideMatch5 any
		var goxidematch6 State = state
		if goxidematch6 == nil {
			panic("goxide: match on nil State at derivefixture/main.gox:647")
		}
		switch goxidevariant7 := goxidematch6.(type) {
		case *Ready:
			_ = goxidevariant7
			if goxidevariant7 == nil {
				panic("goxide: match on nil State at derivefixture/main.gox:647")
			}
			first, second := goxidevariant7.First, goxidevariant7.Second
			goxideMatch5 = first.Text + fmt.Sprint(second.Number)
		case *Unavailable:
			_ = goxidevariant7
			if goxidevariant7 == nil {
				panic("goxide: match on nil State at derivefixture/main.gox:647")
			}
			goxideMatch5 = "bad"
		default:
			_ = goxidevariant7
			panic("goxide: invalid enum State")
		}
		fmt.Println(goxideMatch5)
	}
}
func DefaultRecord() Record {
	return Record{First: DefaultLabel(), Second: DefaultStep(), Optional: sum.None[chan int]()}
}
func DefaultState() State {
	return &Ready{First: DefaultLabel(), Second: DefaultStep()}
}
