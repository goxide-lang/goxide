package main

import (
	"reflect"
	"strings"

	business "example.org/conversion-business"
	model "example.org/conversion-model"
	native "example.org/conversion-native"
	"github.com/goxide-lang/std/sum"
)

func verifyPropagation() {
	for _, success := range []bool{false, true} {
		model.ResetConversions()
		trace := []string{}
		result := model.TryFault(success, native.Sentinel, &trace)
		if success {
			if sum.ValueOk(result) != 8 || model.ConversionCalls() != 0 || !reflect.DeepEqual(trace, []string{"before", "operand", "after", "defer"}) {
				panic("Ok evaluated a conversion or changed operand order")
			}
		} else {
			failure := sum.ValueErr(result)
			if failure.Cause != native.Sentinel || failure.Kind != "fault" || model.ConversionCalls() != 1 || !reflect.DeepEqual(trace, []string{"before", "operand", "defer"}) {
				panic("Err did not convert exactly once and immediately return")
			}
		}
	}
	other := &native.OtherFault{Code: 9}
	model.ResetConversions()
	result := model.TryOther(sum.NewErr[int, *native.OtherFault](other))
	if failure := sum.ValueErr(result); failure.Kind != "other" || failure.Cause != other || model.ConversionCalls() != 1 {
		panic("second source selected the wrong standard From application")
	}
	var nilPointer *native.Fault
	trace := []string{}
	failure := sum.ValueErr(model.TryFault(false, nilPointer, &trace))
	if pointer, ok := failure.Cause.(*native.Fault); !ok || pointer != nil || failure.Cause == nil {
		panic("From propagation normalized a typed nil")
	}
	for _, take := range []bool{false, true} {
		model.ResetConversions()
		trace = nil
		value := model.TryShortCircuit(take, native.Sentinel, &trace)
		if take {
			if sum.TagResult(value) != 2 || model.ConversionCalls() != 1 || !reflect.DeepEqual(trace, []string{"operand"}) {
				panic("taken short-circuit branch did not propagate")
			}
		} else if sum.ValueOk(value) || len(trace) != 0 || model.ConversionCalls() != 0 {
			panic("skipped short-circuit branch evaluated the operand")
		}
	}
	if !model.TryNested(sum.NewErr[int, *native.Fault](native.Sentinel)) || model.TryNested(sum.NewOk[int, *native.Fault](1)) {
		panic("question mark did not return from the nearest closure")
	}
	if sum.ValueOk(model.TryNative(nil)) != 7 || sum.ValueErr(model.TryNative(native.Sentinel)).Err != native.Sentinel {
		panic("go_result changed native signatures or error identity")
	}
	if pointer, ok := sum.ValueErr(model.TryNativeTypedNil()).Err.(*native.Fault); !ok || pointer != nil {
		panic("go_result typed nil was lost by standard From[error]")
	}
	func() {
		defer func() {
			message, ok := recover().(string)
			if !ok || !strings.Contains(message, "invalid Result") {
				panic("invalid Result was accepted during propagation")
			}
		}()
		model.TryInvalid(sum.Result[int, *native.Fault]{})
	}()
	if sum.TagOption(model.KeepOption(sum.None[*native.Fault]())) != 0 || sum.ValueSome(model.KeepOption(sum.NewSome(nilPointer))) != nil {
		panic("Option propagation changed None or Some(nil)")
	}
	local := &model.LocalFault{Value: 4}
	for _, input := range []*model.LocalFault{local, nil} {
		model.ResetConversions()
		direct := model.DirectAssignable(sum.NewErr[int, *model.LocalFault](input))
		if sum.ValueErr(direct) != error(input) || model.ConversionCalls() != 0 {
			panic("TryAssign did not take precedence over an applicable explicit From")
		}
		withoutImport := business.AssignWithoutTraitImport(sum.NewErr[int, *model.LocalFault](input))
		if sum.ValueErr(withoutImport) != error(input) || model.ConversionCalls() != 0 {
			panic("assignment propagation depended on lexical trait imports")
		}
		instantiated := model.InstantiatedAssignable(input)
		if sum.ValueErr(instantiated) != native.Sentinel || model.ConversionCalls() != 1 {
			panic("generic definition-site From was replaced by concrete assignability")
		}
	}
	if sum.ValueErr(business.TryPrivate(42)) != 42 {
		panic("cross-package private error source was not converted in its defining package")
	}
	if sum.ValueOk(business.TryPrivateSuccess(true, 43)) != 43 || sum.ValueErr(business.TryPrivateSuccess(false, 44)) != 44 {
		panic("cross-package private success payload lost inferred identity")
	}
	if sum.ValueSome(business.TryPrivateOption(true, 45)) != 45 || sum.TagOption(business.TryPrivateOption(false, 46)) != 0 {
		panic("cross-package private Option payload changed success or None")
	}
	func() {
		defer func() {
			message, ok := recover().(string)
			if !ok || !strings.Contains(message, "invalid Result") {
				panic("private success payload was read before Result validity was checked")
			}
		}()
		business.TryPrivateInvalid()
	}()
	model.ResetConversions()
	if failure := sum.ValueErr(business.TryWithoutTraitImport()); failure.Cause != native.Sentinel || failure.Kind != "fault" || model.ConversionCalls() != 1 {
		panic("canonical From propagation depended on lexical trait imports")
	}
	for _, mode := range []int{0, 1, 2} {
		trace = nil
		input := sum.NewErr[int, model.Action](model.Action{Mode: mode, Trace: &trace})
		if mode == 0 {
			if sum.TagResult(model.TryAction(input, &trace)) != 2 {
				panic("conversion did not return an Err")
			}
		} else if mode == 1 {
			func() {
				defer func() {
					if recover() != "conversion panic" {
						panic("propagation caught or rewrote a conversion panic")
					}
				}()
				model.TryAction(input, &trace)
				panic("propagation returned after conversion panic")
			}()
		} else {
			done := make(chan struct{})
			go func() {
				defer close(done)
				model.TryAction(input, &trace)
				trace = append(trace, "returned")
			}()
			<-done
		}
		if !reflect.DeepEqual(trace, []string{"from", "from-defer", "caller-defer"}) {
			panic("propagation changed conversion/defer/Goexit frames")
		}
	}
}
