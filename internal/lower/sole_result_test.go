package lower_test

import "testing"

func TestSoleMultiResultGenericCalls(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
trait Read {func read(self Self)int}
impl Read for int {func read(self Self)int{return self}}
impl Read for string {func read(self Self)int{return len(self)}}
func Pair[T Read](value T,err error)int{return value.read()}
func Total[T Read](values ...T)int{total:=0;for _,value:=range values{total+=value.read()};return total}
func Size[T Read,U Read,const N int](first T,values [N]U)int{return first.read()+N+values[0].read()}
var calls int
func native()(int,error){calls++;return 42,nil}
func several()(int,int,int){return 20,21,1}
func arrays()(int,[2]string){return 39,[2]string{"a","b"}}
func caught(value any)(int,error){if value==nil{return 0,nil};return 42,nil}
func main(){
 fmt::Println(Pair(native()),Pair[int](native()))
 value,err:=native();fmt::Println(Pair(value,err),calls)
 fmt::Println(Total(several()),Size(arrays()))
 func(){defer func(){fmt::Println(Pair(caught(recover())))}();panic("caught")}()
}
`}, "42 42\n42 3\n42 42\n42\n")
}

func TestSoleMultiResultPrivateTraitCall(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Read {type Item;func read(self Self)int;func produce(self Self)Self::Item}
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Read;
pub func Pair[T Read](value T,err error)int{return value.read()}
pub func Total[T Read](values ...T)int{total:=0;for _,value:=range values{total+=value.read()};return total}
pub func Catch[T Read](value T,caught *bool){*caught=recover()!=nil && value.read()==42}
pub func Send[T Read](value T,ch chan int){ch<-value.read()}
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Read;
struct secret {n int}
struct hidden {}
impl Read for secret {
 type Item=hidden
 func read(self Self)int{return self.n}
 func produce(self Self)Self::Item{return hidden{}}
}
pub func Values(log *[]int)(secret,error){*log=append(*log,2);return secret{n:42},nil}
pub func Many()(secret,secret){return secret{n:20},secret{n:22}}
pub func Recovered(value any)(secret,error){if value!=nil{return secret{n:42},nil};return secret{},nil}
pub func Capture(log *[]int,caught *bool)(secret,*bool){*log=append(*log,2);return secret{n:42},caught}
pub func Async(log *[]int,ch chan int)(secret,chan int){*log=append(*log,2);return secret{n:42},ch}
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func mark(log *[]int,n int)int{*log=append(*log,n);return n}
func main(){
 log:=[]int{}
 fmt::Println(mark(&log,1)+ops::Pair(model::Values(&log))+mark(&log,3),log)
 fmt::Println(ops::Total(model::Many()))
 func(){defer func(){fmt::Println(ops::Pair(model::Recovered(recover())))}();panic("argument")}()
 log=[]int{};caught:=false
 func(){defer ops::Catch(model::Capture(&log,&caught));log=append(log,3);panic("body")}()
 fmt::Println(caught,log)
 log=[]int{};ch:=make(chan int)
 go ops::Send(model::Async(&log,ch));log=append(log,3)
 fmt::Println(<-ch,log)
}
`,
	}, "46 [1 2 3]\n42\n42\ntrue [2 3]\n42 [2 3]\n")
}
