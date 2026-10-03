package macros

import (
	"strings"
	"testing"
)

func TestBuiltinNativeEvaluationAndLexicalBindings(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! size {($x:expr)=>{len($x)};}
macro_rules! space {($x:expr)=>{cap($x)};}
macro_rules! nested {($x:expr)=>{size!($x)+size!($x)};}
macro_rules! own {()=>{func()int{len:=func(int)int{return 17};return len(1)}()};}
const N=size!("abc")
var a [size!("ab")]int
func main(){
 len:=func(string)[]int{return []int{1,2,3,4}}
 cap:=99;_=cap
 n:=0;next:=func()[3]int{n++;return [3]int{}}
 var p *[4]int
 switch len:=any("abc").(type) {case string:if size!(len)!=3 {panic("type switch binding")}}
 v:=size!(p)+space!(p)
 sum:=nested!(next())
 obj:=struct{len int}{len:7}
 {len:="k";m:=map[string]int{len:3};if m["k"]!=3||size!("a")!=1{panic("map key changed")}}
 {const len=3;m:=map[int]int{len:7};if m[len]!=7||size!("a")!=1{panic("const key changed")}}
 fmt.Println(N,a,v,sum,n,size!(len("x")),own!(),obj.len)
}
`, "3 [0 0] 8 6 2 4 17 7\n")
}
func TestBuiltinDefinitionShadows(t *testing.T) {
	for _, declaration := range []string{
		`var len=func(string)int{return 9}`,
		`var (len=func(string)int{return 9})`,
		`type len string`,
		`struct len { X int }`,
		`const len=3`,
		`import len "strings"`,
	} {
		_, err := Expand("shadow.hgo", []byte("package p\n"+declaration+"\nmacro_rules! size {($x:expr)=>{len($x)};}\nfunc f(){_=size!(\"x\")}\n"))
		if err == nil || !strings.Contains(err.Error(), "definition-site") {
			t.Fatalf("%s: %v", declaration, err)
		}
	}
	run(t, `package main
import "fmt"
func len(string)int{return 12}
macro_rules! size {($x:expr)=>{len($x)};}
func main(){len:=func(string)int{return 99};fmt.Println(size!("abc"),len("abc"))}
`, "12 99\n")
}
func TestBuiltinNotFirstClass(t *testing.T) {
	_, err := Expand("value.hgo", []byte("package p\nmacro_rules! value {()=>{len};}\nvar x=value!()"))
	if err == nil || !strings.Contains(err.Error(), "direct call") {
		t.Fatal(err)
	}
}

func TestBuiltinLocalTypeBoundary(t *testing.T) {
	_, err := Expand("type.hgo", []byte(`package p
macro_rules! size {($x:expr)=>{len($x)};}
func f(){type len int;var v len;_=v;_=size!("abc")}
`))
	if err == nil || !strings.Contains(err.Error(), "local named type") {
		t.Fatal(err)
	}
	run(t, `package main
import "fmt"
import "reflect"
macro_rules! size {($x:expr)=>{len($x)};}
func main() { {type len int;fmt.Print(reflect.TypeOf(len(1)).Name()," ")};fmt.Println(size!("abc")) }
`, "len 3\n")
}
