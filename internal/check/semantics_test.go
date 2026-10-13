package check_test

import (
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
)

func TestSourcePackageEnumIdentity(t *testing.T) {
	sources := map[string]string{
		"fixture/lib/enum.gox":  `package lib;pub enum Box[T any] { Empty, Item(T) }`,
		"fixture/lib/alias.gox": `package lib;type IntBox = Box[int];type Reordered[A,B any] = Result[B,A];func makeBox()IntBox{return Box[int]::Item(1)}`,
		"fixture/main.gox":      `package main;use fixture::lib as lib;func f(x lib::Box[int],y lib::Reordered[string,int])int{return match x {lib::Box::Empty=>0,lib::Box::Item(n)=>n}};func g()int{return match lib::makeBox(){lib::Box::Item(n)=>n,lib::Box::Empty=>0}}`,
	}
	files := map[string]*ast.File{}
	for name, src := range sources {
		f, ds := parser.ParseFile(name, []byte(src))
		if len(ds) > 0 {
			t.Fatalf("parse %s: %v", name, ds)
		}
		files[name] = f
	}
	r := resolve.Resolve(files, nil)
	if len(r.Diagnostics) > 0 {
		t.Fatal(r.Diagnostics)
	}
	info, ds := check.Check(files, r)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if len(info.Matches) != 2 {
		t.Fatalf("matches %d", len(info.Matches))
	}
	for _, m := range info.Matches {
		if m.Enum == nil || m.Enum.Enum.Name != "Box" || len(m.Enum.Args) != 1 || !types.Identical(m.Enum.Args[0], types.Typ[types.Int]) {
			t.Fatalf("instance: %#v", m.Enum)
		}
	}
	var found bool
	for s, typ := range info.Symbols {
		if s.Name.Name == "y" {
			enum := info.EnumOf(typ)
			if enum == nil || enum.Enum.Name != "Result" || !types.Identical(enum.Args[0], types.Typ[types.Int]) || !types.Identical(enum.Args[1], types.Typ[types.String]) {
				t.Fatalf("generic alias: %s %#v", typ, enum)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing generic alias argument")
	}
}

func TestAbsoluteSourcePackageEnum(t *testing.T) {
	root := t.TempDir()
	sources := map[string]string{"lib/enum.gox": `package lib;pub enum E[T any]{V(T)}`, "main.gox": `package main;use fixture::lib;func f(x lib::E[int])int{return match x{lib::E::V(v)=>v}}`}
	files := map[string]*ast.File{}
	for relative, source := range sources {
		name := filepath.Join(root, relative)
		tree, ds := parser.ParseFile(name, []byte(source))
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		files[name] = tree
	}
	res := resolve.Resolve(files, nil)
	if len(res.Diagnostics) > 0 {
		t.Fatal(res.Diagnostics)
	}
	info, ds := check.Check(files, res)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	for _, m := range info.Matches {
		if m.Enum == nil || m.Enum.Enum.Name != "E" || len(m.Enum.Args) != 1 {
			t.Fatalf("enum instance %#v", m)
		}
	}
	if got := info.PackagePaths[filepath.ToSlash(filepath.Join(root, "lib"))]; got != "fixture/lib" {
		t.Fatalf("package path %q", got)
	}
}

func TestVariantIdentityAndInstanceDiagnostics(t *testing.T) {
	for _, src := range []string{
		`enum First{Same};enum Second{Other};func f(x First)int{return match x{Second::Other=>1,_=>0}}`,
		`enum Box[T any]{Item(T)};func f(x Box[int])int{return match x{Box[string]::Item(_)=>1,_=>0}}`,
	} {
		files, r := parse(t, "package p;"+src)
		_, ds := check.Check(files, r)
		if len(ds) != 1 || ds[0].Code != check.VariantMismatch {
			t.Fatalf("variant identity: %v", ds)
		}
	}
}

func TestGenericEnumInstanceAssignability(t *testing.T) {
	files, resolved := parse(t, `package p;enum Pair[T,U any]{Item(T,U)}`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for _, enum := range info.Enums {
		instance := func(a, b types.Type) types.Type {
			t.Helper()
			value, err := types.Instantiate(nil, enum.Type, []types.Type{a, b}, true)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		ints := instance(types.Typ[types.Int], types.Typ[types.Int])
		same := instance(types.Typ[types.Int], types.Typ[types.Int])
		firstDiffers := instance(types.Typ[types.String], types.Typ[types.Int])
		secondDiffers := instance(types.Typ[types.Int], types.Typ[types.String])
		if !types.AssignableTo(ints, same) || !types.AssignableTo(same, ints) {
			t.Fatal("the same enum instance is not assignable")
		}
		for _, other := range []types.Type{firstDiffers, secondDiffers} {
			if types.AssignableTo(ints, other) || types.AssignableTo(other, ints) {
				t.Fatalf("different enum instances remain assignable: %s and %s", ints, other)
			}
		}
	}
}

func TestEnumGeneratedNameConflicts(t *testing.T) {
	for _, source := range []string{`package p;pub enum A{Same};pub enum B{Same}`, `package p;type Same int;pub enum E{Same}`} {
		files, res := parse(t, source)
		_, ds := check.Check(files, res)
		start := strings.LastIndex(source, "Same")
		want := ast.Span{Start: uint32(start), End: uint32(start + 4)}
		if len(ds) != 1 || ds[0].Code != check.EnumGoNameConflict || ds[0].Span != want {
			t.Fatalf("diagnostics %#v", ds)
		}
	}
	files, res := parse(t, `package p;pub enum A{Same};enum B{Same}`)
	if _, ds := check.Check(files, res); len(ds) > 0 {
		t.Fatal(ds)
	}
}

func TestConstraintTypeSetsMethodsAndGenericCalls(t *testing.T) {
	cases := []struct {
		name, src string
		errors    int
	}{
		{"set-positive", `type Number int;enum E[T interface{~int|~int64}]{V(T)};func f(x E[Number]){}`, 0},
		{"set-negative", `enum E[T interface{~int|~int64}]{V(T)};func f(x E[string]){}`, 1},
		{"method-positive", `enum E[T interface{String()string}]{V(T)};type Text struct{};func f(x E[Text]){};func(Text)String()string{return ""}`, 0},
		{"method-negative", `enum E[T interface{String()string}]{V(T)};type Text struct{};func f(x E[Text]){}`, 1},
		{"function-positive", `func keep[T comparable](v T)T{return v};func f(){_=keep[int](1);_=keep("x")}`, 0},
		{"function-negative", `func keep[T comparable](v T)T{return v};func f(){_=keep[[]int]([]int{});_=keep([]int{})}`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, r := parse(t, "package p;"+tc.src)
			_, ds := check.Check(files, r)
			if len(ds) != tc.errors {
				t.Fatalf("diagnostics %v", ds)
			}
			for _, d := range ds {
				if d.Code != check.GenericConstraint {
					t.Fatalf("code %s", d.Code)
				}
			}
		})
	}
}

func TestConstantPayloadKinds(t *testing.T) {
	for _, payload := range []string{"[]int", "map[string]int", "func()"} {
		t.Run(payload, func(t *testing.T) {
			prefix := "package p;enum E{V(" + payload + ")};func f(x E)int{return match x{"
			files, r := parse(t, prefix+`E::V(nil)=>1,E::V(_)=>0}}`)
			_, ds := check.Check(files, r)
			if len(ds) != 1 || ds[0].Code != check.IncomparablePayload {
				t.Fatalf("diagnostics %v", ds)
			}
			files, r = parse(t, prefix+`E::V(n) if n==nil=>1,E::V(_)=>0}}`)
			if _, ds := check.Check(files, r); len(ds) != 0 {
				t.Fatal(ds)
			}
		})
	}
	files, r := parse(t, `package p;const Zero=0;enum E{V(int)};func f(x E)int{return match x{E::V(Zero)=>1,E::V(0x0)=>2,E::V(_)=>3}}`)
	_, ds := check.Check(files, r)
	if len(ds) != 1 || ds[0].Code != check.UnreachableArm {
		t.Fatalf("constant identity: %v", ds)
	}
}

func TestTryNominalFamilyAndAliases(t *testing.T) {
	files, r := parse(t, `package p;type Alias[T any]=Option[T];func f(x Alias[int])Alias[string]{_=x?;return Option[string]::Some("")}`)
	if _, ds := check.Check(files, r); len(ds) != 0 {
		t.Fatal(ds)
	}
	files, r = parse(t, `package p;type Other[T any] Option[T];func f(x Other[int])Option[int]{_=x?;return Option[int]::None}`)
	if _, ds := check.Check(files, r); len(ds) != 1 || ds[0].Code != check.TryReturnType {
		t.Fatalf("nominal family: %v", ds)
	}
}

func TestExpansionDiagnosticOrigin(t *testing.T) {
	source := `package p
enum E { A, B }
macro_rules! inner { ($value:expr) => { match $value { E::A => 1 } }; }
macro_rules! outer { ($value:expr) => { inner!($value) }; }
func f(x E)int{return outer!(x)}
`
	f, ds := parser.ParseFile("main.gox", []byte(source))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	expanded, err := expand.Expand([]*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := resolve.ResolveExpanded(map[string]*ast.File{"main.gox": f}, expanded, nil)
	if len(r.Diagnostics) > 0 {
		t.Fatal(r.Diagnostics)
	}
	info, diagnostics := check.Check(map[string]*ast.File{"main.gox": expanded.Files[0]}, r)
	if len(diagnostics) != 1 || diagnostics[0].Code != check.NonExhaustive {
		t.Fatalf("diagnostics %v", diagnostics)
	}
	for match := range info.Matches {
		origin := expanded.Origins[match]
		d := diagnostics[0]
		if d.Span != match.Span() || d.Origin.File != "main.gox" || d.Origin.Span != origin.Span || d.Origin.Expn != match.Expn || len(d.Origin.Stack) != 2 {
			t.Fatalf("diagnostic origin %#v; node %#v, source %#v", d, match.Base, origin)
		}
		if !strings.Contains(source[origin.Span.Start:origin.Span.End], "match") {
			t.Fatalf("origin does not point to macro template: %q", source[origin.Span.Start:origin.Span.End])
		}
	}
}
