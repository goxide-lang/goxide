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
			nums := goxidevariant2.V1
			nums[0] = 9
		default:
			_ = goxidevariant2
		}
	}
	fmt.Println(DebugState(&state), DebugState(&cloned))
	absent := Record{Outcome: sum.NewOk[Item, []int](Item{})}
	nilcopy := CloneRecord(&absent)
	{
		var goxideValue3 func(a ...any) (n int, err error) = fmt.Println
		var goxideValue4 bool = nilcopy.Values == nil
		var goxideValue5 bool = nilcopy.Table == nil
		var goxideMatch6 any
		var goxidematch7 sum.Option[[]Item] = nilcopy.Choice
		switch goxidetag9 := sum.TagOption(goxidematch7); goxidetag9 {
		case 0:
			goxideMatch6 = "none"
		case 1:
			goxideMatch6 = "some"
		default:
			panic("goxide: invalid enum Option")
		}
		goxideValue3(goxideValue4, goxideValue5, goxideMatch6)
	}
	present := Record{Values: []Item{}, Table: map[string][]Item{}, Choice: sum.NewSome[[]Item](nil), Outcome: sum.NewOk[Item, []int](Item{})}
	emptycopy := CloneRecord(&present)
	{
		var goxideValue10 func(a ...any) (n int, err error) = fmt.Println
		var goxideValue11 bool = emptycopy.Values == nil
		var goxideValue12 bool = emptycopy.Table == nil
		var goxideMatch13 any
		var goxidematch14 sum.Option[[]Item] = emptycopy.Choice
		switch goxidetag16 := sum.TagOption(goxidematch14); goxidetag16 {
		case 0:
			goxideMatch13 = false
		case 1:
			v := sum.ValueSome(goxidematch14)
			goxideMatch13 = v == nil
		default:
			panic("goxide: invalid enum Option")
		}
		goxideValue10(goxideValue11, goxideValue12, goxideMatch13)
	}
	private := Account{Id: 7, name: "hidden"}
	fmt.Println(Debugaccount(&private))
	grouped := Grouped{Value: 11}
	fmt.Println(DebugGrouped(&grouped))
	var empty State = &Empty{}
	var named State = &Named{Name: "secret", Count: 2}
	fmt.Println(DebugState(&empty), DebugState(&named))
}
func CloneItem(goxideValue17 *Item) Item {
	return Item{Text: (*goxideValue17).Text}
}
func DebugItem(goxideValue18 *Item) string {
	return "Item {" + " Text: " + strconv.Quote((*goxideValue18).Text) + " }"
}
func DebugGrouped(goxideValue19 *Grouped) string {
	return "Grouped {" + " Value: " + strconv.FormatInt(int64((*goxideValue19).Value), 10) + " }"
}
func CloneRecord(goxideValue20 *Record) Record {
	return Record{Values: func(goxideSrc21 []Item) []Item {
		if goxideSrc21 == nil {
			return nil
		}
		goxideOut23 := make([]Item, len(goxideSrc21))
		for goxideIndex22 := range goxideSrc21 {
			goxideOut23[goxideIndex22] = CloneItem(&(goxideSrc21[goxideIndex22]))
		}
		return goxideOut23
	}((*goxideValue20).Values), Table: func(goxideSrc24 map[string][]Item) map[string][]Item {
		if goxideSrc24 == nil {
			return nil
		}
		goxideOut27 := make(map[string][]Item, len(goxideSrc24))
		for goxideKey25, goxideItem26 := range goxideSrc24 {
			goxideOut27[goxideKey25] = func(goxideSrc28 []Item) []Item {
				if goxideSrc28 == nil {
					return nil
				}
				goxideOut30 := make([]Item, len(goxideSrc28))
				for goxideIndex29 := range goxideSrc28 {
					goxideOut30[goxideIndex29] = CloneItem(&(goxideSrc28[goxideIndex29]))
				}
				return goxideOut30
			}(goxideItem26)
		}
		return goxideOut27
	}((*goxideValue20).Table), Fixed: func(goxideSrc31 [1]Alias) [1]Alias {
		var goxideOut33 [1]Alias
		for goxideIndex32 := range goxideSrc31 {
			goxideOut33[goxideIndex32] = CloneItem(&(goxideSrc31[goxideIndex32]))
		}
		return goxideOut33
	}((*goxideValue20).Fixed), Choice: func(goxideEnum34 sum.Option[[]Item]) sum.Option[[]Item] {
		switch sum.TagOption(goxideEnum34) {
		case 0:
			return sum.None[[]Item]()
		case 1:
			goxidePayload36 := sum.ValueSome(goxideEnum34)
			return sum.NewSome[[]Item](func(goxideSrc37 []Item) []Item {
				if goxideSrc37 == nil {
					return nil
				}
				goxideOut39 := make([]Item, len(goxideSrc37))
				for goxideIndex38 := range goxideSrc37 {
					goxideOut39[goxideIndex38] = CloneItem(&(goxideSrc37[goxideIndex38]))
				}
				return goxideOut39
			}(goxidePayload36))
		default:
			panic("goxide: invalid enum Option")
		}
	}((*goxideValue20).Choice), Outcome: func(goxideEnum40 sum.Result[Item, []int]) sum.Result[Item, []int] {
		switch sum.TagResult(goxideEnum40) {
		case 1:
			goxidePayload42 := sum.ValueOk(goxideEnum40)
			return sum.NewOk[Item, []int](CloneItem(&(goxidePayload42)))
		case 2:
			goxidePayload43 := sum.ValueErr(goxideEnum40)
			return sum.NewErr[Item, []int](func(goxideSrc44 []int) []int {
				if goxideSrc44 == nil {
					return nil
				}
				goxideOut46 := make([]int, len(goxideSrc44))
				for goxideIndex45 := range goxideSrc44 {
					goxideOut46[goxideIndex45] = goxideSrc44[goxideIndex45]
				}
				return goxideOut46
			}(goxidePayload43))
		default:
			panic("goxide: invalid enum Result")
		}
	}((*goxideValue20).Outcome)}
}
func DebugRecord(goxideValue47 *Record) string {
	return "Record {" + " Values: " + func(goxideSrc48 []Item) string {
		if goxideSrc48 == nil {
			return "nil"
		}
		goxideOut50 := make([]string, len(goxideSrc48))
		for goxideIndex49 := range goxideSrc48 {
			goxideOut50[goxideIndex49] = DebugItem(&(goxideSrc48[goxideIndex49]))
		}
		return "[" + strings.Join(goxideOut50, ", ") + "]"
	}((*goxideValue47).Values) + ", Table: " + func(goxideSrc51 map[string][]Item) string {
		if goxideSrc51 == nil {
			return "nil"
		}
		goxideKeys58 := make([]string, 0, len(goxideSrc51))
		for goxideKey52 := range goxideSrc51 {
			goxideKeys58 = append(goxideKeys58, goxideKey52)
		}
		sort.Strings(goxideKeys58)
		goxideOut54 := make([]string, 0, len(goxideKeys58))
		for _, goxideKey52 := range goxideKeys58 {
			goxideItem53 := goxideSrc51[goxideKey52]
			goxideOut54 = append(goxideOut54, strconv.Quote(goxideKey52)+": "+func(goxideSrc55 []Item) string {
				if goxideSrc55 == nil {
					return "nil"
				}
				goxideOut57 := make([]string, len(goxideSrc55))
				for goxideIndex56 := range goxideSrc55 {
					goxideOut57[goxideIndex56] = DebugItem(&(goxideSrc55[goxideIndex56]))
				}
				return "[" + strings.Join(goxideOut57, ", ") + "]"
			}(goxideItem53))
		}
		return "{" + strings.Join(goxideOut54, ", ") + "}"
	}((*goxideValue47).Table) + ", Fixed: " + func(goxideSrc59 [1]Alias) string {
		goxideOut61 := make([]string, len(goxideSrc59))
		for goxideIndex60 := range goxideSrc59 {
			goxideOut61[goxideIndex60] = DebugItem(&(goxideSrc59[goxideIndex60]))
		}
		return "[" + strings.Join(goxideOut61, ", ") + "]"
	}((*goxideValue47).Fixed) + ", Choice: " + func(goxideEnum62 sum.Option[[]Item]) string {
		switch sum.TagOption(goxideEnum62) {
		case 0:
			return "Option::None"
		case 1:
			goxidePayload64 := sum.ValueSome(goxideEnum62)
			return "Option::Some" + "(" + func(goxideSrc65 []Item) string {
				if goxideSrc65 == nil {
					return "nil"
				}
				goxideOut67 := make([]string, len(goxideSrc65))
				for goxideIndex66 := range goxideSrc65 {
					goxideOut67[goxideIndex66] = DebugItem(&(goxideSrc65[goxideIndex66]))
				}
				return "[" + strings.Join(goxideOut67, ", ") + "]"
			}(goxidePayload64) + ")"
		default:
			panic("goxide: invalid enum Option")
		}
	}((*goxideValue47).Choice) + ", Outcome: " + func(goxideEnum68 sum.Result[Item, []int]) string {
		switch sum.TagResult(goxideEnum68) {
		case 1:
			goxidePayload70 := sum.ValueOk(goxideEnum68)
			return "Result::Ok" + "(" + DebugItem(&(goxidePayload70)) + ")"
		case 2:
			goxidePayload71 := sum.ValueErr(goxideEnum68)
			return "Result::Err" + "(" + func(goxideSrc72 []int) string {
				if goxideSrc72 == nil {
					return "nil"
				}
				goxideOut74 := make([]string, len(goxideSrc72))
				for goxideIndex73 := range goxideSrc72 {
					goxideOut74[goxideIndex73] = strconv.FormatInt(int64(goxideSrc72[goxideIndex73]), 10)
				}
				return "[" + strings.Join(goxideOut74, ", ") + "]"
			}(goxidePayload71) + ")"
		default:
			panic("goxide: invalid enum Result")
		}
	}((*goxideValue47).Outcome) + " }"
}
func Cloneaccount(goxideValue75 *Account) Account {
	return Account{Id: (*goxideValue75).Id, name: (*goxideValue75).name}
}
func Debugaccount(goxideValue76 *Account) string {
	return "account {" + " id: " + strconv.FormatInt(int64((*goxideValue76).Id), 10) + ", name: " + strconv.Quote((*goxideValue76).name) + " }"
}
func CloneState(goxideValue77 *State) State {
	return func(goxideEnum78 State) State {
		switch goxideVariant79 := goxideEnum78.(type) {
		case *Empty:
			if goxideVariant79 == nil {
				panic("goxide: invalid enum State")
			}
			return &Empty{}
		case *Pair:
			if goxideVariant79 == nil {
				panic("goxide: invalid enum State")
			}
			return &Pair{V0: CloneItem(&(goxideVariant79.V0)), V1: func(goxideSrc80 []int) []int {
				if goxideSrc80 == nil {
					return nil
				}
				goxideOut82 := make([]int, len(goxideSrc80))
				for goxideIndex81 := range goxideSrc80 {
					goxideOut82[goxideIndex81] = goxideSrc80[goxideIndex81]
				}
				return goxideOut82
			}(goxideVariant79.V1)}
		case *Named:
			if goxideVariant79 == nil {
				panic("goxide: invalid enum State")
			}
			return &Named{Name: goxideVariant79.Name, Count: goxideVariant79.Count}
		default:
			panic("goxide: invalid enum State")
		}
	}((*goxideValue77))
}
func DebugState(goxideValue83 *State) string {
	return func(goxideEnum84 State) string {
		switch goxideVariant85 := goxideEnum84.(type) {
		case *Empty:
			if goxideVariant85 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Empty"
		case *Pair:
			if goxideVariant85 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Pair" + "(" + DebugItem(&(goxideVariant85.V0)) + ", " + func(goxideSrc86 []int) string {
				if goxideSrc86 == nil {
					return "nil"
				}
				goxideOut88 := make([]string, len(goxideSrc86))
				for goxideIndex87 := range goxideSrc86 {
					goxideOut88[goxideIndex87] = strconv.FormatInt(int64(goxideSrc86[goxideIndex87]), 10)
				}
				return "[" + strings.Join(goxideOut88, ", ") + "]"
			}(goxideVariant85.V1) + ")"
		case *Named:
			if goxideVariant85 == nil {
				panic("goxide: invalid enum State")
			}
			return "State::Named" + " { " + "Name: " + strconv.Quote(goxideVariant85.Name) + ", " + "Count: " + strconv.FormatInt(int64(goxideVariant85.Count), 10) + " }"
		default:
			panic("goxide: invalid enum State")
		}
	}((*goxideValue83))
}
