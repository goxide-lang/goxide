package equality

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/enums"
)

func execute(t *testing.T, source string, cfg Config, assertions string) {
	t.Helper()
	files := map[string][]byte{"input.hgo": []byte(source)}
	prepared, e := Prepare(files, cfg)
	if e != nil {
		t.Fatal(e)
	}
	out, e := Lower(prepared, cfg)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	for name, data := range map[string]string{"go.mod": "module eqfixture\ngo 1.27\n", "main.go": string(out["input.hgo"]), "main_test.go": assertions} {
		if e := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "test", "-race", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("%v\n%s\n%s", e, b, out["input.hgo"])
	}
}
func TestDerivedEquality(t *testing.T) {
	execute(t, `package fixture
type Number int
type Row struct{N Number;Text string}
type Rows [2]Row
type Floating struct{N float64; C complex128}
var Init=Row{N:1}.equals(Row{N:1})
func Values()bool{return Rows{{N:1},{N:2}}.equals(Rows{{N:1},{N:2}})}
func Floats(n float64,c complex128)bool{return Floating{n,c}.equals(Floating{n,c})}
func Big()bool{var n uint64=1<<63;return n.equals(1<<63)}
type Native struct{}
func(Native)equals(v string)int{return len(v)}
func Host()int{return (Native{}).equals("abc")}
func Drop(){n:=1;n.equals(2)}
func Later(trace *[]int){f:=func(n int)int{*trace=append(*trace,n);return n};defer f(1).equals(f(2));*trace=append(*trace,3)}
`, Config{Derives: map[string]Derive{"Number": {PartialEq: true, Eq: true}, "Row": {PartialEq: true, Eq: true}, "Rows": {PartialEq: true, Eq: true}, "Floating": {PartialEq: true}}}, `package fixture
import("testing";"math";"reflect")
func TestValues(t *testing.T){if !Init||!Values()||!Big()||Host()!=3{t.Fatal("values")};if Floats(math.NaN(),0)||Floats(0,complex(math.NaN(),0)){t.Fatal("NaN must not be reflexive")};Drop();var trace []int;Later(&trace);if !reflect.DeepEqual(trace,[]int{1,2,3}){t.Fatal(trace)}}
`)
}
func TestEnumValidationOrder(t *testing.T) {
	d := enums.Decl{Name: "E", Validator: "check", Variants: []enums.Variant{{Name: "Unit", CaseName: "unit"}, {Name: "Item", CaseName: "item", Payload: "int"}}}
	execute(t, `package fixture
type E interface{marker()}
type unit struct{}
func(unit)marker(){}
type item struct{Value int}
func(item)marker(){}
var Trace []int
func check(e E){Trace=append(Trace,3);switch e.(type){case unit,item:default:panic("invalid")}}
func arg(n int)E{Trace=append(Trace,n);return item{1}}
func Run()bool{return arg(1).equals(arg(2))}
func Bad(a,b E)bool{return a.equals(b)}
`, Config{Derives: map[string]Derive{"E": {PartialEq: true, Eq: true}}, Enums: []enums.Decl{d}}, `package fixture
import("testing";"reflect")
func TestOrder(t *testing.T){if !Run()||!reflect.DeepEqual(Trace,[]int{1,2,3,3}){t.Fatal(Trace)};Trace=nil;defer func(){if recover()==nil||!reflect.DeepEqual(Trace,[]int{3}){t.Fatal(Trace)}}();Bad(nil,item{1})}
`)
}
func TestRejectedProtocols(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		derive     map[string]Derive
		want       string
	}{
		{"float-eq", "type T struct{N float64}", map[string]Derive{"T": {PartialEq: true, Eq: true}}, "no automatic Eq"},
		{"pointer", "type T struct{P *int}", map[string]Derive{"T": {PartialEq: true}}, "no automatic PartialEq"},
		{"channel", "type T struct{S chan int}", map[string]Derive{"T": {PartialEq: true}}, "no automatic PartialEq"},
		{"named-no-protocol", "type N int;type T struct{N N}", map[string]Derive{"T": {PartialEq: true}}, "requires an explicit"},
		{"eq-needs-child-eq", "type N float64;type T struct{N N}", map[string]Derive{"N": {PartialEq: true}, "T": {PartialEq: true, Eq: true}}, "no explicit Eq"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := Prepare(map[string][]byte{"input.hgo": []byte("package fixture\n" + tc.body)}, Config{Derives: tc.derive})
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %s got %v", tc.want, e)
			}
		})
	}
}

func TestNativePreservationAndHelperHygiene(t *testing.T) {
	native := []byte("package fixture\n// unusual   spacing retained\ntype Native struct{};func(Native)equals(x string)int{return len(x)}\n")
	files := map[string][]byte{"native.go": native, "source.hgo": []byte(`package fixture
type T struct{N int}
func F()bool{EqualsT:=func(a,b T)bool{return false};_=EqualsT;return (T{1}).equals(T{1})}
func NativeCall()int{return (Native{}).equals("x")}
`)}
	cfg := Config{Native: map[string]bool{"native.go": true}, Derives: map[string]Derive{"T": {PartialEq: true, Eq: true}}}
	out, e := Prepare(files, cfg)
	if e != nil {
		t.Fatal(e)
	}
	out, e = Lower(out, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if string(out["native.go"]) != string(native) {
		t.Fatal("native file rewritten")
	}
	text := string(out["source.hgo"])
	if !strings.Contains(text, "return EqualsT(") || !strings.Contains(text, "__hgo_enum_eq_local_") {
		t.Fatal("helper local capture", text)
	}
	u, e := parse(out)
	if e != nil {
		t.Fatal(e)
	}
	if e = u.check(cfg, false, false); e != nil {
		t.Fatal(e)
	}
}

func TestNativeQuestionReceiver(t *testing.T) {
	source := []byte(`package fixture
type Native struct{}
func(Native)equals(s string)int{return len(s)}
type Result[T any,E any]interface{ result(T,E) }
func get()Result[Native,error]{return nil}
func NativeReceiver()int{return __hgo_question(get()).equals("xx")}
`)
	cfg := Config{Enums: []enums.Decl{{Name: "Result[Native,error]", Family: "Result"}}}
	out, e := Prepare(map[string][]byte{"input.hgo": source}, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(out["input.hgo"]), intrinsic) {
		t.Fatal("native non-bool method captured by bool shim", string(out["input.hgo"]))
	}
}
func TestEqRequiresExplicitPartialEq(t *testing.T) {
	_, e := Prepare(map[string][]byte{"input.hgo": []byte("package fixture;type T int")}, Config{Derives: map[string]Derive{"T": {Eq: true}}})
	if e == nil || !strings.Contains(e.Error(), "requires explicit PartialEq") {
		t.Fatal(e)
	}
}

func TestGeneratedBooleanConstantsCannotBeCaptured(t *testing.T) {
	execute(t, `package fixture
const true=(0!=0)
const false=(0==0)
type X struct{}
type A [2]int
func Run()bool{return (X{}).equals(X{})&&(A{1,2}).equals(A{1,2})&& !(A{1,2}).equals(A{1,3})}
`, Config{Derives: map[string]Derive{"X": {PartialEq: true, Eq: true}, "A": {PartialEq: true, Eq: true}}}, `package fixture
import "testing"
func TestBool(t *testing.T){if !Run(){t.Fatal("captured boolean constants")}}
`)
}
func TestShadowedBoolRejected(t *testing.T) {
	_, e := Prepare(map[string][]byte{"input.hgo": []byte("package fixture;type bool int;type T struct{}")}, Config{Derives: map[string]Derive{"T": {PartialEq: true}}})
	if e == nil || !strings.Contains(e.Error(), "unshadowed predeclared bool") {
		t.Fatal(e)
	}
}

func TestScalarEqualsResultIsTypedBool(t *testing.T) {
	cfg := Config{}
	files := map[string][]byte{"input.hgo": []byte(`package fixture
type B bool
func takes(B){}
func bad(){n:=1;takes(n.equals(1))}
`)}
	prepared, e := Prepare(files, cfg)
	if e != nil {
		t.Fatal(e)
	}
	_, e = Lower(prepared, cfg)
	if e == nil || !strings.Contains(e.Error(), "cannot use") {
		t.Fatalf("typed bool unexpectedly accepted for named B: %v", e)
	}
}

func TestContainerEquality(t *testing.T) {
	execute(t, `package fixture
type Leaf struct{N int}
type Key struct{ID int;Names [2]string}
type Batch struct{Rows [][]Leaf;Index map[Key][]Leaf}
type Values []float64
type Dictionary map[string]int
func Same(a,b Batch)bool{return a.equals(b)}
`, Config{Derives: map[string]Derive{"Leaf": {PartialEq: true, Eq: true}, "Key": {PartialEq: true, Eq: true}, "Batch": {PartialEq: true, Eq: true}, "Values": {PartialEq: true}, "Dictionary": {PartialEq: true, Eq: true}}}, `package fixture
import("testing";"math")
func TestContents(t *testing.T){
 a:=Batch{Rows:[][]Leaf{{{1}},nil},Index:map[Key][]Leaf{{ID:1}:{{2}}}}
 b:=Batch{Rows:[][]Leaf{{{1}},nil},Index:map[Key][]Leaf{{ID:1}:{{2}}}}
 if !Same(a,b){t.Fatal("contents")};b.Rows[0][0].N=2;if Same(a,b){t.Fatal("nested changed")}
 if Same(Batch{},Batch{Rows:[][]Leaf{}})||Same(Batch{},Batch{Index:map[Key][]Leaf{}}){t.Fatal("nil equals nonnil empty")}
 if !Same(Batch{},Batch{}){t.Fatal("nil equality")}
 if Same(Batch{Rows:[][]Leaf{nil}},Batch{Rows:[][]Leaf{{}}}){t.Fatal("nested nil/empty")}
 if Same(Batch{Index:map[Key][]Leaf{{ID:1}:nil}},Batch{Index:map[Key][]Leaf{{ID:1}:{}}}){t.Fatal("map nil/empty values")}
 if Same(Batch{Index:map[Key][]Leaf{{ID:1}:nil}},Batch{Index:map[Key][]Leaf{{ID:2}:nil}}){t.Fatal("missing key with zero value")}
 n:=Values{math.NaN()};if EqualsValues(n,n){t.Fatal("same slice backing does not override NaN")}
 if !EqualsDictionary(Dictionary{"a":1,"b":2},Dictionary{"b":2,"a":1}){t.Fatal("map content")}
}
`)
}
func TestConditionalSumContainers(t *testing.T) {
	source := `package fixture
type Record struct{V Option[[]int];R Result[[]int,int]}
func None()Option[[]int]{return Option[[]int]::None}
func Nil()Option[[]int]{return Option[[]int]::Some(nil)}
func Empty()Option[[]int]{return Option[[]int]::Some([]int{})}
func Good()Result[[]int,int]{return Result[[]int,int]::Ok([]int{1})}
func Failed()Result[[]int,int]{return Result[[]int,int]::Err(2)}
`
	lowered, e := enums.Lower("input.hgo", []byte(source))
	if e != nil {
		t.Fatal(e)
	}
	execute(t, string(lowered.Source), Config{Enums: lowered.Enums, Derives: map[string]Derive{"Record": {PartialEq: true, Eq: true}}}, `package fixture
import("testing";"reflect")
func TestStates(t *testing.T){
 vals:=[]Option[[]int]{None(),Nil(),Empty()}
 for i,a:=range vals{for j,b:=range vals{if EqualsRecord(Record{a,Good()},Record{b,Good()})!=(i==j){t.Fatal("collapsed option states",i,j)}}}
 if EqualsRecord(Record{None(),Good()},Record{None(),Failed()}){t.Fatal("collapsed result variants")}
 if !EqualsRecord(Record{None(),Failed()},Record{None(),Failed()}){t.Fatal("Err equality")}
 var bad=reflect.Zero(reflect.PointerTo(reflect.TypeOf(Empty()))).Interface().(Option[[]int])
 defer func(){if recover()==nil{t.Fatal("right invalid Option skipped after left None")}}()
 EqualsRecord(Record{None(),Good()},Record{bad,Good()})
}
`)
}
func TestContainerProtocolRejections(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"float-key", "type T map[float64]int", "map key"},
		{"float-struct-key", "type T map[struct{F float64}]int", "map key"},
		{"pointer-key", "type T map[*int]int", "map key"},
		{"interface-key", "type T map[any]int", "map key"},
		{"reference-element", "type T []*int", "no automatic"},
		{"recursive-slice", "type T []T", "recursive protocol"},
		{"recursive-map", "type T map[string]T", "recursive protocol"},
		{"shadow-len", "func len(v any)int{return 0};type T []int", "unshadowed predeclared len"},
		{"shadow-nil", "var nil=[]int{};type T []int", "unshadowed predeclared nil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := Prepare(map[string][]byte{"input.hgo": []byte("package fixture\n" + tc.body)}, Config{Derives: map[string]Derive{"T": {PartialEq: true}}})
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %s got %v", tc.want, e)
			}
		})
	}
}

type fixtureImporter map[string]*types.Package

func (i fixtureImporter) Import(path string) (*types.Package, error) { return i[path], nil }
func TestImportedSumFileAliasIdentity(t *testing.T) {
	imp := fixtureImporter{}
	for _, name := range []string{"first", "second"} {
		fs := token.NewFileSet()
		f, e := parser.ParseFile(fs, name+".go", `package sum
type Option[T any]interface{mark(T)}
type Some[T any]struct{Value T}
func(Some[T])mark(T){}
func Check(v Option[int]){}
`, 0)
		if e != nil {
			t.Fatal(e)
		}
		p, e := (&types.Config{}).Check("fixture/"+name, fs, []*ast.File{f}, nil)
		if e != nil {
			t.Fatal(e)
		}
		imp[p.Path()] = p
	}
	files := map[string][]byte{
		"a.hgo": []byte(`package app;import q "fixture/first";type A struct{V q.Option[int]};type FirstSome[T any]=q.Some[T];var FirstCheck=q.Check`),
		"b.hgo": []byte(`package app;import q "fixture/second";type B struct{V q.Option[int]};type SecondSome[T any]=q.Some[T];var SecondCheck=q.Check`),
	}
	family := func(path, prefix string) enums.Decl {
		return enums.Decl{Name: "q.Option", PackagePath: path, Family: "Option", Variants: []enums.Variant{{Name: "Some", CaseName: prefix + "Some", Payload: "bound"}, {Name: "None"}}, Instances: []enums.Decl{{Validator: prefix + "Check"}}}
	}
	cfg := Config{Importer: imp, PackagePath: "fixture/app", Derives: map[string]Derive{"A": {PartialEq: true, Eq: true}, "B": {PartialEq: true, Eq: true}}, Enums: []enums.Decl{family("fixture/first", "First"), family("fixture/second", "Second")}}
	out, e := Prepare(files, cfg)
	if e != nil {
		t.Fatal(e)
	}
	u, e := parse(out)
	if e != nil {
		t.Fatal(e)
	}
	if e = u.check(cfg, false, false); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(out["b.hgo"]), "SecondCheck(a.V)") || strings.Contains(string(out["b.hgo"]), "FirstCheck(a.V)") {
		t.Fatal("wrong producer family", string(out["b.hgo"]))
	}
}

func TestNestedSumContainerInstances(t *testing.T) {
	source := `package fixture
type N struct{Rows Option[[]Option[int]];Results map[string]Result[int,string]}
func sample(n int)N{return N{Rows:Option[[]Option[int]]::Some([]Option[int]{Option[int]::None,Option[int]::Some(n)}),Results:map[string]Result[int,string]{"ok":Result[int,string]::Ok(3),"err":Result[int,string]::Err("bad")}}}
`
	lowered, e := enums.Lower("input.hgo", []byte(source))
	if e != nil {
		t.Fatal(e)
	}
	execute(t, string(lowered.Source), Config{Enums: lowered.Enums, Derives: map[string]Derive{"N": {PartialEq: true, Eq: true}}}, `package fixture
import "testing"
func TestNested(t *testing.T){if !EqualsN(sample(1),sample(1))||EqualsN(sample(1),sample(2)){t.Fatal("nested instance equality")}}
`)
}
func TestImportedMapKeyNeedsSemanticProof(t *testing.T) {
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, "p.go", `package p;type Key int;func EqualKey(a,b Key)bool{return false}`, 0)
	if e != nil {
		t.Fatal(e)
	}
	p, e := (&types.Config{}).Check("fixture/key", fs, []*ast.File{f}, nil)
	if e != nil {
		t.Fatal(e)
	}
	files := map[string][]byte{"input.hgo": []byte(`package app;import p "fixture/key";type Bound=p.Key;var BoundEqual=p.EqualKey;type Table map[Bound]int`)}
	cfg := Config{Importer: fixtureImporter{p.Path(): p}, PackagePath: "fixture/app", Derives: map[string]Derive{"Table": {PartialEq: true, Eq: true}}, Imported: []Protocol{{PackagePath: p.Path(), TypeName: "Bound", Helper: "BoundEqual", PartialEq: true, Eq: true}}}
	_, e = Prepare(files, cfg)
	if e == nil || !strings.Contains(e.Error(), "no proven native-key consistency") {
		t.Fatal(e)
	}
}

func TestImportedNestedPublicSumExecution(t *testing.T) {
	producer := `package sum
type Option[T any]interface{mark(T)}
type some[T any]struct{Value T}
func(some[T])mark(T){}
type Some[T any]=some[T]
func New[T any](v T)Option[T]{return some[T]{v}}
func CheckInt(v Option[int]){switch v.(type){case nil,some[int]:default:panic("invalid int")}}
func CheckOuter(v Option[[]Option[int]]){switch v.(type){case nil:case some[[]Option[int]]:for _,x:=range v.(some[[]Option[int]]).Value{CheckInt(x)};default:panic("invalid outer")}}
type hidden[A any,B any]struct{A A;B B}
type Public[X any,Y any]=hidden[Y,X]
`
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, "sum.go", producer, 0)
	if e != nil {
		t.Fatal(e)
	}
	p, e := (&types.Config{}).Check("eqsum", fs, []*ast.File{f}, nil)
	if e != nil {
		t.Fatal(e)
	}
	src := []byte(`package fixture
import q "eqsum"
type Holder struct{V q.Option[[]q.Option[int]]}
type Case[T any]=q.Some[T]
var CheckInt=q.CheckInt
var CheckOuter=q.CheckOuter
func Sample(n int)Holder{return Holder{q.New([]q.Option[int]{nil,q.New(n)})}}
`)
	cfg := Config{PackagePath: "eqnested", Importer: fixtureImporter{p.Path(): p}, Derives: map[string]Derive{"Holder": {PartialEq: true, Eq: true}}, Enums: []enums.Decl{{Name: "q.Option", PackagePath: p.Path(), Family: "Option", Variants: []enums.Variant{{Name: "Some", CaseName: "Case", Payload: "bound"}, {Name: "None"}}, Instances: []enums.Decl{{Validator: "CheckInt"}, {Validator: "CheckOuter"}}}}}
	prepared, e := Prepare(map[string][]byte{"input.hgo": src}, cfg)
	if e != nil {
		t.Fatal(e)
	}
	out, e := Lower(prepared, cfg)
	if e != nil {
		t.Fatal(e)
	}
	producerDir := t.TempDir()
	for name, data := range map[string]string{"go.mod": "module eqsum\ngo 1.27\n", "sum.go": producer} {
		if e = os.WriteFile(filepath.Join(producerDir, name), []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	dir := t.TempDir()
	for name, data := range map[string]string{"go.mod": "module eqnested\ngo 1.27\nrequire eqsum v0.0.0\nreplace eqsum => " + strconv.Quote(producerDir) + "\n", "main.go": string(out["input.hgo"]), "main_test.go": `package fixture
import "testing"
func TestNested(t *testing.T){if !EqualsHolder(Sample(1),Sample(1))||EqualsHolder(Sample(1),Sample(2)){t.Fatal("imported nested Option")}}
`} {
		if e = os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "test", "-race", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	if data, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("%v\n%s\n%s", e, data, out["input.hgo"])
	}
	// A public generic alias may reverse private origin parameters. Recover the
	// mapping by type identity rather than printing the inaccessible origin name.
	u, e := parse(prepared)
	if e != nil {
		t.Fatal(e)
	}
	if e = u.check(cfg, false, false); e != nil {
		t.Fatal(e)
	}
	g, e := registry(u, cfg)
	if e != nil {
		t.Fatal(e)
	}
	g.file = "input.hgo"
	hidden := p.Scope().Lookup("hidden").Type()
	target, e := types.Instantiate(nil, hidden, []types.Type{types.Typ[types.Int], types.Typ[types.String]}, true)
	if e != nil {
		t.Fatal(e)
	}
	spelling, e := g.typeSpelling(target)
	if e != nil || spelling != "q.Public[string,int]" {
		t.Fatal(spelling, e)
	}
}

func TestManualEquality(t *testing.T) {
	execute(t, `package fixture
 type Number int
 func EqualsNumber(a,b Number)bool{return a%10==b%10}
 type Row struct{N Number}
 type Floating struct{N float64}
 func Values()bool{return Row{1}.equals(Row{11})}
 `, Config{Derives: map[string]Derive{
		"Number":   {PartialEq: true, Eq: true, ManualPartialEq: true, ManualEq: true},
		"Row":      {PartialEq: true, Eq: true},
		"Floating": {PartialEq: true, Eq: true, ManualEq: true},
	}}, `package fixture
 import("testing";"math")
 func TestManual(t *testing.T){if !Values()||EqualsFloating(Floating{math.NaN()},Floating{math.NaN()}){t.Fatal("manual relation or PartialEq generation")}}
 `)
}
func TestManualEnumEntryValidation(t *testing.T) {
	d := enums.Decl{Name: "E", Validator: "check", Variants: []enums.Variant{{Name: "Item", CaseName: "item", Payload: "int"}}}
	execute(t, `package fixture
 type E interface{marker()}
 type item struct{Value int}
 func(item)marker(){}
 var Trace []int
 func check(e E){Trace=append(Trace,3);switch e.(type){case item:default:panic("invalid")}}
 func EqualsE(check E, other E)bool{Trace=append(Trace,4);return true}
 func arg(n int)E{Trace=append(Trace,n);return item{1}}
 func Run()bool{return arg(1).equals(arg(2))}
 `, Config{Derives: map[string]Derive{"E": {PartialEq: true, Eq: true, ManualPartialEq: true, ManualEq: true}}, Enums: []enums.Decl{d}}, `package fixture
 import("testing";"reflect")
 func TestOrder(t *testing.T){if !Run()||!reflect.DeepEqual(Trace,[]int{1,2,3,3,4}){t.Fatal(Trace)};Trace=nil;defer func(){if recover()==nil||!reflect.DeepEqual(Trace,[]int{3,3}){t.Fatal(Trace)}}();EqualsE(item{1},(*item)(nil))}
 `)
}
func TestManualEqualityRejected(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		flags              Derive
	}{
		{"signature", "type N int;func EqualsN(a N,b int)bool{return true}", "signature mismatch", Derive{PartialEq: true, ManualPartialEq: true}},
		{"missing", "type N int", "requires function", Derive{PartialEq: true, ManualPartialEq: true}},
		{"alias", "type N = int;func EqualsN(a,b N)bool{return true}", "concrete defined", Derive{PartialEq: true, ManualPartialEq: true}},
		{"deriveEq", "type N int;func EqualsN(a,b N)bool{return true}", "cannot prove manual", Derive{PartialEq: true, Eq: true, ManualPartialEq: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := Prepare(map[string][]byte{"input.hgo": []byte("package fixture;" + tc.source)}, Config{Derives: map[string]Derive{"N": tc.flags}})
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("%v", e)
			}
		})
	}
	_, e := Prepare(map[string][]byte{"input.hgo": []byte(`package fixture;type N int;func EqualsN(a,b N)bool{return true};type M map[N]int`)}, Config{Derives: map[string]Derive{"N": {PartialEq: true, ManualPartialEq: true}, "M": {PartialEq: true}}})
	if e == nil || !strings.Contains(e.Error(), "manual map key") {
		t.Fatalf("%v", e)
	}
}

func TestManualDeferredRecover(t *testing.T) {
	execute(t, `package fixture
 type N int
 var Caught bool
 func EqualsN(a,b N)bool{Caught=recover()!=nil;return true}
 func Run(){var n N;defer n.equals(n);panic("caught")}
 `, Config{Derives: map[string]Derive{"N": {PartialEq: true, ManualPartialEq: true}}}, `package fixture
 import "testing"
 func TestRecover(t *testing.T){Run();if !Caught{t.Fatal("manual function frame changed")}}
 `)
}
func TestManualEqDoesNotBlockProvenMapKey(t *testing.T) {
	execute(t, `package fixture
 type Key struct{I int}
 type M map[Key]int
 `, Config{Derives: map[string]Derive{"Key": {PartialEq: true, Eq: true, ManualEq: true}, "M": {PartialEq: true, Eq: true}}}, `package fixture
 import "testing"
 func TestKey(t *testing.T){if !EqualsM(M{Key{1}:2},M{Key{1}:2}){t.Fatal("key")}}
 `)
}

func TestManualRecursiveEqualityHygiene(t *testing.T) {
	execute(t, `package fixture
 type N int
 type Other int
 func EqualsN(a,b N)bool{if a==0||b==0{return a==b};return Other(a-1).equals(Other(b-1))}
 func EqualsOther(a,b Other)bool{return N(a).equals(N(b))}
 var Global=N(3).equals(N(3))
 func Shadow(EqualsN func(N,N)bool)bool{local:=EqualsN(1,2);return !local&&N(2).equals(N(2))}
 func Nested()bool{EqualsN:=func(a,b N)bool{return false};f:=func()bool{return !EqualsN(1,1)&&N(1).equals(N(1))};return f()}
 func LocalName()any{type EqualsSelf int;if Self(1).equals(Self(1)){return EqualsSelf(0)};return nil}
 type Self int
 func EqualsSelf(a,b Self)bool{if a==0||b==0{return a==b};return Self(a-1).equals(Self(b-1))}
 `, Config{Derives: map[string]Derive{"N": {PartialEq: true, ManualPartialEq: true}, "Other": {PartialEq: true, ManualPartialEq: true}, "Self": {PartialEq: true, ManualPartialEq: true}}}, `package fixture
 import("testing";"reflect")
 func TestRecursion(t *testing.T){if reflect.TypeOf(LocalName()).Name()!="EqualsSelf"{t.Fatal("local type renamed")};if !Global||!Shadow(func(N,N)bool{return false})||!Nested()||!EqualsSelf(3,3)||EqualsSelf(2,3){t.Fatal("recursion/shadow")}}
 `)
}
