package lower_test

import "testing"

func TestTraitCasesPrivateUnionFunctions(t *testing.T) {
	sources := map[string]string{
		"library/lib.gox": `package library
pub trait Label { func label(self Self) string }
pub trait Marker {}
pub struct Cell[T any] { pub Value T }
impl[T interface{~int}] Label for Cell[T] { func label(self Self) string { return "number" } }
impl[T interface{~string}] Label for Cell[T] { func label(self Self) string { return "text" } }
impl[T interface{~int}] Marker for Cell[T] {}
impl[T interface{~string}] Marker for Cell[T] {}
pub func Read[T interface{~int|~string}](value Cell[T]) string { return value.label() }
pub func ReadPremise[T any](value Cell[T]) string where Cell[T]:Marker { return value.label() }
pub func ReadArray[T interface{~int|~string}, const N int](values [N]Cell[T]) string { return values[0].label() }
pub func Save[T interface{~int|~string}](value Cell[T]) func() string { return value.label }
`,
		"model/lib.gox": `package model
use associatedfixture::library;
type number int
type text string
pub func Number() library::Cell[number] { return library::Cell[number]{Value:7} }
pub func Text() library::Cell[text] { return library::Cell[text]{Value:"seven"} }
pub func Numbers() [2]library::Cell[number] { return [2]library::Cell[number]{Number(),Number()} }
pub func Texts() [2]library::Cell[text] { return [2]library::Cell[text]{Text(),Text()} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::library;
use associatedfixture::model;
func main() {
    first, second := library::Save(model::Number()), library::Save(model::Text())
    fmt::Println(library::Read(model::Number()), library::Read(model::Text()), library::ReadArray(model::Numbers()), library::ReadArray(model::Texts()), first(), second(), library::ReadPremise(model::Number()), library::ReadPremise(model::Text()))
}
`,
	}
	runPrivateAssociatedFixture(t, sources, "number text number text number text number text\n")
}

func TestTraitCasesRuntimeViewsAndRecover(t *testing.T) {
	sources := map[string]string{
		"library/lib.gox": `package library
use fmt;
trait Label { func label(self Self) string }
struct Cell[T any] { value T }
impl[T interface{~int}] Label for Cell[T] { func label(self Self) string { return "number" } }
impl[T interface{~string}] Label for Cell[T] { func label(self Self) string { return "text" } }
pub trait Action { func label(self Self) string; func catch(self Self) }
pub struct Envelope[T any] { pub Value T }
impl[T interface{~int|~string}] Action for Envelope[T] {
    func label(self Self) string { return Label::label(Cell[T]{value:self.Value}) }
    func catch(self Self) { fmt::Println(Label::label(Cell[T]{value:self.Value}),recover()!=nil) }
}
pub func View[T interface{~int|~string}](value Envelope[T]) Action { return value }
pub func Save[T interface{~int|~string}](value Envelope[T]) func() { return value.catch }
`,
		"model/lib.gox": `package model
use associatedfixture::library;
type number int
type text string
pub func Number() library::Envelope[number] { return library::Envelope[number]{Value:7} }
pub func Text() library::Envelope[text] { return library::Envelope[text]{Value:"seven"} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::library;
use associatedfixture::model;
func unwind(value library::Action) { defer value.catch(); panic("view") }
func saved(value func()) { defer value(); panic("saved") }
func main() {
    var direct library::Action = model::Number()
    first,second := library::View(model::Number()),library::View(model::Text())
    fmt::Println(direct.label(),first.label(),second.label())
    unwind(first)
    saved(second.catch)
    saved(library::Save(model::Number()))
}
`,
	}
	runPrivateAssociatedFixture(t, sources, "number number text\nnumber true\ntext true\nnumber true\n")
}

func TestTraitCasesConstRuntimeViewsAndRecover(t *testing.T) {
	sources := map[string]string{
		"library/lib.gox": `package library
use fmt;
trait Label { func label(self Self) string }
pub struct Cell[T any] { pub Value T }
impl[T interface{~int}] Label for Cell[T] { func label(self Self) string { return "number" } }
impl[T interface{~string}] Label for Cell[T] { func label(self Self) string { return "text" } }
pub trait Action { func label(self Self) string; func catch(self Self) }
impl[T interface{~int|~string},const N int] Action for [N]Cell[T] {
    func label(self Self) string { return Label::label(self[0]) }
    func catch(self Self) { fmt::Println(N,Label::label(self[0]),recover()!=nil) }
}
pub func View[T interface{~int|~string},const N int](value [N]Cell[T]) Action { return value }
pub func Save[T interface{~int|~string},const N int](value [N]Cell[T]) func() { return value.catch }
`,
		"model/lib.gox": `package model
use associatedfixture::library;
type number int
type text string
pub func Numbers() [2]library::Cell[number] { return [2]library::Cell[number]{{Value:7},{Value:8}} }
pub func Texts() [2]library::Cell[text] { return [2]library::Cell[text]{{Value:"seven"},{Value:"eight"}} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::library;
use associatedfixture::model;
func unwind(value library::Action) { defer value.catch(); panic("view") }
func saved(value func()) { defer value(); panic("saved") }
func main() {
    var direct library::Action = model::Numbers()
    first,second := library::View(model::Numbers()),library::View(model::Texts())
    fmt::Println(direct.label(),first.label(),second.label())
    unwind(first)
    saved(second.catch)
    saved(library::Save(model::Numbers()))
}
`,
	}
	runPrivateAssociatedFixture(t, sources, "number number text\n2 number true\n2 text true\n2 number true\n")
}

func TestTraitCasesSignaturesFollowAssociatedOutputs(t *testing.T) {
	sources := map[string]string{
		"library/lib.gox": `package library
pub trait Source { type Item; func get(self Self) Self::Item }
pub struct Cell[T any] { pub Value T }
impl[T interface{~int}] Source for Cell[T] { type Item=int;func get(self Self)int{return 7} }
impl[T interface{~string}] Source for Cell[T] { type Item=string;func get(self Self)string{return "seven"} }
pub func Read[T interface{~int|~string}](value Cell[T])Cell[T]::Source::Item {return value.get()}
pub func First[T interface{~int|~string},const N int](value [N]Cell[T])Cell[T]::Source::Item {return value[0].get()}
`,
		"model/lib.gox": `package model
use associatedfixture::library;
type number int
type text string
pub func Number() library::Cell[number] {return library::Cell[number]{Value:7}}
pub func Text() library::Cell[text] {return library::Cell[text]{Value:"seven"}}
pub func Numbers() [2]library::Cell[number] {return [2]library::Cell[number]{Number(),Number()}}
pub func Texts() [2]library::Cell[text] {return [2]library::Cell[text]{Text(),Text()}}
`,
		"main.gox": `package main
use fmt;
use associatedfixture::library;
use associatedfixture::model;
func main() {
    var first int=library::Read(model::Number())
    var second string=library::Read(model::Text())
    var third int=library::First(model::Numbers())
    var fourth string=library::First(model::Texts())
    fmt::Println(first,second,third,fourth)
}
`,
	}
	runPrivateAssociatedFixture(t, sources, "7 seven 7 seven\n")
}
