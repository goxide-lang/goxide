package acceptance_test

import (
	"fmt"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSumCloneDebugLocalRace(t *testing.T) {
	enumFormalStdModule(t, `package fixture; use std; use std::PartialEq;
#[derive(std::derive::Clone,std::derive::Debug)]
type Item struct{Values []int}
type Unused=[]int
#[derive(std::derive::Clone,std::derive::Debug)]
pub enum Payload{A,B([]int),}
#[derive(std::derive::Clone,std::derive::Debug)]
type Branches struct{V Option[Payload];R Result[int,Payload]}
func Branch()Branches{return Branches{R:Result[int,Payload]::Ok(3)}}
type Local=Option[[]Item]
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug)]
type Holder struct{V Local}
#[derive(std::derive::Clone,std::derive::Debug)]
type Reply struct{V Result[[]Item,map[string][]int];Nested Option[Result[[]int,string]]}
func Some(v []Item)Local{return Option[[]Item]::Some(v)}
func Ok(v []Item)Reply{return Reply{V:Result[[]Item,map[string][]int]::Ok(v)}}
func Err(v map[string][]int)Reply{return Reply{V:Result[[]Item,map[string][]int]::Err(v)}}
func Values(v Local)[]Item{return match v{Option[[]Item]::Some(x)=>x,Option[[]Item]::None=>nil,}}
func Data(v Reply)[]Item{return match v.V{Result[[]Item,map[string][]int]::Ok(x)=>x,Result[[]Item,map[string][]int]::Err(_)=>nil,}}
func ErrorData(v Reply)map[string][]int{return match v.V{Result[[]Item,map[string][]int]::Ok(_)=>nil,Result[[]Item,map[string][]int]::Err(x)=>x,}}
func Nested()Reply{return Reply{V:Result[[]Item,map[string][]int]::Ok(nil),Nested:Option[Result[[]int,string]]::Some(Result[[]int,string]::Err("quoted\n"))}}

impl Item {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}

impl Payload {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}

impl Branches {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}

impl Holder {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}

impl Reply {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}
`, `package fixture
import __borrow_std "github.com/goxide-lang/std/sum"
import("testing";"strings")
func TestStatesAndDeepClone(t *testing.T){
 branch:=Branch();bc:=(BranchesOps{}).Copy(&branch);if (BranchesOps{}).Describe(&bc)!="Branches { V: Option::None, R: Result::Ok(3) }"{t.Fatal((BranchesOps{}).Describe(&bc))}

 n:=(HolderOps{}).Initial();if __borrow_std.TagOption(n.V)!=0||__borrow_std.TagOption((HolderOps{}).Copy(&n).V)!=0||(HolderOps{}).Describe(&n)!="Holder { V: Option::None }"{t.Fatal(n,(HolderOps{}).Describe(&n))}
 for _,tc:=range []struct{items []Item;display string}{{nil,"nil"},{[]Item{},"[]"}}{v:=Holder{V:Some(tc.items)};c:=(HolderOps{}).Copy(&v);if (Values(c.V)==nil)!=(tc.items==nil)||(HolderOps{}).Describe(&v)!="Holder { V: Option::Some("+tc.display+") }"{t.Fatal(v,c,(HolderOps{}).Describe(&v))}}
 v:=Holder{V:Some([]Item{{Values:[]int{1}}})};c:=(HolderOps{}).Copy(&v);Values(c.V)[0].Values[0]=9;if Values(v.V)[0].Values[0]!=1{t.Fatal("payload shared")}
 ok:=Ok([]Item{{Values:[]int{2}}});oc:=(ReplyOps{}).Copy(&ok);Data(oc)[0].Values[0]=8;if Data(ok)[0].Values[0]!=2||!strings.Contains((ReplyOps{}).Describe(&ok),"Result::Ok([Item { Values: [2] }])"){t.Fatal(ok,(ReplyOps{}).Describe(&ok))}
 er:=Err(map[string][]int{"x":{3}});ec:=(ReplyOps{}).Copy(&er);ErrorData(ec)["x"][0]=7;if ErrorData(er)["x"][0]!=3||!strings.Contains((ReplyOps{}).Describe(&er),"Result::Err({\"x\": [3]})"){t.Fatal(er,(ReplyOps{}).Describe(&er))}
 nested:=Nested();if (ReplyOps{}).Describe(&nested)!=(ReplyOps{}).Describe(ptrReply((ReplyOps{}).Copy(&nested)))||!strings.Contains((ReplyOps{}).Describe(&nested),"Option::Some(Result::Err(\"quoted\\n\"))"){t.Fatal((ReplyOps{}).Describe(&nested))}
}
func ptrReply(v Reply)*Reply{return &v}
func TestInvalidResult(t *testing.T){for _,f:=range []func(){func(){_=(ReplyOps{}).Copy(&Reply{})},func(){_=(ReplyOps{}).Describe(&Reply{})}}{func(){defer func(){if recover()==nil{t.Fatal("invalid Result accepted")}}();f()}()}}
`, false, true)
}

func TestSumCloneDebugModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "run", "./scripts/build.go", "-o", cli)
	src := formalDependencyDir(t)
	writeStdFormalManifest(t, src, "sums.example/lib", "library", nil)
	packagesWrite(t, src, "lib.gox", "package library; use std; use std::PartialEq; pub mod model; pub mod relay; \n")
	packagesWrite(t, src, "model/sums.gox", `package model; use std; use std::PartialEq;
func Some(v []int)Option[[]int]{return Option[[]int]::Some(v)}
func Ok(v []int)Result[[]int,map[string][]int]{return Result[[]int,map[string][]int]::Ok(v)}
func Err(v map[string][]int)Result[[]int,map[string][]int]{return Result[[]int,map[string][]int]::Err(v)}
#[derive(std::derive::Clone,std::derive::Debug)]
type Item struct{Values []int}
func ItemOption(v Item)Option[Item]{return Option[Item]::Some(v)}
#[derive(std::derive::Clone,std::derive::Debug)]
pub enum State{Valid,}
func StateOption()Option[State]{return Option[State]::Some(State::Valid)}
type secret struct{N int}
type Secret=Option[secret]
func Hidden()Secret{return Option[secret]::None}

impl Item {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}

impl State {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}
`)
	packagesWrite(t, src, "relay/relay.go", `package relay
import __borrow_std "github.com/goxide-lang/std/sum"

type Opt=__borrow_std.Option[[]int]
type Res=__borrow_std.Result[[]int,map[string][]int]
type Flip[A,B any]=__borrow_std.Result[B,A]
`)
	lib := filepath.Join(t.TempDir(), "lib")
	lib = strings.TrimSpace(packagesCommand(t, root, cli, true, "transpile", "--lib", "-o", lib, src))
	appsrc := t.TempDir()
	writeStdFormalManifest(t, appsrc, "sums.example/app", "app", map[string]string{"library": src})

	packagesWrite(t, appsrc, "aliases.go", `package app
import m "sums.example/lib/relay"
type Opt=m.Opt
type Res=m.Res
`)
	packagesWrite(t, appsrc, "app.gox", `package app; use std; use std::PartialEq;
type Level=Opt
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug)]
type Holder struct{V Level}
#[derive(std::derive::Clone,std::derive::Debug)]
type Reply struct{V Res}

impl Holder {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}

impl Reply {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}
`)
	packagesWrite(t, appsrc, "local.gox", `package app; use std; use std::PartialEq;
type Unused=[]int
type Local=Option[[]int]
func LocalSome(v []int)Local{return Option[[]int]::Some(v)}
`)
	packagesWrite(t, appsrc, "peer.gox", `package app; use std; use std::PartialEq;
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug)]
type LocalHolder struct{V Local}

impl LocalHolder {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}
`)
	packagesWrite(t, appsrc, "secret.gox", `package app; use std; use std::PartialEq;
use library::model as m;
use library::relay as r;
#[derive(std::derive::Default)]
type SecretHolder struct{V m::Secret}
#[derive(std::derive::Clone,std::derive::Debug)]
type Reverse struct{V r::Flip[map[string][]int,[]int]}
#[derive(std::derive::Clone,std::derive::Debug)]
type ItemHolder struct{V Option[m::Item]}
#[derive(std::derive::Clone,std::derive::Debug)]
type StateHolder struct{V Option[m::State]}

impl SecretHolder {
 pub func initial() Self { return std::default_value[Self]() }
}

impl Reverse {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}

impl ItemHolder {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}

impl StateHolder {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}
`)
	out := filepath.Join(t.TempDir(), "app")
	compileStdFormalGraph(t, true, appsrc, out)
	repeat := filepath.Join(t.TempDir(), "repeat")
	compileStdFormalGraph(t, true, appsrc, repeat)
	if err := filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(out, path)
		a, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(filepath.Join(repeat, rel))
		if e != nil {
			return e
		}
		if string(a) != string(b) {
			return fmt.Errorf("nondeterministic %s", rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, out, "app_test.go", `package app
import __borrow_std "github.com/goxide-lang/std/sum"

import("testing";"strings";m "sums.example/lib/model")
func TestRegisteredStates(t *testing.T){
 item:=ItemHolder{V:m.ItemOption(m.Item{Values:[]int{1}})};ic:=(ItemHolderOps{}).Copy(&item);__borrow_std.ValueSome(ic.V).Values[0]=9;if __borrow_std.ValueSome(item.V).Values[0]!=1||(ItemHolderOps{}).Describe(&item)!="ItemHolder { V: Option::Some(Item { Values: [1] }) }"{t.Fatal("external payload protocol")}

 if __borrow_std.TagOption((SecretHolderOps{}).Initial().V)!=0||__borrow_std.TagOption((LocalHolderOps{}).Initial().V)!=0{t.Fatal("None default")}
 local:=LocalHolder{V:LocalSome([]int{4})};lc:=(LocalHolderOps{}).Copy(&local);__borrow_std.ValueSome(lc.V)[0]=8;if __borrow_std.ValueSome(local.V)[0]!=4||(LocalHolderOps{}).Describe(&local)!="LocalHolder { V: Option::Some([4]) }"{t.Fatal("peer local protocol")}
 reverse:=Reverse{V:m.Ok([]int{6})};rc:=(ReverseOps{}).Copy(&reverse);__borrow_std.ValueOk(rc.V)[0]=9;if __borrow_std.ValueOk(reverse.V)[0]!=6||(ReverseOps{}).Describe(&reverse)!="Reverse { V: Result::Ok([6]) }"{t.Fatal("alias argument order")}

 n:=(HolderOps{}).Initial();if __borrow_std.TagOption(n.V)!=0||(HolderOps{}).Describe(&n)!="Holder { V: Option::None }"{t.Fatal(n)}
 for _,tc:=range []struct{v []int;text string}{{nil,"nil"},{[]int{},"[]"},{[]int{1},"[1]"}}{v:=Holder{V:m.Some(tc.v)};c:=(HolderOps{}).Copy(&v);data:=__borrow_std.ValueSome(c.V);if (data==nil)!=(tc.v==nil)||(HolderOps{}).Describe(&v)!="Holder { V: Option::Some("+tc.text+") }"{t.Fatal(v,c,(HolderOps{}).Describe(&v))};if len(data)>0{data[0]=9;if tc.v[0]!=1{t.Fatal("shared slice")}}}
 ok:=Reply{V:m.Ok([]int{2})};c:=(ReplyOps{}).Copy(&ok);__borrow_std.ValueOk(c.V)[0]=9;if __borrow_std.ValueOk(ok.V)[0]!=2||!strings.Contains((ReplyOps{}).Describe(&ok),"Result::Ok([2])"){t.Fatal(ok)}
 for _,tc:=range []struct{v map[string][]int;text string}{{nil,"nil"},{map[string][]int{},"{}"}}{v:=Reply{V:m.Err(tc.v)};c:=(ReplyOps{}).Copy(&v);data:=__borrow_std.ValueErr(c.V);if (data==nil)!=(tc.v==nil)||(ReplyOps{}).Describe(&v)!="Reply { V: Result::Err("+tc.text+") }"{t.Fatal(v,c)}}
 er:=Reply{V:m.Err(map[string][]int{"x":{3}})};c=(ReplyOps{}).Copy(&er);__borrow_std.ValueErr(c.V)["x"][0]=9;if __borrow_std.ValueErr(er.V)["x"][0]!=3||!strings.Contains((ReplyOps{}).Describe(&er),"Result::Err({\"x\": [3]})"){t.Fatal(er)}
}
// Option's zero value is valid None. Invalid Result and nested enum values are
// still constructible through the public typed ABI and must remain rejected.
func TestInvalidCases(t *testing.T){
 nested:=StateHolder{V:__borrow_std.NewSome[m.State](nil)}
 for name,f:=range map[string]func(){"result-clone":func(){_=(ReplyOps{}).Copy(&Reply{})},"result-debug":func(){_=(ReplyOps{}).Describe(&Reply{})},"nested-clone":func(){_=(StateHolderOps{}).Copy(&nested)},"nested-debug":func(){_=(StateHolderOps{}).Describe(&nested)}}{
  t.Run(name,func(t *testing.T){defer func(){if recover()==nil{t.Fatal("invalid value accepted")}}();f()})
 }
}
`)
	plain := t.TempDir()
	writeStdNativeConsumerManifest(t, plain, "sums.example/plain", out)
	packagesWrite(t, plain, "main_test.go", `package plain
import("testing";a "sums.example/app";m "sums.example/lib/model")
func TestGoHelpers(t *testing.T){v:=a.Holder{V:m.Some([]int{5})};c:=(a.HolderOps{}).Copy(&v);if (a.HolderOps{}).Describe(&c)!="Holder { V: Option::Some([5]) }"{t.Fatal(c)}}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-mod=mod", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	assertNativeGoRejected(t, out, tool, map[string]nativeGoRejection{
		"forged-option": {"package app\nimport __borrow_std \"github.com/goxide-lang/std/sum\"\ntype forged struct{__borrow_std.Option[[]int]}\nvar _ __borrow_std.Option[[]int]=forged{}\n", "cannot use"},
	})

}

func TestSumProtocolsRejectMissingPayload(t *testing.T) {
	for name, tc := range map[string]struct {
		source, location string
		want             []string
	}{
		"result-default":  {"#[derive(std::derive::Default)]\ntype S struct{V Result[int,string]}", "2:1:", []string{"missing trait implementation:", "Default", "Result[int,string]"}},
		"option-clone":    {"#[derive(std::derive::Clone)]\ntype S struct{V Option[chan int]}", "2:1:", []string{"missing trait implementation:", "Clone"}},
		"result-inactive": {"#[derive(std::derive::Debug)]\ntype S struct{V Result[int,chan int]}", "2:1:", []string{"missing trait implementation:", "Debug"}},
		"new-defined":     {"type Fresh Option[int]\n#[derive(std::derive::Clone)]\ntype S struct{V Fresh}", "3:1:", []string{"missing trait implementation:", "Clone", "(fixture::Fresh)"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testdeps.FormalSource(t, name+".gox", []byte("package fixture; use std; use std::PartialEq;\n"+tc.source))
			if err == nil || !strings.Contains(err.Error(), name+".gox:"+tc.location) {
				t.Fatalf("want protocol rejection at %s, got %v", tc.location, err)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("want diagnostic containing %q, got %v", want, err)
				}
			}
		})
	}
	// None Default must not require any payload protocol.
	if _, err := testdeps.FormalSource(t, "default.gox", []byte("package fixture; use std; use std::PartialEq;\n#[derive(std::derive::Default)]\ntype S struct{V Option[chan int]}\nimpl S {\n pub func initial() Self { return std::default_value[Self]() }\n}\n")); err != nil {
		t.Fatal(err)
	}
}
