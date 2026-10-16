package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"

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
	{
		var gxTry1 sum.Result[int, error] = source()
		var gxOk2 int
		switch sum.TagResult(gxTry1) {
		case 1:
			gxOk2 = sum.ValueOk(gxTry1)
		case 2:
			return sum.NewErr[string, Wrapped](FromWrapped(sum.ValueErr(gxTry1)))
		default:
			panic("goxide: invalid Result at ?")
		}

		_ = gxOk2
	}
	continued = true
	return sum.NewOk[string, Wrapped]("incorrect")
}

func assigned() sum.Result[int, error] {
	{
		var gxTry3 sum.Result[string, Wrapped] = converted()
		var gxOk4 string
		switch sum.TagResult(gxTry3) {
		case 1:
			gxOk4 = sum.ValueOk(gxTry3)
		case 2:
			return sum.NewErr[int, error](sum.ValueErr(gxTry3))
		default:
			panic("goxide: invalid Result at ?")
		}

		_ = gxOk4
	}
	continued = true
	return sum.NewOk[int, error](0)
}

func readError(value sum.Result[int, error]) error {
	{
		var gxMatch5 error
		switch gxTag7 := sum.TagResult(value); gxTag7 {
		case 2:
			err := sum.ValueErr(value)
			gxMatch5 = err
		case 1:
			gxMatch5 = nil
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch5
	}
}

func main() {
	err := readError(assigned())
	wrapper, ok := err.(Wrapped)
	p, typed := wrapper.Cause.(*NilError)
	fmt.Println(conversions, assignableConversions, ok, typed && p == nil && wrapper.Cause != nil, continued)
}
