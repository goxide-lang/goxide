package main

import fmt "fmt"
import sum "github.com/goxide-lang/std/sum"

type _Counter interface {
	isCounter()
}
type _Count struct {
	Value int
}

func (*_Count) isCounter() {
}

type _Empty struct {
}

func (*_Empty) isCounter() {
}

var trace string
var storage = []_Counter{&_Count{Value: 1}}

func alter(value *_Counter) bool { *value = &_Count{Value: 9}; return false }
func owners() []_Counter         { trace += "s"; return storage }
func index() int                 { trace += "i"; return 0 }
func guard(value *int) bool      { trace += "g"; _ = *value; return false }
func allowed(fail bool) sum.Result[bool, string] {
	if fail {
		return sum.NewErr[bool, string]("stop")
	}
	return sum.NewOk[bool, string](true)
}
func tryIndex(fail bool) sum.Result[int, string] {
	trace += "x"
	if fail {
		return sum.NewErr[int, string]("index")
	}
	return sum.NewOk[int, string](0)
}
func arrayProjection(fail bool) sum.Result[int, string] {
	values := [1]int{1}
	gxTry1 := tryIndex(fail)
	switch sum.TagResult(gxTry1) {
	case 1:
	case 2:
		return sum.NewErr[int, string](sum.ValueErr(gxTry1))
	default:
		panic("goxide: invalid Result at ?")
	}
	gxOk2 := sum.ValueOk(gxTry1)
	var gxValue3 int = gxOk2

	element := &values[gxValue3]
	*element += 4
	choices := [1]_Counter{&_Count{Value: 2}}
	gxTry4 := tryIndex(false)
	switch sum.TagResult(gxTry4) {
	case 1:
	case 2:
		return sum.NewErr[int, string](sum.ValueErr(gxTry4))
	default:
		panic("goxide: invalid Result at ?")
	}
	gxOk5 := sum.ValueOk(gxTry4)

	selected := gxOk5
	{
		var gxMatch6 _Counter = choices[selected]
		if gxMatch6 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:28")
		}
		switch gxVar7 := gxMatch6.(type) {
		case *_Count:
			if gxVar7 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:28")
			}
			value := gxVar7.Value
			choices[selected] = &_Count{Value: value + 3}
		case *_Empty:
			if gxVar7 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:28")
			}
		default:
			panic("goxide: invalid enum Counter")
		}
	}
	var gxMatch8 int
	var gxMatch9 _Counter = choices[0]
	if gxMatch9 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:32")
	}
	switch gxVar10 := gxMatch9.(type) {
	case *_Count:
		if gxVar10 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:32")
		}
		value := gxVar10.Value
		gxMatch8 = value
	case *_Empty:
		if gxVar10 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:32")
		}
		gxMatch8 = 0
	default:
		gxMatch8 = 0
	}
	current := gxMatch8
	return sum.NewOk[int, string](values[0] + current)
}
func controlled(fail bool) sum.Result[int, string] {
	defer func() { trace += "done" }()
	var counter _Counter = &_Count{Value: 0}
gxBreak11:
	for i := 0; i < 3; i++ {
		{
			if counter == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:39")
			}
			switch gxVar12 := counter.(type) {
			case *_Count:
				if gxVar12 == nil {
					panic("goxide: match on nil Counter at referencefixture/main.gox:39")
				}
				value := gxVar12.Value
				if i == 0 {
					counter = &_Count{Value: value + 1}
					continue
				}
				counter = &_Count{Value: value + 2}
				break gxBreak11
			case *_Empty:
				if gxVar12 == nil {
					panic("goxide: match on nil Counter at referencefixture/main.gox:39")
				}
			default:
				panic("goxide: invalid enum Counter")
			}
		}

		panic("match changed loop control")
	}
	var gxMatch13 int
	gxOwner14 := &counter
	var gxMatch15 _Counter = *gxOwner14
	if gxMatch15 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:49")
	}
	switch gxVar16 := gxMatch15.(type) {
	case *_Count:
		if gxVar16 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:49")
		}
		gxPayloadRef17 := &gxVar16.Value
		{
			value := gxPayloadRef17
			gxTry18 := allowed(fail)
			switch sum.TagResult(gxTry18) {
			case 1:
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(gxTry18))
			default:
				panic("goxide: invalid Result at ?")
			}
			gxOk19 := sum.ValueOk(gxTry18)
			if gxOk19 {
				gxMatch13 = *value
			} else {
				gxMatch13 = -1
			}
		}
	case *_Empty:
		if gxVar16 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:49")
		}
		gxMatch13 = -2
	default:
		panic("goxide: invalid enum Counter")
	}
	value := gxMatch13

	return sum.NewOk[int, string](value)
}

func main() {
	var counter _Counter = &_Count{Value: 1}
	var gxMatch20 int
	if counter == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:59")
	}
	switch gxVar21 := counter.(type) {
	case *_Count:
		if gxVar21 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:59")
		}
		gxPayload22 := gxVar21.Value
		if alter(&counter) {
			gxMatch20 = -1
		} else {
			value := gxPayload22
			gxMatch20 = value
		}
	case *_Empty:
		if gxVar21 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:59")
		}
		gxMatch20 = 0
	default:
		panic("goxide: invalid enum Counter")
	}
	snapshot := gxMatch20
	var gxMatch23 int
	if counter == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:64")
	}
	switch gxVar24 := counter.(type) {
	case *_Count:
		if gxVar24 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:64")
		}
		value := gxVar24.Value
		gxMatch23 = value
	case *_Empty:
		if gxVar24 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:64")
		}
		gxMatch23 = 0
	default:
		panic("goxide: invalid enum Counter")
	}
	current := gxMatch23
	fmt.Println(snapshot, current)
	var gxMatch25 int
	gxOwner26 := &owners()[index()]
	var gxMatch27 _Counter = *gxOwner26
	if gxMatch27 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:66")
	}
	switch gxVar28 := gxMatch27.(type) {
	case *_Count:
		if gxVar28 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:66")
		}
		gxPayloadRef29 := &gxVar28.Value
		if value := gxPayloadRef29; guard(value) {
			gxMatch25 = -1
		} else {
			value := gxPayloadRef29
			gxMatch25 = *value
		}
	case *_Empty:
		if gxVar28 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:66")
		}
		gxMatch25 = 0
	default:
		panic("goxide: invalid enum Counter")
	}
	observed := gxMatch25

	fmt.Println(observed, trace)
	trace = ""
	var success sum.Result[int, string] = controlled(false)
	{
		var gxMatch30 any
		switch gxTag32 := sum.TagResult(success); gxTag32 {
		case 1:
			value := sum.ValueOk(success)
			gxMatch30 = value
		case 2:
			gxMatch30 = -1
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(gxMatch30, trace)
	}
	trace = ""
	var failure sum.Result[int, string] = controlled(true)
	{
		var gxMatch33 any
		switch gxTag35 := sum.TagResult(failure); gxTag35 {
		case 1:
			gxMatch33 = "wrong"
		case 2:
			err := sum.ValueErr(failure)
			gxMatch33 = err
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(gxMatch33, trace)
	}
	trace = ""
	var array sum.Result[int, string] = arrayProjection(false)
	{
		var gxMatch36 any
		switch gxTag38 := sum.TagResult(array); gxTag38 {
		case 1:
			value := sum.ValueOk(array)
			gxMatch36 = value
		case 2:
			gxMatch36 = -1
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(gxMatch36, trace)
	}
	trace = ""
	var stopped sum.Result[int, string] = arrayProjection(true)
	{
		var gxMatch39 any
		switch gxTag41 := sum.TagResult(stopped); gxTag41 {
		case 1:
			gxMatch39 = "wrong"
		case 2:
			err := sum.ValueErr(stopped)
			gxMatch39 = err
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(gxMatch39, trace)
	}
}
