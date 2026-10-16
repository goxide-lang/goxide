package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"

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

func alter(value *int) bool { *value = 9; return false }
func owners() []_Counter    { trace += "s"; return storage }
func index() int            { trace += "i"; return 0 }
func guard(value *int) bool { trace += "g"; *value += 1; return false }
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
	var gxTry1 sum.Result[int, string] = tryIndex(fail)
	var gxOk2 int
	switch sum.TagResult(gxTry1) {
	case 1:
		gxOk2 = sum.ValueOk(gxTry1)
	case 2:
		return sum.NewErr[int, string](sum.ValueErr(gxTry1))
	default:
		panic("goxide: invalid Result at ?")
	}
	var gxValue3 int = gxOk2

	element := &values[gxValue3]
	*element += 4
	choices := [1]_Counter{&_Count{Value: 2}}
	{
		var gxTry4 sum.Result[int, string] = tryIndex(false)
		var gxOk5 int
		switch sum.TagResult(gxTry4) {
		case 1:
			gxOk5 = sum.ValueOk(gxTry4)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(gxTry4))
		default:
			panic("goxide: invalid Result at ?")
		}
		var gxValue6 int = gxOk5
		gxOwner7 := &choices[gxValue6]
		var gxMatch8 _Counter = *gxOwner7
		if gxMatch8 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:27")
		}
		switch gxVar9 := gxMatch8.(type) {
		case *_Count:
			if gxVar9 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:27")
			}
			gxPayloadRef10 := &gxVar9.Value
			value := gxPayloadRef10
			*value += 3
		case *_Empty:
			if gxVar9 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:27")
			}
		default:
			panic("goxide: invalid enum Counter")
		}
	}
	var gxMatch11 int
	var gxMatch12 _Counter = choices[0]
	if gxMatch12 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:31")
	}
	switch gxVar13 := gxMatch12.(type) {
	case *_Count:
		if gxVar13 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:31")
		}
		value := gxVar13.Value
		gxMatch11 = value
	case *_Empty:
		if gxVar13 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:31")
		}
		gxMatch11 = 0
	default:
		gxMatch11 = 0
	}
	current := gxMatch11
	return sum.NewOk[int, string](values[0] + current)
}
func controlled(fail bool) sum.Result[int, string] {
	defer func() { trace += "done" }()
	var counter _Counter = &_Count{Value: 0}
gxBreak14:
	for i := 0; i < 3; i++ {
		{
			gxOwner15 := &counter
			var gxMatch16 _Counter = *gxOwner15
			if gxMatch16 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:38")
			}
			switch gxVar17 := gxMatch16.(type) {
			case *_Count:
				if gxVar17 == nil {
					panic("goxide: match on nil Counter at referencefixture/main.gox:38")
				}
				gxPayloadRef18 := &gxVar17.Value
				value := gxPayloadRef18
				if i == 0 {
					*value += 1
					continue
				}
				*value += 2
				break gxBreak14
			case *_Empty:
				if gxVar17 == nil {
					panic("goxide: match on nil Counter at referencefixture/main.gox:38")
				}
			default:
				panic("goxide: invalid enum Counter")
			}
		}

		panic("match changed loop control")
	}
	var gxMatch19 int
	gxOwner20 := &counter
	var gxMatch21 _Counter = *gxOwner20
	if gxMatch21 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:48")
	}
	switch gxVar22 := gxMatch21.(type) {
	case *_Count:
		if gxVar22 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:48")
		}
		gxPayloadRef23 := &gxVar22.Value
		{
			value := gxPayloadRef23
			var gxTry24 sum.Result[bool, string] = allowed(fail)
			var gxOk25 bool
			switch sum.TagResult(gxTry24) {
			case 1:
				gxOk25 = sum.ValueOk(gxTry24)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(gxTry24))
			default:
				panic("goxide: invalid Result at ?")
			}
			if gxOk25 {
				gxMatch19 = *value
			} else {
				gxMatch19 = -1
			}
		}
	case *_Empty:
		if gxVar22 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:48")
		}
		gxMatch19 = -2
	default:
		panic("goxide: invalid enum Counter")
	}
	value := gxMatch19

	return sum.NewOk[int, string](value)
}

func main() {
	var counter _Counter = &_Count{Value: 1}
	var gxMatch26 int
	gxOwner27 := &counter
	var gxMatch28 _Counter = *gxOwner27
	if gxMatch28 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:58")
	}
	switch gxVar29 := gxMatch28.(type) {
	case *_Count:
		if gxVar29 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:58")
		}
		gxPayload30 := gxVar29.Value
		gxPayloadRef31 := &gxVar29.Value
		if value := gxPayloadRef31; alter(value) {
			gxMatch26 = -1
		} else {
			value := gxPayload30
			gxMatch26 = value
		}
	case *_Empty:
		if gxVar29 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:58")
		}
		gxMatch26 = 0
	default:
		panic("goxide: invalid enum Counter")
	}
	snapshot := gxMatch26
	var gxMatch32 int
	if counter == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:63")
	}
	switch gxVar33 := counter.(type) {
	case *_Count:
		if gxVar33 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:63")
		}
		value := gxVar33.Value
		gxMatch32 = value
	case *_Empty:
		if gxVar33 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:63")
		}
		gxMatch32 = 0
	default:
		panic("goxide: invalid enum Counter")
	}
	current := gxMatch32
	fmt.Println(snapshot, current)
	var gxMatch34 int
	gxOwner35 := &owners()[index()]
	var gxMatch36 _Counter = *gxOwner35
	if gxMatch36 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:65")
	}
	switch gxVar37 := gxMatch36.(type) {
	case *_Count:
		if gxVar37 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:65")
		}
		gxPayloadRef38 := &gxVar37.Value
		if value := gxPayloadRef38; guard(value) {
			gxMatch34 = -1
		} else {
			value := gxPayloadRef38
			gxMatch34 = *value
		}
	case *_Empty:
		if gxVar37 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:65")
		}
		gxMatch34 = 0
	default:
		panic("goxide: invalid enum Counter")
	}
	observed := gxMatch34

	fmt.Println(observed, trace)
	trace = ""
	var success sum.Result[int, string] = controlled(false)
	{
		var gxMatch39 any
		switch gxTag41 := sum.TagResult(success); gxTag41 {
		case 1:
			value := sum.ValueOk(success)
			gxMatch39 = value
		case 2:
			gxMatch39 = -1
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(gxMatch39, trace)
	}
	trace = ""
	var failure sum.Result[int, string] = controlled(true)
	{
		var gxMatch42 any
		switch gxTag44 := sum.TagResult(failure); gxTag44 {
		case 1:
			gxMatch42 = "wrong"
		case 2:
			err := sum.ValueErr(failure)
			gxMatch42 = err
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(gxMatch42, trace)
	}
	trace = ""
	var array sum.Result[int, string] = arrayProjection(false)
	{
		var gxMatch45 any
		switch gxTag47 := sum.TagResult(array); gxTag47 {
		case 1:
			value := sum.ValueOk(array)
			gxMatch45 = value
		case 2:
			gxMatch45 = -1
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(gxMatch45, trace)
	}
	trace = ""
	var stopped sum.Result[int, string] = arrayProjection(true)
	{
		var gxMatch48 any
		switch gxTag50 := sum.TagResult(stopped); gxTag50 {
		case 1:
			gxMatch48 = "wrong"
		case 2:
			err := sum.ValueErr(stopped)
			gxMatch48 = err
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(gxMatch48, trace)
	}
}
