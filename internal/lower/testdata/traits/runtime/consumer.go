package main

import (
	"fmt"

	"traitruntime/model"
)

func main() {
	fmt.Println(model.Selection())
	fmt.Println(model.Conversions())
	fmt.Println(model.ReceiverOnce(), model.QualifiedFlow(), model.BuildTransformed[model.Cell]().Value)
	value := (model.CounterOps{}).New(4)
	copyView := model.AsCounterScore(value)
	readView := model.AsCounterInspect(&value)
	writeView := value.AsAdjust()
	writeView.Add(3)
	fmt.Println(copyView.Score(), copyView.Twice(), copyView.Shadow(), copyView.Sum(1, 2, 3), readView.Read(), writeView.Read(), value.Score())
	mixed := model.AsCounterMixed(&value)
	mixed.Overwrite(9)
	fmt.Println(mixed.Read(), value.Items[0])
	_, nativeTrait := any(value).(model.Score)
	_, nativeTwice := any(value).(interface{ Twice() int })
	_, concreteAny := model.ConcreteAny().(model.Counter)
	fmt.Println(nativeTrait, nativeTwice, concreteAny)
}
