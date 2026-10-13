package main

import (
	"fmt"
	"github.com/goxide-lang/std/sum"
	"strings"
)

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
	value := explicitPartial()
	sum.ValidateResult(value)
	if sum.TagResult(value) == 2 {
		return sum.NewErr[ReadOutcome](sum.ValueErr(value))
	}
	return sum.NewOk[ReadOutcome, error](sum.ValueOk(value))
}
func sourceError() sum.Result[int, *NilError] {
	var err *NilError
	return sum.NewErr[int](err)
}
func assignableError() sum.Result[string, error] {
	value := sourceError()
	sum.ValidateResult(value)
	if sum.TagResult(value) == 2 {
		return sum.NewErr[string, error](sum.ValueErr(value))
	}
	return sum.NewOk[string, error]("incorrect")
}
func readOutcome(value sum.Result[ReadOutcome, error]) ReadOutcome {
	switch sum.TagResult(value) {
	case 1:
		return sum.ValueOk(value)
	case 2:
		return ReadOutcome{}
	}
	panic("goxide: invalid enum Result")
}
func readError(value sum.Result[string, error]) error {
	switch sum.TagResult(value) {
	case 1:
		return nil
	case 2:
		return sum.ValueErr(value)
	}
	panic("goxide: invalid enum Result")
}
func invalid() sum.Result[int, error] { var value sum.Result[int, error]; return value }
func invalidPropagation(touched *bool) sum.Result[string, error] {
	value := invalid()
	sum.ValidateResult(value)
	if sum.TagResult(value) == 2 {
		return sum.NewErr[string](sum.ValueErr(value))
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
	func() { defer func() { message = fmt.Sprint(recover()) }(); _ = invalidPropagation(&touched) }()
	fmt.Println(out.N, ok && p == nil && out.Err != nil, converted && q == nil && err != nil, strings.Contains(message, "Result"), !touched)
}
