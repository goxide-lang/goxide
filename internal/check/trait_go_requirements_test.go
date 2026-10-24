package check_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestTraitGoPrerequisitesRejectInvalidImplementations(t *testing.T) {
	for name, source := range map[string]string{
		"unused-comparable-self": `trait Key where Self:comparable {};type Bad []int;impl Key for Bad {}`,
		"struct-with-slice":      `trait Key where Self:comparable {};type Bad struct{Values []int};impl Key for Bad {}`,
		"inherited-self":         `trait Child where Self:Key {};trait Key where Self:comparable {};type Bad []int;impl Key for Bad {};impl Child for Bad {}`,
		"native-type-set":        `trait Scalar where Self:interface{~int|~string} {};impl Scalar for bool {}`,
		"missing-native-method":  `trait Reader where Self:interface{Read() int} {};struct Bad{};impl Reader for Bad {}`,
		"trait-input":            `trait Table[K comparable] {};impl Table[[]int] for int {}`,
		"inherited-input":        `trait Key where Self:comparable {};trait Table[K any] where K:Key {};impl Key for []int {};impl Table[[]int] for int {}`,
	} {
		t.Run(name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+source)
			_, diagnostics := check.Check(files, resolved)
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Message, "trait Go prerequisite is not satisfied") || name == "trait-input" && strings.Contains(diagnostic.Message, "trait input does not satisfy declared constraint") {
					return
				}
			}
			t.Fatalf("invalid trait native prerequisite accepted: %v", diagnostics)
		})
	}
}

func TestTraitGoPrerequisitesInDefinitionBounds(t *testing.T) {
	for name, source := range map[string]string{
		"self-comparable":                  `trait Key where Self:comparable {};type ID int;impl Key for ID {};func Index[T Key](value T)map[T]int{return map[T]int{value:7}};func f(){_=Index(ID(1))}`,
		"transitive-and-forward":           `trait Child where Self:Middle {};trait Middle where Self:Key {};trait Key where Self:comparable {};type ID int;impl Key for ID {};impl Middle for ID {};impl Child for ID {};func Index[T Child](value T)map[T]int{return map[T]int{value:7}};func f(){_=Index(ID(1))}`,
		"trait-method-signature":           `trait Child where Self:Key {func index(self Self)map[Self]int};trait Key where Self:comparable {};type ID int;impl Key for ID {};impl Child for ID {func index(self Self)map[Self]int{return map[Self]int{self:7}}}`,
		"native-type-set":                  `trait Scalar where Self:interface{~int|~string} {};impl Scalar for int {};func Double[T Scalar](value T)T{return value+value};func f()int{return Double(21)}`,
		"native-method":                    `trait Reader where Self:interface{Read() int} {};struct Value{};func(v Value)Read()int{return 7};impl Reader for Value {};func Read[T Reader](value T)int{return value.Read()};func f()int{return Read(Value{})}`,
		"inherent-native-method":           `trait Reader where Self:interface{Read() int} {};struct Value{};impl Value{pub func read(self Self)int{return 7}};impl Reader for Value {};func Read[T Reader](value T)int{return value.Read()};func f()int{return Read(Value{})}`,
		"generic-implementation":           `trait Key where Self:comparable {func equal(self Self,other Self)bool};impl[T any] Key for T{func equal(self Self,other Self)bool{return self==other}};func Equal[T Key](left T,right T)bool{return left.equal(right)};func f()bool{return Equal(3,3)}`,
		"input-constraint":                 `trait Key[K any] where K:comparable {};type Value int;impl Key[int] for Value {};func Index[K any,T Key[K]](value T,key K)map[K]T{return map[K]T{key:value}};func f(){_=Index(Value(1),7)}`,
		"inherited-input-constraint":       `trait NativeKey where Self:comparable {};trait Key[K any] where K:NativeKey {};type Value int;impl NativeKey for int {};impl Key[int] for Value {};func Index[K any,T Key[K]](value T,key K)map[K]T{return map[K]T{key:value}};func f(){_=Index(Value(1),7)}`,
		"native-prerequisite-disjointness": `trait Key where Self:comparable {};trait Choice {};impl[T any] Key for T {};impl[T Key] Choice for T {};impl[T any] Choice for []T {}`,
	} {
		t.Run(name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+source)
			_, diagnostics := check.Check(files, resolved)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
		})
	}
}

func TestTraitGoPrerequisitesPreserveNativeMethodOverlap(t *testing.T) {
	files, resolved := parse(t, `package p;
trait Reader where Self:interface{Read() int} {};
struct Value{};
impl Value{pub func read(self Self)int{return 7}};
impl Reader for Value{};
impl[T any] Reader for T{};
`)
	_, diagnostics := check.Check(files, resolved)
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "overlapping applications") {
			return
		}
	}
	t.Fatalf("native prerequisite hid overlapping implementations: %v", diagnostics)
}
