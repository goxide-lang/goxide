package acceptance_test

import (
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
)

func TestSumValueProjectionRace(t *testing.T) {
	enumGeneratedModule(t, `package fixture
enum Mode { Idle, Workers(int), }
type (
 Leaf struct { Mode Mode }
 Envelope struct { Left, Right Leaf; Maybe Option[Mode] }
 Alias = Envelope
)
func shadowType(){type Envelope int;var n Envelope;_=n}
func makeEnvelope() Alias {return Alias{Left:Leaf{Mode:Mode::Idle},Right:Leaf{Mode:Mode::Workers(4)},Maybe:Option[Mode]::None}}
func result(v Alias)Result[Alias,error]{return Result[Alias,error]::Ok(v)}
func propagate(v Result[Alias,error],seen *int)Result[int,error]{_=v?;*seen+=1;return Result[int,error]::Ok(1)}
func guard(seen *int)bool{*seen+=1;return true}
func inspect(v Result[Alias,error],seen *int){match v {Result[Alias,error]::Ok(_) if guard(seen)=>{},Result[Alias,error]::Ok(_)=>{},Result[Alias,error]::Err(_)=>{},}}
func invalidNested() Alias {v:=makeEnvelope();var bad Mode;v.Maybe=Option[Mode]::Some(bad);return v}
`, `package fixture
import "testing"
func TestValidValueProjection(t *testing.T){n:=0;v:=result(makeEnvelope());_=propagate(v,&n);inspect(v,&n);if n!=2{t.Fatal(n)}}
func TestInvalidValueBeforeEffects(t *testing.T){for _,name:=range []string{"first","second","nested"}{t.Run(name,func(t *testing.T){v:=makeEnvelope();switch name{case "first":v.Left=Leaf{};case "second":v.Right=Leaf{};case "nested":v=invalidNested()};for _,use:=range []func(Result[Alias,error],*int){func(v Result[Alias,error],n *int){_=propagate(v,n)},inspect}{n:=0;func(){defer func(){if recover()==nil{t.Error("accepted invalid nested enum")}}();use(result(v),&n)}();if n!=0{t.Fatal("side effect before validation",n)}}})}}
`, false)
}

func TestSumProjectionUnsupportedPaths(t *testing.T) {
	for name, declaration := range map[string]string{
		"pointer":       `type Holder struct { Bad *Mode }`,
		"slice":         `type Holder struct { Bad []Mode }`,
		"grouped-map":   `type ( Holder struct { Bad map[string]Mode } )`,
		"alias-pointer": `type Ref = *Mode;type Holder struct { Bad Ref }`,
		"embedded":      `type Holder struct { Mode }`,
		"blank":         `type Holder struct { _ Mode }`,
		"recursive":     `type Holder struct { Next Option[Holder] }`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := frontend.Compile("projection_bad.hgo", []byte("package p;enum Mode {Idle,};"+declaration+";func f(v Holder)Option[Holder]{return Option[Holder]::Some(v)}"))
			if err == nil || !strings.Contains(err.Error(), "projection") {
				t.Fatalf("expected projection rejection: %v", err)
			}
		})
	}
}
