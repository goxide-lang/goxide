package main

import fmt "fmt"
import sum "github.com/goxide-lang/std/sum"

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
		var gxMatch1 any
		var gxMatch2 sum.Option[chan int] = value.Optional
		switch gxTag4 := sum.TagOption(gxMatch2); gxTag4 {
		case 0:
			gxMatch1 = "none"
		case 1:
			gxMatch1 = "some"
		default:
			panic("goxide: invalid enum Option")
		}
		fmt.Println(gxMatch1)
	}
	{
		var gxMatch5 any
		if state == nil {
			panic("goxide: match on nil State at derivefixture/main.gox:26")
		}
		switch gxVar6 := state.(type) {
		case *Ready:
			if gxVar6 == nil {
				panic("goxide: match on nil State at derivefixture/main.gox:26")
			}
			first, second := gxVar6.First, gxVar6.Second
			gxMatch5 = first.Text + fmt.Sprint(second.Number)
		case *Unavailable:
			if gxVar6 == nil {
				panic("goxide: match on nil State at derivefixture/main.gox:26")
			}
			gxMatch5 = "bad"
		default:
			panic("goxide: invalid enum State")
		}
		fmt.Println(gxMatch5)
	}
}
func DefaultRecord() Record {
	return Record{First: DefaultLabel(), Second: DefaultStep(), Optional: sum.None[chan int]()}
}
func DefaultState() State {
	return &Ready{First: DefaultLabel(), Second: DefaultStep()}
}
