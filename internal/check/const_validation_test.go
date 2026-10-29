package check_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestConstDefinitionArrayIdentity(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"assignment", `func F[const N,M int](a [N]int,b [M]int){a=b}`},
		{"declaration", `func F[const N,M int](a [N]int){var b [M]int=a;_=b}`},
		{"return", `func F[const N,M int](a [N]int)[M]int{return a}`},
		{"closure return", `func F[const N,M int](a [N]int){_=func()[M]int{return a}}`},
		{"multi return", `func F[const N,M int](a [N]int)(int,[M]int){return 1,a}`},
		{"fixed length", `func F[const N int](a [N]int){var b [2]int=a;_=b}`},
		{"call", `func F[const N,M int](a [N]int,f func([M]int)){f(a)}`},
		{"variadic call", `func F[const N,M int](a [N]int,f func(...[M]int)){f(a)}`},
		{"function assignment", `func F[const N,M int](f func([N]int)){var g func([M]int)=f;_=g}`},
		{"compare", `func F[const N,M int](a [N]int,b [M]int)bool{return a==b}`},
		{"nested", `func F[const N,M int](a map[string][]*[N]int){var b map[string][]*[M]int=a;_=b}`},
		{"literal", `func F[const N,M int](a [N]int){_=[][M]int{a}}`},
		{"enum constructor", `enum E[T any]{Value(T)};func F[const N,M int](a [N]int){_=E[[M]int]::Value(a)}`},
		{"enum literal", `enum E[T any]{Value{item T}};func F[const N,M int](a [N]int){_=E[[M]int]::Value{item:a}}`},
		{"map key", `func F[const N,M int](a [N]int,m map[[M]int]int){_=m[a]}`},
		{"delete key", `func F[const N,M int](a [N]int,m map[[M]int]int){delete(m,a)}`},
		{"embedded interface", `func F[const N,M int](x interface{interface{Get()[N]int};interface{Get()[M]int}}){_=x}`},
		{"explicit interface method", `func F[const N,M int](x interface{Get()[N]int;interface{Get()[M]int}}){_=x}`},
		{"channel", `func F[const N,M int](a [N]int,ch chan [M]int){ch<-a}`},
		{"append", `func F[const N,M int](a [N]int,b [][M]int){_=append(b,a)}`},
		{"copy", `func F[const N,M int](a [][N]int,b [][M]int){copy(b,a)}`},
		{"range assignment", `func F[const N,M int](a [][N]int){var b [M]int;for _,b=range a{}}`},
		{"interface assertion", `func F[const N,M int](value interface{Get()[N]int}){_=value.(interface{Get()[M]int})}`},
		{"concrete assertion", `struct Fixed{};func(Fixed)Get()[2]int{return [2]int{}};func F[const N int](value interface{Get()[N]int}){_=value.(Fixed)}`},
		{"interface type switch", `func F[const N,M int](value interface{Get()[N]int}){switch value.(type){case interface{Get()[M]int}:}}`},
		{"concrete type switch", `struct Fixed{};func(Fixed)Get()[2]int{return [2]int{}};func F[const N int](value interface{Get()[N]int}){switch v:=value.(type){case Fixed:_=v}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+test.source)
			_, diagnostics := check.Check(files, resolved)
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Message, "symbolic array lengths must agree") {
					return
				}
			}
			t.Fatalf("uninstantiated definition accepted distinct lengths: %v", diagnostics)
		})
	}
}

func TestConstDefinitionPreservesSharedLengths(t *testing.T) {
	files, resolved := parse(t, `package p
func Copy[T any,const N int](a [N]T)[N]T{return a}
func F[T comparable,const N,M int](a [N]T,b [N]T){
 var saved [N]T=a
 saved=Copy[T,N](b)
 _=saved==a
 var boxed any=a
 _=boxed==a
 values:=[][N]T{a};values=append(values,b);copy(values,[][N]T{b})
 f:=func(a [N]T)[N]T{return a};_=f(saved)
 var unrelated [M]T;_=unrelated
 _=boxed.([M]T)
 switch boxed.(type){case [M]T:}
 var source interface{Get()[N]T}
 _=source.(interface{Get()[N]T;Put([M]T)})
 switch source.(type){case interface{Get()[N]T;Put([M]T)}:}
}`)
	_, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
}
