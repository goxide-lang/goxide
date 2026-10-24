package main

import fmt "fmt"
import std "github.com/goxide-lang/std"
import typeinfo "github.com/goxide-lang/std/typeinfo"

type Data struct {
	Items  []int
	Counts map[string]int
}
type _State interface {
	isState()
	GoxideInternalTypeCarrierForSelf(_State) goxideStateOps
	AsDerivedPartialEq() std.DerivedPartialEq
}
type _Pair struct {
	Value Data
}

func (*_Pair) isState() {
}
func (*_Pair) GoxideInternalTypeCarrierForSelf(_State) goxideStateOps {
	return goxideStateOps{}
}
func (target *_Pair) AsDerivedPartialEq() std.DerivedPartialEq {
	return AsStateDerivedPartialEq(target)
}

type _Empty struct {
}

func (*_Empty) isState() {
}
func (*_Empty) GoxideInternalTypeCarrierForSelf(_State) goxideStateOps {
	return goxideStateOps{}
}
func (target *_Empty) AsDerivedPartialEq() std.DerivedPartialEq {
	return AsStateDerivedPartialEq(target)
}
func (gxView2 goxideDataClone_9b97b809) Clone() Data {
	self := gxView2.target
	return Data{Items: std.GoxideIntSliceCloneClone_2d73407d(&self.Items), Counts: std.GoxideMapStringIntCloneClone_6fe526ba(&self.Counts)}
}
func (gxView3 goxideStateClone_faf8ac7b) Clone() _State {
	self := gxView3.target
	{
		var gxMatch4 _State
		gxOwner5 := &*self
		var gxMatch6 _State = *gxOwner5
		if gxMatch6 == nil {
			panic("goxide: match on nil State at fixture/main.gox:9")
		}
		switch gxVar7 := gxMatch6.(type) {
		case *_Pair:
			if gxVar7 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			gxPayloadRef8 := &gxVar7.Value
			variantField0 := gxPayloadRef8
			gxMatch4 = &_Pair{Value: typeinfo.Of[Data]().GoxideTraitClone_78b3516d_Clone(variantField0)}
		case *_Empty:
			if gxVar7 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			gxMatch4 = &_Empty{}
		default:
			panic("goxide: invalid enum State")
		}
		return gxMatch4
	}
}
func (goxideDataOps) GoxideTraitClone_78b3516d_Clone(self *Data) Data {
	return Data{Items: std.GoxideIntSliceCloneClone_2d73407d(&self.Items), Counts: std.GoxideMapStringIntCloneClone_6fe526ba(&self.Counts)}
}
func (goxideStateOps) GoxideTraitClone_78b3516d_Clone(self *_State) _State {
	{
		var gxMatch32 _State
		gxOwner33 := &*self
		var gxMatch34 _State = *gxOwner33
		if gxMatch34 == nil {
			panic("goxide: match on nil State at fixture/main.gox:9")
		}
		switch gxVar35 := gxMatch34.(type) {
		case *_Pair:
			if gxVar35 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			gxPayloadRef36 := &gxVar35.Value
			variantField0 := gxPayloadRef36
			gxMatch32 = &_Pair{Value: typeinfo.Of[Data]().GoxideTraitClone_78b3516d_Clone(variantField0)}
		case *_Empty:
			if gxVar35 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			gxMatch32 = &_Empty{}
		default:
			panic("goxide: invalid enum State")
		}
		return gxMatch32
	}
}

func main() {
	data := Data{Items: []int{1}, Counts: map[string]int{"one": 2}}
	var view *Data = &data
	clone := typeinfo.Of[Data]().GoxideTraitClone_78b3516d_Clone(view)
	clone.Items[0] = 9
	clone.Counts["one"] = 8
	fmt.Println(typeinfo.Of[Data]().GoxideTraitPartialEq_01e33857_Equals(data, data), typeinfo.Of[Data]().GoxideTraitPartialEq_01e33857_Equals(data, data), data.Items[0], data.Counts["one"], clone.Items[0], clone.Counts["one"])
	fmt.Println(typeinfo.Of[Data]().GoxideTraitDebug_7c1eb6e1_Debug(view))
	var state _State = &_Pair{Value: data}
	var stateView *_State = &state
	var stateClone _State = typeinfo.Of[_State]().GoxideTraitClone_78b3516d_Clone(stateView)
	{
		if stateClone == nil {
			panic("goxide: match on nil State at fixture/main.gox:24")
		}
		switch gxVar1 := stateClone.(type) {
		case *_Pair:
			if gxVar1 == nil {
				panic("goxide: match on nil State at fixture/main.gox:24")
			}
			value := gxVar1.Value
			value.Items[0] = 7
			value.Counts["one"] = 6
		case *_Empty:
			if gxVar1 == nil {
				panic("goxide: match on nil State at fixture/main.gox:24")
			}
		default:
			panic("goxide: invalid enum State")
		}
	}

	fmt.Println(typeinfo.Of[_State]().GoxideTraitPartialEq_01e33857_Equals(state, state), typeinfo.Of[_State]().GoxideTraitPartialEq_01e33857_Equals(state, state))
	fmt.Println(typeinfo.Of[_State]().GoxideTraitDebug_7c1eb6e1_Debug(stateView), typeinfo.Of[_State]().GoxideTraitDebug_7c1eb6e1_Debug(&stateClone))
}

type goxideDataClone_9b97b809 struct {
	target *Data
}
type goxideStateClone_faf8ac7b struct {
	target *_State
}
type goxideDataDebug_93af722b struct {
	target *Data
}

func (gxView9 goxideDataDebug_93af722b) Debug() string {
	self := gxView9.target
	return "Data {" + " Items: " + std.GoxideIntSliceDebugDebug_b47193c1(&self.Items) + ", Counts: " + std.GoxideMapStringIntDebugDebug_108030be(&self.Counts) + " }"
}
func (goxideDataDebug_93af722b) GoxideTraitDebug_7c1eb6e1() {
}
func AsDataDebug(target *Data) std.Debug {
	return goxideDataDebug_93af722b{target: target}
}

type goxideStateDebug_94a0a6bc struct {
	target *_State
}

func (gxView10 goxideStateDebug_94a0a6bc) Debug() string {
	self := gxView10.target
	{
		var gxMatch11 string
		gxOwner12 := &*self
		var gxMatch13 _State = *gxOwner12
		if gxMatch13 == nil {
			panic("goxide: match on nil State at fixture/main.gox:9")
		}
		switch gxVar14 := gxMatch13.(type) {
		case *_Pair:
			if gxVar14 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			gxPayloadRef15 := &gxVar14.Value
			variantField0 := gxPayloadRef15
			gxMatch11 = "State::Pair" + "(" + typeinfo.Of[Data]().GoxideTraitDebug_7c1eb6e1_Debug(variantField0) + ")"
		case *_Empty:
			if gxVar14 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			gxMatch11 = "State::Empty"
		default:
			panic("goxide: invalid enum State")
		}
		return gxMatch11
	}
}
func (goxideStateDebug_94a0a6bc) GoxideTraitDebug_7c1eb6e1() {
}
func AsStateDebug(target *_State) std.Debug {
	return goxideStateDebug_94a0a6bc{target: target}
}

type goxideDataDerivedPartialEq_11876111 struct {
	target Data
}

func (goxideDataDerivedPartialEq_11876111) GoxideTraitDerivedPartialEq_21a040d9() {
}
func AsDataDerivedPartialEq(target Data) std.DerivedPartialEq {
	return goxideDataDerivedPartialEq_11876111{target: target}
}

type goxideStatePartialEq_eb7789ae struct {
	target _State
}

func (goxideDataOps) GoxideTraitDebug_7c1eb6e1_Debug(self *Data) string {
	return "Data {" + " Items: " + std.GoxideIntSliceDebugDebug_b47193c1(&self.Items) + ", Counts: " + std.GoxideMapStringIntDebugDebug_108030be(&self.Counts) + " }"
}
func (goxideStateOps) GoxideTraitDebug_7c1eb6e1_Debug(self *_State) string {
	{
		var gxMatch37 string
		gxOwner38 := &*self
		var gxMatch39 _State = *gxOwner38
		if gxMatch39 == nil {
			panic("goxide: match on nil State at fixture/main.gox:9")
		}
		switch gxVar40 := gxMatch39.(type) {
		case *_Pair:
			if gxVar40 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			gxPayloadRef41 := &gxVar40.Value
			variantField0 := gxPayloadRef41
			gxMatch37 = "State::Pair" + "(" + typeinfo.Of[Data]().GoxideTraitDebug_7c1eb6e1_Debug(variantField0) + ")"
		case *_Empty:
			if gxVar40 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			gxMatch37 = "State::Empty"
		default:
			panic("goxide: invalid enum State")
		}
		return gxMatch37
	}
}

func (gxView16 goxideStatePartialEq_eb7789ae) Equals(other _State) bool {
	self := gxView16.target
	func(gxEnum17 _State) {
		switch gxVar18 := gxEnum17.(type) {
		case *_Pair:
			var gxNilVariant19 *_Pair
			if gxVar18 == gxNilVariant19 {
				panic("goxide: invalid enum State")
			}
		case *_Empty:
			var gxNilVariant20 *_Empty
			if gxVar18 == gxNilVariant20 {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
	}(self)
	func(gxEnum21 _State) {
		switch gxVar22 := gxEnum21.(type) {
		case *_Pair:
			var gxNilVariant23 *_Pair
			if gxVar22 == gxNilVariant23 {
				panic("goxide: invalid enum State")
			}
		case *_Empty:
			var gxNilVariant24 *_Empty
			if gxVar22 == gxNilVariant24 {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
	}(other)
	{
		var gxMatch25 bool
		if self == nil {
			panic("goxide: match on nil State at fixture/main.gox:9")
		}
		switch gxVar26 := self.(type) {
		case *_Pair:
			if gxVar26 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			enumLeft0 := gxVar26.Value
			var gxMatch27 bool
			if other == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			switch gxVar28 := other.(type) {
			case *_Pair:
				if gxVar28 == nil {
					panic("goxide: match on nil State at fixture/main.gox:9")
				}
				enumRight0 := gxVar28.Value
				gxMatch27 = typeinfo.Of[Data]().GoxideTraitPartialEq_01e33857_Equals(enumLeft0, enumRight0)
			case *_Empty:
				if gxVar28 == nil {
					panic("goxide: match on nil State at fixture/main.gox:9")
				}
				gxMatch27 = false
			default:
				gxMatch27 = false
			}
			gxMatch25 = gxMatch27
		case *_Empty:
			if gxVar26 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			var gxMatch29 bool
			if other == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			switch gxVar30 := other.(type) {
			case *_Empty:
				if gxVar30 == nil {
					panic("goxide: match on nil State at fixture/main.gox:9")
				}
				gxMatch29 = true
			case *_Pair:
				if gxVar30 == nil {
					panic("goxide: match on nil State at fixture/main.gox:9")
				}
				gxMatch29 = false
			default:
				gxMatch29 = false
			}
			gxMatch25 = gxMatch29
		default:
			panic("goxide: invalid enum State")
		}
		return gxMatch25
	}
}

type goxideStateDerivedPartialEq_42b233af struct {
	target _State
}

func (goxideStateDerivedPartialEq_42b233af) GoxideTraitDerivedPartialEq_21a040d9() {
}
func AsStateDerivedPartialEq(target _State) std.DerivedPartialEq {
	return goxideStateDerivedPartialEq_42b233af{target: target}
}

type goxideDataPartialEq_6d0e264c struct {
	target Data
}

func (gxView31 goxideDataPartialEq_6d0e264c) Equals(other Data) bool {
	self := gxView31.target
	return std.GoxideIntSlicePartialEqEquals_73bec2dc(self.Items, other.Items) && std.GoxideMapStringIntPartialEqEquals_d5c817a8(self.Counts, other.Counts)
}

type goxideDataOps struct {
}
type GoxideDataOps = goxideDataOps

func (Data) GoxideInternalTypeCarrierForSelf(Data) goxideDataOps {
	return goxideDataOps{}
}
func (goxideDataOps) GoxideTraitPartialEq_01e33857_Equals(self Data, other Data) bool {
	return std.GoxideIntSlicePartialEqEquals_73bec2dc(self.Items, other.Items) && std.GoxideMapStringIntPartialEqEquals_d5c817a8(self.Counts, other.Counts)
}

type goxideStateOps struct {
}
type GoxideStateOps = goxideStateOps

func (goxideStateOps) GoxideTraitPartialEq_01e33857_Equals(self _State, other _State) bool {
	func(gxEnum42 _State) {
		switch gxVar43 := gxEnum42.(type) {
		case *_Pair:
			var gxNilVariant44 *_Pair
			if gxVar43 == gxNilVariant44 {
				panic("goxide: invalid enum State")
			}
		case *_Empty:
			var gxNilVariant45 *_Empty
			if gxVar43 == gxNilVariant45 {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
	}(self)
	func(gxEnum46 _State) {
		switch gxVar47 := gxEnum46.(type) {
		case *_Pair:
			var gxNilVariant48 *_Pair
			if gxVar47 == gxNilVariant48 {
				panic("goxide: invalid enum State")
			}
		case *_Empty:
			var gxNilVariant49 *_Empty
			if gxVar47 == gxNilVariant49 {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
	}(other)
	{
		var gxMatch50 bool
		if self == nil {
			panic("goxide: match on nil State at fixture/main.gox:9")
		}
		switch gxVar51 := self.(type) {
		case *_Pair:
			if gxVar51 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			enumLeft0 := gxVar51.Value
			var gxMatch52 bool
			if other == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			switch gxVar53 := other.(type) {
			case *_Pair:
				if gxVar53 == nil {
					panic("goxide: match on nil State at fixture/main.gox:9")
				}
				enumRight0 := gxVar53.Value
				gxMatch52 = typeinfo.Of[Data]().GoxideTraitPartialEq_01e33857_Equals(enumLeft0, enumRight0)
			case *_Empty:
				if gxVar53 == nil {
					panic("goxide: match on nil State at fixture/main.gox:9")
				}
				gxMatch52 = false
			default:
				gxMatch52 = false
			}
			gxMatch50 = gxMatch52
		case *_Empty:
			if gxVar51 == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			var gxMatch54 bool
			if other == nil {
				panic("goxide: match on nil State at fixture/main.gox:9")
			}
			switch gxVar55 := other.(type) {
			case *_Empty:
				if gxVar55 == nil {
					panic("goxide: match on nil State at fixture/main.gox:9")
				}
				gxMatch54 = true
			case *_Pair:
				if gxVar55 == nil {
					panic("goxide: match on nil State at fixture/main.gox:9")
				}
				gxMatch54 = false
			default:
				gxMatch54 = false
			}
			gxMatch50 = gxMatch54
		default:
			panic("goxide: invalid enum State")
		}
		return gxMatch50
	}
}

type goxideForState struct {
	target *_State
}

func ForState(target *_State) goxideForState {
	return goxideForState{target: target}
}
func (view goxideForState) AsDebug() std.Debug {
	return AsStateDebug(view.target)
}
func (view goxideForState) AsDerivedPartialEq() std.DerivedPartialEq {
	return AsStateDerivedPartialEq(*view.target)
}
func (gxReceiver56 *Data) Clone() Data {
	return typeinfo.Of[Data]().GoxideTraitClone_78b3516d_Clone(gxReceiver56)
}
func (gxReceiver57 *Data) Debug() string {
	return typeinfo.Of[Data]().GoxideTraitDebug_7c1eb6e1_Debug(gxReceiver57)
}
func (gxReceiver58 Data) Equals(other Data) bool {
	return typeinfo.Of[Data]().GoxideTraitPartialEq_01e33857_Equals(gxReceiver58, other)
}
