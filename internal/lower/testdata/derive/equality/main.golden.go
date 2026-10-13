package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"
import math "math"

type User struct {
	ID   int
	Text string
}

type Container struct {
	Users  []User
	Values map[string]int
	Fixed  [2]int
	Choice sum.Option[[]int]
}
type Number float64
type State interface {
	isState()
}
type Empty struct {
}

func (*Empty) isState() {
}

type Person struct {
	Value User
}

func (*Person) isState() {
}

type Values struct {
	Value []int
}

func (*Values) isState() {
}

var order []string

func left() State                   { order = append(order, "left"); return &Empty{} }
func right() State                  { order = append(order, "right"); return &Person{Value: User{ID: 3}} }
func panics(f func()) (result bool) { defer func() { result = recover() != nil }(); f(); return false }

func main() {
	a := Container{Users: []User{User{ID: 1, Text: "a"}}, Values: map[string]int{"a": 0}, Choice: sum.NewSome[[]int](nil)}
	b := Container{Users: []User{User{ID: 1, Text: "b"}}, Values: map[string]int{"a": 0}, Choice: sum.NewSome[[]int](nil)}
	EqualsUser2 := func(a User, b User) bool { return false }
	fmt.Println(EqualsContainer(a, b), EqualsUser(a.Users[0], b.Users[0]), EqualsUser2(a.Users[0], b.Users[0]))
	b.Values = map[string]int{"b": 0}
	fmt.Println(EqualsContainer(a, b))
	c := Container{}
	d := Container{Users: []User{}}
	fmt.Println(EqualsContainer(c, c), EqualsContainer(c, d))
	d = Container{Choice: sum.NewSome[[]int](nil)}
	fmt.Println(EqualsContainer(c, d))
	c = Container{Choice: sum.NewSome[[]int]([]int{})}
	fmt.Println(EqualsContainer(c, d))
	nan := Number(math.NaN())
	fmt.Println(EqualsNumber(nan, nan))
	fmt.Println(EqualsState(left(), right()), order)
	fmt.Println(EqualsState(&Person{Value: User{ID: 1}}, &Person{Value: User{ID: 1, Text: "other"}}))
	var invalid State
	fmt.Println(panics(func() { _ = EqualsState(&Empty{}, invalid) }))
}
func EqualsUser(self User, other User) bool { return self.ID == other.ID }
func EqualsContainer(goxideValue1, goxideOther2 Container) bool {
	return func(goxideLeft3, goxideRight4 []User) bool {
		if (goxideLeft3 == nil) != (goxideRight4 == nil) || len(goxideLeft3) != len(goxideRight4) {
			return 0 != 0
		}
		for goxideIndex5 := range goxideLeft3 {
			if !(EqualsUser(goxideLeft3[goxideIndex5], goxideRight4[goxideIndex5])) {
				return 0 != 0
			}
		}
		return 0 == 0
	}((goxideValue1).Users, (goxideOther2).Users) && func(goxideLeft6, goxideRight7 map[string]int) bool {
		if (goxideLeft6 == nil) != (goxideRight7 == nil) || len(goxideLeft6) != len(goxideRight7) {
			return 0 != 0
		}
		for goxideKey8, goxideItem9 := range goxideLeft6 {
			goxideOther10, goxideFound11 := goxideRight7[goxideKey8]
			if !goxideFound11 || !(goxideItem9 == goxideOther10) {
				return 0 != 0
			}
		}
		return 0 == 0
	}((goxideValue1).Values, (goxideOther2).Values) && func(goxideLeft12, goxideRight13 [2]int) bool {
		for goxideIndex14 := range goxideLeft12 {
			if !(goxideLeft12[goxideIndex14] == goxideRight13[goxideIndex14]) {
				return 0 != 0
			}
		}
		return 0 == 0
	}((goxideValue1).Fixed, (goxideOther2).Fixed) && func(goxideLeft15, goxideRight16 sum.Option[[]int]) bool {
		switch sum.TagOption(goxideLeft15) {
		case 0:
		case 1:
		default:
			panic("goxide: invalid enum Option")
		}
		switch sum.TagOption(goxideRight16) {
		case 0:
		case 1:
		default:
			panic("goxide: invalid enum Option")
		}
		if sum.TagOption(goxideLeft15) != sum.TagOption(goxideRight16) {
			return 0 != 0
		}
		switch sum.TagOption(goxideLeft15) {
		case 0:
			return 0 == 0
		case 1:
			return func(goxideLeft24, goxideRight25 []int) bool {
				if (goxideLeft24 == nil) != (goxideRight25 == nil) || len(goxideLeft24) != len(goxideRight25) {
					return 0 != 0
				}
				for goxideIndex26 := range goxideLeft24 {
					if !(goxideLeft24[goxideIndex26] == goxideRight25[goxideIndex26]) {
						return 0 != 0
					}
				}
				return 0 == 0
			}(sum.ValueSome(goxideLeft15), sum.ValueSome(goxideRight16))
		default:
			panic("goxide: invalid enum Option")
		}
	}((goxideValue1).Choice, (goxideOther2).Choice)
}
func EqualsNumber(goxideValue27, goxideOther28 Number) bool {
	return (goxideValue27 == goxideOther28)
}
func EqualsState(goxideValue29, goxideOther30 State) bool {
	return func(goxideLeft31, goxideRight32 State) bool {
		switch goxideValidated36 := (goxideLeft31).(type) {
		case *Empty:
			if goxideValidated36 == nil {
				panic("goxide: invalid enum State")
			}
		case *Person:
			if goxideValidated36 == nil {
				panic("goxide: invalid enum State")
			}
		case *Values:
			if goxideValidated36 == nil {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
		switch goxideValidated38 := (goxideRight32).(type) {
		case *Empty:
			if goxideValidated38 == nil {
				panic("goxide: invalid enum State")
			}
		case *Person:
			if goxideValidated38 == nil {
				panic("goxide: invalid enum State")
			}
		case *Values:
			if goxideValidated38 == nil {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
		switch goxideVariant33 := goxideLeft31.(type) {
		case *Empty:
			goxideOther34, goxideSame35 := goxideRight32.(*Empty)
			if !goxideSame35 {
				return 0 != 0
			}
			_ = goxideVariant33
			_ = goxideOther34
			return 0 == 0
		case *Person:
			goxideOther34, goxideSame35 := goxideRight32.(*Person)
			if !goxideSame35 {
				return 0 != 0
			}
			_ = goxideVariant33
			_ = goxideOther34
			return EqualsUser(goxideVariant33.Value, goxideOther34.Value)
		case *Values:
			goxideOther34, goxideSame35 := goxideRight32.(*Values)
			if !goxideSame35 {
				return 0 != 0
			}
			_ = goxideVariant33
			_ = goxideOther34
			return func(goxideLeft40, goxideRight41 []int) bool {
				if (goxideLeft40 == nil) != (goxideRight41 == nil) || len(goxideLeft40) != len(goxideRight41) {
					return 0 != 0
				}
				for goxideIndex42 := range goxideLeft40 {
					if !(goxideLeft40[goxideIndex42] == goxideRight41[goxideIndex42]) {
						return 0 != 0
					}
				}
				return 0 == 0
			}(goxideVariant33.Value, goxideOther34.Value)
		default:
			panic("goxide: invalid enum State")
		}
	}(goxideValue29, goxideOther30)
}
