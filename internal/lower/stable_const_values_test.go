package lower_test

import "testing"

func TestStableGoConstLocalAliases(t *testing.T) {
	const library = `package library
pub func Local[const N int]()int{const Count=N;const Next=Count+1;return Next}
pub func Group[const N int]()int{const(Zero=iota;A,B=N+iota,7;C,D;Tail=iota);return Zero+A+B+C+D+Tail}
pub func Shadow[const N int]()int{const Local=N+1;return Local+func()int{const Local=4;return Local}()}
pub func ShadowIota[const N int]()int{const iota=9;const(Local=N+iota;Next);return Local+Next}
pub func BoolGroup[const N int]()(bool,bool,bool,bool){const(A,B bool=N>0,true;C,D);return A,B,C,D}
pub func Switch[const N int](value int)int{const Local=N+1;switch value{case Local:return Local};return 0}
pub func Length[const N int](values [N]int)int{const Count=len(values);return Count}
pub func LocalArray[const N int]()int{var values [N]int;return len(values)}
pub func StaticArray[const N int]()int{const(A,B=N,2);var values [B]int;return A+len(values)}
pub func DiscardedClosure[const N int]()int{var x int;return len([N]func(){func(){y:=x;_=y}})}
pub func FixedConst[const N int]()int{var values [2]int;const Count=len(values);return N+Count}
pub func NestedFixedConst[const N int]()int{var values *[N][2]int;const Count=len(values[0]);return N+Count}
pub func UnusedConst[const N int]()int{var values [N]int;const Count=len(values);return N}
`
	const prefix = `package main;use fmt;use library;`
	runStableGoLibraryFixture(t, library, prefix, []stableGoCase{
		{"aliases and grouped iota", `func main(){fmt::Println(library::Local[2](),library::Group[2](),library::Group[0](),library::Shadow[3](),library::ShadowIota[2]())}`, "3 24 20 8 22\n"},
		{"typed booleans and switch", `func main(){fmt::Println(library::BoolGroup[0]());fmt::Println(library::BoolGroup[2]());fmt::Println(library::Switch[2](3),library::Switch[2](4))}`, "false true false true\ntrue true true true\n3 0\n"},
		{"array lengths", `func main(){fmt::Println(library::Length([3]int{}),library::LocalArray[3](),library::StaticArray[3](),library::DiscardedClosure[3](),library::FixedConst[3](),library::NestedFixedConst[3](),library::UnusedConst[3]())}`, "3 3 5 3 5 5 3\n"},
	})
}
