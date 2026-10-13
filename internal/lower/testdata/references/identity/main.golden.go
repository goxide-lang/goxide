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

func project(counter *_Counter) *int {
	{
		goxideOwner1 := &*counter
		var goxidematch2 _Counter = *goxideOwner1
		if goxidematch2 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:111")
		}
		switch goxidevariant3 := goxidematch2.(type) {
		case *_Count:
			_ = goxidevariant3
			goxidePayloadRef4 := &goxidevariant3.Value
			value := goxidePayloadRef4
			return value
		case *_Empty:
			_ = goxidevariant3
			panic("empty counter")
		default:
			_ = goxidevariant3
			panic("goxide: invalid enum Counter")
		}
	}

	panic("exhaustive match")
}
func read(counter _Counter) int {
	{
		var goxideMatch5 int
		var goxidematch6 _Counter = counter
		if goxidematch6 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:323")
		}
		switch goxidevariant7 := goxidematch6.(type) {
		case *_Count:
			_ = goxidevariant7
			value := goxidevariant7.Value
			goxideMatch5 = value
		case *_Empty:
			_ = goxidevariant7
			goxideMatch5 = 0
		default:
			_ = goxidevariant7
			panic("goxide: invalid enum Counter")
		}
		return goxideMatch5
	}
}
func optionValue(option sum.Option[int]) int {
	{
		var goxideMatch8 int
		var goxidematch9 sum.Option[int] = option
		switch goxidetag11 := sum.TagOption(goxidematch9); goxidetag11 {
		case 1:

			value := sum.ValueSome(goxidematch9)
			goxideMatch8 = value
		case 0:
			goxideMatch8 = 0
		default:
			panic("goxide: invalid enum Option")
		}
		return goxideMatch8
	}
}
func optionProject(option *sum.Option[int]) *int {
	{
		goxideOwner12 := &*option
		var goxidematch13 sum.Option[int] = *goxideOwner12
		switch goxidetag16 := sum.TagOption(goxidematch13); goxidetag16 {
		case 1:
			goxidePayloadRef15 := sum.ProjectSome(goxideOwner12)

			value := goxidePayloadRef15
			return value
		case 0:
			panic("empty option")
		default:
			panic("goxide: invalid enum Option")
		}
	}

	panic("exhaustive match")
}
func resultValue(result sum.Result[int, string]) int {
	{
		var goxideMatch17 int
		var goxidematch18 sum.Result[int, string] = result
		switch goxidetag20 := sum.TagResult(goxidematch18); goxidetag20 {
		case 1:

			value := sum.ValueOk(goxidematch18)
			goxideMatch17 = value
		case 2:
			goxideMatch17 = 0
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch17
	}
}
func resultProject(result *sum.Result[int, string]) *int {
	{
		goxideOwner21 := &*result
		var goxidematch22 sum.Result[int, string] = *goxideOwner21
		switch goxidetag25 := sum.TagResult(goxidematch22); goxidetag25 {
		case 1:
			goxidePayloadRef24 := sum.ProjectOk(goxideOwner21)

			value := goxidePayloadRef24
			return value
		case 2:
			panic("failed result")
		default:
			panic("goxide: invalid enum Result")
		}
	}

	panic("exhaustive match")
}

func main() {
	var counter _Counter = &_Count{Value: 1}
	var before _Counter = counter
	saved := project(&counter)
	{
		goxideOwner26 := &counter
		var goxidematch27 _Counter = *goxideOwner26
		if goxidematch27 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:1200")
		}
		switch goxidevariant28 := goxidematch27.(type) {
		case *_Count:
			_ = goxidevariant28
			goxidePayloadRef29 := &goxidevariant28.Value
			value := goxidePayloadRef29
			*value += 1
		case *_Empty:
			_ = goxidevariant28
		default:
			_ = goxidevariant28
			panic("goxide: invalid enum Counter")
		}
	}

	*saved += 3
	fmt.Println(read(counter), read(before))
	{
		goxideOwner30 := &counter
		var goxidematch31 _Counter = *goxideOwner30
		if goxidematch31 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:1373")
		}
		switch goxidevariant32 := goxidematch31.(type) {
		case *_Count:
			_ = goxidevariant32
			goxidePayloadRef33 := &goxidevariant32.Value
			value := goxidePayloadRef33
			fmt.Println(*value)
		case *_Empty:
			_ = goxidevariant32
		default:
			_ = goxidevariant32
			panic("goxide: invalid enum Counter")
		}
	}

	same := saved == project(&counter)
	counter = &_Count{Value: 10}
	*saved = 7
	fmt.Println(same, read(counter), read(before), *saved)
	counter = &_Empty{}
	*saved = 8
	fmt.Println(read(before), *saved)
	counter = &_Count{Value: 20}
	fmt.Println(read(counter), *saved)
	var option sum.Option[int] = sum.NewSome[int](1)
	var copiedOption sum.Option[int] = option
	optionRef := optionProject(&option)
	*optionRef = 2
	first, copied := optionValue(option), optionValue(copiedOption)
	option = sum.NewSome[int](3)
	replaced := *optionRef
	option = sum.None[int]()

	cleared := *optionRef
	*optionRef = 4
	option = sum.NewSome[int](5)
	fmt.Println(first, copied, replaced, cleared, *optionRef)
	var result sum.Result[int, string] = sum.NewOk[int, string](3)
	var copiedResult sum.Result[int, string] = result
	resultRef := resultProject(&result)
	*resultRef = 4
	written, original := resultValue(result), resultValue(copiedResult)
	result = sum.NewErr[int, string]("changed")
	clearedResult := *resultRef
	result = sum.NewOk[int, string](9)
	fmt.Println(written, original, clearedResult, *resultRef)
}
