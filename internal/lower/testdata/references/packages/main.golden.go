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
		if number == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:15")
		}
		switch gxVar1 := number.(type) {
		case *access.Named[int]:
			if gxVar1 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:15")
			}
			value := gxVar1.Item
			number = &access.Named[int]{Item: value + 1}
		case *access.Empty[int]:
			if gxVar1 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:15")
			}
		default:
			panic("goxide: invalid enum Cargo")
		}
	}
	var text access.Cargo[string] = &access.Named[string]{Item: "old"}
	{
		if text == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:20")
		}
		switch gxVar2 := text.(type) {
		case *access.Named[string]:
			if gxVar2 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:20")
			}
			value := gxVar2.Item
			_ = value
			text = &access.Named[string]{Item: "changed"}
		case *access.Empty[string]:
			if gxVar2 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:20")
			}
		default:
			panic("goxide: invalid enum Cargo")
		}
	}
	{
		var gxMatch3 any
		gxOwner4 := &number
		var gxMatch5 access.Cargo[int] = *gxOwner4
		if gxMatch5 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
		}
		switch gxVar6 := gxMatch5.(type) {
		case *access.Named[int]:
			if gxVar6 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
			}
			gxPayloadRef7 := &gxVar6.Item
			value := gxPayloadRef7
			gxMatch3 = *value
		case *access.Empty[int]:
			if gxVar6 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
			}
			gxMatch3 = 0
		default:
			gxMatch3 = 0
		}
		var gxValue8 any = gxMatch3
		var gxMatch9 any
		gxOwner10 := &text
		var gxMatch11 access.Cargo[string] = *gxOwner10
		if gxMatch11 == nil {
			panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
		}
		switch gxVar12 := gxMatch11.(type) {
		case *access.Named[string]:
			if gxVar12 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
			}
			gxPayloadRef13 := &gxVar12.Item
			value := gxPayloadRef13
			gxMatch9 = *value
		case *access.Empty[string]:
			if gxVar12 == nil {
				panic("goxide: match on nil Cargo at referencefixture/main.gox:24")
			}
			gxMatch9 = ""
		default:
			gxMatch9 = ""
		}
		fmt.Println(gxValue8, gxMatch9)
	}
}
