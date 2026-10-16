package check_test

import (
	"go/types"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
)

func parse(t *testing.T, source string) (map[string]*ast.File, *resolve.Result) {
	t.Helper()
	f, ds := parser.ParseFile("main.gox", []byte(source))
	if len(ds) > 0 {
		t.Fatalf("parse: %v", ds)
	}
	files := map[string]*ast.File{"main.gox": f}
	r := resolve.Resolve(files, nil)
	if len(r.Diagnostics) > 0 {
		t.Fatalf("resolve: %v", r.Diagnostics)
	}
	return files, r
}

func TestDiagnostics(t *testing.T) {
	cases := []struct{ name, code, good, bad, span string }{
		{"exhaustiveness", check.NonExhaustive,
			`enum E { A, B };func f(e E)int{return match e {E::A=>1,E::B=>2}}`,
			`enum E { A, B };func f(e E)int{return match e {E::A=>1}}`, `match e {E::A=>1}`},
		{"reachability", check.UnreachableArm,
			`enum E { A(int) };func f(e E)int{return match e {E::A(0)=>1,E::A(n)=>n}}`,
			`enum E { A(int) };func f(e E)int{return match e {E::A(n)=>n,E::A(0)=>1}}`, `E::A(0)`},
		{"comparable", check.IncomparablePayload,
			`enum E { A([]int) };func f(e E)int{return match e {E::A(n) if len(n)==0=>1,E::A(_)=>2}}`,
			`enum E { A([]int) };func f(e E)int{return match e {E::A(0)=>1,E::A(_)=>2}}`, `0`},
		{"arm-type", check.ArmTypeMismatch,
			`enum E { A, B };func f(e E)int{return match e {E::A=>1,E::B=>2}}`,
			`enum E { A, B };func f(e E)int{return match e {E::A=>1,E::B=>"wrong"}}`, `"wrong"`},
		{"try-return", check.TryReturnType,
			`func f(r Result[int,error])Result[string,error]{x:=r?;return Result[string,error]::Ok(string(x))}`,
			`func f(r Result[int,error])Option[int]{x:=r?;return Option[int]::Some(x)}`, `r?`},
		{"generic", check.GenericConstraint,
			`enum E[T comparable] { A(T) };func f(e E[int])int{return match e {E::A(n)=>n}}`,
			`enum E[T comparable] { A(T) };func f(e E[[]int])int{return 0}`, `[]int`},
		{"enum-nil-constructor", check.EnumNilLiteral,
			`enum Inner{A};enum Outer{Wrap(Inner)};func f(){_=Outer::Wrap(Inner::A)}`,
			`enum Inner{A};enum Outer{Wrap(Inner)};func f(){_=Outer::Wrap(nil)}`, `nil`},
		{"enum-nil-struct-constructor", check.EnumNilLiteral,
			`enum Inner{A};enum Outer{Wrap{inner Inner}};func f(){_=Outer::Wrap{inner:Inner::A}}`,
			`enum Inner{A};enum Outer{Wrap{inner Inner}};func f(){_=Outer::Wrap{inner:nil}}`, `nil`},
		{"enum-nil-variable", check.EnumNilLiteral,
			`enum E{A};var e E=E::A`,
			`enum E{A};var e E=nil`, `nil`},
		{"enum-nil-local-variable", check.EnumNilLiteral,
			`enum E{A};func f(){var e E=E::A;_=e}`,
			`enum E{A};func f(){var e E=nil;_=e}`, `nil`},
		{"enum-nil-assignment", check.EnumNilLiteral,
			`enum E{A};func f(e E){e=E::A}`,
			`enum E{A};func f(e E){e=nil}`, `nil`},
		{"enum-nil-return", check.EnumNilLiteral,
			`enum E{A};func f()E{return E::A}`,
			`enum E{A};func f()E{return (nil)}`, `nil`},
		{"enum-nil-argument", check.EnumNilLiteral,
			`enum E{A};func sink(e E){};func f(){sink(E::A)}`,
			`enum E{A};func sink(e E){};func f(){sink(nil)}`, `nil`},
		{"enum-nil-conversion", check.EnumNilLiteral,
			`enum E{A};func f()E{return E(E::A)}`,
			`enum E{A};func f()E{return E(nil)}`, `nil`},
		{"enum-nil-alias", check.EnumNilLiteral,
			`enum E{A};type Alias=E;func f()Alias{return E::A}`,
			`enum E{A};type Alias=E;func f()Alias{return nil}`, `nil`},
		{"enum-nil-slice", check.EnumNilLiteral,
			`enum E{A};func f(){_=[]E{E::A}}`,
			`enum E{A};func f(){_=[]E{nil}}`, `nil`},
		{"enum-nil-array", check.EnumNilLiteral,
			`enum E{A};func f(){_=[1]E{E::A}}`,
			`enum E{A};func f(){_=[1]E{nil}}`, `nil`},
		{"enum-nil-map", check.EnumNilLiteral,
			`enum E{A};func f(){_=map[string]E{"key":E::A}}`,
			`enum E{A};func f(){_=map[string]E{"key":nil}}`, `nil`},
		{"enum-nil-struct", check.EnumNilLiteral,
			`enum E{A};type S struct{Value E};func f(){_=[]S{{Value:E::A}}}`,
			`enum E{A};type S struct{Value E};func f(){_=[]S{{Value:nil}}}`, `nil`},
		{"enum-nil-option", check.EnumNilLiteral,
			`var value Option[int]=Option[int]::Some(1)`,
			`var value Option[int]=nil`, `nil`},
		{"enum-nil-generic-payload", check.EnumNilLiteral,
			`enum E{A};func f(){_=Option[E]::Some(E::A)}`,
			`enum E{A};func f(){_=Option[E]::Some(nil)}`, `nil`},
		{"enum-nil-generic-argument", check.EnumNilLiteral,
			`enum E{A};func pick[T any](a,b T)T{return a};func f(){_=pick(E::A,E::A)}`,
			`enum E{A};func pick[T any](a,b T)T{return a};func f(){_=pick(nil,E::A)}`, `nil`},
		{"enum-nil-generic-variadic", check.EnumNilLiteral,
			`enum E{A};func pick[T any](a T,rest ...T)T{return a};func f(){_=pick(E::A,E::A,E::A)}`,
			`enum E{A};func pick[T any](a T,rest ...T)T{return a};func f(){_=pick(E::A,E::A,nil)}`, `nil`},
		{"enum-nil-inferred-match", check.EnumNilLiteral,
			`enum E{A};func f(b bool){_=match b{true=>E::A,_=>E::A}}`,
			`enum E{A};func f(b bool){_=match b{true=>nil,_=>E::A}}`, `nil`},
		{"enum-nil-append", check.EnumNilLiteral,
			`enum E{A};func f(es []E){_=append(es,E::A)}`,
			`enum E{A};func f(es []E){_=append(es,nil)}`, `nil`},
		{"enum-nil-send", check.EnumNilLiteral,
			`enum E{A};func f(ch chan E){ch<-E::A}`,
			`enum E{A};func f(ch chan E){ch<-nil}`, `nil`},
		{"enum-nil-comparison-still-allowed", check.EnumNilLiteral,
			`enum E{A};var zero E;func f(e E)bool{return e==nil||nil!=e};func g(e E)bool{switch e{case nil:return true;default:return false}}`,
			`enum E{A};func f()E{return nil}`, `nil`},
		{"non-enum-nil-still-allowed", check.EnumNilLiteral,
			`enum E{A};enum Box{Hold(*int)};func f(){var p *E=nil;var err error=nil;_=Option[*int]::Some(nil);_=Result[int,error]::Err(nil);_=Box::Hold(nil);_,_=p,err}`,
			`enum E{A};func f()E{return nil}`, `nil`},
		{"nested-exhaustiveness", check.NonExhaustive,
			`enum Inner{A,B};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A)=>1,Outer::Wrap(p)=>2}}`,
			`enum Inner{A,B};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A)=>1}}`, `match x{Outer::Wrap(Inner::A)=>1}`},
		{"nested-reachability", check.UnreachableArm,
			`enum Inner{A,B};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A)=>1,Outer::Wrap(p)=>2}}`,
			`enum Inner{A,B};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(_)=>1,Outer::Wrap(Inner::A)=>2}}`, `Outer::Wrap(Inner::A)`},
		{"nested-struct-exhaustiveness", check.NonExhaustive,
			`enum Inner{A,B};enum Outer{Wrap{inner Inner}};func f(x Outer)int{return match x{Outer::Wrap{inner:Inner::A}=>1,Outer::Wrap{inner:p}=>2}}`,
			`enum Inner{A,B};enum Outer{Wrap{inner Inner}};func f(x Outer)int{return match x{Outer::Wrap{inner:Inner::A}=>1}}`, `match x{Outer::Wrap{inner:Inner::A}=>1}`},
		{"nested-single-variant", check.NonExhaustive,
			`enum Inner{A(int)};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A(_))=>1}}`,
			`enum Inner{A(int)};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A(0))=>1}}`, `match x{Outer::Wrap(Inner::A(0))=>1}`},
		{"nested-variant-identity", check.VariantMismatch,
			`enum Inner{A,B};enum Other{C};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A)=>1,Outer::Wrap(p)=>2}}`,
			`enum Inner{A,B};enum Other{C};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Other::C)=>1,Outer::Wrap(p)=>2}}`, `Other::C`},
		{"nested-instance-identity", check.VariantMismatch,
			`enum Inner[T any]{A(T),B};enum Outer{Wrap(Inner[int])};func f(x Outer)int{return match x{Outer::Wrap(Inner[int]::A(_))=>1,Outer::Wrap(p)=>2}}`,
			`enum Inner[T any]{A(T),B};enum Outer{Wrap(Inner[int])};func f(x Outer)int{return match x{Outer::Wrap(Inner[string]::A(_))=>1,Outer::Wrap(p)=>2}}`, `Inner[string]::A(_)`},
		{"nested-union-exhaustiveness", check.NonExhaustive,
			`enum Inner{A,B};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A)=>1,Outer::Wrap(Inner::B)=>2}}`,
			`enum Inner{A,B};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A)=>1}}`, `match x{Outer::Wrap(Inner::A)=>1}`},
		{"nested-union-guard", check.NonExhaustive,
			`enum Inner{A,B};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A)=>1,Outer::Wrap(Inner::B)=>2}}`,
			`enum Inner{A,B};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A)=>1,Outer::Wrap(Inner::B) if false=>2}}`, `match x{Outer::Wrap(Inner::A)=>1,Outer::Wrap(Inner::B) if false=>2}`},
		{"nested-product-exhaustiveness", check.NonExhaustive,
			`enum Inner{A,B};enum Outer{Pair(Inner,Inner)};func f(x Outer)int{return match x{Outer::Pair(Inner::A,_)=>1,Outer::Pair(Inner::B,Inner::A)=>2,Outer::Pair(Inner::B,Inner::B)=>3}}`,
			`enum Inner{A,B};enum Outer{Pair(Inner,Inner)};func f(x Outer)int{return match x{Outer::Pair(Inner::A,_)=>1,Outer::Pair(Inner::B,Inner::A)=>2}}`, `match x{Outer::Pair(Inner::A,_)=>1,Outer::Pair(Inner::B,Inner::A)=>2}`},
		{"nested-union-reachability", check.UnreachableArm,
			`enum Inner{A,B};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A)=>1,Outer::Wrap(Inner::B)=>2}}`,
			`enum Inner{A,B};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::A)=>1,Outer::Wrap(Inner::B)=>2,Outer::Wrap(p)=>3}}`, `Outer::Wrap(p)`},
		{"nested-recursive-exhaustiveness", check.NonExhaustive,
			`enum Inner{Leaf,Next(Inner)};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::Leaf)=>1,Outer::Wrap(Inner::Next(_))=>2}}`,
			`enum Inner{Leaf,Next(Inner)};enum Outer{Wrap(Inner)};func f(x Outer)int{return match x{Outer::Wrap(Inner::Leaf)=>1}}`, `match x{Outer::Wrap(Inner::Leaf)=>1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, res := parse(t, "package p;"+tc.good)
			if _, ds := check.Check(files, res); len(ds) != 0 {
				t.Fatalf("positive: %v", ds)
			}
			src := "package p;" + tc.bad
			files, res = parse(t, src)
			_, ds := check.Check(files, res)
			if len(ds) != 1 {
				t.Fatalf("diagnostics = %v", ds)
			}
			start := strings.Index(src, tc.span)
			want := ast.Span{Start: uint32(start), End: uint32(start + len(tc.span))}
			if ds[0].Code != tc.code || ds[0].Span != want || ds[0].File != "main.gox" || ds[0].Origin.Span != want {
				t.Fatalf("diagnostic = %#v, want code %s span %#v", ds[0], tc.code, want)
			}
		})
	}
	t.Run("native-go-enum-nil-still-allowed", func(t *testing.T) {
		files, res := parse(t, "package p;enum E{A};var e E=nil")
		files["main.gox"].NativeGo = true
		if _, ds := check.Check(files, res); len(ds) != 0 {
			t.Fatalf("native Go nil: %v", ds)
		}
	})
}

func TestGenericInstancesAndBindings(t *testing.T) {
	files, res := parse(t, `package p
enum Box[T any] { Full(T), Empty }
func f(a Box[int],b Box[string]) { x:=match a {Box::Full(n)=>n,Box::Empty=>0}; y:=match b {Box::Full(s)=>s,Box::Empty=>""}; _,_=x,y }
`)
	info, ds := check.Check(files, res)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	seen := map[string]bool{}
	for _, m := range info.Matches {
		if m.Enum == nil || len(m.Enum.Args) != 1 {
			t.Fatalf("missing instantiated enum: %#v", m)
		}
		arg := m.Enum.Args[0]
		seen[arg.String()] = true
		if !types.Identical(m.Type, arg) {
			t.Fatalf("match type %s, payload %s", m.Type, arg)
		}
	}
	if !seen["int"] || !seen["string"] {
		t.Fatalf("instances: %v", seen)
	}
}

func TestStatementMatchAndNearestFunction(t *testing.T) {
	files, res := parse(t, `package p
enum E { A, B }
func f(e E)int { match e {E::A=>{return 1},E::B=>{return 2}} }
func g(r Option[int])Option[int] { f:=func()int {return r?}; return Option[int]::Some(f()) }
`)
	info, ds := check.Check(files, res)
	if len(ds) != 1 || ds[0].Code != check.TryReturnType {
		t.Fatalf("diagnostics: %v", ds)
	}
	for _, m := range info.Matches {
		if !m.Statement {
			t.Fatal("terminating match treated as value")
		}
	}
}

func TestExplicitOneStepConversion(t *testing.T) {
	source := `package p
type A struct{}
type B struct{}
type C struct{}
func convert(a A)B{return B{}}
func onward(b B)C{return C{}}
func f(r Result[int,A])Result[int,B]{return Result[int,B]::Ok(r?)}
`
	files, res := parse(t, source)
	var helpers []resolve.Ref
	for _, item := range files["main.gox"].Items {
		if f, ok := item.(*ast.FuncDecl); ok && f.Name.Name == "convert" {
			helpers = append(helpers, resolve.Ref{File: "main.gox", ID: f.ID()})
		}
	}
	if _, ds := check.Check(files, res); len(ds) != 1 || ds[0].Code != check.TryReturnType {
		t.Fatalf("unregistered conversion accepted: %v", ds)
	}
	info, ds := check.CheckWithConversions(files, res, helpers)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tr := range info.Tries {
		if tr.Conversion == nil || tr.Conversion.Name.Name != "convert" {
			t.Fatalf("conversion: %#v", tr)
		}
	}
	if _, ds := check.CheckWithConversions(files, res, append(helpers, helpers...)); len(ds) != 1 || ds[0].Code != check.TryReturnType {
		t.Fatalf("ambiguous conversion: %v", ds)
	}
	files, res = parse(t, strings.ReplaceAll(source, "Result[int,B]", "Result[int,C]"))
	helpers = nil
	for _, item := range files["main.gox"].Items {
		if f, ok := item.(*ast.FuncDecl); ok && (f.Name.Name == "convert" || f.Name.Name == "onward") {
			helpers = append(helpers, resolve.Ref{File: "main.gox", ID: f.ID()})
		}
	}
	if _, ds := check.CheckWithConversions(files, res, helpers); len(ds) != 1 || ds[0].Code != check.TryReturnType {
		t.Fatalf("conversion chain accepted: %v", ds)
	}
}
