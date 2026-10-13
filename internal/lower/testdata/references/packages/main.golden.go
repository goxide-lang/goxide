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
			goxidePayloadRef4 := &goxidevariant3.Item
			value := goxidePayloadRef4
			*value += 1
		case *access.Empty[int]:
			_ = goxidevariant3
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
			goxidePayloadRef8 := &goxidevariant7.Item
			value := goxidePayloadRef8
			*value = "changed"
		case *access.Empty[string]:
			_ = goxidevariant7
		default:
			_ = goxidevariant7
			panic("goxide: invalid enum Cargo")
		}
	}
	{
		var goxideValue9 func(a ...any) (n int, err error) = fmt.Println
		var goxideMatch10 any
		goxideOwner11 := &number
		var goxidematch12 access.Cargo[int] = *goxideOwner11
		if goxidematch12 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:652")
		}
		switch goxidevariant13 := goxidematch12.(type) {
		case *access.Named[int]:
			_ = goxidevariant13
			goxidePayloadRef14 := &goxidevariant13.Item
			value := goxidePayloadRef14
			goxideMatch10 = *value
		default:
			_ = goxidevariant13
			goxideMatch10 = 0
		}
		var goxideValue15 any = goxideMatch10
		var goxideMatch16 any
		goxideOwner17 := &text
		var goxidematch18 access.Cargo[string] = *goxideOwner17
		if goxidematch18 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:723")
		}
		switch goxidevariant19 := goxidematch18.(type) {
		case *access.Named[string]:
			_ = goxidevariant19
			goxidePayloadRef20 := &goxidevariant19.Item
			value := goxidePayloadRef20
			goxideMatch16 = *value
		default:
			_ = goxidevariant19
			goxideMatch16 = ""
		}
		goxideValue9(goxideValue15, goxideMatch16)
	}
}
