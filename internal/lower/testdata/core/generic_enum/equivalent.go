package main

import (
	"fmt"
	"github.com/goxide-lang/std/sum"
)

type Box[T any] interface{ isBox(T) }
type Empty[T any] struct{}
type Item[T any] struct{ Value T }

func (*Empty[T]) isBox(T) {}
func (*Item[T]) isBox(T)  {}

type Parcel[T any] interface{ isparcel(T) }
type Packed[T any] struct{ Value T }
type Vacant[T any] struct{}

func (*Packed[T]) isparcel(T) {}
func (*Vacant[T]) isparcel(T) {}

type _Secret interface{ isSecret() }
type _Hidden struct{ Value int }

func (*_Hidden) isSecret() {}

var global Box[int] = &Item[int]{8}
var globalText = fmt.Sprint(integer(global))

func reveal(x _Secret) int {
	if x == nil {
		panic("goxide: match on nil Secret")
	}
	switch v := x.(type) {
	case *_Hidden:
		return v.Value
	}
	panic("unreachable")
}
func parcelText(x Parcel[string]) string {
	if x == nil {
		panic("goxide: match on nil parcel")
	}
	switch v := x.(type) {
	case *Packed[string]:
		return v.Value
	case *Vacant[string]:
		return "empty"
	}
	panic("unreachable")
}

func integer(x Box[int]) int {
	if x == nil {
		panic("goxide: match on nil Box")
	}
	switch v := x.(type) {
	case *Item[int]:
		return v.Value
	case *Empty[int]:
		return -1
	}
	panic("unreachable")
}

func text(x Box[string]) string {
	if x == nil {
		panic("goxide: match on nil Box")
	}
	switch v := x.(type) {
	case *Item[string]:
		return v.Value
	case *Empty[string]:
		return "empty"
	}
	panic("unreachable")
}

func optionInt(x sum.Option[int]) int {
	switch sum.TagOption(x) {
	case 1:
		return sum.ValueSome(x)
	case 0:
		return -1
	}
	panic("goxide: invalid enum Option")
}

func optionText(x sum.Option[string]) string {
	switch sum.TagOption(x) {
	case 1:
		return sum.ValueSome(x)
	case 0:
		return "none"
	}
	panic("goxide: invalid enum Option")
}

func presentNil(x sum.Option[*int]) bool {
	switch sum.TagOption(x) {
	case 1:
		return sum.ValueSome(x) == nil
	case 0:
		return false
	}
	panic("goxide: invalid enum Option")
}

func main() {
	global = &Empty[int]{}
	global = &Item[int]{9}
	fmt.Println(integer(&Item[int]{42}), text(&Item[string]{"hello"}), optionInt(sum.NewSome(3)), optionText(sum.NewSome("world")), presentNil(sum.NewSome[*int](nil)), reveal(&_Hidden{7}), parcelText(&Packed[string]{"mapped"}), globalText, integer(global))
}
