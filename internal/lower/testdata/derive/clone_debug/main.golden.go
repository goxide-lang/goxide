package main

import sum "github.com/goxide-lang/std/sum"
import sort "sort"
import strconv "strconv"
import strings "strings"
import fmt "fmt"

type Item struct{ Text string }
type (
	Alias = Item

	Grouped struct{ Value int }
)
type Record struct {
	Values  []Item
	Table   map[string][]Item
	Fixed   [1]Alias
	Choice  sum.Option[[]Item]
	Outcome sum.Result[Item, []int]
}
type Account struct {
	Id   int
	name string
}
type State interface {
	isState()
}
type Empty struct {
}

func (*Empty) isState() {
}

type Pair struct {
	V0 Item
	V1 []int
}

func (*Pair) isState() {
}

type Named struct {
	Name  string
	Count int
}

func (*Named) isState() {
}

func main() {
	value := Record{Values: []Item{Item{Text: "a"}}, Table: map[string][]Item{"key": []Item{Item{Text: "b"}}}, Fixed: [1]Alias{Item{Text: "c"}}, Choice: sum.NewSome[[]Item]([]Item{}), Outcome: sum.NewErr[Item, []int](nil)}
	assigned := value
	assigned.Values[0].Text = "shared"
	copy := CloneRecord(&value)
	copy.Values[0].Text = "copy"
	copy.Table["key"][0].Text = "changed"
	copy.Fixed[0].Text = "fixed"
	fmt.Println(value.Values[0].Text, value.Table["key"][0].Text, value.Fixed[0].Text)
	fmt.Println(DebugRecord(&value))
	var state State = &Pair{V0: Item{Text: "quoted"}, V1: []int{1, 2}}
	var cloned State = CloneState(&state)
	{
		var goxidematch1 State = cloned
		if goxidematch1 == nil {
			panic("goxide: match on nil State at derivefixture/main.gox:1152")
		}
		switch goxidevariant2 := goxidematch1.(type) {
		case *Pair:
			_ = goxidevariant2
			if goxidevariant2 == nil {
				panic("goxide: match on nil State at derivefixture/main.gox:1152")
			}
			nums := goxidevariant2.V1
			nums[0] = 9
		case *Empty:
			_ = goxidevariant2
			if goxidevariant2 == nil {
				panic("goxide: match on nil State at derivefixture/main.gox:1152")
			}
		case *Named:
			_ = goxidevariant2
			if goxidevariant2 == nil {
				panic("goxide: match on nil State at derivefixture/main.gox:1152")
			}
		default:
			_ = goxidevariant2
		}
	}
	fmt.Println(DebugState(&state), DebugState(&cloned))
	absent := Record{Outcome: sum.NewOk[Item, []int](Item{})}
	nilcopy := CloneRecord(&absent)
	{
		var goxideValue3 bool = nilcopy.Values == nil
		var goxideValue4 bool = nilcopy.Table == nil
		var goxideMatch5 any
		var goxidematch6 sum.Option[[]Item] = nilcopy.Choice
		switch goxidetag8 := sum.TagOption(goxidematch6); goxidetag8 {
		case 0:
			goxideMatch5 = "none"
		case 1:
			goxideMatch5 = "some"
		default:
			panic("goxide: invalid enum Option")
		}
		fmt.Println(goxideValue3, goxideValue4, goxideMatch5)
	}
	present := Record{Values: []Item{}, Table: map[string][]Item{}, Choice: sum.NewSome[[]Item](nil), Outcome: sum.NewOk[Item, []int](Item{})}
	emptycopy := CloneRecord(&present)
	{
		var goxideValue9 bool = emptycopy.Values == nil
		var goxideValue10 bool = emptycopy.Table == nil
		var goxideMatch11 any
		var goxidematch12 sum.Option[[]Item] = emptycopy.Choice
		switch goxidetag14 := sum.TagOption(goxidematch12); goxidetag14 {
		case 0:
			goxideMatch11 = false
		case 1:
			v := sum.ValueSome(goxidematch12)
			goxideMatch11 = v == nil
		default:
			panic("goxide: invalid enum Option")
		}
		fmt.Println(goxideValue9, goxideValue10, goxideMatch11)
	}
	private := Account{Id: 7, name: "hidden"}
	fmt.Println(Debugaccount(&private))
	grouped := Grouped{Value: 11}
	fmt.Println(DebugGrouped(&grouped))
	var empty State = &Empty{}
	var named State = &Named{Name: "secret", Count: 2}
	fmt.Println(DebugState(&empty), DebugState(&named))
}
func CloneItem(goxideValue15 *Item) Item {
	return Item{Text: (*goxideValue15).Text}
}
func DebugItem(goxideValue16 *Item) string {
	return "Item {" + " Text: " + strconv.Quote((*goxideValue16).Text) + " }"
}
func DebugGrouped(goxideValue17 *Grouped) string {
	return "Grouped {" + " Value: " + strconv.FormatInt(int64((*goxideValue17).Value), 10) + " }"
}
func CloneRecord(goxideValue18 *Record) Record {
	return Record{Values: func(goxideSrc19 []Item) []Item {
		if goxideSrc19 == nil {
			return nil
		}
		goxideOut21 := make([]Item, len(goxideSrc19))
		for goxideIndex20 := range goxideSrc19 {
			goxideOut21[goxideIndex20] = CloneItem(&(goxideSrc19[goxideIndex20]))
		}
		return goxideOut21
	}((*goxideValue18).Values), Table: func(goxideSrc22 map[string][]Item) map[string][]Item {
		if goxideSrc22 == nil {
			return nil
		}
		goxideOut25 := make(map[string][]Item, len(goxideSrc22))
		for goxideKey23, goxideItem24 := range goxideSrc22 {
			goxideOut25[goxideKey23] = func(goxideSrc26 []Item) []Item {
				if goxideSrc26 == nil {
					return nil
				}
				goxideOut28 := make([]Item, len(goxideSrc26))
				for goxideIndex27 := range goxideSrc26 {
					goxideOut28[goxideIndex27] = CloneItem(&(goxideSrc26[goxideIndex27]))
				}
				return goxideOut28
			}(goxideItem24)
		}
		return goxideOut25
	}((*goxideValue18).Table), Fixed: func(goxideSrc29 [1]Alias) [1]Alias {
		var goxideOut31 [1]Alias
		for goxideIndex30 := range goxideSrc29 {
			goxideOut31[goxideIndex30] = CloneItem(&(goxideSrc29[goxideIndex30]))
		}
		return goxideOut31
	}((*goxideValue18).Fixed), Choice: func(goxideEnum32 sum.Option[[]Item]) sum.Option[[]Item] {
		switch sum.TagOption(goxideEnum32) {
		case 0:
			return sum.None[[]Item]()
		case 1:
			goxidePayload34 := sum.ValueSome(goxideEnum32)
			return sum.NewSome[[]Item](func(goxideSrc35 []Item) []Item {
				if goxideSrc35 == nil {
					return nil
				}
				goxideOut37 := make([]Item, len(goxideSrc35))
				for goxideIndex36 := range goxideSrc35 {
					goxideOut37[goxideIndex36] = CloneItem(&(goxideSrc35[goxideIndex36]))
				}
				return goxideOut37
			}(goxidePayload34))
		default:
			panic("goxide: invalid enum Option")
		}
	}((*goxideValue18).Choice), Outcome: func(goxideEnum38 sum.Result[Item, []int]) sum.Result[Item, []int] {
		switch sum.TagResult(goxideEnum38) {
		case 1:
			goxidePayload40 := sum.ValueOk(goxideEnum38)
			return sum.NewOk[Item, []int](CloneItem(&(goxidePayload40)))
		case 2:
			goxidePayload41 := sum.ValueErr(goxideEnum38)
			return sum.NewErr[Item, []int](func(goxideSrc42 []int) []int {
				if goxideSrc42 == nil {
					return nil
				}
				goxideOut44 := make([]int, len(goxideSrc42))
				for goxideIndex43 := range goxideSrc42 {
					goxideOut44[goxideIndex43] = goxideSrc42[goxideIndex43]
				}
				return goxideOut44
			}(goxidePayload41))
		default:
			panic("goxide: invalid enum Result")
		}
	}((*goxideValue18).Outcome)}
}
func DebugRecord(goxideValue45 *Record) string {
	return "Record {" + " Values: " + func(goxideSrc46 []Item) string {
		if goxideSrc46 == nil {
			return "nil"
		}
		goxideOut48 := make([]string, len(goxideSrc46))
		for goxideIndex47 := range goxideSrc46 {
			goxideOut48[goxideIndex47] = DebugItem(&(goxideSrc46[goxideIndex47]))
		}
		return "[" + strings.Join(goxideOut48, ", ") + "]"
	}((*goxideValue45).Values) + ", Table: " + func(goxideSrc49 map[string][]Item) string {
		if goxideSrc49 == nil {
			return "nil"
		}
		goxideKeys56 := make([]string, 0, len(goxideSrc49))
		for goxideKey50 := range goxideSrc49 {
			goxideKeys56 = append(goxideKeys56, goxideKey50)
		}
		sort.Strings(goxideKeys56)
		goxideOut52 := make([]string, 0, len(goxideKeys56))
		for _, goxideKey50 := range goxideKeys56 {
			goxideItem51 := goxideSrc49[goxideKey50]
			goxideOut52 = append(goxideOut52, strconv.Quote(goxideKey50)+": "+func(goxideSrc53 []Item) string {
				if goxideSrc53 == nil {
					return "nil"
				}
				goxideOut55 := make([]string, len(goxideSrc53))
				for goxideIndex54 := range goxideSrc53 {
					goxideOut55[goxideIndex54] = DebugItem(&(goxideSrc53[goxideIndex54]))
				}
				return "[" + strings.Join(goxideOut55, ", ") + "]"
			}(goxideItem51))
		}
		return "{" + strings.Join(goxideOut52, ", ") + "}"
	}((*goxideValue45).Table) + ", Fixed: " + func(goxideSrc57 [1]Alias) string {
		goxideOut59 := make([]string, len(goxideSrc57))
		for goxideIndex58 := range goxideSrc57 {
			goxideOut59[goxideIndex58] = DebugItem(&(goxideSrc57[goxideIndex58]))
		}
		return "[" + strings.Join(goxideOut59, ", ") + "]"
	}((*goxideValue45).Fixed) + ", Choice: " + func(goxideEnum60 sum.Option[[]Item]) string {
		switch sum.TagOption(goxideEnum60) {
		case 0:
			return "Option::None"
		case 1:
			goxidePayload62 := sum.ValueSome(goxideEnum60)
			return "Option::Some" + "(" + func(goxideSrc63 []Item) string {
				if goxideSrc63 == nil {
					return "nil"
				}
				goxideOut65 := make([]string, len(goxideSrc63))
				for goxideIndex64 := range goxideSrc63 {
					goxideOut65[goxideIndex64] = DebugItem(&(goxideSrc63[goxideIndex64]))
				}
				return "[" + strings.Join(goxideOut65, ", ") + "]"
			}(goxidePayload62) + ")"
		default:
			panic("goxide: invalid enum Option")
		}
	}((*goxideValue45).Choice) + ", Outcome: " + func(goxideEnum66 sum.Result[Item, []int]) string {
		switch sum.TagResult(goxideEnum66) {
		case 1:
			goxidePayload68 := sum.ValueOk(goxideEnum66)
			return "Result::Ok" + "(" + DebugItem(&(goxidePayload68)) + ")"
		case 2:
			goxidePayload69 := sum.ValueErr(goxideEnum66)
			return "Result::Err" + "(" + func(goxideSrc70 []int) string {
				if goxideSrc70 == nil {
					return "nil"
				}
				goxideOut72 := make([]string, len(goxideSrc70))
				for goxideIndex71 := range goxideSrc70 {
					goxideOut72[goxideIndex71] = strconv.FormatInt(int64(goxideSrc70[goxideIndex71]), 10)
				}
				return "[" + strings.Join(goxideOut72, ", ") + "]"
			}(goxidePayload69) + ")"
		default:
			panic("goxide: invalid enum Result")
		}
	}((*goxideValue45).Outcome) + " }"
}
func Cloneaccount(goxideValue73 *Account) Account {
	return Account{Id: (*goxideValue73).Id, name: (*goxideValue73).name}
}
func Debugaccount(goxideValue74 *Account) string {
	return "account {" + " id: " + strconv.FormatInt(int64((*goxideValue74).Id), 10) + ", name: " + strconv.Quote((*goxideValue74).name) + " }"
}
func CloneState(goxideValue75 *State) State {
	return func(goxideEnum76 State) State {
		switch goxideVariant77 := goxideEnum76.(type) {
		case *Empty:
			if goxideVariant77 == nil {
				panic("goxide: invalid enum State")
			}
			return &Empty{}
		case *Pair:
			if goxideVariant77 == nil {
				panic("goxide: invalid enum State")
			}
			return &Pair{V0: CloneItem(&(goxideVariant77.V0)), V1: func(goxideSrc78 []int) []int {
				if goxideSrc78 == nil {
					return nil
				}
				goxideOut80 := make([]int, len(goxideSrc78))
				for goxideIndex79 := range goxideSrc78 {
					goxideOut80[goxideIndex79] = goxideSrc78[goxideIndex79]
				}
				return goxideOut80
			}(goxideVariant77.V1)}
		case *Named:
			if goxideVariant77 == nil {
				panic("goxide: invalid enum State")
			}
			return &Named{Name: goxideVariant77.Name, Count: goxideVariant77.Count}
		default:
			panic("goxide: invalid enum State")
		}
	}((*goxideValue75))
}
func DebugState(goxideValue81 *State) string {
	return func(goxideEnum82 State) string {
		switch goxideVariant83 := goxideEnum82.(type) {
		case *Empty:
			if goxideVariant83 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Empty"
		case *Pair:
			if goxideVariant83 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Pair" + "(" + DebugItem(&(goxideVariant83.V0)) + ", " + func(goxideSrc84 []int) string {
				if goxideSrc84 == nil {
					return "nil"
				}
				goxideOut86 := make([]string, len(goxideSrc84))
				for goxideIndex85 := range goxideSrc84 {
					goxideOut86[goxideIndex85] = strconv.FormatInt(int64(goxideSrc84[goxideIndex85]), 10)
				}
				return "[" + strings.Join(goxideOut86, ", ") + "]"
			}(goxideVariant83.V1) + ")"
		case *Named:
			if goxideVariant83 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Named" + " { " + "Name: " + strconv.Quote(goxideVariant83.Name) + ", " + "Count: " + strconv.FormatInt(int64(goxideVariant83.Count), 10) + " }"
		default:
			panic("goxide: invalid enum State")
		}
	}((*goxideValue81))
}
