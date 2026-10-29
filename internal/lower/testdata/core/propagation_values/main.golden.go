package main

import fmt "fmt"
import sum "github.com/goxide-lang/std/sum"
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
	gxTry1 := explicitPartial()
	switch sum.TagResult(gxTry1) {
	case 1:
	case 2:
		return sum.NewErr[ReadOutcome, error](sum.ValueErr(gxTry1))
	default:
		panic("goxide: invalid Result at ?")
	}
	gxOk2 := sum.ValueOk(gxTry1)

	value := gxOk2
	return sum.NewOk[ReadOutcome, error](value)
}

func sourceError() sum.Result[int, *NilError] {
	var err *NilError
	return sum.NewErr[int, *NilError](err)
}

func assignableError() sum.Result[string, error] {
	{
		gxTry3 := sourceError()
		switch sum.TagResult(gxTry3) {
		case 1:
		case 2:
			return sum.NewErr[string, error](sum.ValueErr(gxTry3))
		default:
			panic("goxide: invalid Result at ?")
		}
	}

	return sum.NewOk[string, error]("incorrect")
}

func readOutcome(value sum.Result[ReadOutcome, error]) ReadOutcome {
	{
		var gxMatch4 ReadOutcome
		switch gxTag6 := sum.TagResult(value); gxTag6 {
		case 1:
			out := sum.ValueOk(value)
			gxMatch4 = out
		case 2:
			gxMatch4 = ReadOutcome{}
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch4
	}
}

func readError(value sum.Result[string, error]) error {
	{
		var gxMatch7 error
		switch gxTag9 := sum.TagResult(value); gxTag9 {
		case 1:
			gxMatch7 = nil
		case 2:
			err := sum.ValueErr(value)
			gxMatch7 = err
		default:
			panic("goxide: invalid enum Result")
		}
		return gxMatch7
	}
}

func invalid() sum.Result[int, error] { var value sum.Result[int, error]; return value }
func invalidPropagation(touched *bool) sum.Result[string, error] {
	{
		gxTry10 := invalid()
		switch sum.TagResult(gxTry10) {
		case 1:
		case 2:
			return sum.NewErr[string, error](sum.ValueErr(gxTry10))
		default:
			panic("goxide: invalid Result at ?")
		}
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
