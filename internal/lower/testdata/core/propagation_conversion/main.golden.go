package main

import fmt "fmt"
import sum "github.com/goxide-lang/std/sum"

type NilError struct{}

func (*NilError) Error() string { panic("typed nil must not be formatted") }

type Wrapped struct{ Cause error }

func (Wrapped) Error() string { return "wrapped" }

var conversions int
var assignableConversions int
var continued bool

func (GoxideWrappedFromErrorOps_aab95634) From(err error) Wrapped {
	conversions++
	return Wrapped{Cause: err}
}

func (GoxideErrorFromWrappedOps_0374e4f6) From(err Wrapped) error {
	assignableConversions++
	return err
}

func source() sum.Result[int, error] {
	var err *NilError
	return sum.NewErr[int, error](err)
}

func converted() sum.Result[string, Wrapped] {
	{
		gxTry1 := source()
		switch sum.TagResult(gxTry1) {
		case 1:
		case 2:
			return sum.NewErr[string, Wrapped]((GoxideWrappedFromErrorOps_aab95634{}).From(sum.ValueErr(gxTry1)))
		default:
			panic("goxide: invalid Result at ?")
		}
	}

	continued = true
	return sum.NewOk[string, Wrapped]("incorrect")
}

func assigned() sum.Result[int, error] {
	{
		gxTry2 := converted()
		switch sum.TagResult(gxTry2) {
		case 1:
		case 2:
			return sum.NewErr[int, error](sum.ValueErr(gxTry2))
		default:
			panic("goxide: invalid Result at ?")
		}
	}

	continued = true
	return sum.NewOk[int, error](0)
}

func readError(value sum.Result[int, error]) error {
	{
		var gxMatch3 error
		switch gxTag5 := sum.TagResult(value); gxTag5 {
		case 2:
			err := sum.ValueErr(value)
			gxMatch3 = err
		case 1:
			gxMatch3 = nil
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch3
	}
}

func main() {
	err := readError(assigned())
	wrapper, ok := err.(Wrapped)
	p, typed := wrapper.Cause.(*NilError)
	fmt.Println(conversions, assignableConversions, ok, typed && p == nil && wrapper.Cause != nil, continued)
}

type GoxideWrappedFromErrorOps_aab95634 struct {
}
type GoxideErrorFromWrappedOps_0374e4f6 struct {
}
