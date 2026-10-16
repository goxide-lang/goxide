package acceptance_test

import (
	"strings"
	"testing"
)

func TestSumValueProjectionRace(t *testing.T) {
	enumGeneratedModule(t, `package fixture
pub enum Mode { Idle, Workers(int), }
type (
 Leaf struct { Mode Mode }
 Envelope struct { Left, Right Leaf; Maybe Option[Mode]; Modes []Mode; Lookup map[string]Mode }
 Alias = Envelope
)
func shadowType(){type Envelope int;var n Envelope;_=n}
func makeEnvelope() Alias {return Alias{Left:Leaf{Mode:Mode::Idle},Right:Leaf{Mode:Mode::Workers(4)},Maybe:Option[Mode]::None,Modes:[]Mode{Mode::Idle},Lookup:map[string]Mode{"ok":Mode::Idle}}}
func result(v Alias)Result[Alias,error]{return Result[Alias,error]::Ok(v)}
func propagate(v Result[Alias,error],seen *int)Result[int,error]{_=v?;*seen+=1;return Result[int,error]::Ok(1)}
func guard(seen *int)bool{*seen+=1;return true}
func inspect(v Result[Alias,error],seen *int){match v {Result[Alias,error]::Ok(_) if guard(seen)=>{},Result[Alias,error]::Ok(_)=>{},Result[Alias,error]::Err(_)=>{},}}
func observeMode(value Mode,seen *int){match value{_=>{*seen+=1},}}
type Ref = *Mode
type Holder struct { Direct *Mode; Alias Ref; _ Mode }
struct GoxideHolder { Direct *Mode }
func pass(v Holder)Option[Holder]{return Option[Holder]::Some(v)}
func passGoxide(v GoxideHolder)Option[GoxideHolder]{return Option[GoxideHolder]::Some(v)}
func preserved()bool{v:=Mode::Idle;h:=Holder{Direct:&v,Alias:&v};g:=GoxideHolder{Direct:&v};a:=match pass(h){Option[Holder]::Some(x)=>x,Option[Holder]::None=>Holder{}};b:=match passGoxide(g){Option[GoxideHolder]::Some(x)=>x,Option[GoxideHolder]::None=>GoxideHolder{}};return a.Direct==&v&&a.Alias==&v&&b.Direct==&v}
`, `package fixture
import __borrow_std "github.com/goxide-lang/std/sum"

import("testing";"fmt";"reflect";"strings")
func TestValidValueProjection(t *testing.T){n:=0;v:=result(makeEnvelope());_=propagate(v,&n);inspect(v,&n);if n!=2{t.Fatal(n)};if !preserved(){t.Fatal("sum changed enum pointer fields")}}
func TestInvalidValueBeforeEffects(t *testing.T){for _,name:=range []string{"first","second","nested","slice","map","reflect-zero-slice","reflect-zero-map"}{t.Run(name,func(t *testing.T){
 v:=makeEnvelope();var bad Mode
 switch name{case "first":v.Left=Leaf{};bad=v.Left.Mode;case "second":v.Right=Leaf{};bad=v.Right.Mode;case "nested":v.Maybe=__borrow_std.NewSome[Mode](nil);bad=__borrow_std.ValueSome(v.Maybe);case "slice":v.Modes=[]Mode{nil};bad=v.Modes[0];case "map":v.Lookup=map[string]Mode{"bad":nil};bad=v.Lookup["bad"];case "reflect-zero-slice","reflect-zero-map":reflected:=reflect.Zero(reflect.TypeOf((*Mode)(nil)).Elem());if !reflected.IsNil(){t.Fatal("non-nil zero enum")};if name=="reflect-zero-slice"{v.Modes=[]Mode{bad};bad=v.Modes[0]}else{v.Lookup=map[string]Mode{"bad":bad};bad=v.Lookup["bad"]}}
 for _,use:=range []func(__borrow_std.Result[Alias,error],*int){func(v __borrow_std.Result[Alias,error],n *int){_=propagate(v,n)},inspect}{n:=0;use(result(v),&n);if n!=1{t.Fatal("outer projection skipped its continuation or guard",n)}}
 seen:=0;func(){defer func(){if p:=recover();!strings.Contains(fmt.Sprint(p),"nil Mode")||seen!=0{t.Fatalf("payload dispatch panic=%v effect=%d",p,seen)}}();observeMode(bad,&seen)}()
 })}}
`, false, true)
}

func TestSumProjectionUnsupportedPaths(t *testing.T) {
	for name, declaration := range map[string]string{
		"embedded":  `type Holder struct { Mode }`,
		"recursive": `type Holder struct { Next Option[Holder] }`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceLanguageFixture("projection_bad.gox", []byte("package p;enum Mode {Idle,};"+declaration+";func f(v Holder)Option[Holder]{return Option[Holder]::Some(v)}"))
			if err == nil || !strings.Contains(err.Error(), "projection") {
				t.Fatalf("expected projection rejection: %v", err)
			}
		})
	}
}
