package main

import sort "sort"
import strconv "strconv"
import strings "strings"
import fmt "fmt"

type Data struct {
	Items  []int
	Counts map[string]int
}
type _State interface {
	isState()
}
type _Pair struct {
	Value Data
}

func (*_Pair) isState() {
}

type _Empty struct {
}

func (*_Empty) isState() {
}

func main() {
	data := Data{Items: []int{1}, Counts: map[string]int{"one": 2}}
	var view *Data = &data
	clone := CloneData(view)
	clone.Items[0] = 9
	clone.Counts["one"] = 8
	fmt.Println(EqualsData((*view), data), EqualsData(*view, data), data.Items[0], data.Counts["one"], clone.Items[0], clone.Counts["one"])
	fmt.Println(DebugData(view))
	var state _State = &_Pair{Value: data}
	var stateView *_State = &state
	var stateClone _State = CloneState(stateView)
	{
		var goxidematch1 _State = stateClone
		if goxidematch1 == nil {
			panic("goxide: match on nil State at referencefixture/main.gox:650")
		}
		switch goxidevariant2 := goxidematch1.(type) {
		case *_Pair:
			_ = goxidevariant2
			value := goxidevariant2.Value
			value.Items[0] = 7
			value.Counts["one"] = 6
		case *_Empty:
			_ = goxidevariant2
		default:
			_ = goxidevariant2
			panic("goxide: invalid enum State")
		}
	}

	fmt.Println(EqualsState((*stateView), state), EqualsState(*stateView, state))
	fmt.Println(DebugState(stateView), DebugState(&stateClone))
}
func CloneData(goxideValue3 *Data) Data {
	return Data{Items: func(goxideSrc4 []int) []int {
		if goxideSrc4 == nil {
			return nil
		}
		goxideOut6 := make([]int, len(goxideSrc4))
		for goxideIndex5 := range goxideSrc4 {
			goxideOut6[goxideIndex5] = goxideSrc4[goxideIndex5]
		}
		return goxideOut6
	}((*goxideValue3).Items), Counts: func(goxideSrc7 map[string]int) map[string]int {
		if goxideSrc7 == nil {
			return nil
		}
		goxideOut10 := make(map[string]int, len(goxideSrc7))
		for goxideKey8, goxideItem9 := range goxideSrc7 {
			goxideOut10[goxideKey8] = goxideItem9
		}
		return goxideOut10
	}((*goxideValue3).Counts)}
}
func DebugData(goxideValue11 *Data) string {
	return "Data {" + " Items: " + func(goxideSrc12 []int) string {
		if goxideSrc12 == nil {
			return "nil"
		}
		goxideOut14 := make([]string, len(goxideSrc12))
		for goxideIndex13 := range goxideSrc12 {
			goxideOut14[goxideIndex13] = strconv.FormatInt(int64(goxideSrc12[goxideIndex13]), 10)
		}
		return "[" + strings.Join(goxideOut14, ", ") + "]"
	}((*goxideValue11).Items) + ", Counts: " + func(goxideSrc15 map[string]int) string {
		if goxideSrc15 == nil {
			return "nil"
		}
		goxideKeys19 := make([]string, 0, len(goxideSrc15))
		for goxideKey16 := range goxideSrc15 {
			goxideKeys19 = append(goxideKeys19, goxideKey16)
		}
		sort.Strings(goxideKeys19)
		goxideOut18 := make([]string, 0, len(goxideKeys19))
		for _, goxideKey16 := range goxideKeys19 {
			goxideItem17 := goxideSrc15[goxideKey16]
			goxideOut18 = append(goxideOut18, strconv.Quote(goxideKey16)+": "+strconv.FormatInt(int64(goxideItem17), 10))
		}
		return "{" + strings.Join(goxideOut18, ", ") + "}"
	}((*goxideValue11).Counts) + " }"
}
func EqualsData(goxideValue20, goxideOther21 Data) bool {
	return func(goxideLeft22, goxideRight23 []int) bool {
		if (goxideLeft22 == nil) != (goxideRight23 == nil) || len(goxideLeft22) != len(goxideRight23) {
			return 0 != 0
		}
		for goxideIndex24 := range goxideLeft22 {
			if !(goxideLeft22[goxideIndex24] == goxideRight23[goxideIndex24]) {
				return 0 != 0
			}
		}
		return 0 == 0
	}((goxideValue20).Items, (goxideOther21).Items) && func(goxideLeft25, goxideRight26 map[string]int) bool {
		if (goxideLeft25 == nil) != (goxideRight26 == nil) || len(goxideLeft25) != len(goxideRight26) {
			return 0 != 0
		}
		for goxideKey27, goxideItem28 := range goxideLeft25 {
			goxideOther29, goxideFound30 := goxideRight26[goxideKey27]
			if !goxideFound30 || !(goxideItem28 == goxideOther29) {
				return 0 != 0
			}
		}
		return 0 == 0
	}((goxideValue20).Counts, (goxideOther21).Counts)
}
func CloneState(goxideValue31 *_State) _State {
	return func(goxideEnum32 _State) _State {
		switch goxideVariant33 := goxideEnum32.(type) {
		case *_Pair:
			if goxideVariant33 == nil {
				panic("goxide: invalid enum State")
			}
			return &_Pair{Value: CloneData(&(goxideVariant33.Value))}
		case *_Empty:
			if goxideVariant33 == nil {
				panic("goxide: invalid enum State")
			}
			return &_Empty{}
		default:
			panic("goxide: invalid enum State")
		}
	}((*goxideValue31))
}
func DebugState(goxideValue34 *_State) string {
	return func(goxideEnum35 _State) string {
		switch goxideVariant36 := goxideEnum35.(type) {
		case *_Pair:
			if goxideVariant36 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Pair" + "(" + DebugData(&(goxideVariant36.Value)) + ")"
		case *_Empty:
			if goxideVariant36 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Empty"
		default:
			panic("goxide: invalid enum State")
		}
	}((*goxideValue34))
}
func EqualsState(goxideValue37, goxideOther38 _State) bool {
	return func(goxideLeft39, goxideRight40 _State) bool {
		switch goxideValidated44 := (goxideLeft39).(type) {
		case *_Pair:
			if goxideValidated44 == nil {
				panic("goxide: invalid enum State")
			}
		case *_Empty:
			if goxideValidated44 == nil {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
		switch goxideValidated47 := (goxideRight40).(type) {
		case *_Pair:
			if goxideValidated47 == nil {
				panic("goxide: invalid enum State")
			}
		case *_Empty:
			if goxideValidated47 == nil {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
		switch goxideVariant41 := goxideLeft39.(type) {
		case *_Pair:
			goxideOther42, goxideSame43 := goxideRight40.(*_Pair)
			if !goxideSame43 {
				return 0 != 0
			}
			_ = goxideVariant41
			_ = goxideOther42
			return EqualsData(goxideVariant41.Value, goxideOther42.Value)
		case *_Empty:
			goxideOther42, goxideSame43 := goxideRight40.(*_Empty)
			if !goxideSame43 {
				return 0 != 0
			}
			_ = goxideVariant41
			_ = goxideOther42
			return 0 == 0
		default:
			panic("goxide: invalid enum State")
		}
	}(goxideValue37, goxideOther38)
}
