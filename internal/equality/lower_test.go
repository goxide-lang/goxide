package equality

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		{"slice", "type T struct{S []int}", map[string]Derive{"T": {PartialEq: true}}, "no automatic PartialEq"},
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
	if !strings.Contains(text, "return __hgo_enum_eq_bound_") {
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
