package lower_test

import "testing"

func TestConstMacroCapturedArguments(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
const Count = 3
macro_rules! apply { ($T:type,$n:expr,$value:expr) => { Size[$T,$n]($value) }; }
macro_rules! forward { ($T:type,$n:expr,$value:expr) => { apply!($T,$n,$value) }; }
macro_rules! single { ($n:expr) => { CountSize[$n]() }; }
macro_rules! named { ($n:ident) => { CountSize[$n]() }; }
macro_rules! tokens { ($n:tt) => { CountSize[$n]() }; }
macro_rules! index { ($values:expr,$index:expr) => { $values[$index] }; }
macro_rules! fixed { ($n:expr) => { Identity[[$n]int]([$n]int{1,2,3}) }; }
func Size[T any,const N int](value [N]T) int { return len(value) }
func CountSize[const N int]() int { var value [N]int; return len(value) }
func Identity[T any](value T) T { return value }
func Forward[T any,const N int](value [N]T) int { return forward!(T,N,value) }
func main() {
 a := [3]int{1,2,3}
 fmt::Println(apply!(int,3,a),apply!(int,Count,a),apply!(int,1+2,a),Forward(a),single!(Count),index!(a,1+1),fixed!(3)[2],named!(Count),tokens!(Count),tokens!(3))
}
`}, "3 3 3 3 3 3 3 3 3 3\n")
}
