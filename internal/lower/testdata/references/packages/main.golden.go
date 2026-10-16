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
		gxOwner1 := &number
		var gxMatch2 access.Cargo[int] = *gxOwner1
		if gxMatch2 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:15")
		}
		switch gxVar3 := gxMatch2.(type) {
		case *access.Named[int]:
			if gxVar3 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:15")
			}
			gxPayloadRef4 := &gxVar3.Item
			value := gxPayloadRef4
			*value += 1
		case *access.Empty[int]:
			if gxVar3 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:15")
			}
		default:
			panic("goxide: invalid enum Cargo")
		}
	}
	var text access.Cargo[string] = &access.Named[string]{Item: "old"}
	{
		gxOwner5 := &text
		var gxMatch6 access.Cargo[string] = *gxOwner5
		if gxMatch6 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:20")
		}
		switch gxVar7 := gxMatch6.(type) {
		case *access.Named[string]:
			if gxVar7 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:20")
			}
			gxPayloadRef8 := &gxVar7.Item
			value := gxPayloadRef8
			*value = "changed"
		case *access.Empty[string]:
			if gxVar7 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:20")
			}
		default:
			panic("goxide: invalid enum Cargo")
		}
	}
	{
		var gxMatch9 any
		gxOwner10 := &number
		var gxMatch11 access.Cargo[int] = *gxOwner10
		if gxMatch11 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
		}
		switch gxVar12 := gxMatch11.(type) {
		case *access.Named[int]:
			if gxVar12 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
			}
			gxPayloadRef13 := &gxVar12.Item
			value := gxPayloadRef13
			gxMatch9 = *value
		case *access.Empty[int]:
			if gxVar12 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
			}
			gxMatch9 = 0
		default:
			gxMatch9 = 0
		}
		var gxValue14 any = gxMatch9
		var gxMatch15 any
		gxOwner16 := &text
		var gxMatch17 access.Cargo[string] = *gxOwner16
		if gxMatch17 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
		}
		switch gxVar18 := gxMatch17.(type) {
		case *access.Named[string]:
			if gxVar18 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
			}
			gxPayloadRef19 := &gxVar18.Item
			value := gxPayloadRef19
			gxMatch15 = *value
		case *access.Empty[string]:
			if gxVar18 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
			}
			gxMatch15 = ""
		default:
			gxMatch15 = ""
		}
		fmt.Println(gxValue14, gxMatch15)
	}
}
