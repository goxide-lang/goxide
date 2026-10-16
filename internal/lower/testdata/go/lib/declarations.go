package lib

import "fmt"

// offset is shared by both source files.
const Offset = 2
const (
	first = iota
	second
)

var Seed = 1
var left, right = 2, 3

type Number = int
type localNumber int

// widget retains its field documentation and tag.
type Widget struct {
	Value int `json:"value"`
	Note  string
}

type _Private struct {
	hidden int
}

type Base struct{ Offset int }
type Wrapper struct{ Base }
type PointerWrapper struct{ *Base }
type Valuer interface{ Value() int }
type EmbeddedInterface interface{ Valuer }
type Box[T any] struct{ Value T }

func (b Base) Value() int              { return b.Offset }
func (b Box[T]) Get() T                { return b.Value }
func choose[A, B any](a A, b B) (A, B) { return a, b }

//goxide:lower-fixture
func Identity[T interface{ ~int | ~int64 }](value T) T {
	return value
}

func Answer() int {
	p := _Private{hidden: 3}
	var value Number = Offset + Seed + p.hidden + second
	var wrapped Valuer = Wrapper{Base: Base{Offset: int(value)}}
	box := Box[int]{Value: wrapped.Value()}
	_ = fmt.Sprintf("%d", box.Get())
	_ = fmt.Sprint(left, right)
	return box.Get()
}
