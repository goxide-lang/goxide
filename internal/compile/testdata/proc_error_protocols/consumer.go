package main

import (
	"errors"
	model "example.org/error-model"
	"fmt"
)

type causeError struct{ code int }

func (e *causeError) Error() string { return "underlying" }

func verify() error {
	cause := &causeError{code: 17}
	value := model.New(cause)
	var native error = value
	if native.Error() != "cannot open { data }: 3 1.25 true" || !errors.Is(native, cause) || errors.Unwrap(native) != cause {
		return errors.New("native error method or cause changed")
	}
	var found *causeError
	if !errors.As(native, &found) || found != cause || found.code != 17 {
		return errors.New("native errors.As did not reach original cause")
	}
	var nilCause *causeError
	unwrapped := errors.Unwrap(model.New(nilCause))
	if typed, ok := unwrapped.(*causeError); !ok || typed != nil || unwrapped == nil {
		return errors.New("typed-nil source interface was collapsed")
	}
	if model.New(nil).Unwrap() != nil || model.EmptyError().Error() != "" || model.CodeError().Error() != "value 7" || !model.Check() {
		return errors.New("ordinary source behavior changed")
	}
	if _, ok := model.EmptyError().(interface{ Unwrap() error }); ok {
		return errors.New("source-free struct acquired Unwrap")
	}
	return nil
}
func main() {
	if err := verify(); err != nil {
		panic(err)
	}
	fmt.Println("ordinary Error struct migration passed")
}
