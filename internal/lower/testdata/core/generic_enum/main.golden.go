package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"

type Box[T any] interface {
	isBox(T)
}
type Empty[T any] struct {
}

func (*Empty[T]) isBox(T) {}

type Item[T any] struct {
	Value T
}

func (*Item[T]) isBox(T) {}

type Parcel[T any] interface {
	isparcel(T)
}
type Packed[T any] struct {
	Value T
}

func (*Packed[T]) isparcel(T) {}

type Vacant[T any] struct {
}

func (*Vacant[T]) isparcel(T) {}

type _Secret interface {
	isSecret()
}
type _Hidden struct {
	Value int
}

func (*_Hidden) isSecret() {
}

var global Box[int] = &Item[int]{Value: 8}
var globalText = func() string {
	var goxideMatch1 string
	var goxidematch2 Box[int] = global
	if goxidematch2 == nil {
		panic("goxide: match on nil Box at corefixture/main.gox:185")
	}
	switch goxidevariant3 := goxidematch2.(type) {
	case *Item[int]:
		_ = goxidevariant3
		n := goxidevariant3.Value
		goxideMatch1 = fmt.Sprint(n)
	case *Empty[int]:
		_ = goxidevariant3
		goxideMatch1 = "empty"
	default:
		_ = goxidevariant3
		panic("goxide: invalid enum Box")
	}
	return goxideMatch1
}()

func reveal(x _Secret) int {
	{
		var goxideMatch4 int
		var goxidematch5 _Secret = x
		if goxidematch5 == nil {
			panic("goxide: match on nil Secret at corefixture/main.gox:293")
		}
		switch goxidevariant6 := goxidematch5.(type) {
		case *_Hidden:
			_ = goxidevariant6
			n := goxidevariant6.Value
			goxideMatch4 = n
		default:
			_ = goxidevariant6
			panic("goxide: invalid enum Secret")
		}
		return goxideMatch4
	}
}

func parcelText(x Parcel[string]) string {
	{
		var goxideMatch7 string
		var goxidematch8 Parcel[string] = x
		if goxidematch8 == nil {
			panic("goxide: match on nil parcel at corefixture/main.gox:382")
		}
		switch goxidevariant9 := goxidematch8.(type) {
		case *Packed[string]:
			_ = goxidevariant9
			s := goxidevariant9.Value
			goxideMatch7 = s
		case *Vacant[string]:
			_ = goxidevariant9
			goxideMatch7 = "empty"
		default:
			_ = goxidevariant9
			panic("goxide: invalid enum parcel")
		}
		return goxideMatch7
	}
}

func integer(x Box[int]) int {
	{
		var goxideMatch10 int
		var goxidematch11 Box[int] = x
		if goxidematch11 == nil {
			panic("goxide: match on nil Box at corefixture/main.gox:486")
		}
		switch goxidevariant12 := goxidematch11.(type) {
		case *Item[int]:
			_ = goxidevariant12
			n := goxidevariant12.Value
			goxideMatch10 = n
		case *Empty[int]:
			_ = goxidevariant12
			goxideMatch10 = -1
		default:
			_ = goxidevariant12
			panic("goxide: invalid enum Box")
		}
		return goxideMatch10
	}
}

func text(x Box[string]) string {
	{
		var goxideMatch13 string
		var goxidematch14 Box[string] = x
		if goxidematch14 == nil {
			panic("goxide: match on nil Box at corefixture/main.gox:579")
		}
		switch goxidevariant15 := goxidematch14.(type) {
		case *Item[string]:
			_ = goxidevariant15
			s := goxidevariant15.Value
			goxideMatch13 = s
		case *Empty[string]:
			_ = goxidevariant15
			goxideMatch13 = "empty"
		default:
			_ = goxidevariant15
			panic("goxide: invalid enum Box")
		}
		return goxideMatch13
	}
}

func optionInt(x sum.Option[int]) int {
	{
		var goxideMatch16 int
		var goxidematch17 sum.Option[int] = x
		switch goxidetag19 := sum.TagOption(goxidematch17); goxidetag19 {
		case 1:

			n := sum.ValueSome(goxidematch17)
			goxideMatch16 = n
		case 0:
			goxideMatch16 = -1
		default:
			panic("goxide: invalid enum Option")
		}
		return goxideMatch16
	}
}

func optionText(x sum.Option[string]) string {
	{
		var goxideMatch20 string
		var goxidematch21 sum.Option[string] = x
		switch goxidetag23 := sum.TagOption(goxidematch21); goxidetag23 {
		case 1:

			s := sum.ValueSome(goxidematch21)
			goxideMatch20 = s
		case 0:
			goxideMatch20 = "none"
		default:
			panic("goxide: invalid enum Option")
		}
		return goxideMatch20
	}
}

func presentNil(x sum.Option[*int]) bool {
	{
		var goxideMatch24 bool
		var goxidematch25 sum.Option[*int] = x
		switch goxidetag27 := sum.TagOption(goxidematch25); goxidetag27 {
		case 1:

			p := sum.ValueSome(goxidematch25)
			goxideMatch24 = p == nil
		case 0:
			goxideMatch24 = false
		default:
			panic("goxide: invalid enum Option")
		}
		return goxideMatch24
	}
}

func main() {
	global = &Empty[int]{}
	global = &Item[int]{Value: 9}
	fmt.Println(integer(&Item[int]{Value: 42}), text(&Item[string]{Value: "hello"}), optionInt(sum.NewSome[int](3)), optionText(sum.NewSome[string]("world")), presentNil(sum.NewSome[*int](nil)), reveal(&_Hidden{Value: 7}), parcelText(&Packed[string]{Value: "mapped"}), globalText, integer(global))
}
