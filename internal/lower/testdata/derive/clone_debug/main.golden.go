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
		if cloned == nil {
			panic("goxide: match on nil State at derivefixture/main.gox:1152")
		}
		switch gxVar1 := cloned.(type) {
		case *Pair:
			if gxVar1 == nil {
				panic("goxide: match on nil State at derivefixture/main.gox:1152")
			}
			nums := gxVar1.V1
			nums[0] = 9
		case *Empty:
			if gxVar1 == nil {
				panic("goxide: match on nil State at derivefixture/main.gox:1152")
			}
		case *Named:
			if gxVar1 == nil {
				panic("goxide: match on nil State at derivefixture/main.gox:1152")
			}
		default:
		}
	}
	fmt.Println(DebugState(&state), DebugState(&cloned))
	absent := Record{Outcome: sum.NewOk[Item, []int](Item{})}
	nilcopy := CloneRecord(&absent)
	{
		var gxValue2 bool = nilcopy.Values == nil
		var gxValue3 bool = nilcopy.Table == nil
		var gxMatch4 any
		var gxMatch5 sum.Option[[]Item] = nilcopy.Choice
		switch gxTag7 := sum.TagOption(gxMatch5); gxTag7 {
		case 0:
			gxMatch4 = "none"
		case 1:
			gxMatch4 = "some"
		default:
			panic("goxide: invalid enum Option")
		}
		fmt.Println(gxValue2, gxValue3, gxMatch4)
	}
	present := Record{Values: []Item{}, Table: map[string][]Item{}, Choice: sum.NewSome[[]Item](nil), Outcome: sum.NewOk[Item, []int](Item{})}
	emptycopy := CloneRecord(&present)
	{
		var gxValue8 bool = emptycopy.Values == nil
		var gxValue9 bool = emptycopy.Table == nil
		var gxMatch10 any
		var gxMatch11 sum.Option[[]Item] = emptycopy.Choice
		switch gxTag13 := sum.TagOption(gxMatch11); gxTag13 {
		case 0:
			gxMatch10 = false
		case 1:
			v := sum.ValueSome(gxMatch11)
			gxMatch10 = v == nil
		default:
			panic("goxide: invalid enum Option")
		}
		fmt.Println(gxValue8, gxValue9, gxMatch10)
	}
	private := Account{Id: 7, name: "hidden"}
	fmt.Println(Debugaccount(&private))
	grouped := Grouped{Value: 11}
	fmt.Println(DebugGrouped(&grouped))
	var empty State = &Empty{}
	var named State = &Named{Name: "secret", Count: 2}
	fmt.Println(DebugState(&empty), DebugState(&named))
}
func CloneItem(gxValue14 *Item) Item {
	return Item{Text: (*gxValue14).Text}
}
func DebugItem(gxValue15 *Item) string {
	return "Item {" + " Text: " + strconv.Quote((*gxValue15).Text) + " }"
}
func DebugGrouped(gxValue16 *Grouped) string {
	return "Grouped {" + " Value: " + strconv.FormatInt(int64((*gxValue16).Value), 10) + " }"
}
func CloneRecord(gxValue17 *Record) Record {
	return Record{Values: func(gxSrc18 []Item) []Item {
		if gxSrc18 == nil {
			return nil
		}
		gxOut20 := make([]Item, len(gxSrc18))
		for gxIndex19 := range gxSrc18 {
			gxOut20[gxIndex19] = CloneItem(&(gxSrc18[gxIndex19]))
		}
		return gxOut20
	}((*gxValue17).Values), Table: func(gxSrc21 map[string][]Item) map[string][]Item {
		if gxSrc21 == nil {
			return nil
		}
		gxOut24 := make(map[string][]Item, len(gxSrc21))
		for gxKey22, gxItem23 := range gxSrc21 {
			gxOut24[gxKey22] = func(gxSrc25 []Item) []Item {
				if gxSrc25 == nil {
					return nil
				}
				gxOut27 := make([]Item, len(gxSrc25))
				for gxIndex26 := range gxSrc25 {
					gxOut27[gxIndex26] = CloneItem(&(gxSrc25[gxIndex26]))
				}
				return gxOut27
			}(gxItem23)
		}
		return gxOut24
	}((*gxValue17).Table), Fixed: func(gxSrc28 [1]Alias) [1]Alias {
		var gxOut30 [1]Alias
		for gxIndex29 := range gxSrc28 {
			gxOut30[gxIndex29] = CloneItem(&(gxSrc28[gxIndex29]))
		}
		return gxOut30
	}((*gxValue17).Fixed), Choice: func(gxEnum31 sum.Option[[]Item]) sum.Option[[]Item] {
		switch sum.TagOption(gxEnum31) {
		case 0:
			return sum.None[[]Item]()
		case 1:
			gxPayload33 := sum.ValueSome(gxEnum31)
			return sum.NewSome[[]Item](func(gxSrc34 []Item) []Item {
				if gxSrc34 == nil {
					return nil
				}
				gxOut36 := make([]Item, len(gxSrc34))
				for gxIndex35 := range gxSrc34 {
					gxOut36[gxIndex35] = CloneItem(&(gxSrc34[gxIndex35]))
				}
				return gxOut36
			}(gxPayload33))
		default:
			panic("goxide: invalid enum Option")
		}
	}((*gxValue17).Choice), Outcome: func(gxEnum37 sum.Result[Item, []int]) sum.Result[Item, []int] {
		switch sum.TagResult(gxEnum37) {
		case 1:
			gxPayload39 := sum.ValueOk(gxEnum37)
			return sum.NewOk[Item, []int](CloneItem(&(gxPayload39)))
		case 2:
			gxPayload40 := sum.ValueErr(gxEnum37)
			return sum.NewErr[Item, []int](func(gxSrc41 []int) []int {
				if gxSrc41 == nil {
					return nil
				}
				gxOut43 := make([]int, len(gxSrc41))
				for gxIndex42 := range gxSrc41 {
					gxOut43[gxIndex42] = gxSrc41[gxIndex42]
				}
				return gxOut43
			}(gxPayload40))
		default:
			panic("goxide: invalid enum Result")
		}
	}((*gxValue17).Outcome)}
}
func DebugRecord(gxValue44 *Record) string {
	return "Record {" + " Values: " + func(gxSrc45 []Item) string {
		if gxSrc45 == nil {
			return "nil"
		}
		gxOut47 := make([]string, len(gxSrc45))
		for gxIndex46 := range gxSrc45 {
			gxOut47[gxIndex46] = DebugItem(&(gxSrc45[gxIndex46]))
		}
		return "[" + strings.Join(gxOut47, ", ") + "]"
	}((*gxValue44).Values) + ", Table: " + func(gxSrc48 map[string][]Item) string {
		if gxSrc48 == nil {
			return "nil"
		}
		gxKeys55 := make([]string, 0, len(gxSrc48))
		for gxKey49 := range gxSrc48 {
			gxKeys55 = append(gxKeys55, gxKey49)
		}
		sort.Strings(gxKeys55)
		gxOut51 := make([]string, 0, len(gxKeys55))
		for _, gxKey49 := range gxKeys55 {
			gxItem50 := gxSrc48[gxKey49]
			gxOut51 = append(gxOut51, strconv.Quote(gxKey49)+": "+func(gxSrc52 []Item) string {
				if gxSrc52 == nil {
					return "nil"
				}
				gxOut54 := make([]string, len(gxSrc52))
				for gxIndex53 := range gxSrc52 {
					gxOut54[gxIndex53] = DebugItem(&(gxSrc52[gxIndex53]))
				}
				return "[" + strings.Join(gxOut54, ", ") + "]"
			}(gxItem50))
		}
		return "{" + strings.Join(gxOut51, ", ") + "}"
	}((*gxValue44).Table) + ", Fixed: " + func(gxSrc56 [1]Alias) string {
		gxOut58 := make([]string, len(gxSrc56))
		for gxIndex57 := range gxSrc56 {
			gxOut58[gxIndex57] = DebugItem(&(gxSrc56[gxIndex57]))
		}
		return "[" + strings.Join(gxOut58, ", ") + "]"
	}((*gxValue44).Fixed) + ", Choice: " + func(gxEnum59 sum.Option[[]Item]) string {
		switch sum.TagOption(gxEnum59) {
		case 0:
			return "Option::None"
		case 1:
			gxPayload61 := sum.ValueSome(gxEnum59)
			return "Option::Some" + "(" + func(gxSrc62 []Item) string {
				if gxSrc62 == nil {
					return "nil"
				}
				gxOut64 := make([]string, len(gxSrc62))
				for gxIndex63 := range gxSrc62 {
					gxOut64[gxIndex63] = DebugItem(&(gxSrc62[gxIndex63]))
				}
				return "[" + strings.Join(gxOut64, ", ") + "]"
			}(gxPayload61) + ")"
		default:
			panic("goxide: invalid enum Option")
		}
	}((*gxValue44).Choice) + ", Outcome: " + func(gxEnum65 sum.Result[Item, []int]) string {
		switch sum.TagResult(gxEnum65) {
		case 1:
			gxPayload67 := sum.ValueOk(gxEnum65)
			return "Result::Ok" + "(" + DebugItem(&(gxPayload67)) + ")"
		case 2:
			gxPayload68 := sum.ValueErr(gxEnum65)
			return "Result::Err" + "(" + func(gxSrc69 []int) string {
				if gxSrc69 == nil {
					return "nil"
				}
				gxOut71 := make([]string, len(gxSrc69))
				for gxIndex70 := range gxSrc69 {
					gxOut71[gxIndex70] = strconv.FormatInt(int64(gxSrc69[gxIndex70]), 10)
				}
				return "[" + strings.Join(gxOut71, ", ") + "]"
			}(gxPayload68) + ")"
		default:
			panic("goxide: invalid enum Result")
		}
	}((*gxValue44).Outcome) + " }"
}
func Cloneaccount(gxValue72 *Account) Account {
	return Account{Id: (*gxValue72).Id, name: (*gxValue72).name}
}
func Debugaccount(gxValue73 *Account) string {
	return "account {" + " id: " + strconv.FormatInt(int64((*gxValue73).Id), 10) + ", name: " + strconv.Quote((*gxValue73).name) + " }"
}
func CloneState(gxValue74 *State) State {
	return func(gxEnum75 State) State {
		switch gxVar76 := gxEnum75.(type) {
		case *Empty:
			if gxVar76 == nil {
				panic("goxide: invalid enum State")
			}
			return &Empty{}
		case *Pair:
			if gxVar76 == nil {
				panic("goxide: invalid enum State")
			}
			return &Pair{V0: CloneItem(&(gxVar76.V0)), V1: func(gxSrc77 []int) []int {
				if gxSrc77 == nil {
					return nil
				}
				gxOut79 := make([]int, len(gxSrc77))
				for gxIndex78 := range gxSrc77 {
					gxOut79[gxIndex78] = gxSrc77[gxIndex78]
				}
				return gxOut79
			}(gxVar76.V1)}
		case *Named:
			if gxVar76 == nil {
				panic("goxide: invalid enum State")
			}
			return &Named{Name: gxVar76.Name, Count: gxVar76.Count}
		default:
			panic("goxide: invalid enum State")
		}
	}((*gxValue74))
}
func DebugState(gxValue80 *State) string {
	return func(gxEnum81 State) string {
		switch gxVar82 := gxEnum81.(type) {
		case *Empty:
			if gxVar82 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Empty"
		case *Pair:
			if gxVar82 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Pair" + "(" + DebugItem(&(gxVar82.V0)) + ", " + func(gxSrc83 []int) string {
				if gxSrc83 == nil {
					return "nil"
				}
				gxOut85 := make([]string, len(gxSrc83))
				for gxIndex84 := range gxSrc83 {
					gxOut85[gxIndex84] = strconv.FormatInt(int64(gxSrc83[gxIndex84]), 10)
				}
				return "[" + strings.Join(gxOut85, ", ") + "]"
			}(gxVar82.V1) + ")"
		case *Named:
			if gxVar82 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Named" + " { " + "Name: " + strconv.Quote(gxVar82.Name) + ", " + "Count: " + strconv.FormatInt(int64(gxVar82.Count), 10) + " }"
		default:
			panic("goxide: invalid enum State")
		}
	}((*gxValue80))
}
