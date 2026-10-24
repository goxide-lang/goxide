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

func project(counter *_Counter) *int {
	{
		gxOwner1 := &*counter
		var gxMatch2 _Counter = *gxOwner1
		if gxMatch2 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:8")
		}
		switch gxVar3 := gxMatch2.(type) {
		case *_Count:
			if gxVar3 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:8")
			}
			gxPayloadRef4 := &gxVar3.Value
			value := gxPayloadRef4
			return value
		case *_Empty:
			if gxVar3 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:8")
			}
			panic("empty counter")
		default:
			panic("goxide: invalid enum Counter")
		}
	}

	panic("exhaustive match")
}
func read(counter _Counter) int {
	{
		var gxMatch5 int
		if counter == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:15")
		}
		switch gxVar6 := counter.(type) {
		case *_Count:
			if gxVar6 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:15")
			}
			value := gxVar6.Value
			gxMatch5 = value
		case *_Empty:
			if gxVar6 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:15")
			}
			gxMatch5 = 0
		default:
			panic("goxide: invalid enum Counter")
		}
		return gxMatch5
	}
}
func replace(counter *_Counter, value int) { *counter = &_Count{Value: value} }
func optionValue(option sum.Option[int]) int {
	{
		var gxMatch7 int
		switch gxTag9 := sum.TagOption(option); gxTag9 {
		case 1:
			value := sum.ValueSome(option)
			gxMatch7 = value
		case 0:
			gxMatch7 = 0
		default:
			panic("goxide: invalid enum Option")
		}
		return gxMatch7
	}
}
func resultValue(result sum.Result[int, string]) int {
	{
		var gxMatch10 int
		switch gxTag12 := sum.TagResult(result); gxTag12 {
		case 1:
			value := sum.ValueOk(result)
			gxMatch10 = value
		case 2:
			gxMatch10 = 0
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch10
	}
}

func main() {
	var counter _Counter = &_Count{Value: 1}
	var before _Counter = counter
	saved := project(&counter)
	same := saved == project(&counter)
	replace(&counter, 10)
	fmt.Println(same, read(counter), read(before), *saved)
	counter = &_Empty{}
	fmt.Println(read(counter), read(before), *saved)
	counter = &_Count{Value: 20}
	fmt.Println(read(counter), *saved)
	var option sum.Option[int] = sum.NewSome[int](1)
	var copiedOption sum.Option[int] = option
	option = sum.NewSome[int](2)
	first, copied := optionValue(option), optionValue(copiedOption)
	option = sum.None[int]()

	fmt.Println(first, copied, optionValue(option))
	var result sum.Result[int, string] = sum.NewOk[int, string](3)
	var copiedResult sum.Result[int, string] = result
	result = sum.NewErr[int, string]("changed")
	fmt.Println(resultValue(result), resultValue(copiedResult))
}
