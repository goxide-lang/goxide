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
		gxOwner1 := &*counter
		var gxMatch2 _Counter = *gxOwner1
		if gxMatch2 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:111")
		}
		switch gxVar3 := gxMatch2.(type) {
		case *_Count:
			if gxVar3 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:111")
			}
			gxPayloadRef4 := &gxVar3.Value
			value := gxPayloadRef4
			return value
		case *_Empty:
			if gxVar3 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:111")
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
			panic("goxide: match on nil Counter at referencefixture/main.gox:323")
		}
		switch gxVar6 := counter.(type) {
		case *_Count:
			if gxVar6 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:323")
			}
			value := gxVar6.Value
			gxMatch5 = value
		case *_Empty:
			if gxVar6 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:323")
			}
			gxMatch5 = 0
		default:
			panic("goxide: invalid enum Counter")
		}
		return gxMatch5
	}
}
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
func optionProject(option *sum.Option[int]) *int {
	{
		gxOwner10 := &*option
		var gxMatch11 sum.Option[int] = *gxOwner10
		switch gxTag14 := sum.TagOption(gxMatch11); gxTag14 {
		case 1:
			gxPayloadRef13 := sum.ProjectSome(gxOwner10)

			value := gxPayloadRef13
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
		var gxMatch15 int
		switch gxTag17 := sum.TagResult(result); gxTag17 {
		case 1:
			value := sum.ValueOk(result)
			gxMatch15 = value
		case 2:
			gxMatch15 = 0
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch15
	}
}
func resultProject(result *sum.Result[int, string]) *int {
	{
		gxOwner18 := &*result
		var gxMatch19 sum.Result[int, string] = *gxOwner18
		switch gxTag22 := sum.TagResult(gxMatch19); gxTag22 {
		case 1:
			gxPayloadRef21 := sum.ProjectOk(gxOwner18)

			value := gxPayloadRef21
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
		gxOwner23 := &counter
		var gxMatch24 _Counter = *gxOwner23
		if gxMatch24 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:1200")
		}
		switch gxVar25 := gxMatch24.(type) {
		case *_Count:
			if gxVar25 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:1200")
			}
			gxPayloadRef26 := &gxVar25.Value
			value := gxPayloadRef26
			*value += 1
		case *_Empty:
			if gxVar25 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:1200")
			}
		default:
			panic("goxide: invalid enum Counter")
		}
	}

	*saved += 3
	fmt.Println(read(counter), read(before))
	{
		gxOwner27 := &counter
		var gxMatch28 _Counter = *gxOwner27
		if gxMatch28 == nil {
			panic("goxide: match on nil Counter at referencefixture/main.gox:1373")
		}
		switch gxVar29 := gxMatch28.(type) {
		case *_Count:
			if gxVar29 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:1373")
			}
			gxPayloadRef30 := &gxVar29.Value
			value := gxPayloadRef30
			fmt.Println(*value)
		case *_Empty:
			if gxVar29 == nil {
				panic("goxide: match on nil Counter at referencefixture/main.gox:1373")
			}
		default:
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
