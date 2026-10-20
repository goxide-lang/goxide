package acceptance_test

import (
	"fmt"
	"github.com/goxide-lang/goxide/internal/rootconfig"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExplicitEqualityModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	mod := func(name, lib string) string {
		s := fmt.Sprintf("module %s\ngo 1.27\n", name)
		if lib != "" {
			s += fmt.Sprintf("require equal.example/lib v0.0.0\nreplace equal.example/lib => %s\n", filepath.ToSlash(lib))
		}
		return s
	}
	libsrc := t.TempDir()
	packagesWrite(t, libsrc, "go.mod", mod("equal.example/lib", ""))
	packagesWrite(t, libsrc, "model/model.gox", `package model
#[derive(PartialEq,Eq)]
type Count uint64
#[derive(PartialEq,Eq)]
type Point struct{X int;Y int}
#[derive(PartialEq,Eq)]
type Row [2]Point
#[derive(PartialEq,Eq)]
pub enum Leaf{LeafUnit,}
#[derive(PartialEq,Eq)]
pub enum E{Unit,P(Point),A([2]int),Child(Leaf),}
#[derive(PartialEq)]
pub enum Float{Value(float64),}
#[derive(PartialEq)]
type Complex struct{Z complex128}
#[derive(PartialEq,Eq)]
type Key struct{ID int;Flags [2]bool}
#[derive(PartialEq,Eq)]
type Dataset struct{Slices [][]int;Lookup map[string][]Point;Codes map[Key]Row;Enabled map[bool]int;Items []E;Events map[string]E}
#[derive(PartialEq,Eq)]
type Optional struct{Value Option[[]int]}
#[derive(PartialEq,Eq)]
type OptionalLeaf struct{Value Option[Leaf]}
#[derive(PartialEq)]
type FloatCollections struct{Values []float64;ByName map[string]complex128}
#[derive(PartialEq,Eq)]
pub enum Collection{Empty,Data(Dataset),}
func ResultSeed()Result[[]int,map[string]int]{return Result[[]int,map[string]int]::Ok(nil)}
func Local()bool{return E::P(Point{X:1,Y:2}).equals(E::P(Point{X:1,Y:2}))}
`)
	packagesWrite(t, libsrc, "relay/relay.go", `package relay
import __borrow_std "github.com/goxide-lang/std/sum"

import m "equal.example/lib/model"
type Alias=m.E
type Point=m.Point
type Dataset=m.Dataset
type Options=__borrow_std.Option[[]int]
type New m.E
`)
	lib := filepath.Join(t.TempDir(), "lib")
	packagesCommand(t, root, cli, true, "transpile", "-o", lib, libsrc)
	appFor := func(code string) string {
		src := t.TempDir()
		packagesWrite(t, src, "go.mod", mod("equal.example/app", lib))
		writeAcceptanceRoots(t, src, map[string]rootconfig.Mapping{"model": {Module: "equal.example/lib", Package: "equal.example/lib/model"}, "relay": {Module: "equal.example/lib", Package: "equal.example/lib/relay"}})
		packagesWrite(t, src, "app.gox", code)
		return src
	}
	app := appFor(`package app
use model as m;use relay as r;
#[derive(PartialEq,Eq)]
type Pair struct{P r::Point;E r::Alias}
#[derive(PartialEq,Eq)]
type ExternalSums struct{Direct Option[[]int];Alias r::Options;Result Result[[]int,map[string]int];Nested Option[m::Leaf]}
var Initial=m::Point{X:1}.equals(m::Point{X:1})
func Same(a,b r::Alias)bool{return a.equals(b)}
func IsNil(v m::E)bool{return v==nil}
func IsPresent(v m::E)bool{return v!=nil}
func PairSame(a,b Pair)bool{return a.equals(b)}
func ExternalSumsSame(a,b ExternalSums)bool{return a.equals(b)}
func Big()bool{return m::Count(1<<63).equals(1<<63)}
func Row()bool{a:=m::Row{{X:1},{Y:2}};return a.equals(m::Row{{X:1},{Y:2}})}
func FloatSame(a,b m::Float)bool{return a.equals(b)}
func ComplexSame(a,b m::Complex)bool{return a.equals(b)}
func DatasetSame(a,b r::Dataset)bool{return a.equals(b)}
func OptionalSame(a,b m::Optional)bool{return a.equals(b)}
func OptionalLeafSame(a,b m::OptionalLeaf)bool{return a.equals(b)}
func CollectionSame(a,b m::Collection)bool{return a.equals(b)}
func FloatsSame(a,b m::FloatCollections)bool{return a.equals(b)}
func Skipped(a,b r::Alias)bool{return false && Left(a).equals(Right(b))}
func Shadow(a,b r::Alias)bool{EqualsE:=func(r::Alias,r::Alias)bool{panic("shadow")};_=EqualsE;m:=3;_=m;return a.equals(b)}
func Load(a r::Alias)Result[r::Alias,string]{return Result[r::Alias,string]::Ok(a)}
func Question(a,b r::Alias)Result[bool,string]{v:=Load(a)?.equals(b);return Result[bool,string]::Ok(v)}
func OptionFlow(x Option[int])Option[int]{n:=x?;return Option[int]::Some(n)}
var Trace string
func Left(a r::Alias)r::Alias{Trace+="L";return a}
func Right(a r::Alias)r::Alias{Trace+="R";return a}
func Ordered(a,b r::Alias)bool{return Left(a).equals(Right(b))}
macro_rules! equalityZero {()=>{int(0)};}
macro_rules! equalityOuter {()=>{equalityZero!()};}
macro_rules! equalityConvert {($x:expr)=>{int($x)};}
#[derive(PartialEq,Eq)]
type EqualityItem struct{N int}
func EqualityDirect()int{return equalityZero!()}
func EqualityFirst()int{return equalityOuter!()}
func EqualitySecond()int{return equalityOuter!()}
func EqualityNative()int{return int(0)}
var EqualityCalls int
func equalityNext()int{EqualityCalls++;return EqualityCalls}
func EqualityEffect()int{return equalityConvert!(equalityNext())+equalityConvert!(equalityNext())}
`)
	out := filepath.Join(t.TempDir(), "out")
	compileAcceptanceGraph(t, true, app, out, libsrc)
	for _, pair := range [][2]string{{libsrc, lib}, {app, out}} {
		second := filepath.Join(t.TempDir(), "repeat")
		if pair[0] == libsrc {
			packagesCommand(t, root, cli, true, "transpile", "-o", second, libsrc)
		} else {
			compileAcceptanceGraph(t, true, pair[0], second, libsrc)
		}
		if err := filepath.WalkDir(pair[1], func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(pair[1], path)
			a, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(filepath.Join(second, rel))
			if err != nil {
				return err
			}
			if string(a) != string(b) {
				return fmt.Errorf("nondeterministic %s", rel)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	packagesWrite(t, out, "app_test.go", `package app
import __borrow_std "github.com/goxide-lang/std/sum"

import("testing";"math";"strings";"fmt";m "equal.example/lib/model")
func TestValues(t *testing.T){if EqualityDirect()!=0||EqualityFirst()!=0||EqualitySecond()!=0||EqualityNative()!=0{t.Fatal("macro zero conversion")};EqualityCalls=0;if EqualityEffect()!=3||EqualityCalls!=2{t.Fatal("macro argument evaluation count",EqualityCalls)};if !EqualsEqualityItem(EqualityItem{N:1},EqualityItem{N:1})||EqualsEqualityItem(EqualityItem{N:1},EqualityItem{N:2}){t.Fatal("macro adjacent derived equality")};a:=&m.P{Value: m.Point{X:1,Y:2}};b:=&m.P{Value: m.Point{X:1,Y:2}};if !IsNil(nil)||IsNil(a)||IsPresent(nil)||!IsPresent(a){t.Fatal("enum nil comparison")};if !Initial||!Same(a,b)||Same(a,&m.Unit{})||Same(a,&m.P{Value: m.Point{X:3}})||!Row()||!Big()||!Shadow(a,b)||!PairSame(Pair{m.Point{X:2},a},Pair{m.Point{X:2},b}){t.Fatal("value equality")};if !__borrow_std.ValueOk(Question(a, b)){t.Fatal("question")};if __borrow_std.TagOption(OptionFlow(__borrow_std.None[int]()))!=0{t.Fatal("None propagation")};n:=math.NaN();if FloatSame(&m.Value{Value: n},&m.Value{Value: n})||ComplexSame(m.Complex{Z:complex(n,0)},m.Complex{Z:complex(n,0)}){t.Fatal("NaN")};if !FloatSame(&m.Value{Value: math.Copysign(0,-1)},&m.Value{Value: 0}){t.Fatal("zero")}}
func TestRecursiveContainers(t *testing.T){
 key:=m.Key{ID:7,Flags:[2]bool{true,false}}
 makeData:=func()m.Dataset{return m.Dataset{Slices:[][]int{{1,2},{3}},Lookup:map[string][]m.Point{"p":{{X:1,Y:2}}},Codes:map[m.Key]m.Row{key:{{X:4},{Y:5}}},Enabled:map[bool]int{true:1,false:0},Items:[]m.E{&m.Unit{},&m.P{Value: m.Point{X:8}}},Events:map[string]m.E{"ready":&m.Unit{}}}}
 a,b:=makeData(),makeData();if !DatasetSame(a,b)||!CollectionSame(&m.Data{Value: a},&m.Data{Value: b})||CollectionSame(&m.Empty{},&m.Data{Value: a}){t.Fatal("recursive containers")}
 b.Lookup["p"][0].Y=9;if DatasetSame(a,b){t.Fatal("nested map/slice value")};b=makeData();b.Codes[key]=m.Row{};if DatasetSame(a,b){t.Fatal("stable struct/array key")};b=makeData();delete(b.Enabled,false);b.Enabled[false]=2;if DatasetSame(a,b){t.Fatal("bool key value")};b=makeData();b.Events["ready"]=&m.P{Value: m.Point{}};if DatasetSame(a,b){t.Fatal("map enum value")}
 if DatasetSame(m.Dataset{Lookup:map[string][]m.Point{"a":nil}},m.Dataset{Lookup:map[string][]m.Point{"b":nil}}){t.Fatal("missing key must differ from zero value")}
 if !DatasetSame(m.Dataset{Lookup:map[string][]m.Point{"a":{},"b":{}}},m.Dataset{Lookup:map[string][]m.Point{"b":{},"a":{}}}){t.Fatal("map insertion order")}
}
func TestNilEmptyAndOptions(t *testing.T){
 zero:=m.Dataset{};if !DatasetSame(zero,zero)||DatasetSame(zero,m.Dataset{Slices:[][]int{}})||DatasetSame(zero,m.Dataset{Lookup:map[string][]m.Point{}}){t.Fatal("nil vs empty container")}
 if DatasetSame(m.Dataset{Slices:[][]int{nil}},m.Dataset{Slices:[][]int{{}}}){t.Fatal("nested nil vs empty")}
 none:=m.Optional{Value:__borrow_std.None[[]int]()};nilSome:=m.Optional{Value:__borrow_std.NewSome[[]int](nil)};emptySome:=m.Optional{Value:__borrow_std.NewSome[[]int]([]int{})}
 if !OptionalSame(none,none)||!OptionalSame(nilSome,nilSome)||!OptionalSame(emptySome,emptySome)||OptionalSame(none,nilSome)||OptionalSame(nilSome,emptySome)||OptionalSame(none,emptySome){t.Fatal("None/Some(nil)/Some(empty)")}
 func(){defer func(){if recover()==nil{t.Fatal("invalid nested Option payload")}}();OptionalLeafSame(m.OptionalLeaf{Value:__borrow_std.NewSome[m.Leaf](nil)},m.OptionalLeaf{})}()
}
func TestConsumerDerivedExternalSumContainers(t *testing.T){
 okNil:=__borrow_std.NewOk[[]int,map[string]int](nil);okEmpty:=__borrow_std.NewOk[[]int,map[string]int]([]int{})
 errNil:=__borrow_std.NewErr[[]int,map[string]int](nil);errEmpty:=__borrow_std.NewErr[[]int,map[string]int](map[string]int{})
 base:=ExternalSums{Result:okNil};if !ExternalSumsSame(base,base){t.Fatal("external conditional Eq")}
 for _,result:=range []__borrow_std.Result[[]int,map[string]int]{okEmpty,errNil,errEmpty}{if ExternalSumsSame(base,ExternalSums{Result:result}){t.Fatal("result variant/nil distinctions")}}
 if ExternalSumsSame(ExternalSums{Result:errNil},ExternalSums{Result:errEmpty}){t.Fatal("Err nil/empty map")}
 sameOk:=ExternalSums{Result:__borrow_std.NewOk[[]int,map[string]int]([]int{1,2})};if !ExternalSumsSame(sameOk,ExternalSums{Result:__borrow_std.NewOk[[]int,map[string]int]([]int{1,2})})||ExternalSumsSame(sameOk,ExternalSums{Result:__borrow_std.NewOk[[]int,map[string]int]([]int{1,3})}){t.Fatal("Ok slice contents")}
 a:=ExternalSums{Result:__borrow_std.NewErr[[]int,map[string]int](map[string]int{"x":1})};b:=ExternalSums{Result:__borrow_std.NewErr[[]int,map[string]int](map[string]int{"x":1})}
 if !ExternalSumsSame(a,b){t.Fatal("Err map contents")};b.Result=__borrow_std.NewErr[[]int,map[string]int](map[string]int{"x":2});if ExternalSumsSame(a,b){t.Fatal("Err map value difference")}
 for _,alias:=range []bool{false,true}{
  nilSome:=base;emptySome:=base
  if alias{nilSome.Alias=__borrow_std.NewSome[[]int](nil);emptySome.Alias=__borrow_std.NewSome[[]int]([]int{})}else{nilSome.Direct=__borrow_std.NewSome[[]int](nil);emptySome.Direct=__borrow_std.NewSome[[]int]([]int{})}
  if !ExternalSumsSame(nilSome,nilSome)||ExternalSumsSame(base,nilSome)||ExternalSumsSame(nilSome,emptySome){t.Fatal("consumer Option None/Some nil/empty",alias)}
 }
 for _,bad:=range []__borrow_std.Result[[]int,map[string]int]{{}}{func(){defer func(){if recover()==nil{t.Fatal("consumer Result validation")}}();ExternalSumsSame(ExternalSums{Result:bad},base)}()}
 func(){bad:=base;bad.Nested=__borrow_std.NewSome[m.Leaf](nil);defer func(){if recover()==nil{t.Fatal("consumer nested Option validation")}}();ExternalSumsSame(bad,base)}()
}
func TestContainerNaNAndShortCircuit(t *testing.T){
 nan:=math.NaN();a:=m.FloatCollections{Values:[]float64{nan}};if FloatsSame(a,a){t.Fatal("slice NaN reflexivity")};a=m.FloatCollections{ByName:map[string]complex128{"z":complex(nan,0)}};if FloatsSame(a,a){t.Fatal("map value NaN reflexivity")}
 Trace="";if Skipped(nil,nil)||Trace!=""{t.Fatal("boolean short circuit")}
 // Struct and container comparison short-circuit; unvisited enum tails are not new validation boundaries.
 if DatasetSame(m.Dataset{Slices:[][]int{{1}},Items:[]m.E{nil}},m.Dataset{Slices:[][]int{{2}},Items:[]m.E{nil}}){t.Fatal("struct first field")}
 left:=m.Dataset{Items:[]m.E{&m.Unit{},nil}};right:=m.Dataset{Items:[]m.E{&m.P{Value: m.Point{}},nil}}
 if DatasetSame(left,right){t.Fatal("slice first element")}
 for _,bad:=range []m.E{nil}{func(){defer func(){if recover()==nil{t.Fatal("visited enum element")}}();DatasetSame(m.Dataset{Items:[]m.E{bad}},m.Dataset{Items:[]m.E{&m.Unit{}}})}();func(){defer func(){if recover()==nil{t.Fatal("visited map enum element")}}();DatasetSame(m.Dataset{Events:map[string]m.E{"x":bad}},m.Dataset{Events:map[string]m.E{"x":&m.Unit{}}})}()}
 // The outer enum's existing validator still runs before comparison, including its nested payload validation.
 func(){defer func(){if recover()==nil{t.Fatal("outer enum must validate before unequal fields")}}();CollectionSame(&m.Data{Value: left},&m.Data{Value: right})}()
}
func TestLeftValidationFirst(t *testing.T){Trace="";defer func(){p:=recover();if p==nil||!strings.Contains(fmt.Sprint(p),"Leaf")||Trace!="LR"{t.Fatal(p,Trace)}}();Ordered(&m.Child{Value: nil},nil)}
func TestValidationAndOrder(t *testing.T){valid:=&m.Unit{};for _,bad:=range []m.E{nil}{func(){Trace="";defer func(){if recover()==nil||Trace!="LR"{t.Fatal("evaluation/validation",Trace)}}();Ordered(bad,valid)}();func(){Trace="";defer func(){if recover()==nil||Trace!="LR"{t.Fatal("right validation",Trace)}}();Ordered(valid,bad)}()}}
`)
	packagesWrite(t, lib, "model/model_test.go", `package model
import "testing"
func TestHelpers(t *testing.T){if !EqualsDataset(Dataset{},Dataset{})||!Local()||!EqualsPoint(Point{X:2},Point{X:2})||!EqualsE(&A{Value: [2]int{1,2}},&A{Value: [2]int{1,2}}){t.Fatal("helpers")}}
`)
	plain := t.TempDir()
	packagesWrite(t, plain, "go.mod", mod("equal.example/plain", lib))
	packagesWrite(t, plain, "plain_test.go", `package plain
import("testing";m "equal.example/lib/model")
func TestNative(t *testing.T){a:=&m.Unit{};if !m.EqualsDataset(m.Dataset{Slices:[][]int{{1}}},m.Dataset{Slices:[][]int{{1}}})||!m.EqualsE(a,a)||a!=a{t.Fatal("ordinary Go behavior")}}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	assertNativeGoRejected(t, out, tool, map[string]nativeGoRejection{
		"alias-option-nil": {"package app\nvar _=ExternalSums{Alias:nil}\n", "cannot use nil"},
	})
	for name, tc := range map[string]struct{ code, want string }{
		"nested-macro-conversion": {`#[derive(PartialEq,Eq)]
type EqualityItem struct{N int}
macro_rules! zero {()=>{int("bad")};}
macro_rules! outer {()=>{zero!()};}
func Run()int{return outer!()}`, "cannot convert"},
		"enum-direct":          {`use model as m;func Bad(a,b m::E)bool{return a==b}`, "explicit PartialEq"},
		"enum-not-equal":       {`use model as m;func Bad(a,b m::E)bool{return a!=b}`, "explicit PartialEq"},
		"float-eq":             {"#[derive(PartialEq,Eq)]\ntype Bad struct{F float64}", "Eq"},
		"complex-eq":           {"#[derive(PartialEq,Eq)]\ntype Bad complex128", "Eq"},
		"missing-partial":      {"#[derive(Eq)]\ntype Bad struct{N int}", "PartialEq"},
		"slice-pointer":        {"#[derive(PartialEq)]\ntype Bad struct{Items []*int}", "no explicit PartialEq implementation"},
		"slice-float-eq":       {"#[derive(PartialEq,Eq)]\ntype Bad struct{Items []float64}", "Eq"},
		"map-complex-value-eq": {"#[derive(PartialEq,Eq)]\ntype Bad struct{Items map[string]complex128}", "Eq"},
		"map-float-key":        {"#[derive(PartialEq)]\ntype Bad struct{Items map[float64]int}", "map"},
		"map-complex-key":      {"#[derive(PartialEq)]\ntype Bad struct{Items map[complex128]int}", "map"},
		"map-interface-key":    {"#[derive(PartialEq)]\ntype Bad struct{Items map[any]int}", "map"},
		"map-pointer-key":      {"#[derive(PartialEq)]\ntype Bad struct{Items map[*int]int}", "map"},
		"map-enum-key":         {"pub enum Key{A,}\n#[derive(PartialEq)]\ntype Bad struct{Items map[Key]int}", "map"},
		"map-nested-float-key": {"type Key struct{F float64}\n#[derive(PartialEq)]\ntype Bad struct{Items map[Key]int}", "map"},
		"pointer":              {"#[derive(PartialEq,Eq)]\ntype Bad struct{P *int}", "no explicit PartialEq implementation"},
		"unregistered":         {`use relay as r;func Bad(a,b r::New)bool{return a.equals(b)}`, "equals"},
		"map-key": {`pub enum Bad{Items([]int),}
func F(){_=map[Bad]int{}}`, "map key"},
		"nested-map-key": {`pub enum Bad{Items([]int),}
type Key struct{B Bad}
func F(){_=map[Key]int{}}`, "map key"},
		"wrapped-comparison": {`pub enum E{A,S([]int),}
func Bad()bool{v:=struct{E E}{E::S([]int{1})};return v==v}`, "non-comparable payload"},
		"unsafe-switch": {`pub enum E{A,S([]int),}
func Bad(v E){switch v{case E::A:}}`, "switch comparison"},
		"interface-map-index": {`pub enum E{A,S([]int),}
func Bad(v E){m:=map[any]int{};m[v]=1}`, "map key"},
		"interface-map-literal": {`pub enum E{A,S([]int),}
func Bad(){_=map[any]int{E::S([]int{1}):1}}`, "map key"},
		"mixed-interface-comparison": {`pub enum E{A,S([]int),}
func Bad(v E,other any)bool{return v==other}`, "non-comparable payload"},
		"imported-partial-not-eq": {`use model as m;
#[derive(PartialEq,Eq)]
type Bad struct{F m::Float}`, "Eq"},
		"scalar-result-type": {`type B bool
func Takes(B){}
func Bad(){a:=1;Takes(a.equals(1))}`, "cannot use"},
		"interface-map-delete": {`pub enum E{A,S([]int),}
func Bad(v E){delete(map[any]int{},v)}`, "map key"},
		"interface-switch-case": {`pub enum E{A,S([]int),}
func Bad(v E,other any){switch other{case v:}}`, "switch case comparison"},
		"helper-collision": {"#[derive(PartialEq)]\ntype X struct{N int}\nfunc EqualsX(a,b X)bool{return true}", "conflicts with a package declaration"},
	} {
		t.Run(name, func(t *testing.T) {
			src := appFor("package app\n" + tc.code)
			msg := compileAcceptanceGraph(t, false, src, filepath.Join(t.TempDir(), "out"), libsrc)
			if !strings.Contains(msg, tc.want) {
				t.Fatal(msg)
			}
			if name == "nested-macro-conversion" && (!strings.Contains(msg, "macro outer") || !strings.Contains(msg, "macro zero")) {
				t.Fatal("nested conversion lost macro definitions and callers:", msg)
			}
		})
	}

}
