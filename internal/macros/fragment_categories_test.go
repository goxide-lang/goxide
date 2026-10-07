package macros

import (
	"fmt"
	"strings"
	"testing"
)

func TestFragmentCategoriesRejectTypeSlots(t *testing.T) {
	for _, fragment := range []string{"expr", "ident", "tt"} {
		for _, tc := range []struct{ name, body string }{
			{"variable", `func()int{var value $T;return 0}()`},
			{"constant", `func()int{const value $T=1;return 0}()`},
			{"parameter", `func(value $T)int{return 0}(1)`},
			{"result", `func()$T{return 1}()`},
			{"named result", `func()(value $T){return}()`},
			{"variadic", `func(values ...$T)int{return 0}(1)`},
			{"function variable", `func()int{var fn func($T)$T;return 0}()`},
			{"slice", `func()int{var value []$T;return 0}()`},
			{"array", `func()int{var value [2]$T;return 0}()`},
			{"map key", `func()int{var value map[$T]int;return 0}()`},
			{"map value", `func()int{var value map[int]$T;return 0}()`},
			{"channel", `func()int{var value chan $T;return 0}()`},
			{"receive channel", `func()int{var value <-chan $T;return 0}()`},
			{"send channel", `func()int{var value chan<- $T;return 0}()`},
			{"pointer", `func()int{var value *$T;return 0}()`},
			{"parenthesized", `func()int{var value ($T);return 0}()`},
			{"literal", `[]$T{1}`},
			{"whole literal type", `$T{1}`},
			{"assertion", `any(1).($T)`},
			{"type case", `func()int{switch any(1).(type){case $T:return 1};return 0}()`},
			{"type case after value switch", `func()int{switch 1{case 1:};switch any(1).(type){case $T:return 1};return 0}()`},
			{"literal in array length", `func()int{var value [len([2]$T{})]int;return 0}()`},
			{"assertion in array length", `func()int{var value [len(any(1).($T))]int;return 0}()`},
			{"selected type", `func()int{var value $T.Member;return 0}()`},
			{"selected type name", `func()int{var value $T.$T;return 0}()`},
			{"instantiated type", `func()int{var value $T[int];return 0}()`},
			{"instantiated type argument", `func()int{var value int[$T];return 0}()`},
			{"multiple type arguments", `func()int{var value int[int,$T];return 0}()`},
		} {
			t.Run(fragment+"/"+tc.name, func(t *testing.T) {
				src := fmt.Sprintf("package p\nmacro_rules! m {($T:%s)=>{%s};}\nfunc f(){_=m!(int)}\n", fragment, tc.body)
				_, err := Expand("categories.gox", []byte(src))
				if err == nil || !strings.Contains(err.Error(), "macro type position does not support captured syntax") {
					t.Fatalf("type-slot capture not diagnosed: %v", err)
				}
			})
		}
	}
	// nil's spelling is not proof of a special binding in a captured type list.
	_, err := Expand("nil.gox", []byte(`package p
macro_rules! m {($n:expr)=>{switch any(1).(type){case $n:}};}
func f(){m!(nil)}
`))
	if err == nil || !strings.Contains(err.Error(), "macro type position does not support captured syntax") {
		t.Fatalf("captured nil admitted to type list: %v", err)
	}
}

func TestFragmentCategoriesPreserveCallerExpressions(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! identity {($x:expr)=>{$x};}
macro_rules! length {($n:expr)=>{func()int{var a [$n]int;return len(a)}()};}
macro_rules! allocate {($n:expr)=>{new([$n]int)};}
macro_rules! invoke {($f:expr,$x:expr)=>{$f($x)};}
macro_rules! pointer_call {($f:expr)=>{(*$f)(4)};}
macro_rules! dereference {($p:expr)=>{*$p};}
macro_rules! index {($x:expr,$i:expr)=>{$x[$i]};}
macro_rules! choose {($x:expr)=>{func()int{switch 4{case $x:return 4};return 0}()};}
macro_rules! bind {($name:ident,$x:expr)=>{var $name int=$x};}
macro_rules! parameter {($name:ident)=>{func($name int)int{return $name}(6)};}
macro_rules! result {($name:ident)=>{func()($name int){$name=7;return}()};}
macro_rules! arguments {($f:ident,$args:tt)=>{$f$args};}
func main(){
 type Local int
 values:=identity!(make([]Local,2))
 expression:=identity!(func(value Local)Local{return value}(Local(3)))
 var boxed any=Local(4)
 assertion:=identity!(boxed.(Local))
 literal:=identity!([]Local{5})
 fmt.Printf("%T %d %d %T %v %T %v %T %v\n",values,len(values),values[0],expression,expression,assertion,assertion,literal,literal[0])
 fmt.Println(length!(len([2]Local{})),len(allocate!(len([2]Local{}))))
 f:=func(value int)int{return value+1}
 n:=8
 bind!(bound,9)
 fmt.Println(invoke!(f,4),pointer_call!(&f),dereference!(&n),index!([]int{3,5},1),choose!(4),bound,parameter!(argument),result!(output),arguments!(f,(9)))
 // The captured call retains a caller-local make function, not the builtin.
 make:=func()Local{return 10}
 fmt.Printf("%T %v\n",identity!(make()),identity!(make()))
}
`, "[]main.Local 2 0 main.Local 3 main.Local 4 []main.Local 5\n2 2\n5 5 8 5 4 9 6 7 10\nmain.Local 10\n")
}
