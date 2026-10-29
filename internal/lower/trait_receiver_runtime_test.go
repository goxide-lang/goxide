package lower_test

import "testing"

func TestBorrowedValueSelectsValueOrNativePointerImplementation(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
use bytes;
trait Read { func read(self &Self) int }
impl Read for int { func read(self &Self) int { return 10 } }
impl[T Read] Read for *T { func read(self &Self) int { return 99 } }
trait PointerOnly { func pointer(self &Self) int }
impl PointerOnly for *int { func pointer(self &Self) int { return **self } }
func Through[T Read](value &T) int { return Read::read(value) }
func Borrow(value &int) &int { return value }
type Holder struct { Field int; Values []int }
func Fields(value &Holder) int { return (&value.Field).read()+Read::read(&value.Field)+(&value.Values[0]).read() }
func Array(value &[2]int) int { return (&(*value)[0]).read() }
func Slice(value &[]int) int { return Read::read(&(*value)[0]) }
func main() {
 value:=7
 var reference &int=&value
 copied:=reference
 pointer:=&value
 var borrowedPointer &*int=&pointer
 var writable &mut int=&mut value
 read:=reference.read
 var view Read=reference
 var pointerView Read=borrowedPointer
 holder:=Holder{Field:2,Values:[]int{3}}
 array:=[2]int{4,5}
 slice:=[]int{6}
 buffer:=bytes::NewBufferString("native")
 method:=buffer.Bytes
 fmt::Println(string(buffer.Bytes()),string(method()))
 fmt::Println(Through[int](reference),Through[*int](borrowedPointer),reference.read(),copied.read(),Borrow(reference).read(),read(),view.read(),pointerView.read(),(&*reference).read(),writable.read(),Fields(&holder),Array(&array),Slice(&slice),pointer.pointer())
}
`}, "native native\n10 99 10 10 10 10 10 99 10 10 30 10 10 7\n")
}
