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
		if stateClone == nil {
			panic("goxide: match on nil State at referencefixture/main.gox:22")
		}
		switch gxVar1 := stateClone.(type) {
		case *_Pair:
			if gxVar1 == nil {
				panic("goxide: match on nil State at referencefixture/main.gox:22")
			}
			value := gxVar1.Value
			value.Items[0] = 7
			value.Counts["one"] = 6
		case *_Empty:
			if gxVar1 == nil {
				panic("goxide: match on nil State at referencefixture/main.gox:22")
			}
		default:
			panic("goxide: invalid enum State")
		}
	}

	fmt.Println(EqualsState((*stateView), state), EqualsState(*stateView, state))
	fmt.Println(DebugState(stateView), DebugState(&stateClone))
}
func CloneData(gxValue2 *Data) Data {
	return Data{Items: func(gxSrc3 []int) []int {
		if gxSrc3 == nil {
			return nil
		}
		gxOut5 := make([]int, len(gxSrc3))
		for gxIndex4 := range gxSrc3 {
			gxOut5[gxIndex4] = gxSrc3[gxIndex4]
		}
		return gxOut5
	}((*gxValue2).Items), Counts: func(gxSrc6 map[string]int) map[string]int {
		if gxSrc6 == nil {
			return nil
		}
		gxOut9 := make(map[string]int, len(gxSrc6))
		for gxKey7, gxItem8 := range gxSrc6 {
			gxOut9[gxKey7] = gxItem8
		}
		return gxOut9
	}((*gxValue2).Counts)}
}
func DebugData(gxValue10 *Data) string {
	return "Data {" + " Items: " + func(gxSrc11 []int) string {
		if gxSrc11 == nil {
			return "nil"
		}
		gxOut13 := make([]string, len(gxSrc11))
		for gxIndex12 := range gxSrc11 {
			gxOut13[gxIndex12] = strconv.FormatInt(int64(gxSrc11[gxIndex12]), 10)
		}
		return "[" + strings.Join(gxOut13, ", ") + "]"
	}((*gxValue10).Items) + ", Counts: " + func(gxSrc14 map[string]int) string {
		if gxSrc14 == nil {
			return "nil"
		}
		gxKeys18 := make([]string, 0, len(gxSrc14))
		for gxKey15 := range gxSrc14 {
			gxKeys18 = append(gxKeys18, gxKey15)
		}
		sort.Strings(gxKeys18)
		gxOut17 := make([]string, 0, len(gxKeys18))
		for _, gxKey15 := range gxKeys18 {
			gxItem16 := gxSrc14[gxKey15]
			gxOut17 = append(gxOut17, strconv.Quote(gxKey15)+": "+strconv.FormatInt(int64(gxItem16), 10))
		}
		return "{" + strings.Join(gxOut17, ", ") + "}"
	}((*gxValue10).Counts) + " }"
}
func EqualsData(gxValue19, gxOther20 Data) bool {
	return func(gxLeft21, gxRight22 []int) bool {
		if (gxLeft21 == nil) != (gxRight22 == nil) || len(gxLeft21) != len(gxRight22) {
			return 0 != 0
		}
		for gxIndex23 := range gxLeft21 {
			if !(gxLeft21[gxIndex23] == gxRight22[gxIndex23]) {
				return 0 != 0
			}
		}
		return 0 == 0
	}((gxValue19).Items, (gxOther20).Items) && func(gxLeft24, gxRight25 map[string]int) bool {
		if (gxLeft24 == nil) != (gxRight25 == nil) || len(gxLeft24) != len(gxRight25) {
			return 0 != 0
		}
		for gxKey26, gxItem27 := range gxLeft24 {
			gxOther28, gxFound29 := gxRight25[gxKey26]
			if !gxFound29 || !(gxItem27 == gxOther28) {
				return 0 != 0
			}
		}
		return 0 == 0
	}((gxValue19).Counts, (gxOther20).Counts)
}
func CloneState(gxValue30 *_State) _State {
	return func(gxEnum31 _State) _State {
		switch gxVar32 := gxEnum31.(type) {
		case *_Pair:
			if gxVar32 == nil {
				panic("goxide: invalid enum State")
			}
			return &_Pair{Value: CloneData(&(gxVar32.Value))}
		case *_Empty:
			if gxVar32 == nil {
				panic("goxide: invalid enum State")
			}
			return &_Empty{}
		default:
			panic("goxide: invalid enum State")
		}
	}((*gxValue30))
}
func DebugState(gxValue33 *_State) string {
	return func(gxEnum34 _State) string {
		switch gxVar35 := gxEnum34.(type) {
		case *_Pair:
			if gxVar35 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Pair" + "(" + DebugData(&(gxVar35.Value)) + ")"
		case *_Empty:
			if gxVar35 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Empty"
		default:
			panic("goxide: invalid enum State")
		}
	}((*gxValue33))
}
func EqualsState(gxValue36, gxOther37 _State) bool {
	return func(gxLeft38, gxRight39 _State) bool {
		switch gxValidated43 := (gxLeft38).(type) {
		case *_Pair:
			if gxValidated43 == nil {
				panic("goxide: invalid enum State")
			}
		case *_Empty:
			if gxValidated43 == nil {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
		switch gxValidated46 := (gxRight39).(type) {
		case *_Pair:
			if gxValidated46 == nil {
				panic("goxide: invalid enum State")
			}
		case *_Empty:
			if gxValidated46 == nil {
				panic("goxide: invalid enum State")
			}
		default:
			panic("goxide: invalid enum State")
		}
		switch gxVar40 := gxLeft38.(type) {
		case *_Pair:
			gxOther41, gxSame42 := gxRight39.(*_Pair)
			if !gxSame42 {
				return 0 != 0
			}
			_ = gxVar40
			_ = gxOther41
			return EqualsData(gxVar40.Value, gxOther41.Value)
		case *_Empty:
			gxOther41, gxSame42 := gxRight39.(*_Empty)
			if !gxSame42 {
				return 0 != 0
			}
			_ = gxVar40
			_ = gxOther41
			return 0 == 0
		default:
			panic("goxide: invalid enum State")
		}
	}(gxValue36, gxOther37)
}
