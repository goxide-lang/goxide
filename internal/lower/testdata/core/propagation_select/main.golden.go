package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"

func typed() sum.Option[int] {
	{
		var goxideTry2 sum.Option[int] = sum.NewSome[int](7)
		var goxideOk3 int
		switch sum.TagOption(goxideTry2) {
		case 1:
			goxideOk3 = sum.ValueSome(goxideTry2)
		case 0:
			return sum.None[int]()
		default:
			panic("goxide: invalid Option at ?")
		}

		n := goxideOk3
		var goxideTry4 sum.Option[any] = sum.NewSome[any](n)
		var goxideOk5 any
		switch sum.TagOption(goxideTry4) {
		case 1:
			goxideOk5 = sum.ValueSome(goxideTry4)
		case 0:
			return sum.None[int]()
		default:
			panic("goxide: invalid Option at ?")
		}
		switch value := goxideOk5.(type) {
		case int:
			return sum.NewSome[int](value + n)
		default:
			return sum.None[int]()

		}
	}
}

func send(ok bool, channel chan int, trace *[]string) sum.Option[int] {
	first := func() chan int { *trace = append(*trace, "channel"); return nil }
	value := func() int { *trace = append(*trace, "value"); return 1 }
	load := func() sum.Option[int] {
		*trace = append(*trace, "load")
		if !ok {
			return sum.None[int]()

		}
		return sum.NewSome[int](7)
	}
	{
		var goxideValue7 chan int = first()
		var goxideValue8 int = value()
		var goxideValue9 chan int = channel
		var goxideTry10 sum.Option[int] = load()
		var goxideOk11 int
		switch sum.TagOption(goxideTry10) {
		case 1:
			goxideOk11 = sum.ValueSome(goxideTry10)
		case 0:
			return sum.None[int]()
		default:
			panic("goxide: invalid Option at ?")
		}
		var goxideValue12 int = goxideOk11
		select {
		case goxideValue7 <- goxideValue8:

			panic("disabled send")
		case goxideValue9 <- goxideValue12:

			return sum.NewSome[int](<-channel)
		}
	}

}

func receive(channel chan int) sum.Option[int] {
	values := []int{0}
	{
		var goxideValue14 chan int = channel
		select {
		case goxideReceive15 := <-goxideValue14:
			var goxideValue16 []int = values
			var goxideTry17 sum.Option[int] = sum.None[int]()
			var goxideOk18 int
			switch sum.TagOption(goxideTry17) {
			case 1:
				goxideOk18 = sum.ValueSome(goxideTry17)
			case 0:
				return sum.None[int]()
			default:
				panic("goxide: invalid Option at ?")
			}
			var goxideValue19 int = goxideOk18
			goxideValue16[goxideValue19] = goxideReceive15

			panic("failed receive target")
		}
	}

	return sum.NewSome[int](values[0])
}

func disabled(ready chan int) sum.Option[int] {
	var blocked chan int
	values := []int{0}
	{
		var goxideValue21 chan int = blocked
		var goxideValue27 chan int = ready
		select {
		case goxideReceive22 := <-goxideValue21:
			var goxideValue23 []int = values
			var goxideTry24 sum.Option[int] = sum.None[int]()
			var goxideOk25 int
			switch sum.TagOption(goxideTry24) {
			case 1:
				goxideOk25 = sum.ValueSome(goxideTry24)
			case 0:
				return sum.None[int]()
			default:
				panic("goxide: invalid Option at ?")
			}
			var goxideValue26 int = goxideOk25
			goxideValue23[goxideValue26] = goxideReceive22

			panic("disabled receive")
		case value := <-goxideValue27:
			return sum.NewSome[int](value)
		}
	}

}

func optionInt(value sum.Option[int]) int {
	{
		var goxideMatch28 int
		var goxidematch29 sum.Option[int] = value
		switch goxidetag31 := sum.TagOption(goxidematch29); goxidetag31 {
		case 1:

			n := sum.ValueSome(goxidematch29)
			goxideMatch28 = n
		case 0:
			goxideMatch28 = -1
		default:
			panic("goxide: invalid enum Option")
		}
		return goxideMatch28
	}
}

func main() {
	fmt.Println(optionInt(typed()))
	for _, ok := range []bool{false, true} {
		trace := []string{}
		channel := make(chan int, 1)
		var value sum.Option[int] = send(ok, channel, &trace)
		fmt.Println(optionInt(value), trace, len(channel))
	}
	channel := make(chan int, 1)
	channel <- 7
	var value sum.Option[int] = receive(channel)
	fmt.Println(optionInt(value), len(channel))
	channel <- 9
	value = disabled(channel)
	fmt.Println(optionInt(value), len(channel))
}
