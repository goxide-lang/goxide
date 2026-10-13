package main

import sum "github.com/goxide-lang/std/sum"
import fmt "fmt"
import strings "strings"

type NilError struct{}

func (*NilError) Error() string { panic("typed nil error was formatted") }

type ReadOutcome struct {
	N   int
	Err error
}

func nativePartial() (int, error) { var err *NilError; return 3, err }

func explicitPartial() sum.Result[ReadOutcome, error] {
	n, err := nativePartial()
	return sum.NewOk[ReadOutcome, error](ReadOutcome{N: n, Err: err})
}

func preservePartial() sum.Result[ReadOutcome, error] {
	var goxideTry1 sum.Result[ReadOutcome, error] = explicitPartial()
	var goxideOk2 ReadOutcome
	switch sum.TagResult(goxideTry1) {
	case 1:
		goxideOk2 = sum.ValueOk(goxideTry1)
	case 2:
		return sum.NewErr[ReadOutcome, error](sum.ValueErr(goxideTry1))
	default:
		panic("goxide: invalid Result at ?")
	}

	value := goxideOk2
	return sum.NewOk[ReadOutcome, error](value)
}

func sourceError() sum.Result[int, *NilError] {
	var err *NilError
	return sum.NewErr[int, *NilError](err)
}

func assignableError() sum.Result[string, error] {
	{
		var goxideTry3 sum.Result[int, *NilError] = sourceError()
		var goxideOk4 int
		switch sum.TagResult(goxideTry3) {
		case 1:
			goxideOk4 = sum.ValueOk(goxideTry3)
		case 2:
			return sum.NewErr[string, error](sum.ValueErr(goxideTry3))
		default:
			panic("goxide: invalid Result at ?")
		}

		_ = goxideOk4
	}
	return sum.NewOk[string, error]("incorrect")
}

func readOutcome(value sum.Result[ReadOutcome, error]) ReadOutcome {
	{
		var goxideMatch5 ReadOutcome
		var goxidematch6 sum.Result[ReadOutcome, error] = value
		switch goxidetag8 := sum.TagResult(goxidematch6); goxidetag8 {
		case 1:

			out := sum.ValueOk(goxidematch6)
			goxideMatch5 = out
		case 2:
			goxideMatch5 = ReadOutcome{}
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch5
	}
}

func readError(value sum.Result[string, error]) error {
	{
		var goxideMatch9 error
		var goxidematch10 sum.Result[string, error] = value
		switch goxidetag12 := sum.TagResult(goxidematch10); goxidetag12 {
		case 1:
			goxideMatch9 = nil
		case 2:
			err := sum.ValueErr(goxidematch10)
			goxideMatch9 = err
		default:
			panic("goxide: invalid enum Result")
		}
		return goxideMatch9
	}
}

func invalid() sum.Result[int, error] { var value sum.Result[int, error]; return value }
func invalidPropagation(touched *bool) sum.Result[string, error] {
	{
		var goxideTry13 sum.Result[int, error] = invalid()
		var goxideOk14 int
		switch sum.TagResult(goxideTry13) {
		case 1:
			goxideOk14 = sum.ValueOk(goxideTry13)
		case 2:
			return sum.NewErr[string, error](sum.ValueErr(goxideTry13))
		default:
			panic("goxide: invalid Result at ?")
		}

		_ = goxideOk14
	}
	*touched = true
	return sum.NewOk[string, error]("incorrect")
}

func main() {
	out := readOutcome(preservePartial())
	p, ok := out.Err.(*NilError)
	err := readError(assignableError())
	q, converted := err.(*NilError)
	message := ""
	touched := false
	func() {
		defer func() { message = fmt.Sprint(recover()) }()
		_ = invalidPropagation(&touched)
	}()
	fmt.Println(out.N, ok && p == nil && out.Err != nil, converted && q == nil && err != nil, strings.Contains(message, "Result"), !touched)
}
