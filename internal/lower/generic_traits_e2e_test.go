package lower_test

import "testing"

func TestGenericTraitAssociatedEvidenceExecution(t *testing.T) {
	runMacroBindingSource(t, `package main
use fmt;
trait Source { type Item; func get(self Self) Self::Item }
struct Box[T any] { Value T }
impl[T any] Source for Box[T] {
    type Item = T
    func get(self Self) Self::Item { return self.Value }
}
struct Wrapped[T any] { Value T }
impl[T any] Source for Wrapped[T] where T: Source {
    type Item = T::Source::Item
    func get(self Self) Self::Item { return self.Value.get() }
}
func Read[T Source](value T) T::Source::Item { return value.get() }
func Forward[T Source](value T) T::Source::Item { return Read[T](value) }
func main() {
    value := Box[int]{Value: 42}
    wrapped := Wrapped[Box[int]]{Value: value}
    read := Read[Wrapped[Box[int]]]
    fmt::Println(Read[Box[int]](value), Read(wrapped), read(wrapped), Forward(wrapped))
}
`, "42 42 42 42\n")
}

func TestGenericTraitMultipleAndInheritedEvidenceExecution(t *testing.T) {
	runMacroBindingSource(t, `package main
use fmt;
trait Number { type Item; func get(self Self) Self::Item }
trait Text { type Item; func get(self Self) Self::Item }
trait Both where Self: Number[Item=int] + Text[Item=string] {}
struct Cell { Value int }
impl Number for Cell { type Item = int; func get(self Self) Self::Item { return self.Value } }
impl Text for Cell { type Item = string; func get(self Self) Self::Item { return "cell" } }
impl Both for Cell {}
impl Cell { pub func get(self Self) int { return Number::get(self) } }
func Pair[T any](value T) (int, string) where T: Number[Item=int] + Text[Item=string] {
    return value.get::<Number>(), value.get::<Text>()
}
func Inherited[T Both](value T) (int, string) { return Pair[T](value) }
func Forward[T Both](value T) (int, string) { return Inherited(value) }
func main() {
    value := Cell{Value: 7}
    explicit := Pair[Cell]
    inherited := Inherited[Cell]
    a, b := explicit(value)
    c, d := inherited(value)
    e, f := Forward(value)
    fmt::Println(a, b, c, d, e, f)
}
`, "7 cell 7 cell 7 cell\n")
}

func TestGenericTraitCompositePremiseExecution(t *testing.T) {
	runMacroBindingSource(t, `package main
use fmt;
trait Source { type Item; func get(self Self) Self::Item }
struct Box[T any] { Value T }
impl[T any] Source for Box[T] {
    type Item = T
    func get(self Self) Self::Item { return self.Value }
}

func Read[T any](value Box[T]) Box[T]::Source::Item where Box[T]: Source {
    return value.get()
}
func Forward[T any](value Box[T]) Box[T]::Source::Item where Box[T]: Source {
    return Read[T](value)
}
func main() {
    value := Box[int]{Value: 19}
    read := Read[int]
    fmt::Println(read(value), Read(value), Forward[int](value))
}
`, "19 19 19\n")
}

func TestConcreteImplResolvedPrerequisite(t *testing.T) {
	runMacroBindingSource(t, `package main
use fmt;
trait Source { type Item; func get(self Self) Self::Item }
struct Box[T any] { Value T }
impl[T any] Source for Box[T] {
    type Item = T
    func get(self Self) Self::Item { return self.Value }
}
trait Output { type Item; func read(self Self) Self::Item }
struct Reader {}
impl Output for Reader where Box[int]: Source {
    type Item = Box[int]::Source::Item
    func read(self Self) Self::Item { return Box[int]{Value: 42}.get() }
}
func main() {
    var view Output[Item=int] = Reader{}
    fmt::Println(Reader{}.read(), view.read())
}
`, "42 42\n")
}
