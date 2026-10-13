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
