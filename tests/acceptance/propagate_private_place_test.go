package acceptance_test

import (
	"path/filepath"
	"strings"
	"testing"
)

const privatePlaceAssertions = `package main
import("testing";"errors";"strconv";"places.example/update/model")
func catchPanic(f func())(v any){defer func(){v=recover()}();f();return}
func TestPromotedReadOperands(t *testing.T){tree:=model.NewTree(0);ok:=parse("7");bad:=parse("bad");if n,e:=unpack(readIndex(&tree,ok));n!=7||e!=nil{t.Fatal(n,e)};if _,e:=unpack(readIndex(&tree,bad));e==nil{t.Fatal("index failure lost")};m:=map[int]int{};if _,e:=unpack(readKey(&tree,m,ok));e!=nil||m[0]!=7{t.Fatal(e,m)};a:=[1]int{};if _,e:=unpack(readArgument(&tree,&a,ok));e!=nil||a[0]!=7{t.Fatal(e,a)};if _,e:=unpack(readKey(&tree,m,bad));e==nil||m[0]!=7{t.Fatal(e,m)};if _,e:=unpack(readArgument(&tree,&a,bad));e==nil||a[0]!=7{t.Fatal(e,a)}}
func TestPrivateStock(t *testing.T){tree:=model.NewTree(3);if n,e:=unpack(stock(&tree,"7"));n!=7||e!=nil{t.Fatal(n,e)};_,e:=unpack(stock(&tree,"bad"));var cause *strconv.NumError;if !errors.As(e,&cause)||tree.Count!=7{t.Fatal(e,tree.Count)};if n,e:=unpack(stockFields(&tree,63,"7"));n!=7||e!=nil||uint64(tree.Wide)!=uint64(1)<<63||!tree.Enabled{t.Fatal(n,e,tree)};tree.Wide=1;if n,e:=unpack(compoundPrivate(&tree));n!=4||e!=nil{t.Fatal(n,e)}}
func TestPrivateRebinding(t *testing.T){tree:=model.NewTree(3);old:=model.Observe(tree);if _,e:=unpack(replaceDuringRHS(&tree));e!=nil{t.Fatal(e)};if !((old()==7&&tree.Count==99)||(old()==3&&tree.Count==7)){t.Fatal("result outside Go allowed read order",old(),tree.Count)};tree=model.NewTree(3);old=model.Observe(tree);_,e:=unpack(replaceDuringWrite(&tree));if e!=nil||old()!=7||tree.Count!=99{t.Fatal("two write phases",e,old(),tree.Count)};tree=model.NewTree(3);root:=&tree;old=model.Observe(tree);_,e=unpack(replaceRootRHS(&root));if e!=nil||!((old()==7&&root.Count==99)||(old()==3&&root.Count==7)){t.Fatal("root read order",e,old(),root.Count)}}
func TestPrivateNilFinalAndIntermediate(t *testing.T){tree:=model.NilLeaf();first:=0;if v:=catchPanic(func(){_=writeThenNil(&first,&tree,"7")});v==nil||first!=5{t.Fatal("final nil must follow first write",v,first)};tree=model.NilBranch();first=0;if v:=catchPanic(func(){_=writeThenNil(&first,&tree,"7")});v==nil||first!=0{t.Fatal("intermediate nil must precede all writes",v,first)};first=0;if v:=catchPanic(func(){_=writeThenNil(&first,nil,"7")});v==nil||first!=0{t.Fatal("root nil must precede all writes",v,first)}}
func TestPrivateFailureNoWrites(t *testing.T){for _,tree:=range []model.Tree{model.NewTree(3),model.NilLeaf()}{first:=0;_,e:=unpack(writeThenNil(&first,&tree,"bad"));if e==nil||first!=0{t.Fatal(e,first)}};tree:=model.NewTree(3);if optionNumber(optionPrivate(&tree,false))!=-1||tree.Count!=3{t.Fatal("None wrote")};if optionNumber(optionPrivate(&tree,true))!=8||tree.Count!=8{t.Fatal("Some failed")}}
func TestPrivateDirectRecover(t *testing.T){for _,text:=range []string{"2","bad"}{tree:=model.NewTree(3);v:=catchPanic(func(){defer deferredRecover(&tree,text);panic("original")});if text=="2"{if v!=nil||tree.Count!=42{t.Fatal(v,tree.Count)}}else if v!="original"||tree.Count!=3{t.Fatal("failed question must skip recover and preserve outer panic",v,tree.Count)}}}
func TestPrivateNamedAndBlankReturns(t *testing.T){tree:=model.NewTree(3);if n,e:=unpack(namedFailure(&tree,"bad"));n!=77||e!=nil||tree.Count!=3{t.Fatal(n,e,tree.Count)};if _,e:=unpack(blankFailure(&tree));e==nil||tree.Count!=3{t.Fatal(e,tree.Count)}}
func TestPrivateUserPanicAndGoexit(t *testing.T){for _,kind:=range []int{0,1}{tree:=model.NewTree(3);trace:=[]string{};if n,e:=unpack(deferredBehavior(&tree,kind,&trace));n!=77||e!=nil||tree.Count!=3||len(trace)!=1||trace[0]!="defer"{t.Fatal(n,e,tree.Count,trace)}};done:=make(chan struct{});trace:=[]string{};tree:=model.NewTree(3);returned:=false;go func(){defer close(done);_=deferredBehavior(&tree,2,&trace);returned=true}();<-done;if returned||tree.Count!=3||len(trace)!=1||trace[0]!="defer"{t.Fatal(returned,tree.Count,trace)}}
func TestPrivateScopeAndGoto(t *testing.T){tree:=model.NewTree(3);if n,e:=unpack(blockClosure(&tree));n!=3||e!=nil{t.Fatal(n,e)};if _,e:=unpack(shadowResult(&tree));e==nil||tree.Count!=3{t.Fatal(e,tree.Count)};if n,e:=unpack(privateGoto(&tree,true));n!=3||e!=nil{t.Fatal(n,e)};if n,e:=unpack(privateGoto(&tree,false));n!=7||e!=nil{t.Fatal(n,e)}}
func TestPrivateGuardBoundaries(t *testing.T){tree:=model.NewTree(3);var seen any="unset";if _,e:=unpack(repeatedGuards(&tree,&seen));e==nil||seen!=nil||tree.Count!=1{t.Fatal(e,seen,tree.Count)};if n,e:=unpack(nestedPrivate(&tree));n!=9||e!=nil{t.Fatal(n,e)};if n,e:=unpack(privateAliases());n!=9||e!=nil{t.Fatal(n,e)};if v:=catchPanic(func(){_=invalidPrivate(&tree)});v==nil{t.Fatal("invalid sum became business failure")}}
func TestPrivateQuestionsInLHS(t *testing.T){tree:=model.NewTree(3);if n,e:=unpack(privateLHS(&tree,true));n!=8||e!=nil||tree.Values[0]!=8{t.Fatal(n,e,tree.Values)};if _,e:=unpack(privateLHS(nil,false));e==nil{t.Fatal("nil root should not be used after failed LHS question")}}
`

func privateAssignmentNegatives(t *testing.T, cli, outside, root string) {
	t.Helper()
	for name, body := range map[string]string{
		"readonly-field":    `func f(r &dep.Tree)Option[int]{r.X=Option[int]::Some(1)?;return Option[int]::None}`,
		"readonly-array":    `func f(r &dep.Tree)Option[int]{r.A[0]=Option[int]::Some(1)?;return Option[int]::None}`,
		"outer-bare-return": `func f(r *dep.Tree)Option[int]{if true{r.X=Option[int]::Some(1)?};return}`,
		"bare-return":       `func f(r *dep.Tree)Option[int]{r.X=Option[int]::Some(1)?;return}`,
		"type-mismatch":     `func f(r *dep.Tree)Option[int]{r.X=Option[string]::Some("a")?;return Option[int]::None}`,
		"not-addressable":   `func f(r *dep.Tree)Option[int]{r.Value().N=Option[int]::Some(1)?;return Option[int]::None}`,
	} {
		t.Run(name, func(t *testing.T) {
			src := t.TempDir()
			packagesWrite(t, src, "go.mod", "module private.example/fixture\n\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => "+root+"\n")
			packagesWrite(t, src, "dep/dep.go", `package dep;type inner struct{X int;A [2]int};type Tree struct{*inner};type V struct{N int};func(*Tree)Value()V{return V{}}`)
			packagesWrite(t, src, "main.hgo", `package fixture;import "private.example/fixture/dep";`+body)
			got := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "out"), src)
			if !strings.Contains(got, "main.hgo") {
				t.Fatalf("located rejection: %s", got)
			}
			if name == "readonly-field" || name == "readonly-array" {
				if !strings.Contains(got, "read-only") && !strings.Contains(got, "readonly") {
					t.Fatalf("permission diagnostic: %s", got)
				}
			}
		})
	}
}
