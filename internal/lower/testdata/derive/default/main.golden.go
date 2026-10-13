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
		var goxideValue1 func(a ...any) (n int, err error) = fmt.Println
		var goxideMatch2 any
		var goxidematch3 sum.Option[chan int] = value.Optional
		switch goxidetag5 := sum.TagOption(goxidematch3); goxidetag5 {
		case 0:
			goxideMatch2 = "none"
		case 1:
			goxideMatch2 = "some"
		default:
			panic("goxide: invalid enum Option")
		}
		goxideValue1(goxideMatch2)
	}
	{
		var goxideValue6 func(a ...any) (n int, err error) = fmt.Println
		var goxideMatch7 any
		var goxidematch8 State = state
		if goxidematch8 == nil {
			panic("goxide: match on nil State at derivefixture/main.gox:647")
		}
		switch goxidevariant9 := goxidematch8.(type) {
		case *Ready:
			_ = goxidevariant9
			first, second := goxidevariant9.First, goxidevariant9.Second
			goxideMatch7 = first.Text + fmt.Sprint(second.Number)
		case *Unavailable:
			_ = goxidevariant9
			goxideMatch7 = "bad"
		default:
			_ = goxidevariant9
			panic("goxide: invalid enum State")
		}
		goxideValue6(goxideMatch7)
	}
}
func DefaultRecord() Record {
	return Record{First: DefaultLabel(), Second: DefaultStep(), Optional: sum.None[chan int]()}
}
func DefaultState() State {
	return &Ready{First: DefaultLabel(), Second: DefaultStep()}
}
