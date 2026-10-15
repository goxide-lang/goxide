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
	var goxideTry1 sum.Result[int, string] = tryIndex(fail)
	var goxideOk2 int
	switch sum.TagResult(goxideTry1) {
	case 1:
		goxideOk2 = sum.ValueOk(goxideTry1)
	case 2:
		return sum.NewErr[int, string](sum.ValueErr(goxideTry1))
	default:
		panic("goxide: invalid Result at ?")
	}
	var goxideValue3 int = goxideOk2

	element := &values[goxideValue3]
	*element += 4
	choices := [1]_Counter{&_Count{Value: 2}}
	{
		var goxideTry4 sum.Result[int, string] = tryIndex(false)
		var goxideOk5 int
		switch sum.TagResult(goxideTry4) {
		case 1:
			goxideOk5 = sum.ValueOk(goxideTry4)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(goxideTry4))
		default:
			panic("goxide: invalid Result at ?")
		}
		var goxideValue6 int = goxideOk5
		goxideOwner7 := &choices[goxideValue6]
		var goxidematch8 _Counter = *goxideOwner7
		if goxidematch8 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:850")
		}
		switch goxidevariant9 := goxidematch8.(type) {
		case *_Count:
			_ = goxidevariant9
			if goxidevariant9 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:850")
			}
			goxidePayloadRef10 := &goxidevariant9.Value
			value := goxidePayloadRef10
			*value += 3
		case *_Empty:
			_ = goxidevariant9
			if goxidevariant9 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:850")
			}
		default:
			_ = goxidevariant9
			panic("goxide: invalid enum Counter")
		}
	}
	var goxideMatch11 int
	var goxidematch12 _Counter = choices[0]
	if goxidematch12 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:990")
	}
	switch goxidevariant13 := goxidematch12.(type) {
	case *_Count:
		_ = goxidevariant13
		if goxidevariant13 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:990")
		}
		value := goxidevariant13.Value
		goxideMatch11 = value
	case *_Empty:
		_ = goxidevariant13
		if goxidevariant13 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:990")
		}
		goxideMatch11 = 0
	default:
		_ = goxidevariant13
		goxideMatch11 = 0
	}
	current := goxideMatch11
	return sum.NewOk[int, string](values[0] + current)
}
func controlled(fail bool) sum.Result[int, string] {
	defer func() { trace += "done" }()
	var counter _Counter = &_Count{Value: 0}
goxideBreak14:
	for i := 0; i < 3; i++ {
		{
			goxideOwner15 := &counter
			var goxidematch16 _Counter = *goxideOwner15
			if goxidematch16 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:1260")
			}
			switch goxidevariant17 := goxidematch16.(type) {
			case *_Count:
				_ = goxidevariant17
				if goxidevariant17 == nil {
					panic("goxide: match on nil Counter at referencefixture/main.gox:1260")
				}
				goxidePayloadRef18 := &goxidevariant17.Value
				value := goxidePayloadRef18
				if i == 0 {
					*value += 1
					continue
				}
				*value += 2
				break goxideBreak14
			case *_Empty:
				_ = goxidevariant17
				if goxidevariant17 == nil {
					panic("goxide: match on nil Counter at referencefixture/main.gox:1260")
				}
			default:
				_ = goxidevariant17
				panic("goxide: invalid enum Counter")
			}
		}

		panic("match changed loop control")
	}
	var goxideMatch19 int
	goxideOwner20 := &counter
	var goxidematch21 _Counter = *goxideOwner20
	if goxidematch21 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:1542")
	}
	switch goxidevariant22 := goxidematch21.(type) {
	case *_Count:
		_ = goxidevariant22
		if goxidevariant22 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:1542")
		}
		goxidePayloadRef23 := &goxidevariant22.Value
		{
			value := goxidePayloadRef23
			var goxideTry24 sum.Result[bool, string] = allowed(fail)
			var goxideOk25 bool
			switch sum.TagResult(goxideTry24) {
			case 1:
				goxideOk25 = sum.ValueOk(goxideTry24)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(goxideTry24))
			default:
				panic("goxide: invalid Result at ?")
			}
			if goxideOk25 {
				goxideMatch19 = *value
			} else {
				goxideMatch19 = -1
			}
		}
	case *_Empty:
		_ = goxidevariant22
		if goxidevariant22 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:1542")
		}
		goxideMatch19 = -2
	default:
		_ = goxidevariant22
		panic("goxide: invalid enum Counter")
	}
	value := goxideMatch19

	return sum.NewOk[int, string](value)
}

func main() {
	var counter _Counter = &_Count{Value: 1}
	var goxideMatch26 int
	goxideOwner27 := &counter
	var goxidematch28 _Counter = *goxideOwner27
	if goxidematch28 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:1794")
	}
	switch goxidevariant29 := goxidematch28.(type) {
	case *_Count:
		_ = goxidevariant29
		if goxidevariant29 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:1794")
		}
		goxidePayload30 := goxidevariant29.Value
		goxidePayloadRef31 := &goxidevariant29.Value
		if value := goxidePayloadRef31; alter(value) {
			goxideMatch26 = -1
		} else {
			value := goxidePayload30
			goxideMatch26 = value
		}
	case *_Empty:
		_ = goxidevariant29
		if goxidevariant29 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:1794")
		}
		goxideMatch26 = 0
	default:
		_ = goxidevariant29
		panic("goxide: invalid enum Counter")
	}
	snapshot := goxideMatch26
	var goxideMatch32 int
	var goxidematch33 _Counter = counter
	if goxidematch33 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:1961")
	}
	switch goxidevariant34 := goxidematch33.(type) {
	case *_Count:
		_ = goxidevariant34
		if goxidevariant34 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:1961")
		}
		value := goxidevariant34.Value
		goxideMatch32 = value
	case *_Empty:
		_ = goxidevariant34
		if goxidevariant34 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:1961")
		}
		goxideMatch32 = 0
	default:
		_ = goxidevariant34
		panic("goxide: invalid enum Counter")
	}
	current := goxideMatch32
	fmt.Println(snapshot, current)
	var goxideMatch35 int
	goxideOwner36 := &owners()[index()]
	var goxidematch37 _Counter = *goxideOwner36
	if goxidematch37 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:2083")
	}
	switch goxidevariant38 := goxidematch37.(type) {
	case *_Count:
		_ = goxidevariant38
		if goxidevariant38 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:2083")
		}
		goxidePayloadRef39 := &goxidevariant38.Value
		if value := goxidePayloadRef39; guard(value) {
			goxideMatch35 = -1
		} else {
			value := goxidePayloadRef39
			goxideMatch35 = *value
		}
	case *_Empty:
		_ = goxidevariant38
		if goxidevariant38 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:2083")
		}
		goxideMatch35 = 0
	default:
		_ = goxidevariant38
		panic("goxide: invalid enum Counter")
	}
	observed := goxideMatch35

	fmt.Println(observed, trace)
	trace = ""
	var success sum.Result[int, string] = controlled(false)
	{
		var goxideMatch40 any
		var goxidematch41 sum.Result[int, string] = success
		switch goxidetag43 := sum.TagResult(goxidematch41); goxidetag43 {
		case 1:

			value := sum.ValueOk(goxidematch41)
			goxideMatch40 = value
		case 2:
			goxideMatch40 = -1
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(goxideMatch40, trace)
	}
	trace = ""
	var failure sum.Result[int, string] = controlled(true)
	{
		var goxideMatch44 any
		var goxidematch45 sum.Result[int, string] = failure
		switch goxidetag47 := sum.TagResult(goxidematch45); goxidetag47 {
		case 1:
			goxideMatch44 = "wrong"
		case 2:
			err := sum.ValueErr(goxidematch45)
			goxideMatch44 = err
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(goxideMatch44, trace)
	}
	trace = ""
	var array sum.Result[int, string] = arrayProjection(false)
	{
		var goxideMatch48 any
		var goxidematch49 sum.Result[int, string] = array
		switch goxidetag51 := sum.TagResult(goxidematch49); goxidetag51 {
		case 1:

			value := sum.ValueOk(goxidematch49)
			goxideMatch48 = value
		case 2:
			goxideMatch48 = -1
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(goxideMatch48, trace)
	}
	trace = ""
	var stopped sum.Result[int, string] = arrayProjection(true)
	{
		var goxideMatch52 any
		var goxidematch53 sum.Result[int, string] = stopped
		switch goxidetag55 := sum.TagResult(goxidematch53); goxidetag55 {
		case 1:
			goxideMatch52 = "wrong"
		case 2:
			err := sum.ValueErr(goxidematch53)
			goxideMatch52 = err
		default:
			panic("goxide: invalid enum Result")
		}
		fmt.Println(goxideMatch52, trace)
	}
}
