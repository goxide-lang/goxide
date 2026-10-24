package lower_test

import "testing"

func TestTraitMethodBindingBeforeArgumentControlFlow(t *testing.T) {
	generated := carrierSources(t, map[string]string{
		"carrierfixture/library/model.gox": `package library
use fmt;
pub trait Compare { func equals(self Self, other Self) bool }
type Value int
impl Compare for Value { func equals(self Self, other Self)bool{return self==other} }
var trace []string
func left() Value {trace=append(trace,"left");return Value(4)}
func right(ok bool) Option[Value] {trace=append(trace,"right");if !ok{return Option[Value]::None};return Option[Value]::Some(Value(4))}
func compare(ok bool) Option[bool] {return Option[bool]::Some(left().equals(right(ok)?))}
pub func exercise(ok bool)string {trace=nil;result:=match compare(ok){Option::Some(value)=>value,Option::None=>false};return fmt::Sprint(result,trace)}
`,
	})
	carrierGoConsumer(t, generated, `package main
import("fmt";"carrierfixture/library")
func main(){fmt.Println(library.Exercise(true));fmt.Println(library.Exercise(false))}
`, "true [left right]\nfalse [left right]\n")
}
