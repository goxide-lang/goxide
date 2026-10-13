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
		var goxideTry1 sum.Result[int, error] = source()
		var goxideOk2 int
		switch sum.TagResult(goxideTry1) {
		case 1:
			goxideOk2 = sum.ValueOk(goxideTry1)
		case 2:
			return sum.NewErr[string, Wrapped](FromWrapped(sum.ValueErr(goxideTry1)))
		default:
			panic("goxide: invalid Result at ?")
		}

		_ = goxideOk2
	}
	continued = true
	return sum.NewOk[string, Wrapped]("incorrect")
}

func assigned() sum.Result[int, error] {
	{
		var goxideTry3 sum.Result[string, Wrapped] = converted()
		var goxideOk4 string
		switch sum.TagResult(goxideTry3) {
		case 1:
			goxideOk4 = sum.ValueOk(goxideTry3)
		case 2:
			return sum.NewErr[int, error](sum.ValueErr(goxideTry3))
		default:
			panic("goxide: invalid Result at ?")
		}

		_ = goxideOk4
	}
	continued = true
	return sum.NewOk[int, error](0)
}

func readError(value sum.Result[int, error]) error {
	{
		var goxideMatch5 error
		var goxidematch6 sum.Result[int, error] = value
		switch goxidetag8 := sum.TagResult(goxidematch6); goxidetag8 {
		case 2:

			err := sum.ValueErr(goxidematch6)
			goxideMatch5 = err
		case 1:
			goxideMatch5 = nil
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch5
	}
}

func main() {
	err := readError(assigned())
	wrapper, ok := err.(Wrapped)
	p, typed := wrapper.Cause.(*NilError)
	fmt.Println(conversions, assignableConversions, ok, typed && p == nil && wrapper.Cause != nil, continued)
}
