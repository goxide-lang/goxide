package acceptance_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
)

func TestSumCloneDebugLocalRace(t *testing.T) {
	enumGeneratedModule(t, `package fixture
#[derive(Clone,Debug)]
type Item struct{Values []int}
type Unused=[]int
#[derive(Clone,Debug)]
enum Payload{A,B([]int),}
#[derive(Clone,Debug)]
type Branches struct{V Option[Payload];R Result[int,Payload]}
func Branch()Branches{return Branches{R:Result[int,Payload]::Ok(3)}}
type Local=Option[[]Item]
#[derive(Default,Clone,Debug)]
type Holder struct{V Local}
#[derive(Clone,Debug)]
type Reply struct{V Result[[]Item,map[string][]int];Nested Option[Result[[]int,string]]}
func Some(v []Item)Local{return Option[[]Item]::Some(v)}
func Ok(v []Item)Reply{return Reply{V:Result[[]Item,map[string][]int]::Ok(v)}}
func Err(v map[string][]int)Reply{return Reply{V:Result[[]Item,map[string][]int]::Err(v)}}
func Values(v Local)[]Item{return match v{Option[[]Item]::Some(x)=>x,Option[[]Item]::None=>nil,}}
func Data(v Reply)[]Item{return match v.V{Result[[]Item,map[string][]int]::Ok(x)=>x,Result[[]Item,map[string][]int]::Err(_)=>nil,}}
func ErrorData(v Reply)map[string][]int{return match v.V{Result[[]Item,map[string][]int]::Ok(_)=>nil,Result[[]Item,map[string][]int]::Err(x)=>x,}}
func Nested()Reply{return Reply{V:Result[[]Item,map[string][]int]::Ok(nil),Nested:Option[Result[[]int,string]]::Some(Result[[]int,string]::Err("quoted\n"))}}
`, `package fixture
import("testing";"strings")
func TestStatesAndDeepClone(t *testing.T){
 branch:=Branch();bc:=CloneBranches(&branch);if DebugBranches(&bc)!="Branches { V: Option::None, R: Result::Ok(3) }"{t.Fatal(DebugBranches(&bc))}

 n:=DefaultHolder();if n.V!=nil||CloneHolder(&n).V!=nil||DebugHolder(&n)!="Holder { V: Option::None }"{t.Fatal(n,DebugHolder(&n))}
 for _,tc:=range []struct{items []Item;display string}{{nil,"nil"},{[]Item{},"[]"}}{v:=Holder{V:Some(tc.items)};c:=CloneHolder(&v);if (Values(c.V)==nil)!=(tc.items==nil)||DebugHolder(&v)!="Holder { V: Option::Some("+tc.display+") }"{t.Fatal(v,c,DebugHolder(&v))}}
 v:=Holder{V:Some([]Item{{Values:[]int{1}}})};c:=CloneHolder(&v);Values(c.V)[0].Values[0]=9;if Values(v.V)[0].Values[0]!=1{t.Fatal("payload shared")}
 ok:=Ok([]Item{{Values:[]int{2}}});oc:=CloneReply(&ok);Data(oc)[0].Values[0]=8;if Data(ok)[0].Values[0]!=2||!strings.Contains(DebugReply(&ok),"Result::Ok([Item { Values: [2] }])"){t.Fatal(ok,DebugReply(&ok))}
 er:=Err(map[string][]int{"x":{3}});ec:=CloneReply(&er);ErrorData(ec)["x"][0]=7;if ErrorData(er)["x"][0]!=3||!strings.Contains(DebugReply(&er),"Result::Err({\"x\": [3]})"){t.Fatal(er,DebugReply(&er))}
 nested:=Nested();if DebugReply(&nested)!=DebugReply(ptrReply(CloneReply(&nested)))||!strings.Contains(DebugReply(&nested),"Option::Some(Result::Err(\"quoted\\n\"))"){t.Fatal(DebugReply(&nested))}
}
func ptrReply(v Reply)*Reply{return &v}
func TestInvalidResult(t *testing.T){for _,f:=range []func(){func(){_=CloneReply(&Reply{})},func(){_=DebugReply(&Reply{})}}{func(){defer func(){if recover()==nil{t.Fatal("invalid Result accepted")}}();f()}()}}
`, false, true)
}

func TestSumCloneDebugModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	mod := func(name, lib string) string {
		s := fmt.Sprintf("module %s\ngo 1.27\n", name)
		if lib != "" {
			s += fmt.Sprintf("require sums.example/lib v0.0.0\nreplace sums.example/lib => %s\n", filepath.ToSlash(lib))
		}
		return s
	}
	src := t.TempDir()
	packagesWrite(t, src, "go.mod", mod("sums.example/lib", ""))
	packagesWrite(t, src, "model/sums.gox", `package model
func Some(v []int)Option[[]int]{return Option[[]int]::Some(v)}
func Ok(v []int)Result[[]int,map[string][]int]{return Result[[]int,map[string][]int]::Ok(v)}
func Err(v map[string][]int)Result[[]int,map[string][]int]{return Result[[]int,map[string][]int]::Err(v)}
#[derive(Clone,Debug)]
type Item struct{Values []int}
func ItemOption(v Item)Option[Item]{return Option[Item]::Some(v)}
type secret struct{N int}
type Secret=Option[secret]
func Hidden()Secret{return Option[secret]::None}
`)
	packagesWrite(t, src, "relay/relay.go", `package relay
import m "sums.example/lib/model"
type Opt=m.Option[[]int]
type Res=m.Result[[]int,map[string][]int]
type Flip[A,B any]=m.Result[B,A]
`)
	lib := filepath.Join(t.TempDir(), "lib")
	packagesCommand(t, root, cli, true, "transpile", "-o", lib, src)
	appsrc := t.TempDir()
	packagesWrite(t, appsrc, "go.mod", mod("sums.example/app", lib))
	packagesWrite(t, appsrc, "aliases.go", `package app
import m "sums.example/lib/relay"
type Opt=m.Opt
type Res=m.Res
`)
	packagesWrite(t, appsrc, "app.gox", `package app
type Level=Opt
#[derive(Default,Clone,Debug)]
type Holder struct{V Level}
#[derive(Clone,Debug)]
type Reply struct{V Res}
`)
	packagesWrite(t, appsrc, "local.gox", `package app
type Unused=[]int
type Local=Option[[]int]
func LocalSome(v []int)Local{return Option[[]int]::Some(v)}
`)
	packagesWrite(t, appsrc, "peer.gox", `package app
#[derive(Default,Clone,Debug)]
type LocalHolder struct{V Local}
`)
	packagesWrite(t, appsrc, "secret.gox", `package app
import m "sums.example/lib/model"
import r "sums.example/lib/relay"
#[derive(Default)]
type SecretHolder struct{V m.Secret}
#[derive(Clone,Debug)]
type Reverse struct{V r.Flip[map[string][]int,[]int]}
#[derive(Clone,Debug)]
type ItemHolder struct{V m.Option[m.Item]}
`)
	out := filepath.Join(t.TempDir(), "app")
	packagesCommand(t, root, cli, true, "transpile", "-o", out, appsrc)
	repeat := filepath.Join(t.TempDir(), "repeat")
	packagesCommand(t, root, cli, true, "transpile", "-o", repeat, appsrc)
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
import("testing";"strings";m "sums.example/lib/model")
type forged struct{m.Option[[]int]}
func TestRegisteredStates(t *testing.T){
 item:=ItemHolder{V:m.ItemOption(m.Item{Values:[]int{1}})};ic:=CloneItemHolder(&item);ic.V.(m.GoxideOptionSome[m.Item]).Value.Values[0]=9;if item.V.(m.GoxideOptionSome[m.Item]).Value.Values[0]!=1||DebugItemHolder(&item)!="ItemHolder { V: Option::Some(Item { Values: [1] }) }"{t.Fatal("external payload protocol")}

 if DefaultSecretHolder().V!=nil||DefaultLocalHolder().V!=nil{t.Fatal("None default")}
 local:=LocalHolder{V:LocalSome([]int{4})};lc:=CloneLocalHolder(&local);lc.V.(GoxideOptionSome[[]int]).Value[0]=8;if local.V.(GoxideOptionSome[[]int]).Value[0]!=4||DebugLocalHolder(&local)!="LocalHolder { V: Option::Some([4]) }"{t.Fatal("peer local protocol")}
 reverse:=Reverse{V:m.Ok([]int{6})};rc:=CloneReverse(&reverse);rc.V.(m.GoxideResultOk[[]int,map[string][]int]).Value[0]=9;if reverse.V.(m.GoxideResultOk[[]int,map[string][]int]).Value[0]!=6||DebugReverse(&reverse)!="Reverse { V: Result::Ok([6]) }"{t.Fatal("alias argument order")}

 n:=DefaultHolder();if n.V!=nil||DebugHolder(&n)!="Holder { V: Option::None }"{t.Fatal(n)}
 for _,tc:=range []struct{v []int;text string}{{nil,"nil"},{[]int{},"[]"},{[]int{1},"[1]"}}{v:=Holder{V:m.Some(tc.v)};c:=CloneHolder(&v);data:=c.V.(m.GoxideOptionSome[[]int]).Value;if (data==nil)!=(tc.v==nil)||DebugHolder(&v)!="Holder { V: Option::Some("+tc.text+") }"{t.Fatal(v,c,DebugHolder(&v))};if len(data)>0{data[0]=9;if tc.v[0]!=1{t.Fatal("shared slice")}}}
 ok:=Reply{V:m.Ok([]int{2})};c:=CloneReply(&ok);c.V.(m.GoxideResultOk[[]int,map[string][]int]).Value[0]=9;if ok.V.(m.GoxideResultOk[[]int,map[string][]int]).Value[0]!=2||!strings.Contains(DebugReply(&ok),"Result::Ok([2])"){t.Fatal(ok)}
 for _,tc:=range []struct{v map[string][]int;text string}{{nil,"nil"},{map[string][]int{},"{}"}}{v:=Reply{V:m.Err(tc.v)};c:=CloneReply(&v);data:=c.V.(m.GoxideResultErr[[]int,map[string][]int]).Value;if (data==nil)!=(tc.v==nil)||DebugReply(&v)!="Reply { V: Result::Err("+tc.text+") }"{t.Fatal(v,c)}}
 er:=Reply{V:m.Err(map[string][]int{"x":{3}})};c=CloneReply(&er);c.V.(m.GoxideResultErr[[]int,map[string][]int]).Value["x"][0]=9;if er.V.(m.GoxideResultErr[[]int,map[string][]int]).Value["x"][0]!=3||!strings.Contains(DebugReply(&er),"Result::Err({\"x\": [3]})"){t.Fatal(er)}
}
func TestInvalidCases(t *testing.T){for _,v:=range []m.Option[[]int]{(*m.GoxideOptionSome[[]int])(nil),&m.GoxideOptionSome[[]int]{},forged{}}{x:=Holder{V:v};for _,f:=range []func(){func(){_=CloneHolder(&x)},func(){_=DebugHolder(&x)}}{func(){defer func(){if recover()==nil{t.Fatal("invalid case accepted")}}();f()}()}}}
`)
	plain := t.TempDir()
	packagesWrite(t, plain, "go.mod", mod("sums.example/plain", lib)+fmt.Sprintf("require sums.example/app v0.0.0\nreplace sums.example/app => %s\n", filepath.ToSlash(out)))
	packagesWrite(t, plain, "main_test.go", `package plain
import("testing";a "sums.example/app";m "sums.example/lib/model")
func TestGoHelpers(t *testing.T){v:=a.Holder{V:m.Some([]int{5})};c:=a.CloneHolder(&v);if a.DebugHolder(&c)!="Holder { V: Option::Some([5]) }"{t.Fatal(c)}}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	// The producer has no concrete validator for Option[string]. Same family is insufficient.
	bad := t.TempDir()
	packagesWrite(t, bad, "go.mod", mod("sums.example/bad", lib))
	packagesWrite(t, bad, "bad.gox", "package bad\nimport m \"sums.example/lib/model\"\n#[derive(Clone)]\ntype W struct{V m.Option[string]}\n")
	packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "bad"), bad)
	artifact := filepath.Join(lib, "model", ".goxide.enums.json")
	saved, e := os.ReadFile(artifact)
	if e != nil {
		t.Fatal(e)
	}
	changed := strings.Replace(string(saved), `"Family": "Option"`, `"Family": "Other"`, 1)
	if changed == string(saved) {
		t.Fatal("no tamper")
	}
	packagesWrite(t, lib, "model/.goxide.enums.json", changed)
	log := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "tampered"), appsrc)
	if !strings.Contains(log, "aliases.go:") || !strings.Contains(log, "metadata") {
		t.Fatal(log)
	}
}

func TestSumProtocolsRejectMissingPayload(t *testing.T) {
	for name, body := range map[string]string{
		"result-default":  "#[derive(Default)]\ntype S struct{V Result[int,string]}",
		"option-clone":    "#[derive(Clone)]\ntype S struct{V Option[chan int]}",
		"result-inactive": "#[derive(Debug)]\ntype S struct{V Result[int,chan int]}",
		"new-defined":     "type Fresh Option[int]\n#[derive(Clone)]\ntype S struct{V Fresh}",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := frontend.Compile(name+".gox", []byte("package fixture\n"+body))
			if err == nil {
				t.Fatal("unexpected protocol")
			}
		})
	}
	// None Default must not require any payload protocol.
	if _, err := frontend.Compile("default.gox", []byte("package fixture\n#[derive(Default)]\ntype S struct{V Option[chan int]}")); err != nil {
		t.Fatal(err)
	}
}
