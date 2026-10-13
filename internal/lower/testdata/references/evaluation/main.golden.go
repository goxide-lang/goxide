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
	var goxideArray1 *[1]int = &values
	var goxideTry2 sum.Result[int, string] = tryIndex(fail)
	var goxideOk3 int
	switch sum.TagResult(goxideTry2) {
	case 1:
		goxideOk3 = sum.ValueOk(goxideTry2)
	case 2:
		return sum.NewErr[int, string](sum.ValueErr(goxideTry2))
	default:
		panic("goxide: invalid Result at ?")
	}
	var goxideValue4 int = goxideOk3

	element := &goxideArray1[goxideValue4]
	*element += 4
	choices := [1]_Counter{&_Count{Value: 2}}
	{
		var goxideArray5 *[1]_Counter = &choices
		var goxideTry6 sum.Result[int, string] = tryIndex(false)
		var goxideOk7 int
		switch sum.TagResult(goxideTry6) {
		case 1:
			goxideOk7 = sum.ValueOk(goxideTry6)
		case 2:
			return sum.NewErr[int, string](sum.ValueErr(goxideTry6))
		default:
			panic("goxide: invalid Result at ?")
		}
		var goxideValue8 int = goxideOk7
		goxideOwner9 := &goxideArray5[goxideValue8]
		var goxidematch10 _Counter = *goxideOwner9
		if goxidematch10 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:850")
		}
		switch goxidevariant11 := goxidematch10.(type) {
		case *_Count:
			_ = goxidevariant11
			goxidePayloadRef12 := &goxidevariant11.Value

			value := goxidePayloadRef12
			*value += 3
		case *_Empty:
			_ = goxidevariant11
		default:
			_ = goxidevariant11
			panic("goxide: invalid enum Counter")
		}
	}
	var goxideMatch13 int
	var goxidematch14 _Counter = choices[0]
	if goxidematch14 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:990")
	}
	switch goxidevariant15 := goxidematch14.(type) {
	case *_Count:
		_ = goxidevariant15
		value := goxidevariant15.Value
		goxideMatch13 = value
	default:
		_ = goxidevariant15
		goxideMatch13 = 0
	}
	current := goxideMatch13
	return sum.NewOk[int, string](values[0] + current)
}
func controlled(fail bool) sum.Result[int, string] {
	defer func() { trace += "done" }()
	var counter _Counter = &_Count{Value: 0}
goxideBreak16:
	for i := 0; i < 3; i++ {
		{
			goxideOwner17 := &counter
			var goxidematch18 _Counter = *goxideOwner17
			if goxidematch18 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:1260")
			}
			switch goxidevariant19 := goxidematch18.(type) {
			case *_Count:
				_ = goxidevariant19
				goxidePayloadRef20 := &goxidevariant19.Value
				value := goxidePayloadRef20
				if i == 0 {
					*value += 1
					continue
				}
				*value += 2
				break goxideBreak16
			case *_Empty:
				_ = goxidevariant19
			default:
				_ = goxidevariant19
				panic("goxide: invalid enum Counter")
			}
		}

		panic("match changed loop control")
	}
	var goxideMatch21 int
	goxideOwner22 := &counter
	var goxidematch23 _Counter = *goxideOwner22
	if goxidematch23 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:1542")
	}
	switch goxidevariant24 := goxidematch23.(type) {
	case *_Count:
		_ = goxidevariant24
		goxidePayloadRef25 := &goxidevariant24.Value
		{
			value := goxidePayloadRef25
			var goxideTry26 sum.Result[bool, string] = allowed(fail)
			var goxideOk27 bool
			switch sum.TagResult(goxideTry26) {
			case 1:
				goxideOk27 = sum.ValueOk(goxideTry26)
			case 2:
				return sum.NewErr[int, string](sum.ValueErr(goxideTry26))
			default:
				panic("goxide: invalid Result at ?")
			}
			if goxideOk27 {
				goxideMatch21 = *value
			} else {
				goxideMatch21 = -1
			}
		}
	case *_Empty:
		_ = goxidevariant24
		goxideMatch21 = -2
	default:
		_ = goxidevariant24
		panic("goxide: invalid enum Counter")
	}
	value := goxideMatch21

	return sum.NewOk[int, string](value)
}

func main() {
	var counter _Counter = &_Count{Value: 1}
	var goxideMatch28 int
	goxideOwner29 := &counter
	var goxidematch30 _Counter = *goxideOwner29
	if goxidematch30 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:1794")
	}
	switch goxidevariant31 := goxidematch30.(type) {
	case *_Count:
		_ = goxidevariant31
		goxidePayload32 := goxidevariant31.Value
		goxidePayloadRef33 := &goxidevariant31.Value
		if value := goxidePayloadRef33; alter(value) {
			goxideMatch28 = -1
		} else {
			value := goxidePayload32
			goxideMatch28 = value
		}
	case *_Empty:
		_ = goxidevariant31
		goxideMatch28 = 0
	default:
		_ = goxidevariant31
		panic("goxide: invalid enum Counter")
	}
	snapshot := goxideMatch28
	var goxideMatch34 int
	var goxidematch35 _Counter = counter
	if goxidematch35 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:1961")
	}
	switch goxidevariant36 := goxidematch35.(type) {
	case *_Count:
		_ = goxidevariant36
		value := goxidevariant36.Value
		goxideMatch34 = value
	case *_Empty:
		_ = goxidevariant36
		goxideMatch34 = 0
	default:
		_ = goxidevariant36
		panic("goxide: invalid enum Counter")
	}
	current := goxideMatch34
	fmt.Println(snapshot, current)
	var goxideMatch37 int
	goxideOwner38 := &owners()[index()]
	var goxidematch39 _Counter = *goxideOwner38
	if goxidematch39 == nil {
		panic("goxide: match on nil Counter at referencefixture/main.gox:2083")
	}
	switch goxidevariant40 := goxidematch39.(type) {
	case *_Count:
		_ = goxidevariant40
		goxidePayloadRef41 := &goxidevariant40.Value
		if value := goxidePayloadRef41; guard(value) {
			goxideMatch37 = -1
		} else {
			value := goxidePayloadRef41
			goxideMatch37 = *value
		}
	case *_Empty:
		_ = goxidevariant40
		goxideMatch37 = 0
	default:
		_ = goxidevariant40
		panic("goxide: invalid enum Counter")
	}
	observed := goxideMatch37

	fmt.Println(observed, trace)
	trace = ""
	var success sum.Result[int, string] = controlled(false)
	{
		var goxideValue42 func(a ...any) (n int, err error) = fmt.Println
		var goxideMatch43 any
		var goxidematch44 sum.Result[int, string] = success
		switch goxidetag46 := sum.TagResult(goxidematch44); goxidetag46 {
		case 1:

			value := sum.ValueOk(goxidematch44)
			goxideMatch43 = value
		case 2:
			goxideMatch43 = -1
		default:
			panic("goxide: invalid enum Result")
		}
		goxideValue42(goxideMatch43, trace)
	}
	trace = ""
	var failure sum.Result[int, string] = controlled(true)
	{
		var goxideValue47 func(a ...any) (n int, err error) = fmt.Println
		var goxideMatch48 any
		var goxidematch49 sum.Result[int, string] = failure
		switch goxidetag51 := sum.TagResult(goxidematch49); goxidetag51 {
		case 1:
			goxideMatch48 = "wrong"
		case 2:
			err := sum.ValueErr(goxidematch49)
			goxideMatch48 = err
		default:
			panic("goxide: invalid enum Result")
		}
		goxideValue47(goxideMatch48, trace)
	}
	trace = ""
	var array sum.Result[int, string] = arrayProjection(false)
	{
		var goxideValue52 func(a ...any) (n int, err error) = fmt.Println
		var goxideMatch53 any
		var goxidematch54 sum.Result[int, string] = array
		switch goxidetag56 := sum.TagResult(goxidematch54); goxidetag56 {
		case 1:

			value := sum.ValueOk(goxidematch54)
			goxideMatch53 = value
		case 2:
			goxideMatch53 = -1
		default:
			panic("goxide: invalid enum Result")
		}
		goxideValue52(goxideMatch53, trace)
	}
	trace = ""
	var stopped sum.Result[int, string] = arrayProjection(true)
	{
		var goxideValue57 func(a ...any) (n int, err error) = fmt.Println
		var goxideMatch58 any
		var goxidematch59 sum.Result[int, string] = stopped
		switch goxidetag61 := sum.TagResult(goxidematch59); goxidetag61 {
		case 1:
			goxideMatch58 = "wrong"
		case 2:
			err := sum.ValueErr(goxidematch59)
			goxideMatch58 = err
		default:
			panic("goxide: invalid enum Result")
		}
		goxideValue57(goxideMatch58, trace)
	}
}
