package lower_test

import "testing"

func TestArrayLayoutOperandsRemainUnevaluated(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{
		"model/lib.gox": `package model
use r#unsafe;
var calls int
func tick() uintptr { calls++; return 0 }
pub func Count() int {
 var left [r#unsafe::Sizeof(tick())]byte
 var right [r#unsafe::Alignof(tick())]byte
 if len(left) == 0 || len(right) == 0 { panic("zero layout") }
 var values *[3]int
 var constant [len(values) + cap(values) + len("你")]byte
 if len(constant) != 9 { panic("length constant") }
 return calls
}
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
func main() { fmt::Println(model::Count()) }
`,
	}, "0\n")
}
