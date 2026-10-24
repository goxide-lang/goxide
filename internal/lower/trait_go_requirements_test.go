package lower_test

import "testing"

func TestTraitGoPrerequisitesGenerateOrdinaryGo(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
trait Child where Self:Key {}
trait Key where Self:comparable {}
type ID int
impl Key for ID {}
impl Child for ID {}
func Index[T Child](value T)map[T]int{return map[T]int{value:7}}

trait Table[K any] where K:comparable {}
struct Cell { N int }
impl Table[int] for Cell {}
func Store[K any,T Table[K]](value T,key K)map[K]T{return map[K]T{key:value}}

trait EqualKey where Self:comparable {func equal(self Self,other Self)bool}
impl[T any] EqualKey for T{func equal(self Self,other Self)bool{return self==other}}
func Equal[T EqualKey](left T,right T)bool{return left.equal(right)}

trait Reader where Self:interface{Read() int} {}
impl Cell { pub func read(self Self)int{return self.N} }
impl Reader for Cell {}
func Read[T Reader](value T)int{return value.Read()}
func main(){fmt::Println(Index(ID(1))[ID(1)],Store(Cell{N:8},2)[2].N,Equal(3,3),Equal("a","b"),Read(Cell{N:9}))}
`}, "7 8 true false 9\n")
}
