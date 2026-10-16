package main

import (
	"fmt"
	"github.com/goxide-lang/std/sum"
)

func typed() sum.Option[int] {
	first := sum.NewSome(7)
	if sum.TagOption(first) == 0 {
		return sum.None[int]()
	}
	n := sum.ValueSome(first)
	second := sum.NewSome[any](n)
	if sum.TagOption(second) == 0 {
		return sum.None[int]()
	}
	switch value := sum.ValueSome(second).(type) {
	case int:
		return sum.NewSome(value + n)
	default:
		return sum.None[int]()
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
		return sum.NewSome(7)
	}
	firstChannel, firstValue := first(), value()
	second := load()
	if sum.TagOption(second) == 0 {
		return sum.None[int]()
	}
	select {
	case firstChannel <- firstValue:
		panic("disabled send")
	case channel <- sum.ValueSome(second):
		return sum.NewSome(<-channel)
	}
}
func receive(channel chan int) sum.Option[int] {
	values := []int{0}
	select {
	case received := <-channel:
		index := sum.None[int]()
		if sum.TagOption(index) == 0 {
			return sum.None[int]()
		}
		values[sum.ValueSome(index)] = received
		panic("failed receive target")
	}
}
func disabled(ready chan int) sum.Option[int] {
	var blocked chan int
	values := []int{0}
	select {
	case received := <-blocked:
		index := sum.None[int]()
		if sum.TagOption(index) == 0 {
			return sum.None[int]()
		}
		values[sum.ValueSome(index)] = received
		panic("disabled receive")
	case value := <-ready:
		return sum.NewSome(value)
	}
}
func optionInt(value sum.Option[int]) int {
	switch sum.TagOption(value) {
	case 1:
		return sum.ValueSome(value)
	case 0:
		return -1
	}
	panic("goxide: invalid enum Option")
}
func main() {
	fmt.Println(optionInt(typed()))
	for _, ok := range []bool{false, true} {
		trace := []string{}
		channel := make(chan int, 1)
		value := send(ok, channel, &trace)
		fmt.Println(optionInt(value), trace, len(channel))
	}
	channel := make(chan int, 1)
	channel <- 7
	value := receive(channel)
	fmt.Println(optionInt(value), len(channel))
	channel <- 9
	value = disabled(channel)
	fmt.Println(optionInt(value), len(channel))
}
