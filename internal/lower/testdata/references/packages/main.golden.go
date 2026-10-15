package main

import fmt "fmt"
import access "referencefixture/kit"

func main() {
	store := access.Store{Value: 10}
	view := access.Read(&store)
	writer := access.Edit(&store)
	*writer += 1
	{
		value := &store.Value
		*value += 1
	}

	fmt.Println(*view, store.Value)
	var number access.Cargo[int] = &access.Named[int]{Item: 6}
	{
		goxideOwner1 := &number
		var goxidematch2 access.Cargo[int] = *goxideOwner1
		if goxidematch2 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:322")
		}
		switch goxidevariant3 := goxidematch2.(type) {
		case *access.Named[int]:
			_ = goxidevariant3
			if goxidevariant3 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:322")
			}
			goxidePayloadRef4 := &goxidevariant3.Item
			value := goxidePayloadRef4
			*value += 1
		case *access.Empty[int]:
			_ = goxidevariant3
			if goxidevariant3 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:322")
			}
		default:
			_ = goxidevariant3
			panic("goxide: invalid enum Cargo")
		}
	}
	var text access.Cargo[string] = &access.Named[string]{Item: "old"}
	{
		goxideOwner5 := &text
		var goxidematch6 access.Cargo[string] = *goxideOwner5
		if goxidematch6 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:503")
		}
		switch goxidevariant7 := goxidematch6.(type) {
		case *access.Named[string]:
			_ = goxidevariant7
			if goxidevariant7 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:503")
			}
			goxidePayloadRef8 := &goxidevariant7.Item
			value := goxidePayloadRef8
			*value = "changed"
		case *access.Empty[string]:
			_ = goxidevariant7
			if goxidevariant7 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:503")
			}
		default:
			_ = goxidevariant7
			panic("goxide: invalid enum Cargo")
		}
	}
	{
		var goxideMatch9 any
		goxideOwner10 := &number
		var goxidematch11 access.Cargo[int] = *goxideOwner10
		if goxidematch11 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:652")
		}
		switch goxidevariant12 := goxidematch11.(type) {
		case *access.Named[int]:
			_ = goxidevariant12
			if goxidevariant12 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:652")
			}
			goxidePayloadRef13 := &goxidevariant12.Item
			value := goxidePayloadRef13
			goxideMatch9 = *value
		case *access.Empty[int]:
			_ = goxidevariant12
			if goxidevariant12 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:652")
			}
			goxideMatch9 = 0
		default:
			_ = goxidevariant12
			goxideMatch9 = 0
		}
		var goxideValue14 any = goxideMatch9
		var goxideMatch15 any
		goxideOwner16 := &text
		var goxidematch17 access.Cargo[string] = *goxideOwner16
		if goxidematch17 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:723")
		}
		switch goxidevariant18 := goxidematch17.(type) {
		case *access.Named[string]:
			_ = goxidevariant18
			if goxidevariant18 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:723")
			}
			goxidePayloadRef19 := &goxidevariant18.Item
			value := goxidePayloadRef19
			goxideMatch15 = *value
		case *access.Empty[string]:
			_ = goxidevariant18
			if goxidevariant18 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:723")
			}
			goxideMatch15 = ""
		default:
			_ = goxidevariant18
			goxideMatch15 = ""
		}
		fmt.Println(goxideValue14, goxideMatch15)
	}
}
