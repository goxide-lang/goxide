package lower_test

import (
	"strings"
	"testing"
)

func TestConstDefinitionTypeSwitches(t *testing.T) {
	for _, source := range []string{
		`func F[const N,M int](x any){switch x.(type){case [N]int:case [M]int:}}`,
		`func F[const N,M int](x any)int{switch v:=x.(type){case [N]int:return len(v);case [M]int:return cap(v);default:return 0}}`,
		`func F[const N,M int](x any)int{switch v:=x.(type){case [N]int,[M]int:_=v.(int);return 1;default:return 0}}`,
		`func F[const N,M int](x any){switch v:=x.(type){case [N]int:_=v;case [M]int:default:}}`,
		`func F[const N,M int](x any){outer:for{switch v:=x.(type){case [N]int:_=v;break outer;case [M]int:continue outer;default:return}}}`,
		`func F[const N,M int](input any)int{switch x:=input;v:=x.(type){case [N]int:return len(v);case [M]int:return cap(v);default:return 0}}`,
		`func F[const N,M int](x any){type A=[N]int;type B=[M]int;switch x.(type){case A:case B:}}`,
		`func F[const N,M int](input any)int{out:switch input.(type){case [N]int:break out;case [M]int:return 2;default:return 0};return 1}`,
	} {
		t.Run(source, func(t *testing.T) {
			_, err := lowerDeriveSources(map[string]string{"switch.gox": "package p\n" + source})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConstDefinitionTypeSwitchRejectsInvalidBranches(t *testing.T) {
	for _, test := range []struct{ source, diagnostic string }{
		{`func F[const N,M int](x any)int{switch x.(type){case [N]int:return 1;case [M]int:return 2}}`, "missing return"},
		{`func F[const N,M int](x any){switch v:=x.(type){case [N]int:case [M]int:}}`, "declared and not used"},
		{`func F[const N,M int](x any){switch x.(type){case [N]int:fallthrough;case [M]int:}}`, "cannot fallthrough"},
		{`func F[const N,M int](x interface{Foo()}){switch x.(type){case [N]int:case [M]int:}}`, "impossible type switch case"},
		{`func F[const N,M int](x interface{Foo()}){switch x.(type){case [N]int,[M]int:}}`, "impossible type switch case"},
		{`func F[const N,M int](x any){switch v:=x.(type){case [N]int:var s string=v;_=s;case [M]int:}}`, "cannot use v"},
		{`func F[const N int](x any){switch x.(type){case [N]int:case [N]int:}}`, "duplicate case"},
		{`func F[const N,M int](x any){switch x.(type){case [N]int:case [M]int:}};func main(){F[2,2]([2]int{})}`, "duplicate case"},
		{`func F[const N,M int](input any)int{out:switch input.(type){case [N]int:break out;case [M]int:return 2;default:return 0}}`, "missing return"},
		{`func F[const N,M int](input any)int{switch input.(type){case [N]int:break;case [M]int:return 2;default:return 0}}`, "missing return"},
		{`func F[const N int](input any){switch input.(type){case nil:case nil:case [N]int:}}`, "duplicate case"},
		{`func F[const N,M int](input any){switch value:=input.(type){case [N]int,[M]int:_=value[0];default:}}`, "cannot index"},
	} {
		t.Run(test.source, func(t *testing.T) {
			_, err := lowerDeriveSources(map[string]string{"switch.gox": "package p\n" + test.source})
			if err == nil || !strings.Contains(err.Error(), test.diagnostic) {
				t.Fatalf("expected %s, got %v", test.diagnostic, err)
			}
		})
	}
}

func TestConstTypeSwitchRuntimeUsesConcreteLengths(t *testing.T) {
	runMacroBindingSource(t, `package main
func classify[const N,M int](value any) int {
 switch found:=value.(type) {
 case [N]int:return len(found)
 case [M]int:return cap(found)
 default:return 0
 }
}
func main(){println(classify[2,3]([2]int{}),classify[2,3]([3]int{}),classify[2,3]("other"))}
`, "2 3 0\n")
}

func TestConstDefinitionTypeSwitchFlowPrelude(t *testing.T) {
	for _, source := range []string{
		`func F[const N,M int](input Result[any,error])Result[int,error]{switch value:=input?;value.(type){case [N]int:return Result[int,error]::Ok(1);case [M]int:return Result[int,error]::Ok(2);default:return Result[int,error]::Ok(0)}}`,
		`func F[const N,M int](input Result[any,error])Result[int,error]{switch value:=(input?).(type){case [N]int:return Result[int,error]::Ok(len(value));case [M]int:return Result[int,error]::Ok(cap(value));default:return Result[int,error]::Ok(0)}}`,
	} {
		t.Run(source, func(t *testing.T) {
			_, err := lowerDeriveSources(map[string]string{"switch.gox": "package p\n" + source})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConstTypeSwitchFlowEvaluatesGuardOnce(t *testing.T) {
	runMacroBindingSource(t, `package main
use fmt;
var calls int
func next(value any, fail bool) Result[any,string] {
 calls++
 if fail {return Result[any,string]::Err("stopped")}
 return Result[any,string]::Ok(value)
}
func classify[const N,M int](value any, fail bool) Result[int,string] {
 switch found:=(next(value,fail)?).(type) {
 case [N]int:return Result[int,string]::Ok(len(found))
 case [M]int:return Result[int,string]::Ok(cap(found))
 default:return Result[int,string]::Ok(0)
 }
}
func status(value Result[int,string]) string {
 return match value {Result::Ok(number)=>fmt::Sprint(number),Result::Err(message)=>message}
}
func main(){
 println(status(classify[2,3]([2]int{},false)),calls)
 println(status(classify[2,3]([3]int{},false)),calls)
 println(status(classify[2,3]("other",false)),calls)
 println(status(classify[2,3]([2]int{},true)),calls)
}
`, "2 1\n3 2\n0 3\nstopped 4\n")
}
