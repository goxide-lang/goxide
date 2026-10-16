package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"

func typed() sum.Option[int] {
	{
		var gxTry2 sum.Option[int] = sum.NewSome[int](7)
		var gxOk3 int
		switch sum.TagOption(gxTry2) {
		case 1:
			gxOk3 = sum.ValueSome(gxTry2)
		case 0:
			return sum.None[int]()
		default:
			panic("goxide: invalid Option at ?")
		}

		n := gxOk3
		var gxTry4 sum.Option[any] = sum.NewSome[any](n)
		var gxOk5 any
		switch sum.TagOption(gxTry4) {
		case 1:
			gxOk5 = sum.ValueSome(gxTry4)
		case 0:
			return sum.None[int]()
		default:
			panic("goxide: invalid Option at ?")
		}
		switch value := gxOk5.(type) {
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
		var gxValue7 chan int = first()
		var gxValue8 int = value()
		var gxValue9 chan int = channel
		var gxTry10 sum.Option[int] = load()
		var gxOk11 int
		switch sum.TagOption(gxTry10) {
		case 1:
			gxOk11 = sum.ValueSome(gxTry10)
		case 0:
			return sum.None[int]()
		default:
			panic("goxide: invalid Option at ?")
		}
		var gxValue12 int = gxOk11
		select {
		case gxValue7 <- gxValue8:

			panic("disabled send")
		case gxValue9 <- gxValue12:

			return sum.NewSome[int](<-channel)
		}
	}

}

func receive(channel chan int) sum.Option[int] {
	values := []int{0}
	{
		var gxValue14 chan int = channel
		select {
		case gxReceive15 := <-gxValue14:
			var gxValue16 []int = values
			var gxTry17 sum.Option[int] = sum.None[int]()
			var gxOk18 int
			switch sum.TagOption(gxTry17) {
			case 1:
				gxOk18 = sum.ValueSome(gxTry17)
			case 0:
				return sum.None[int]()
			default:
				panic("goxide: invalid Option at ?")
			}
			var gxValue19 int = gxOk18
			gxValue16[gxValue19] = gxReceive15

			panic("failed receive target")
		}
	}

	return sum.NewSome[int](values[0])
}

func disabled(ready chan int) sum.Option[int] {
	var blocked chan int
	values := []int{0}
	{
		var gxValue21 chan int = blocked
		var gxValue27 chan int = ready
		select {
		case gxReceive22 := <-gxValue21:
			var gxValue23 []int = values
			var gxTry24 sum.Option[int] = sum.None[int]()
			var gxOk25 int
			switch sum.TagOption(gxTry24) {
			case 1:
				gxOk25 = sum.ValueSome(gxTry24)
			case 0:
				return sum.None[int]()
			default:
				panic("goxide: invalid Option at ?")
			}
			var gxValue26 int = gxOk25
			gxValue23[gxValue26] = gxReceive22

			panic("disabled receive")
		case value := <-gxValue27:
			return sum.NewSome[int](value)
		}
	}

}

func optionInt(value sum.Option[int]) int {
	{
		var gxMatch28 int
		switch gxTag30 := sum.TagOption(value); gxTag30 {
		case 1:
			n := sum.ValueSome(value)
			gxMatch28 = n
		case 0:
			gxMatch28 = -1
		default:
			panic("goxide: invalid enum Option")
		}
		return gxMatch28
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
