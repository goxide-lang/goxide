package lower_test

import "testing"

func TestMacroBindingsPreserveGenericTypeEmission(t *testing.T) {
	generated := carrierSources(t, map[string]string{
		"carrierfixture/library/model.gox": `package library
macro_rules! names { () => { pub struct T {} } }
names!()
pub trait Marker {}
impl Marker for int {}
pub enum Choice[T any] { Empty, Value(T) }
pub func require[T Marker](value T) T { return value }
pub func identity[T any](value T) T { var copy T=value; return copy }
pub func choose[T any](value T) Choice[T] { return Choice[T]::Value(value) }
pub func exercise() int { return require(identity(7)) }
`,
	})
	carrierGoConsumer(t, generated, `package main
import("fmt";"carrierfixture/library")
func main(){
    var value library.Choice[int]=library.Choose(9)
    fmt.Println(library.Exercise(),value.(*library.Value[int]).Value)
}
`, "7 9\n")
}

func TestMacroBindingsPreserveImplTypeEmission(t *testing.T) {
	generated := carrierSources(t, map[string]string{
		"carrierfixture/library/model.gox": `package library
macro_rules! names { () => { pub struct T {} } }
names!()
pub trait Build { func build() Self }
impl Build for int { func build() int { return 7 } }
impl[T Build] Build for []T {
    func build() []T {
        result := make([]T, 0)
        appendOne := func() { result = append(result, T::build()) }
        appendOne()
        return result
    }
}
pub func makeValue[V Build]() V { return V::build() }
pub func exercise() int { return makeValue[[]int]()[0] }
`,
	})
	carrierGoConsumer(t, generated, `package main
import("fmt";"carrierfixture/library")
func main(){ fmt.Println(library.Exercise()) }
`, "7\n")
}
