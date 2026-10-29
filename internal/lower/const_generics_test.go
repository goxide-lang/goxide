package lower_test

import (
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"testing"
)

func TestConstArrayFunctions(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
type Box[T any] struct { value T }
func Copy[T any,const N int](value [N]T) [N]T { var result [N]T; for i,v := range value { result[i]=v }; return result }
func Size[const N int]() int { return N }
func Forward[T any,const N int](value [N]T) [N]T { return Copy[T,N](value) }
func Wrap[T any,const N int](value [N]T) Box[[N]T] { return Box[[N]T]{value:value} }
func main() {
 f := Copy[int,2]
 a := f([2]int{20,22})
 b := Forward([2]string{"a","b"})
 c := Wrap([2]int{20,22})
 z := Copy([0]int{})
 fmt::Println(a[0]+a[1], b[0]+b[1], c.value[1], Size[0](),len(z))
}
`}, "42 ab 22 0 0\n")
}

func TestConstArrayProtocolCallbacks(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
trait Read { func read(self Self) int }
impl Read for int { func read(self Self) int { return self } }
impl Read for string { func read(self Self) int { return len(self) } }
impl[T Read,const N int] Read for [N]T { func read(self Self) int { var result int; for _,v := range self { result += v.read() }; return result } }
trait First { type Item; func first(self Self) Self::Item }
impl[T any,const N int] First for [N]T { type Item = T; func first(self Self) Self::Item { return self[0] } }
func ReadArray[T Read,const N int](value [N]T) int { return value.read() }
func main() { a := [2]int{20,22}; b := [2]string{"a","bb"}; f := a.read; var view Read = a; fmt::Println(ReadArray(a),ReadArray(b),f(),view.read(),a.first(),b.first()) }
`}, "42 3 42 42 20 a\n")
}

func TestConstArrayPrivateCrossPackage(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Read { func read(self Self) int }
impl[T Read,const N int] Read for [N]T { func read(self Self) int { var result int; for _,v := range self { result += v.read() }; return result } }
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Read;
pub func Total[T Read,const N int](value [N]T) int { var result int; for _,v := range value { result += v.read() }; return result }
pub func Identity[T any,const N int](value [N]T) [N]T { return value }
pub func Reader[T Read,const N int](value [N]T) func([N]T) int { return Total[T,N] }
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Read;
struct secret { value int }
impl Read for secret { func read(self Self) int { return self.value } }
pub func Make() [2]secret { return [2]secret{{value:20},{value:22}} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::protocol::Read;
use associatedfixture::ops;
use associatedfixture::model;
func main() { values:=model::Make(); method:=values.read; var view Read = values; fmt::Println(ops::Total(values),ops::Total(ops::Identity(values)),ops::Reader(values)(values),method(),view.read()) }
`,
	}, "42 42 42 42 42\n")
}

func TestConstArrayGeneratedLengthDeclarations(t *testing.T) {
	output, err := lowerDeriveSources(map[string]string{"main.gox": `package main
func Copy[T any,const N int](value [N]T) [N]T { return value }
func main() { Copy([2]int{}); Copy([2]string{}); Copy([0]int{}) }
`})
	if err != nil {
		t.Fatal(err)
	}
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "main.go", output["main.gox"], 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, declaration := range file.Decls {
		function, ok := declaration.(*goast.FuncDecl)
		if !ok || function.Name.Name == "main" {
			continue
		}
		count++
		if function.Type.TypeParams == nil || len(function.Type.TypeParams.List) != 1 || function.Type.TypeParams.List[0].Names[0].Name != "T" {
			t.Fatalf("ordinary T lost in %s: %s", function.Name.Name, output["main.gox"])
		}
		result := function.Type.Results.List[0].Type.(*goast.FuncType)
		array, ok := result.Params.List[0].Type.(*goast.ArrayType)
		if !ok || array.Elt.(*goast.Ident).Name != "T" {
			t.Fatalf("element specialized in %s", function.Name.Name)
		}
		length, ok := array.Len.(*goast.BasicLit)
		if !ok || length.Value != "2" && length.Value != "0" {
			t.Fatalf("length was not fixed in %s", function.Name.Name)
		}
	}
	if count != 2 {
		t.Fatalf("generated %d declarations for two lengths: %s", count, output["main.gox"])
	}
}

func TestConstArrayRecursionAndMultipleLengths(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
trait Width { func width(self Self) int }
impl[const N int] Width for [N]int { func width(self Self) int { return N } }
func Recur[T any,const N int](value [N]T,remaining int) [N]T { if remaining == 0 { return value }; return Recur[T,N](value,remaining-1) }
func Matrix[const M,N int](value [M][N]int) int { return M*10+N }
func main() { values:=Recur([2]int{20,22},3); fmt::Println(values[0]+values[1],[0]int{}.width(),[3]int{}.width(),Matrix([2][3]int{})) }
`}, "42 0 3 23\n")
}
