package main

import (
	"fmt"
	"github.com/goxide-lang/std/sum"
)

type NilError struct{}

func (*NilError) Error() string { panic("typed nil must not be formatted") }

type Wrapped struct{ Cause error }

func (Wrapped) Error() string { return "wrapped" }

var conversions int
var assignableConversions int
var continued bool

func FromWrapped(err error) Wrapped    { conversions++; return Wrapped{Cause: err} }
func FromAssignable(err Wrapped) error { assignableConversions++; return err }
func source() sum.Result[int, error] {
	var err *NilError
	return sum.NewErr[int, error](err)
}
func converted() sum.Result[string, Wrapped] {
	value := source()
	sum.ValidateResult(value)
	if sum.TagResult(value) == 2 {
		return sum.NewErr[string](FromWrapped(sum.ValueErr(value)))
	}
	continued = true
	return sum.NewOk[string, Wrapped]("incorrect")
}
func assigned() sum.Result[int, error] {
	value := converted()
	sum.ValidateResult(value)
	if sum.TagResult(value) == 2 {
		return sum.NewErr[int, error](sum.ValueErr(value))
	}
	continued = true
	return sum.NewOk[int, error](0)
}
func readError(value sum.Result[int, error]) error {
	switch sum.TagResult(value) {
	case 2:
		return sum.ValueErr(value)
	case 1:
		return nil
	}
	panic("goxide: invalid enum Result")
}
func main() {
	err := readError(assigned())
	wrapper, ok := err.(Wrapped)
	p, typed := wrapper.Cause.(*NilError)
	fmt.Println(conversions, assignableConversions, ok, typed && p == nil && wrapper.Cause != nil, continued)
}
