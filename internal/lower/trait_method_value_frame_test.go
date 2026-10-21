package lower_test

import "testing"

func TestTraitMethodValueRetainsDeferredRecoverFrame(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{
		"model/lib.gox": `package model
struct secret {}
pub func Make() []secret { return []secret{{}} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
trait Catch { func catchPanic(self Self, caught *bool) }
impl[T any] Catch for []T {
 func catchPanic(self Self, caught *bool) { *caught = recover() != nil }
}
type Concrete int
impl Catch for Concrete {
 func catchPanic(self Self, caught *bool) { *caught = recover() != nil }
}
trait CatchWith { type Item; func catchWith(self Self, caught *bool, values ...Self::Item) }
impl[T any] CatchWith for []T {
 type Item = T
 func catchWith(self Self, caught *bool, values ...T) { *caught = recover() != nil && len(values) == 1 }
}
trait RecoverSelf { func catchSelf(self Self, caught *bool) Self }
impl[T any] RecoverSelf for []T {
 func catchSelf(self Self, caught *bool) Self { *caught = recover() != nil; return self }
}
trait DeclaredCatch { func catchNative(self Self, caught *bool) }
type Adapted int
func (value Adapted) catchNative(caught *bool) { *caught = recover() != nil }
declare impl DeclaredCatch for Adapted;
trait BorrowedCatch { func catchValue(self &Self, caught *bool) }
func (value Adapted) catchValue(caught *bool) { *caught = recover() != nil }
declare impl BorrowedCatch for Adapted;
type Native int
func (value Native) catchPanic(caught *bool) { *caught = recover() != nil }
func direct(caught *bool) {
 value := []int{1}
 defer value.catchPanic(caught)
 panic("direct")
}
func saved(caught *bool) {
 value := []int{1}
 deferred := value.catchPanic
 defer deferred(caught)
 panic("saved")
}
func directPrivate(caught *bool) {
 value := model::Make()
 defer value.catchPanic(caught)
 panic("direct private")
}
func savedPrivate(caught *bool) {
 value := model::Make()
 deferred := value.catchPanic
 defer deferred(caught)
 panic("saved private")
}
func concreteDirect(caught *bool) {
 value := Concrete(1)
 defer value.catchPanic(caught)
 panic("concrete direct")
}
func concreteSaved(caught *bool) {
 value := Concrete(1)
 deferred := value.catchPanic
 defer deferred(caught)
 panic("concrete saved")
}
func bind[T Catch](value T) func(*bool) { return value.catchPanic }
func forward[T Catch](value T) func(*bool) { return bind[T](value) }
func boundSaved(caught *bool) {
 deferred := forward([]int{1})
 defer deferred(caught)
 panic("bound")
}
func boundPrivateSaved(caught *bool) {
 deferred := forward(model::Make())
 defer deferred(caught)
 panic("private bound")
}
func privateVariadic(caught *bool) {
 values := model::Make()
 deferred := values.catchWith
 defer deferred(caught, values...)
 panic("private variadic")
}
func privateSelfResult(caught *bool) {
 values := model::Make()
 deferred := values.catchSelf
 defer deferred(caught)
 panic("private self result")
}
func declaredDirect(caught *bool) {
 value := Adapted(1)
 defer value.catchNative::<DeclaredCatch>(caught)
 panic("declared direct")
}
func declaredSaved(caught *bool) {
 value := Adapted(1)
 deferred := value.catchNative::<DeclaredCatch>;
 defer deferred(caught)
 panic("declared saved")
}
func declaredBorrowedDirect(caught *bool) {
 value := Adapted(1)
 defer value.catchValue::<BorrowedCatch>(caught)
 panic("declared borrowed direct")
}
func declaredBorrowedSaved(caught *bool) {
 value := Adapted(1)
 deferred := value.catchValue::<BorrowedCatch>;
 defer deferred(caught)
 panic("declared borrowed saved")
}
func borrowedBound[T BorrowedCatch](value &T, caught *bool) {
 defer value.catchValue(caught)
 panic("declared borrowed bound")
}
func declaredBorrowedBound(caught *bool) {
 value := Adapted(1)
 borrowedBound(&value, caught)
}
func native(caught *bool) {
 value := Native(1)
 deferred := value.catchPanic
 defer deferred(caught)
 panic("native")
}
func observe(run func(*bool)) bool {
 caught := false
 func() {
  defer func() { _ = recover() }()
  run(&caught)
 }()
 return caught
}
func main() {
 fmt::Println(observe(direct), observe(saved), observe(directPrivate), observe(savedPrivate), observe(native))
 fmt::Println("concrete", observe(concreteDirect), observe(concreteSaved), "bound", observe(boundSaved), observe(boundPrivateSaved), "private-variadic", observe(privateVariadic), "private-self-result", observe(privateSelfResult))
 fmt::Println("declared", observe(declaredDirect), observe(declaredSaved), "borrowed", observe(declaredBorrowedDirect), observe(declaredBorrowedSaved), observe(declaredBorrowedBound))
}
`}, "true true true true true\nconcrete true true bound true true private-variadic true private-self-result true\ndeclared true true borrowed true true true\n")
}

func TestTraitMethodValuePreservesReceiverCopiesAndBorrowedSlots(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
trait Mixed {
 func next(self Self) int
 func peek(self &Self) int
 func add(self &mut Self, n int) int
}
type Counter[T any] struct { N int; value T }
trait Marker {}
impl[T any] Marker for []T {}
impl[T any] Counter[T] where []T:Marker {
 pub func ownNext(self Self) int { self.N++; return self.N }
}
impl[T any] Mixed for Counter[T] {
 func next(self Self) int { self.N++; return self.N }
 func peek(self &Self) int { return self.N }
 func add(self &mut Self, n int) int { self.N += n; return self.N }
}
func selectCounter(calls *int, value *Counter[int]) *Counter[int] { *calls += 1; return value }
func main() {
 calls := 0
 value := Counter[int]{N:3,value:7}
 next := selectCounter(&calls, &value).next
 peek := selectCounter(&calls, &value).peek
 add := selectCounter(&calls, &value).add
 own := selectCounter(&calls, &value).ownNext
 value.N = 10
 first, second := next(), next()
 before := peek()
 one, two := add(5), add(2)
 fmt::Println(first, second, before, one, two, peek(), value.N, calls, own(), own())
}
`}, "4 4 10 15 17 17 17 4 4 4\n")
}

func TestTraitMethodValueConstCompositeBoundFrame(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{
		"model/lib.gox": `package model
struct secret {}
pub func Make() [2]secret { return [2]secret{} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
trait Catch { func catchPanic(self Self, caught *bool) }
impl[T any,const N int] Catch for [N]T {
 func catchPanic(self Self, caught *bool) { *caught = recover() != nil }
}
func bindArray[T any,const N int](value [N]T) (func(*bool)) where [N]T:Catch { return value.catchPanic }
func forwardArray[T any,const N int](value [N]T) (func(*bool)) where [N]T:Catch { return bindArray[T,N](value) }
func nameable(caught *bool) {
 deferred := forwardArray([2]int{1,2})
 defer deferred(caught)
 panic("nameable const array")
}
func private(caught *bool) {
 deferred := forwardArray(model::Make())
 defer deferred(caught)
 panic("private const array")
}
func observe(run func(*bool)) bool {
 caught := false
 func() { defer func() { _ = recover() }(); run(&caught) }()
 return caught
}
func main() { fmt::Println(observe(nameable), observe(private)) }
`}, "true true\n")
}
