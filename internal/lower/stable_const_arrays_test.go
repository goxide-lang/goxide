package lower_test

import "testing"

const stableArrayConsumer = `package main
use fmt;
use library;
var trace string
func mark(name string, value int) int { trace += name; return value }
func caught(run func()) (yes bool) { defer func(){yes=recover()!=nil}(); run(); return }
`

func TestStableGoConstArrayIndexSliceAndAssignment(t *testing.T) {
	const library = `package library
pub func Index[const N int](base func()*[N]int, index func()int) int { return base()[index()] }
pub func Slice[const N int](base func()*[N]int, low,high,max func()int) []int { return base()[low():high():max()] }
pub func Set[const N int](base func()*[N]int, index func()int, right func()int) { base()[index()]=right() }
pub func Add[const N int](base func()*[N]int, index func()int, right func()int) { base()[index()]+=right() }
pub func Tuple[const N int](base *[N]int, first,second func()int, right func()(int,int)) { base[first()],base[second()]=right() }
pub func BlankTuple[const N int](base *[N]int, right func()(int,int)) { _,base[0]=right() }
pub func Shift[const N int](base *[N]int, amount func()uint) { base[0]<<=amount() }
pub func MutableIndex[const N int](base *[N]int) int { return (*base)[func()int{base[0]=99;return 0}()] }
`
	runStableGoLibraryFixture(t, library, stableArrayConsumer, []stableGoCase{
		{"nil index evaluates index first", `func main(){
 var missing *[2]int
 bad:=caught(func(){library::Index[2](func()*[2]int{trace+="b";return missing},func()int{return mark("i",0)})})
 fmt::Println(bad,trace)
 trace=""
 bad=caught(func(){library::Index[2](func()*[2]int{trace+="b";return missing},func()int{trace+="i";panic("index")})})
 fmt::Println(bad,trace)
}`, "true bi\ntrue bi\n"},
		{"slice bounds precede nil and retain storage", `func main(){
 var missing *[2]int
 bad:=caught(func(){library::Slice[2](func()*[2]int{trace+="b";return missing},func()int{return mark("l",0)},func()int{return mark("h",1)},func()int{return mark("m",2)})})
 fmt::Println(bad,trace)
 trace="";values:=[2]int{10,20}
 slice:=library::Slice[2](func()*[2]int{trace+="b";return &values},func()int{return mark("l",0)},func()int{return mark("h",1)},func()int{return mark("m",2)})
 slice[0]=42
 fmt::Println(trace,len(slice),cap(slice),values[0])
}`, "true blhm\nblhm 1 2 42\n"},
		{"assignment right precedes nil and bounds", `func main(){
 var missing *[2]int
 bad:=caught(func(){library::Set[2](func()*[2]int{trace+="b";return missing},func()int{return mark("i",0)},func()int{return mark("r",42)})})
 fmt::Println(bad,trace)
 trace="";values:=[2]int{10,20}
 bad=caught(func(){library::Set[2](func()*[2]int{trace+="b";return &values},func()int{return mark("i",2)},func()int{return mark("r",42)})})
 fmt::Println(bad,trace,values[0],values[1])
 trace=""
 bad=caught(func(){library::Add[2](func()*[2]int{trace+="b";return missing},func()int{return mark("i",0)},func()int{return mark("r",42)})})
 fmt::Println(bad,trace)
}`, "true bir\ntrue bir 10 20\ntrue bir\n"},
		{"tuple writes occur after all operands", `func main(){
 values:=[2]int{10,20}
 bad:=caught(func(){library::Tuple[2](&values,func()int{return mark("a",0)},func()int{return mark("b",2)},func()(int,int){trace+="r";return 42,43})})
 fmt::Println(bad,trace,values[0],values[1])
 trace=""
 library::BlankTuple[2](&values,func()(int,int){trace+="r";return 99,21})
 library::Shift[2](&values,func()uint{trace+="s";return 1})
 fmt::Println(trace,values[0])
 fmt::Println(library::MutableIndex[2](&values),values[0])
}`, "true abr 42 20\nrs 42\n99 99\n"},
	})
}

func TestStableGoConstArrayRangeLengthAndControl(t *testing.T) {
	const library = `package library
use r#unsafe;
pub func ValueRange[const N int](values *[N]int) int {total:=0;for i,value:=range *values{if i==0{values[1]=99};total+=value};return total}
pub func PointerRange[const N int](values *[N]int) int {total:=0;for i,value:=range values{if i==0{values[1]=99};total+=value};return total}
pub func PointerIdentity[const N int](slot **[N]int, replacement *[N]int) int {total:=0;for _,value:=range *slot{*slot=replacement;total+=value};return total}
pub func Keys[const N int](values *[N]int) int {total:=0;for i:=range *values{total+=i+1};return total}
pub func NoVariables[const N int](values *[N]int) int {total:=0;for range *values{total++};return total}
pub func KeyCall[const N int](base func()*[N]int) int {total:=0;for i:=range base(){total+=i+1};return total}
pub func ValueCall[const N int](base func()*[N]int) int {total:=0;for i:=range *base(){total+=i+1};return total}
pub func Values[const N int](values *[N]int) int {total:=0;for _,value:=range values{total+=value};return total}
pub func RangeAssign[const N int](source,target *[N]int,index func()int){for target[index()],target[index()]=range source{}}
pub func Lengths[const N int](values *[N]int)(int,int){return len(*values),cap(*values)}
pub func PointerLength[const N int](base func()*[N]int)int{return len(base())}
pub func ValueLength[const N int](base func()*[N]int)int{return len(*base())}
pub func LiteralLength[const N int](value func()int)int{return len([N]int{value()})}
pub func NestedLength[const N int](values *[N]int)int{return len([N]int{len(*values)})}
pub func LayoutLength[const N int](values *[N][2]int, zero func()[0]int)int{return len(values[r#unsafe::Sizeof(zero())])}
pub func LayoutAlignment[const N int](values *[N][2]int, zero func()[0]int)int{return cap(values[r#unsafe::Alignof(zero())-r#unsafe::Alignof(zero())])}
pub func PostIncrement[const N int](values *[N]int)int{total:=0;for i:=0;i<3;values[0]++{i++;if i==2{continue};total+=values[0]};return total}
pub func PostAssign[const N int](values *[N]int)int{total:=0;for i:=0;i<3;values[0]=values[0]+1{i++;if i==2{continue};total+=values[0]};return total}
pub func Nested[const M,N int](values *[M][N]int, row,column int)int{values[row][column]++;return values[row][column]}
`
	runStableGoLibraryFixture(t, library, stableArrayConsumer, []stableGoCase{
		{"range value snapshot and pointer identity", `func main(){
 first:=[2]int{1,2};second:=[2]int{1,2}
 fmt::Println(library::ValueRange[2](&first),first[1],library::PointerRange[2](&second),second[1])
 a:=[2]int{1,2};b:=[2]int{10,20};selected:=&a
 total:=library::PointerIdentity[2](&selected,&b)
 fmt::Println(total,selected==&b)
}`, "3 99 100 99\n3 true\n"},
		{"single variable nil range and zero length", `func main(){
 var missing *[2]int;var empty *[0]int
 fmt::Println(library::Keys[2](missing),library::NoVariables[2](missing),library::Keys[0](empty),library::Values[0](empty))
 calls:=0
 fmt::Println(library::KeyCall[2](func()*[2]int{calls++;return missing}),calls)
 bad:=caught(func(){library::ValueCall[0](func()*[0]int{calls++;return empty})})
 fmt::Println(bad,calls)
}`, "3 2 0 0\n3 1\ntrue 2\n"},
		{"range assignment operands precede element checks", `func main(){
 var missing *[2]int
 target:=[2]int{10,20}
 bad:=caught(func(){library::RangeAssign[2](missing,&target,func()int{trace+="i";return 2})})
 fmt::Println(bad,trace,target[0],target[1])
}`, "true ii 10 20\n"},
		{"len cap only evaluate required operands", `func main(){
 var missing *[2]int;var empty *[0]int
 length,capacity:=library::Lengths[2](missing);zero,zeroCapacity:=library::Lengths[0](empty)
 fmt::Println(length,capacity,zero,zeroCapacity,library::NestedLength[2](missing))
 calls:=0
 fmt::Println(library::PointerLength[2](func()*[2]int{calls++;return missing}),calls)
 bad:=caught(func(){library::ValueLength[0](func()*[0]int{calls++;return empty})})
 fmt::Println(bad,calls)
 fmt::Println(library::LiteralLength[2](func()int{calls++;return 42}),calls)
}`, "2 2 0 0 2\n2 1\ntrue 2\n2 3\n"},
		{"loop post and nested array storage", `func main(){
 a:=[2]int{};b:=[2]int{}
 fmt::Println(library::PostIncrement[2](&a),a[0],library::PostAssign[2](&b),b[0])
 nested:=[2][3]int{{1,2,3},{4,5,6}}
 fmt::Println(library::Nested[2,3](&nested,1,2),nested[1][2])
}`, "2 3 2 3\n7 7\n"},
		{"fixed layout calls remain unevaluated", `func main(){
 var missing *[2][2]int
 calls:=0
 zero:=func()[0]int{calls++;return [0]int{}}
 fmt::Println(library::LayoutLength[2](missing,zero),library::LayoutAlignment[2](missing,zero),calls)
}`, "2 2 0\n"},
	})
}
