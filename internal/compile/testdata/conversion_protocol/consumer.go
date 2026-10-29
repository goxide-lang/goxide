package main

import (
	"fmt"
	"reflect"

	business "example.org/conversion-business"
	model "example.org/conversion-model"
	native "example.org/conversion-native"
)

func observe(call func(*bool)) (caught bool, escaped any) {
	defer func() { escaped = recover() }()
	call(&caught)
	return
}

func verify() {
	for _, call := range []func(int) model.Count{model.Numeric, model.InferredInto, model.ReturnContext, model.ArgumentContext, model.UniqueInput, model.Factory()} {
		if value := call(7); value.N != 7 {
			panic("wrong From application or known context")
		}
	}
	if model.Textual("abc").N != 3 || model.FactoryExplicit()("abcd").N != 4 || model.Saved(11)().N != 11 || model.SavedImplicit(13)().N != 13 {
		panic("explicit application or method value")
	}
	one, two := model.Both(5, "six")
	if one.N != 5 || two.N != 3 {
		panic("multiple ParamEnv applications were conflated")
	}
	if summary := model.BoxFault(native.Sentinel); summary.Err != native.Sentinel {
		panic("a known From[error] did not preserve ordinary argument boxing")
	}
	if business.HiddenRoundTrip(41) != 41 || business.HiddenSource(23).N != 23 {
		panic("private source or target")
	}
	value := model.Count{N: 5}
	view := model.CountView(value)
	value.N = 9
	if view.Into() != 5 || model.IntView(6).Into().N != 6 {
		panic("Into view did not capture the selected source value")
	}
	values := []int{1, 2}
	sliceView := model.SliceView(values)
	values[0] = 8
	result := sliceView.Into()
	result[1] = 9
	if values[0] != 8 || values[1] != 9 || business.Ints(values)[0] != 8 || business.Texts([]string{"a"})[0] != "a" || business.Ints(nil) != nil {
		panic("generic independent Into lost ordinary shared storage")
	}
	var nilPointer *native.Fault
	var typedNil error = nilPointer
	if model.NilIdentity(nilPointer) != nil || model.NilIdentity(native.Sentinel) != native.Sentinel || model.ErrorIdentity(nil) != nil || model.ErrorIdentity(typedNil) != typedNil {
		panic("identity normalized nil or copied an object")
	}
	if pointer, ok := model.ErrorIdentity(typedNil).(*native.Fault); !ok || pointer != nil {
		panic("identity lost typed nil")
	}
	for _, call := range []func(*bool){model.DeferredFrom, model.SavedFrom} {
		if caught, escaped := observe(call); !caught || escaped != nil {
			panic("From gained a forwarding frame")
		}
	}
	for _, call := range []func(*bool){model.DeferredInto, model.SavedInto, model.RuntimeInto} {
		if caught, escaped := observe(call); caught || escaped == nil {
			panic("Into lost its ordinary body calling From")
		}
	}
	for _, call := range []func(model.Action) model.Outcome{model.ConvertAction, model.IntoAction} {
		trace := []string{}
		call(model.Action{Trace: &trace})
		if !reflect.DeepEqual(trace, []string{"from", "from-defer"}) {
			panic("conversion did not execute exactly once")
		}
		trace = nil
		func() {
			defer func() {
				if recover() != "conversion panic" {
					panic("conversion panic was caught or changed")
				}
				trace = append(trace, "caller-defer")
			}()
			call(model.Action{Mode: 1, Trace: &trace})
			panic("conversion returned after panic")
		}()
		if !reflect.DeepEqual(trace, []string{"from", "from-defer", "caller-defer"}) {
			panic("panic/defer ordering")
		}
		trace = nil
		done := make(chan struct{})
		go func() {
			defer func() { trace = append(trace, "caller-defer"); close(done) }()
			call(model.Action{Mode: 2, Trace: &trace})
			trace = append(trace, "returned")
		}()
		<-done
		if !reflect.DeepEqual(trace, []string{"from", "from-defer", "caller-defer"}) {
			panic("Goexit was changed into a return")
		}
	}
}

func main() {
	verify()
	fmt.Println("parameterized conversions, private types, views and call frames")
}
