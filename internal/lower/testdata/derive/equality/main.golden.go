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
func EqualsContainer(gxValue1, gxOther2 Container) bool {
	return func(gxLeft3, gxRight4 []User) bool {
		if (gxLeft3 == nil) != (gxRight4 == nil) || len(gxLeft3) != len(gxRight4) {
			return 0 != 0
		}
		for gxIndex5 := range gxLeft3 {
			if !(EqualsUser(gxLeft3[gxIndex5], gxRight4[gxIndex5])) {
				return 0 != 0
			}
		}
		return 0 == 0
	}((gxValue1).Users, (gxOther2).Users) && func(gxLeft6, gxRight7 map[string]int) bool {
		if (gxLeft6 == nil) != (gxRight7 == nil) || len(gxLeft6) != len(gxRight7) {
			return 0 != 0
		}
		for gxKey8, gxItem9 := range gxLeft6 {
			gxOther10, gxFound11 := gxRight7[gxKey8]
			if !gxFound11 || !(gxItem9 == gxOther10) {
				return 0 != 0
			}
		}
		return 0 == 0
	}((gxValue1).Values, (gxOther2).Values) && func(gxLeft12, gxRight13 [2]int) bool {
		for gxIndex14 := range gxLeft12 {
			if !(gxLeft12[gxIndex14] == gxRight13[gxIndex14]) {
				return 0 != 0
			}
		}
		return 0 == 0
	}((gxValue1).Fixed, (gxOther2).Fixed) && func(gxLeft15, gxRight16 sum.Option[[]int]) bool {
		switch sum.TagOption(gxLeft15) {
		case 0:
		case 1:
		default:
			panic("goxide: invalid enum Option")
		}
		switch sum.TagOption(gxRight16) {
		case 0:
		case 1:
		default:
			panic("goxide: invalid enum Option")
		}
		if sum.TagOption(gxLeft15) != sum.TagOption(gxRight16) {
			return 0 != 0
		}
		switch sum.TagOption(gxLeft15) {
		case 0:
			return 0 == 0
		case 1:
			return func(gxLeft24, gxRight25 []int) bool {
				if (gxLeft24 == nil) != (gxRight25 == nil) || len(gxLeft24) != len(gxRight25) {
					return 0 != 0
				}
				for gxIndex26 := range gxLeft24 {
					if !(gxLeft24[gxIndex26] == gxRight25[gxIndex26]) {
						return 0 != 0
					}
				}
				return 0 == 0
			}(sum.ValueSome(gxLeft15), sum.ValueSome(gxRight16))
		default:
			panic("goxide: invalid enum Option")
		}
	}((gxValue1).Choice, (gxOther2).Choice)
}
func EqualsNumber(gxValue27, gxOther28 Number) bool {
	return (gxValue27 == gxOther28)
}
func EqualsState(gxValue29, gxOther30 State) bool {
	return func(gxLeft31, gxRight32 State) bool {
		switch gxValidated36 := (gxLeft31).(type) {
		case *Empty:
			if gxValidated36 == nil {
				panic("goxide: invalid enum State")
			}
		case *Person:
			if gxValidated36 == nil {
				panic("goxide: invalid enum State")
			}
		case *Values:
			if gxValidated36 == nil {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
		switch gxValidated38 := (gxRight32).(type) {
		case *Empty:
			if gxValidated38 == nil {
				panic("goxide: invalid enum State")
			}
		case *Person:
			if gxValidated38 == nil {
				panic("goxide: invalid enum State")
			}
		case *Values:
			if gxValidated38 == nil {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
		switch gxVar33 := gxLeft31.(type) {
		case *Empty:
			gxOther34, gxSame35 := gxRight32.(*Empty)
			if !gxSame35 {
				return 0 != 0
			}
			_ = gxVar33
			_ = gxOther34
			return 0 == 0
		case *Person:
			gxOther34, gxSame35 := gxRight32.(*Person)
			if !gxSame35 {
				return 0 != 0
			}
			_ = gxVar33
			_ = gxOther34
			return EqualsUser(gxVar33.Value, gxOther34.Value)
		case *Values:
			gxOther34, gxSame35 := gxRight32.(*Values)
			if !gxSame35 {
				return 0 != 0
			}
			_ = gxVar33
			_ = gxOther34
			return func(gxLeft40, gxRight41 []int) bool {
				if (gxLeft40 == nil) != (gxRight41 == nil) || len(gxLeft40) != len(gxRight41) {
					return 0 != 0
				}
				for gxIndex42 := range gxLeft40 {
					if !(gxLeft40[gxIndex42] == gxRight41[gxIndex42]) {
						return 0 != 0
					}
				}
				return 0 == 0
			}(gxVar33.Value, gxOther34.Value)
		default:
			panic("goxide: invalid enum State")
		}
	}(gxValue29, gxOther30)
}
