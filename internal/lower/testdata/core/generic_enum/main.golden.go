package main

import fmt "fmt"
import sum "github.com/goxide-lang/std/sum"

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
	var gxMatch1 string
	if global == nil {
		panic("goxide: match on nil Box at corefixture/main.gox:10")
	}
	switch gxVar2 := global.(type) {
	case *Item[int]:
		if gxVar2 == nil {
			panic("goxide: match on nil Box at corefixture/main.gox:10")
		}
		n := gxVar2.Value
		gxMatch1 = fmt.Sprint(n)
	case *Empty[int]:
		if gxVar2 == nil {
			panic("goxide: match on nil Box at corefixture/main.gox:10")
		}
		gxMatch1 = "empty"
	default:
		panic("goxide: invalid enum Box")
	}
	return gxMatch1
}()

func reveal(x _Secret) int {
	{
		var gxMatch3 int
		if x == nil {
			panic("goxide: match on nil Secret at corefixture/main.gox:13")
		}
		switch gxVar4 := x.(type) {
		case *_Hidden:
			if gxVar4 == nil {
				panic("goxide: match on nil Secret at corefixture/main.gox:13")
			}
			n := gxVar4.Value
			gxMatch3 = n
		default:
			panic("goxide: invalid enum Secret")
		}
		return gxMatch3
	}
}

func parcelText(x Parcel[string]) string {
	{
		var gxMatch5 string
		if x == nil {
			panic("goxide: match on nil parcel at corefixture/main.gox:17")
		}
		switch gxVar6 := x.(type) {
		case *Packed[string]:
			if gxVar6 == nil {
				panic("goxide: match on nil parcel at corefixture/main.gox:17")
			}
			s := gxVar6.Value
			gxMatch5 = s
		case *Vacant[string]:
			if gxVar6 == nil {
				panic("goxide: match on nil parcel at corefixture/main.gox:17")
			}
			gxMatch5 = "empty"
		default:
			panic("goxide: invalid enum parcel")
		}
		return gxMatch5
	}
}

func integer(x Box[int]) int {
	{
		var gxMatch7 int
		if x == nil {
			panic("goxide: match on nil Box at corefixture/main.gox:21")
		}
		switch gxVar8 := x.(type) {
		case *Item[int]:
			if gxVar8 == nil {
				panic("goxide: match on nil Box at corefixture/main.gox:21")
			}
			n := gxVar8.Value
			gxMatch7 = n
		case *Empty[int]:
			if gxVar8 == nil {
				panic("goxide: match on nil Box at corefixture/main.gox:21")
			}
			gxMatch7 = -1
		default:
			panic("goxide: invalid enum Box")
		}
		return gxMatch7
	}
}

func text(x Box[string]) string {
	{
		var gxMatch9 string
		if x == nil {
			panic("goxide: match on nil Box at corefixture/main.gox:25")
		}
		switch gxVar10 := x.(type) {
		case *Item[string]:
			if gxVar10 == nil {
				panic("goxide: match on nil Box at corefixture/main.gox:25")
			}
			s := gxVar10.Value
			gxMatch9 = s
		case *Empty[string]:
			if gxVar10 == nil {
				panic("goxide: match on nil Box at corefixture/main.gox:25")
			}
			gxMatch9 = "empty"
		default:
			panic("goxide: invalid enum Box")
		}
		return gxMatch9
	}
}

func optionInt(x sum.Option[int]) int {
	{
		var gxMatch11 int
		switch gxTag13 := sum.TagOption(x); gxTag13 {
		case 1:
			n := sum.ValueSome(x)
			gxMatch11 = n
		case 0:
			gxMatch11 = -1
		default:
			panic("goxide: invalid enum Option")
		}
		return gxMatch11
	}
}

func optionText(x sum.Option[string]) string {
	{
		var gxMatch14 string
		switch gxTag16 := sum.TagOption(x); gxTag16 {
		case 1:
			s := sum.ValueSome(x)
			gxMatch14 = s
		case 0:
			gxMatch14 = "none"
		default:
			panic("goxide: invalid enum Option")
		}
		return gxMatch14
	}
}

func presentNil(x sum.Option[*int]) bool {
	{
		var gxMatch17 bool
		switch gxTag19 := sum.TagOption(x); gxTag19 {
		case 1:
			p := sum.ValueSome(x)
			gxMatch17 = p == nil
		case 0:
			gxMatch17 = false
		default:
			panic("goxide: invalid enum Option")
		}
		return gxMatch17
	}
}

func main() {
	global = &Empty[int]{}
	global = &Item[int]{Value: 9}
	fmt.Println(integer(&Item[int]{Value: 42}), text(&Item[string]{Value: "hello"}), optionInt(sum.NewSome[int](3)), optionText(sum.NewSome[string]("world")), presentNil(sum.NewSome[*int](nil)), reveal(&_Hidden{Value: 7}), parcelText(&Packed[string]{Value: "mapped"}), globalText, integer(global))
}
