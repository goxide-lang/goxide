package lower_test

import "testing"

func TestTraitTemplateDistinctChannelDirections(t *testing.T) {
	runMacroBindingSource(t, `package main
trait Tag { func tag(self Self) int }
impl[T any] Tag for chan T { func tag(self Self) int { return 1 } }
impl[T any] Tag for <-chan T { func tag(self Self) int { return 2 } }
func main() {
    channel := make(chan int)
    var receiver <-chan int = channel
    println(Tag::tag(channel), Tag::tag(receiver))
}
`, "1 2\n")
}

func TestTraitInstanceNaturalNumericSuffix(t *testing.T) {
	runMacroBindingSource(t, `package main
trait Tag { func tag(self Self) int }
impl Tag for struct { Value int } { func tag(self Self) int { return 1 } }
impl Tag for struct { Value string } { func tag(self Self) int { return 2 } }
type Record2 int
impl Tag for Record2 { func tag(self Self) int { return 3 } }
func Read[T Tag](value T) int { return Tag::tag(value) }
func main() {
    var integers struct { Value int }
    var strings struct { Value string }
    println(Read(integers), Read(strings), Read(Record2(0)))
}
`, "1 2 3\n")
}

func TestTraitTemplateCallbackImportAlias(t *testing.T) {
	runMacroBindingSource(t, `package main
use fmt as goxideTCopy;
trait Copy { func copy(self &Self) Self }
impl Copy for int { func copy(self &Self) Self { return *self } }
func CopyWith[T Copy](value &T) T {
    goxideTCopy::Println("called")
    return Copy::copy(value)
}
func main() { value := 7; copied := CopyWith[int](&value); println(copied) }
`, "called\n7\n")
}
