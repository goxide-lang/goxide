package lower_test

import "testing"

func TestConstArrayConditionalFunctionCallback(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
trait Serialize { func serialize(self &Self) int }
func Leaf[T Serialize](value &T) int { return Serialize::serialize(value) }
func Sized[const M int]() int { return M }
func Write[T Serialize](value &T) int { return Recur(value,2) }
func Recur[T Serialize](value &T,remaining int) int { if remaining>0 { return Recur(value,remaining-1) };return Leaf(value)+Sized[2]()-2 }
impl Serialize for int { func serialize(self &Self) int { return *self } }
impl Serialize for string { func serialize(self &Self) int { return len(*self) } }
impl[T any,const N int] Serialize for [N]T where T:Serialize {
 func serialize(self &Self) int {
  var total int
  for index:=range *self {
   callback:=func()int{return Write(&(*self)[index])}
   total+=callback()
  }
  return total
 }
}
func main(){values:=[2]int{20,22};words:=[2]string{"a","bb"};empty:=[0]int{};more:=[3]int{1,2,3};fmt::Println(Write(&values),Write(&words),Write(&empty),Write(&more))}
`}, "42 3 0 6\n")
}
