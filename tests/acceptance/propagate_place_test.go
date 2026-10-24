package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPropagationAssignmentPlacesRace(t *testing.T) {
	enumGeneratedModule(t, placePropagationSource, placePropagationAssertions, false)
}

const placePropagationSource = `package fixture
use errors;
var failure=errors::New("failed")
func num(n int,ok bool,trace *[]string)Result[int,error]{*trace=append(*trace,"rhs");if !ok{return Result[int,error]::Err(failure)};return Result[int,error]::Ok(n)}
func value(v Result[int,error])(int,error){match v{Result[int,error]::Ok(n)=>{return n,nil},Result[int,error]::Err(e)=>{return 0,e},};panic("unreachable")}
type Cell struct{X int;A [2]int;P *Cell;S []int;M map[int]int}
type Embed struct{*Cell}
func basic(p *int,s []int,a *[2]int,m map[int]int,c *Cell,ok bool,trace *[]string)Result[int,error]{*p,s[0],a[0],m[0],c.X=1,2,3,4,num(5,ok,trace)?;return Result[int,error]::Ok(1)}
func locations(p **int,s *[]int,m *map[int]int,a *[2]int,c **Cell,trace *[]string)Result[int,error]{*(*p),(*s)[0],(*m)[0],(*a)[0],(*c).X=10,20,30,40,change(p,s,m,a,c,trace)?;return Result[int,error]::Ok(1)}
func change(p **int,s *[]int,m *map[int]int,a *[2]int,c **Cell,trace *[]string)Result[int,error]{x:=99;*p=&x;*s=[]int{99};*m=map[int]int{0:99};*a=[2]int{99,99};*c=&Cell{X:99};return num(50,true,trace)}
func indexResult(n int,ok bool,trace *[]string)Result[int,error]{*trace=append(*trace,"index");if !ok{return Result[int,error]::Err(failure)};return Result[int,error]::Ok(n)}
func lhsQuestion(s []int,a *[2]int,m map[int]int,ok bool,trace *[]string)Result[int,error]{s[indexResult(0,true,trace)?],a[indexResult(0,ok,trace)?],m[indexResult(0,true,trace)?]=num(1,true,trace)?,2,3;return Result[int,error]::Ok(1)}
func pointerResult(p *Cell,ok bool,trace *[]string)Result[*Cell,error]{*trace=append(*trace,"pointer");if !ok{return Result[*Cell,error]::Err(failure)};return Result[*Cell,error]::Ok(p)}
func pointerQuestion(p *Cell,ok bool,trace *[]string)Result[int,error]{pointerResult(p,ok,trace)?.X=num(9,true,trace)?;return Result[int,error]::Ok(1)}
func aliases(s []int,trace *[]string)Result[int,error]{s[0],s[0]=1,num(2,true,trace)?;return Result[int,error]::Ok(s[0])}
func compound(p *int,trace *[]string)Result[int,error]{*p+=mutate(p,trace)?;return Result[int,error]::Ok(*p)}
func mutate(p *int,trace *[]string)Result[int,error]{*p=10;return num(2,true,trace)}
func nested(x *int,p *Cell,s []Cell,kind int,ok bool,trace *[]string)Result[int,error]{
 if kind==0{*x,p.A[0]=5,num(7,ok,trace)?}
 if kind==1{*x,s[3].A[0]=5,num(7,ok,trace)?}
 if kind==2{*x,p.P.X=5,num(7,ok,trace)?}
 if kind==3{*x,p.P.A[0]=5,num(7,ok,trace)?}
 if kind==4{*x,p.S[0]=5,num(7,ok,trace)?}
 if kind==5{*x,p.M[0]=5,num(7,ok,trace)?}
 return Result[int,error]::Ok(1)
}
func promoted(e *Embed,replacement *Cell,ok bool,trace *[]string)Result[int,error]{e.X=replace(e,replacement,ok,trace)?;return Result[int,error]::Ok(1)}
func replace(e *Embed,p *Cell,ok bool,trace *[]string)Result[int,error]{e.Cell=p;return num(7,ok,trace)}
func nilMap(x *int,m map[int]int,ok bool,trace *[]string)Result[int,error]{*x,m[0]=5,num(7,ok,trace)?;return Result[int,error]::Ok(1)}
func badKey(x *int,m map[any]int,ok bool,trace *[]string)Result[int,error]{*x,m[[]int{1}]=5,num(7,ok,trace)?;return Result[int,error]::Ok(1)}
func tuple() (int,int){return 4,5}
func tupleAssignment(s []int,trace *[]string)Result[int,error]{s[indexResult(0,true,trace)?],s[1]=tuple();return Result[int,error]::Ok(1)}
func checkedLookup(s []int,m map[int]int,ok bool,trace *[]string)Result[int,error]{var found bool;s[0],found=m[indexResult(1,ok,trace)?];if !found{return Result[int,error]::Ok(-1)};return Result[int,error]::Ok(s[0])}
func arrayStorage(a *[2]Cell,trace *[]string)Result[int,error]{a[0].A[0],a[0].A[1]=num(8,true,trace)?,9;return Result[int,error]::Ok(1)}
func index(trace *[]string)int{*trace=append(*trace,"index");return 0}
func ifDestination(s []int,ok bool,trace *[]string)Result[int,error]{if s[index(trace)]=num(7,ok,trace)?;s[0]>0{return Result[int,error]::Ok(s[0])};return Result[int,error]::Ok(0)}
func swapSlice(s *[]int)Result[[]int,error]{*s=[]int{99};return Result[[]int,error]::Ok(*s)}
func laterLHS(s *[]int)Result[int,error]{(*s)[0],swapSlice(s)?[0]=7,8;return Result[int,error]::Ok(1)}
func sharedAlias(trace *[]string)Result[int,error]{a:=[2]int{};var r &[2]int=&a;var w &mut [2]int=&mut a;w[0]=num(8,true,trace)?;return Result[int,error]::Ok(r[0])}
func readonlyRead(r &[2]int,trace *[]string)Result[int,error]{x:=r[0];return num(x,true,trace)}
func writable(r &mut [2]int,trace *[]string)Result[int,error]{r[0]=num(8,true,trace)?;return Result[int,error]::Ok(r[0])}
func checkWritable(trace *[]string)Result[int,error]{a:=[2]int{};var r &mut [2]int=&mut a;writable(r,trace)?;return Result[int,error]::Ok(a[0])}
`

const placePropagationAssertions = `package fixture
import("testing";"reflect";"errors")
func panicRun(t *testing.T,f func()){t.Helper();defer func(){if recover()==nil{t.Fatal("missing panic")}}();f()}
func TestBasicAtomicFailure(t *testing.T){for _,ok:=range []bool{false,true}{p:=0;s:=[]int{0};a:=[2]int{};m:=map[int]int{};c:=&Cell{};trace:=[]string{};_,e:=value(basic(&p,s,&a,m,c,ok,&trace));if ok{if e!=nil||p!=1||s[0]!=2||a[0]!=3||m[0]!=4||c.X!=5{t.Fatal(p,s,a,m,c,e)}}else if !errors.Is(e,failure)||p!=0||s[0]!=0||a[0]!=0||len(m)!=0||c.X!=0{t.Fatal(p,s,a,m,c,e)}}}
func TestSavedContainersAndStorage(t *testing.T){v:=0;p:=&v;s:=[]int{0};oldS:=s;m:=map[int]int{};oldM:=m;a:=[2]int{};c:=&Cell{};oldC:=c;trace:=[]string{};_,e:=value(locations(&p,&s,&m,&a,&c,&trace));if e!=nil||v!=10||oldS[0]!=20||oldM[0]!=30||a[0]!=40||a[1]!=99||oldC.X!=50||*p!=99||s[0]!=99||m[0]!=99||c.X!=99{t.Fatal(v,oldS,oldM,a,oldC,p,s,m,c,e)}}
func TestQuestionsInDestinations(t *testing.T){for _,ok:=range []bool{false,true}{s:=[]int{0};a:=[2]int{};m:=map[int]int{};trace:=[]string{};_,e:=value(lhsQuestion(s,&a,m,ok,&trace));want:=[]string{"index","index"};if ok{want=append(want,"index","rhs");if e!=nil||s[0]!=1||a[0]!=2||m[0]!=3{t.Fatal(e,s,a,m)}}else if !errors.Is(e,failure)||s[0]!=0||a[0]!=0||len(m)!=0{t.Fatal(e,s,a,m)};if !reflect.DeepEqual(trace,want){t.Fatal(trace,want)}};trace:=[]string{};c:=&Cell{};_,e:=value(pointerQuestion(c,true,&trace));if e!=nil||c.X!=9{t.Fatal(c,e)};trace=nil;_,e=value(pointerQuestion(nil,false,&trace));if !errors.Is(e,failure)||!reflect.DeepEqual(trace,[]string{"pointer"}){t.Fatal(e,trace)}}
func TestAliasesCompoundAndTuples(t *testing.T){trace:=[]string{};s:=[]int{0,0};if n,e:=value(aliases(s,&trace));n!=2||e!=nil{t.Fatal(n,e)};p:=0;if n,e:=value(compound(&p,&trace));n!=12||e!=nil{t.Fatal(n,e)};_,_=value(tupleAssignment(s,&trace));if !reflect.DeepEqual(s,[]int{4,5}){t.Fatal(s)};if n,e:=value(checkedLookup(s,map[int]int{1:6},true,&trace));n!=6||e!=nil{t.Fatal(n,e)};a:=[2]Cell{};_,_=value(arrayStorage(&a,&trace));if a[0].A!=[2]int{8,9}{t.Fatal(a)};if n,e:=value(checkWritable(&trace));n!=8||e!=nil{t.Fatal(n,e)}}
func TestPanicPhases(t *testing.T){for _,kind:=range []int{0,1,2,3,4,5}{for _,ok:=range []bool{false,true}{x:=0;trace:=[]string{};var p *Cell;operandPanic:=kind>=2;call:=func(){_,e:=value(nested(&x,p,nil,kind,ok,&trace));if !errors.Is(e,failure){t.Fatal(e)}};if operandPanic||ok{panicRun(t,call)}else{call()};wantX:=0;if ok&&!operandPanic{wantX=5};if x!=wantX{t.Fatal(kind,ok,x,wantX)};wantLen:=1;if operandPanic{wantLen=0};if len(trace)!=wantLen{t.Fatal(kind,ok,trace)}}};for _,kind:=range []int{2,3,4,5}{x:=0;trace:=[]string{};panicRun(t,func(){_=nested(&x,&Cell{},nil,kind,true,&trace)});if x!=5||len(trace)!=1{t.Fatal(kind,x,trace)}}}
func TestIfInitializerDestination(t *testing.T){for _,ok:=range []bool{false,true}{s:=[]int{0};trace:=[]string{};n,e:=value(ifDestination(s,ok,&trace));if !reflect.DeepEqual(trace,[]string{"index","rhs"}){t.Fatal(trace)};if ok{if n!=7||e!=nil||s[0]!=7{t.Fatal(n,e,s)}}else if !errors.Is(e,failure)||s[0]!=0{t.Fatal(e,s)}}}
func TestLaterLHSSideEffectAndReadonlyAlias(t *testing.T){s:=[]int{0};old:=s;_,e:=value(laterLHS(&s));if e!=nil||old[0]!=7||s[0]!=8{t.Fatal(e,old,s)};trace:=[]string{};if n,e:=value(sharedAlias(&trace));n!=8||e!=nil{t.Fatal(n,e)}}
func TestPromotedPointerCapture(t *testing.T){old:=&Cell{};next:=&Cell{X:99};e:=&Embed{old};trace:=[]string{};_,err:=value(promoted(e,next,true,&trace));if err!=nil||old.X!=7||e.Cell!=next||next.X!=99{t.Fatal(e,old,next,err)};for _,ok:=range []bool{false,true}{e=&Embed{};trace=nil;call:=func(){_,err=value(promoted(e,next,ok,&trace));if !errors.Is(err,failure){t.Fatal(err)}};if ok{panicRun(t,call)}else{call()};if len(trace)!=1||e.Cell!=next{t.Fatal(trace,e)}}}
func TestMapWritePanics(t *testing.T){for _,bad:=range []bool{false,true}{for _,ok:=range []bool{false,true}{x:=0;trace:=[]string{};call:=func(){var e error;if bad{_,e=value(badKey(&x,map[any]int{},ok,&trace))}else{_,e=value(nilMap(&x,nil,ok,&trace))};if !errors.Is(e,failure){t.Fatal(e)}};if ok{panicRun(t,call)}else{call()};want:=0;if ok{want=5};if x!=want||len(trace)!=1{t.Fatal(bad,ok,x,trace)}}}}
`

func TestPropagationAssignmentPlaceRejections(t *testing.T) {
	for name, source := range map[string]string{
		"returned-array":   `func f()Option[int]{Option[[1]int]::Some([1]int{})?[0]=1;return Option[int]::Some(0)}`,
		"returned-struct":  `type S struct{X int};func f()Option[int]{Option[S]::Some(S{})?.X=1;return Option[int]::Some(0)}`,
		"string":           `func f()Option[int]{s:="a";s[0]=Option[byte]::Some(1)?;return Option[int]::Some(0)}`,
		"short":            `func f(p *int)Option[int]{*p:=Option[int]::Some(1)?;return Option[int]::Some(0)}`,
		"readonly-array":   `func f(r &[2]int)Option[int]{r[0]=Option[int]::Some(1)?;return Option[int]::Some(0)}`,
		"readonly-slice":   `type S struct{Items []int};func f(r &S)Option[int]{r.Items[0]=Option[int]::Some(1)?;return Option[int]::Some(0)}`,
		"readonly-map":     `type S struct{Items map[int]int};func f(r &S)Option[int]{r.Items[0]=Option[int]::Some(1)?;return Option[int]::Some(0)}`,
		"readonly-field":   `type S struct{X int};func f(r &S)Option[int]{r.X=Option[int]::Some(1)?;return Option[int]::Some(0)}`,
		"readonly-pointer": `func f(r &int)Option[int]{*r=Option[int]::Some(1)?;return Option[int]::Some(0)}`,
		"constant-bound":   `func f(a *[2]int)Option[int]{a[3]=Option[int]::Some(1)?;return Option[int]::Some(0)}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceLanguageFixture("place_bad.gox", []byte("package fixture\n"+source))
			if err == nil || !strings.Contains(err.Error(), "place_bad.gox") {
				t.Fatalf("expected located rejection: %v", err)
			}
		})
	}
}

func TestPropagationAssignmentInstalledBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin := t.TempDir()
	cli := filepath.Join(bin, "goxide")
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "run", "./scripts/build.go", "-o", cli)
	outside := t.TempDir()
	source := t.TempDir()
	if err := os.CopyFS(source, os.DirFS(filepath.Join(root, "examples/propagate_places"))); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.gox", "private.gox"} {
		path := filepath.Join(source, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.ReplaceAll(string(data), `};panic("unreachable")}`, `}}`))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	fallbackSource := filepath.Join(source, "effects_fallback.gox")
	fallbackInput, err := os.ReadFile(fallbackSource)
	if err != nil {
		t.Fatal(err)
	}
	fallbackInput = []byte(strings.ReplaceAll(strings.ReplaceAll(string(fallbackInput), "use unsafe;", "use r#unsafe;"), "unsafe::", "r#unsafe::"))
	if err := os.WriteFile(fallbackSource, fallbackInput, 0600); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, source, "macro_propagation.gox", macroPropagationSource)
	packagesWrite(t, source, "model/macro.go", macroPropagationModel)
	generated := filepath.Join(t.TempDir(), "module")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source)
	repeated := filepath.Join(t.TempDir(), "repeat")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", repeated, source)
	if err := filepath.WalkDir(generated, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(generated, path)
		if err != nil {
			return err
		}
		first, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		second, err := os.ReadFile(filepath.Join(repeated, rel))
		if err != nil {
			return err
		}
		if string(first) != string(second) {
			t.Errorf("repeated propagation output differs: %s", rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, generated, "macro_propagation_test.go", macroPropagationAssertions)
	packagesWrite(t, generated, "business_test.go", `package main
import("testing";"errors";"strconv")
func TestUpdate(t *testing.T){s:=[]int{0};m:=map[int]int{};n,e:=unpack(update(s,m,"0","42"));if e!=nil||n!=42||s[0]!=42||m[0]!=9{t.Fatal(n,e,s,m)};_,e=unpack(update(s,m,"0","bad"));var cause *strconv.NumError;if !errors.As(e,&cause)||s[0]!=42||m[0]!=9{t.Fatal(e,s,m)};_,e=unpack(update(s,m,"bad","42"));if !errors.As(e,&cause){t.Fatal(e)}}
func TestPrivateTypes(t *testing.T){if n,e:=unpack(hidden(63));n!=1||e!=nil{t.Fatal(n,e)};if n,e:=unpack(distinctTypes());n!=1||e!=nil{t.Fatal(n,e)}}
`)
	indexGo, err := os.ReadFile(filepath.Join(generated, "index_reads.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(strings.Fields(string(indexGo)), ""), ".Catch(") || strings.Contains(strings.Join(strings.Fields(string(indexGo)), ""), ".Assignment[") {
		t.Fatal("rvalue index/key/call-argument unexpectedly uses assignment guard")
	}

	packagesWrite(t, generated, "private_test.go", privatePlaceAssertions)

	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	deliveryCommand(t, generated, tool, true, nil, "vet", "./...")
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--go", tool, source); got != "42 9 <nil>\ninvalid value 42 9\nstock 7 <nil>\ninvalid stock 7\n" {
		t.Fatalf("business: %q", got)
	}
	deliveryCommand(t, generated, tool, true, []string{"GODEBUG=panicnil=1"}, "test", "-race", "-count=1", "-v", "-run", "TestPrivateUserPanicAndGoexit", ".")
	privateAssignmentNegatives(t, cli, outside)

}

func TestPropagationUnsafeOperandBoundary(t *testing.T) {
	for _, expr := range []string{
		`r#unsafe::Sizeof([1]int{Option[int]::Some(1)?})`,
		`r#unsafe::Alignof([1]int{Option[int]::Some(1)?})`,
		`r#unsafe::Offsetof(Option[S]::Some(S{})?.X)`,
	} {
		_, err := compileAcceptanceLanguageFixture("unsafe_bad.gox", []byte(`package fixture;use r#unsafe;type S struct{X int};func f()Option[int]{n:=`+expr+`;return Option[int]::Some(int(n))}`))
		if err == nil || !strings.Contains(err.Error(), "unsafe compile-time operands") {
			t.Fatalf("%s: %v", expr, err)
		}
	}
	enumGeneratedModule(t, `package fixture
use r#unsafe;
func Sizeof(n int)int{return n}
func allowed(p *int)Option[int]{q:=r#unsafe::Add(r#unsafe::Pointer(p),Option[uintptr]::Some(0)?);s:=r#unsafe::Slice((*int)(q),Option[int]::Some(1)?);n:=Sizeof(Option[int]::Some(s[0])?);return Option[int]::Some(n)}
func result(v Option[int])int{match v{Option[int]::Some(n)=>{return n},Option[int]::None=>{return -1},};panic("unreachable")}
`, `package fixture
import "testing"
func TestEvaluatedUnsafeAndOrdinaryName(t *testing.T){n:=7;if result(allowed(&n))!=7{t.Fatal("evaluated operands")}}
`, false)
}

const macroPropagationSource = `package main
use model;
var trace string
macro_rules! payload {($x:expr)=>{$x?};}
macro_rules! length {($x:expr)=>{len($x?)};}
func number(mark string,ok bool,n int)Option[int]{trace+=mark;if !ok{return Option[int]::None};return Option[int]::Some(n)}
func list(mark string,ok bool)Option[[]int]{trace+=mark;if !ok{return Option[[]int]::None};return Option[[]int]::Some([]int{1,2})}
func Expr(ok bool)Option[int]{n:=payload!(number("A",ok,3));m:=length!(list("B",true));return Option[int]::Some(n+m)}
func Nearest()Option[int]{f:=func()Option[int]{_=payload!(number("N",false,0));trace+="bad";return Option[int]::Some(1)};_=f();trace+="O";return Option[int]::Some(9)}
func nestedValue()Option[Option[int]]{return Option[Option[int]]::Some(number("N",true,4))}
func Nested()Option[int]{x:=payload!(nestedValue())?;return Option[int]::Some(x)}
func Effect(t *model::MacroTree,ok bool)Option[int]{t.Count=payload!(number("E",ok,3))+payload!(number("F",true,4));return Option[int]::Some(t.Count)}
func Frame(t *model::MacroTree,ok bool,d int)Option[int]{defer func(){trace+="D"}();calc:=func(n int)int{return 12/n};t.Count=calc(payload!(number("G",ok,d)));trace+="Z";return Option[int]::Some(t.Count)}
func Short(t *model::MacroTree,on bool)Option[int]{t.Enabled=on&&payload!(number("S",false,0))>0;return Option[int]::Some(1)}
func Unwrap(x Option[int])(int,bool){match x{Option[int]::Some(n)=>{return n,true},Option[int]::None=>{return 0,false},}}
`

const macroPropagationModel = `package model
type macroInner struct{Count int;Enabled bool}
type MacroTree struct{*macroInner}
func NewMacroTree(n int)MacroTree{return MacroTree{&macroInner{Count:n}}}
`

const macroPropagationAssertions = `package main
import ("testing";"places.example/update/model";__borrow_std "github.com/goxide-lang/std/sum")
func TestMacroPropagationBehavior(t *testing.T){
 for _,tc:=range []struct{call func()__borrow_std.Option[int];value int;ok bool;trace string}{
 {func()__borrow_std.Option[int]{return Expr(true)},5,true,"AB"},
 {func()__borrow_std.Option[int]{return Expr(false)},0,false,"A"},
 {Nearest,9,true,"NO"},{Nested,4,true,"N"},
 }{trace="";n,ok:=Unwrap(tc.call());if n!=tc.value||ok!=tc.ok||trace!=tc.trace{t.Fatalf("got %d %v %q; want %d %v %q",n,ok,trace,tc.value,tc.ok,tc.trace)}}
}
func TestMacroPropagationPrivateRoutes(t *testing.T){
 tree:=model.NewMacroTree(9)
 for _,tc:=range []struct{call func()__borrow_std.Option[int];value int;ok bool;trace string;stored int}{
 {func()__borrow_std.Option[int]{return Effect(&tree,true)},7,true,"EF",7},
 {func()__borrow_std.Option[int]{return Effect(&tree,false)},0,false,"E",7},
 {func()__borrow_std.Option[int]{return Frame(&tree,true,3)},4,true,"GZD",4},
 {func()__borrow_std.Option[int]{return Frame(&tree,false,0)},0,false,"GD",4},
 {func()__borrow_std.Option[int]{return Short(&tree,false)},1,true,"",4},
 {func()__borrow_std.Option[int]{return Short(&tree,true)},0,false,"S",4},
 }{trace="";n,ok:=Unwrap(tc.call());if n!=tc.value||ok!=tc.ok||trace!=tc.trace||tree.Count!=tc.stored{t.Fatalf("got %d %v %q stored=%d",n,ok,trace,tree.Count)}}
 trace="";func(){defer func(){if recover()==nil{t.Error("native division panic was swallowed")}}();_=Frame(&tree,true,0)}()
 if trace!="GD"||tree.Count!=4{t.Fatalf("panic mutated destination or defer: %s %d",trace,tree.Count)}
}
`
