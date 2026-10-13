package main

import (
	"fmt"
	"github.com/goxide-lang/std/sum"
)

type Gate interface{ isGate() }
type Enter struct{ Value int }
type Shut struct{}

func (*Enter) isGate() {}
func (*Shut) isGate()  {}

func load(ok bool, trace *[]string) sum.Result[int, string] {
	*trace = append(*trace, "load")
	if !ok {
		return sum.NewErr[int]("failed")
	}
	return sum.NewOk[int, string](4)
}

func truth(trace *[]string) sum.Result[bool, string] {
	value := load(false, trace)
	sum.ValidateResult(value)
	if sum.TagResult(value) == 2 {
		return sum.NewErr[bool](sum.ValueErr(value))
	}
	return sum.NewOk[bool, string](sum.ValueOk(value) > 0)
}

func shortCircuit(left bool, trace *[]string) sum.Result[string, string] {
	value := false
	if left {
		result := truth(trace)
		sum.ValidateResult(result)
		if sum.TagResult(result) == 2 {
			return sum.NewErr[string](sum.ValueErr(result))
		}
		value = sum.ValueOk(result)
	}
	return sum.NewOk[string, string](fmt.Sprint(value))
}

func closure(trace *[]string) sum.Result[string, string] {
	inner := func() sum.Result[int, string] {
		defer func() { *trace = append(*trace, "inner-cleanup") }()
		value := load(false, trace)
		sum.ValidateResult(value)
		if sum.TagResult(value) == 2 {
			return sum.NewErr[int](sum.ValueErr(value))
		}
		return sum.NewOk[int, string](sum.ValueOk(value))
	}
	_ = inner()
	*trace = append(*trace, "outer")
	return sum.NewOk[string, string]("outer")
}

func deferred(ok bool, trace *[]string) sum.Result[string, string] {
	defer func() { *trace = append(*trace, "cleanup") }()
	value := load(ok, trace)
	sum.ValidateResult(value)
	if sum.TagResult(value) == 2 {
		return sum.NewErr[string](sum.ValueErr(value))
	}
	defer func(n int) { *trace = append(*trace, "deferred") }(sum.ValueOk(value))
	return sum.NewOk[string, string]("1")
}

func guarded(ok bool, trace *[]string) sum.Result[string, string] {
	defer func() { *trace = append(*trace, "guard-cleanup") }()
	var gate Gate = &Enter{1}
	if gate == nil {
		panic("goxide: match on nil Gate")
	}
	switch v := gate.(type) {
	case *Enter:
		if v.Value == 0 {
			result := truth(trace)
			sum.ValidateResult(result)
			if sum.TagResult(result) == 2 {
				return sum.NewErr[string](sum.ValueErr(result))
			}
			if sum.ValueOk(result) {
				return sum.NewOk[string, string]("incorrect")
			}
		}
		result := load(ok, trace)
		sum.ValidateResult(result)
		if sum.TagResult(result) == 2 {
			return sum.NewErr[string](sum.ValueErr(result))
		}
		if sum.ValueOk(result) == v.Value+3 {
			return sum.NewOk[string, string]("guard")
		}
	}
	return sum.NewOk[string, string]("other")
}

func optional(ok bool) sum.Option[string] {
	if !ok {
		return sum.None[string]()
	}
	return sum.NewSome("abc")
}
func length(ok bool) sum.Option[int] {
	value := optional(ok)
	sum.ValidateOption(value)
	if sum.TagOption(value) == 0 {
		return sum.None[int]()
	}
	return sum.NewSome(len(sum.ValueSome(value)))
}
func optionInt(value sum.Option[int]) int {
	switch sum.TagOption(value) {
	case 1:
		return sum.ValueSome(value)
	case 0:
		return -1
	}
	panic("goxide: invalid enum Option")
}
func text(value sum.Result[string, string]) string {
	switch sum.TagResult(value) {
	case 1:
		return sum.ValueOk(value)
	case 2:
		return sum.ValueErr(value)
	}
	panic("goxide: invalid enum Result")
}
func main() {
	for _, left := range []bool{false, true} {
		trace := []string{}
		result := shortCircuit(left, &trace)
		fmt.Println(text(result), trace)
	}
	trace := []string{}
	result := closure(&trace)
	fmt.Println(text(result), trace)
	for _, ok := range []bool{false, true} {
		trace := []string{}
		result := deferred(ok, &trace)
		fmt.Println(text(result), trace)
	}
	fmt.Println(optionInt(length(true)), optionInt(length(false)))
	for _, ok := range []bool{false, true} {
		trace := []string{}
		result := guarded(ok, &trace)
		fmt.Println(text(result), trace)
	}
}
