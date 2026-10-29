package lower_test

import "testing"

func TestTraitMutualRecursionRunsConcretePartners(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
trait Score { func score(self Self, count int) int }
trait Other { func other(self Self, count int) int }
impl Score for int { func score(self Self, count int) int { return self } }
impl Score for string { func score(self Self, count int) int { return len(self) } }
type Box[T any] struct { value T }
impl[T Score] Score for Box[T] {
 func score(self Self, count int) int { if count == 0 { return self.value.score(0) }; return Other::other(self, count-1) }
}
impl[T Score] Other for Box[T] {
 func other(self Self, count int) int { if count == 0 { return self.value.score(0) }; return Score::score(self, count-1) }
}
func Even[T Score](value T, count int) int { if count == 0 { return value.score(3) }; return Odd[T](value, count-1) }
func Odd[T Score](value T, count int) int { if count == 0 { return value.score(3) }; return Even[T](value, count-1) }
func main() {
 number, text := Box[int]{value:7}, Box[string]{value:"seven"}
 fmt::Println(Even[Box[int]](number,4), Odd(text,3), Other::other(Box[int]{value:9},2))
}
`}, "7 5 9\n")
}
