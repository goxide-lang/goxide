package check_test

import (
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestTraitStaticAndAssociatedEvidence(t *testing.T) {
	files, res := parse(t, `package p
trait Build { func make() Self }
trait Source { type Item; func get(self &Self) Self::Item }
type Cell struct { n int }
impl Build for Cell { func make() Self { return Self{n: 7} } }
impl Source for Cell { type Item = int; func get(self &Self) Self::Item { return self.n } }
func Make[T Build]() T { return T::make() }
func Read[T Source](value &T) T::Source::Item { return value.get() }
func ReadWhere[T any](value &T) T::Source::Item where T: Source { return value.get() }
func run() int { c:=Make[Cell](); a:=Read[Cell](&c); b:=ReadWhere[Cell](&c); return a+b }
`)
	info, ds := check.Check(files, res)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if len(info.Impls) != 2 || len(info.GenericBounds) != 3 {
		t.Fatalf("missing explicit evidence: impls=%d bounds=%d", len(info.Impls), len(info.GenericBounds))
	}
}

func TestTraitSourceReceiverCandidateOrder(t *testing.T) {
	files, res := parse(t, `package p
trait P{func get(self &Self)string}
type S struct{}
impl S {func get(self Self)int{return 1}}
impl P for S {func get(self &Self)string{return "borrow"}}
func borrow(x &S)&S{return x}
func direct(x &S)string{return x.get()}
func local(x &S)string{y:=x;return y.get()}
func returned(x &S)string{return borrow(x).get()}
func native(x S)int{return x.get()}
`)
	info, ds := check.Check(files, res)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	borrowed, values := 0, 0
	for _, selected := range info.Members {
		if selected.Member.Name == "get" {
			if selected.Member.Receiver == check.ReadRef {
				borrowed++
			} else {
				values++
			}
		}
	}
	if borrowed != 3 || values != 1 {
		t.Fatalf("receiver choice: read=%d value=%d", borrowed, values)
	}
}

func TestTraitReferenceSignatureAndAssociatedPermissions(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"parameter", `trait P{func f(self Self,p &D)};type S struct{};impl P for S{func f(self Self,p &mut D){p.N++}}`, "reference permissions do not match"},
		{"nested-function", `trait P{func f(self Self,p func(&D))};type S struct{};impl P for S{func f(self Self,p func(&mut D)){}}`, "reference permissions do not match"},
		{"nested-slice", `trait P{func f(self Self,p []&D)};type S struct{};impl P for S{func f(self Self,p []&mut D){}}`, "reference permissions do not match"},
		{"result", `trait P{func f(self Self)&mut D};type S struct{};impl P for S{func f(self Self)&D{return nil}}`, "reference permissions do not match"},
		{"bound-result", `trait P{type Item;func get(self Self)Self::Item};func f[T P[Item=&D]](x T){x.get().N=1}`, "cannot write through"},
		{"bound-projection", `trait P{type Item;func get(self Self)Self::Item};func f[T P[Item=&D]](x T){var y T::P::Item=x.get();y.N=1}`, "cannot write through"},
		{"bound-mutable", `trait P{type Item;func get(self Self)Self::Item};func f[T P[Item=&mut D]](x T){var y T::P::Item=x.get();y.N=1}`, ""},
		{"impl-result", `trait P{type Item;func get(self Self)Self::Item};type S struct{p &D};impl P for S{type Item=&D;func get(self Self)Self::Item{return self.p}};func f(s S){s.get().N=1}`, "cannot write through"},
		{"view-result", `trait P{type Item;func get(self Self)Self::Item};type S struct{p &D};impl P for S{type Item=&D;func get(self Self)Self::Item{return self.p}};func f(s S){var v P[Item=&D]=s;v.get().N=1}`, "cannot write through"},
		{"view-binding", `trait P{type Item;func get(self Self)Self::Item};type S struct{p &D};impl P for S{type Item=&D;func get(self Self)Self::Item{return self.p}};func f(s S){var v P[Item=&mut D]=s;_ = v}`, "cannot erase or increase"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, res := parse(t, "package p;type D struct{N int};"+tc.source)
			_, ds := check.Check(files, res)
			if tc.want == "" {
				if len(ds) != 0 {
					t.Fatal(ds)
				}
				return
			}
			for _, d := range ds {
				if strings.Contains(d.Message, tc.want) {
					return
				}
			}
			t.Fatalf("want %q, got %v", tc.want, ds)
		})
	}
}

func TestTraitAliasCanonicalImplementation(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		source := `package p;trait P{func get(self Self)int};type S struct{};type Alias=S;impl P for Alias{func get(self Self)int{return 1}};func f(s S)int{return s.get()}`
		if duplicate {
			source += `;impl P for S{func get(self Self)int{return 2}}`
		}
		files, res := parse(t, source)
		info, ds := check.Check(files, res)
		if !duplicate {
			if len(ds) != 0 {
				t.Fatal(ds)
			}
			for _, impl := range info.Impls {
				if impl.Target != impl.Base {
					t.Fatalf("alias target %s was not canonical", impl.Target)
				}
			}
			continue
		}
		found := false
		for _, d := range ds {
			found = found || strings.Contains(d.Message, "duplicate trait implementation")
		}
		if !found {
			t.Fatal(ds)
		}
	}
}

func TestTraitQualifiedSourcePackages(t *testing.T) {
	sources := map[string]string{
		"api/api.gox":         `package api;pub trait Source{type Item;func Get(self Self)Self::Item}`,
		"other/api.gox":       `package other;pub trait Source{type Item;func Get(self Self)Self::Item}`,
		"model/model.gox":     `package model;type Value struct{};func(self Value)Get()int{return 7}`,
		"adapter/adapter.gox": `package adapter;use fixture::api;use fixture::other;use fixture::model;declare impl api::Source[Item=int] for model::Value;declare impl other::Source[Item=int] for model::Value;func Read[T api::Source](x T)T::api::Source::Item{return x.Get()};func F(x model::Value)int{return api::Source::Get(x)+x.Get::<other::Source>()};func V(x model::Value)api::Source[Item=int]{return x};func Project(x model::Value)model::Value::api::Source::Item{return x.Get()}`,
	}
	root := t.TempDir()
	files := map[string]*ast.File{}
	for relative, source := range sources {
		name := filepath.Join(root, relative)
		f, ds := parser.ParseFile(name, []byte(source))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		files[name] = f
	}
	res := resolve.Resolve(files, nil)
	if len(res.Diagnostics) != 0 {
		t.Fatal(res.Diagnostics)
	}
	info, ds := check.Check(files, res)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	markers := map[string]bool{}
	for _, trait := range info.Traits {
		if markers[trait.Marker] {
			t.Fatal("same-named traits share marker")
		}
		markers[trait.Marker] = true
	}
	if len(info.Impls) != 2 || len(info.GenericBounds) != 1 {
		t.Fatalf("impls=%d bounds=%d", len(info.Impls), len(info.GenericBounds))
	}
	for _, file := range files {
		for _, item := range file.Items {
			if fn, ok := item.(*ast.FuncDecl); ok && fn.Name.Name == "Project" {
				if got := info.TypeOverrides[fn.Results[0].Type]; !types.Identical(got, types.Typ[types.Int]) {
					t.Fatalf("package-qualified concrete projection = %v, want int", got)
				}
			}
		}
	}
	for _, selected := range info.Members {
		if selected.Bound != nil {
			if len(selected.Signature.Results().String()) == 0 || !types.Identical(selected.Signature.Results().At(0).Type(), selected.Bound.Associated["Item"]) {
				t.Fatal("projection lost bound identity")
			}
		}
	}
}

func TestTraitRuntimePermissions(t *testing.T) {
	base := `package p
trait CounterOps { func add(self &mut Self, n int) }
type Counter struct { n int }
impl CounterOps for Counter { func add(self &mut Self,n int) { self.n+=n } }
`
	for _, tc := range []struct{ name, body, want string }{
		{"write", `func f(){c:=Counter{};var view CounterOps=&mut c;view.add(1)}`, ""},
		{"readonly", `func f(c &Counter){var view CounterOps=c;view.add(1)}`, check.ReferencePermission},
		{"temporary", `func produce() Counter{return Counter{}};func f(){var view CounterOps=produce();view.add(1)}`, check.ReferenceAddress},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, res := parse(t, base+tc.body)
			_, ds := check.Check(files, res)
			if tc.want == "" {
				if len(ds) != 0 {
					t.Fatal(ds)
				}
				return
			}
			for _, d := range ds {
				if d.Code == tc.want {
					return
				}
			}
			t.Fatalf("wanted %s, got %v", tc.want, ds)
		})
	}
}

func TestTraitRejectsMissingAndAmbiguousImplementations(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"missing", `trait P {func get(self Self) int}; type S struct{};impl P for S {}`, "missing trait member"},
		{"signature", `trait P {func get(self &Self) int};type S struct{};impl P for S{func get(self Self) int{return 0}}`, "signature does not match"},
		{"associated", `trait P {type Item;func get(self Self) Self::Item};type S struct{};impl P for S{func get(self Self) int{return 0}}`, "missing associated type"},
		{"ambiguous", `trait P{func get(self Self)int};trait Q{func get(self Self)int};type S struct{};impl P for S{func get(self Self)int{return 1}};impl Q for S{func get(self Self)int{return 2}};func f(s S)int{return s.get()}`, "ambiguous trait method"},
		{"body", `trait P{func get(self Self)int};type S struct{};impl P for S{func get(self Self)int}`, "impl member requires a body"},
		{"associated-cycle", `trait P{type A;type B;func get(self Self)Self::A};type S struct{};impl P for S{type A=Self::B;type B=Self::A;func get(self Self)Self::A{return 1}}`, "cyclic associated type"},
		{"associated-constraint-permission", `type D struct{N int};trait P{type Item;func get(self Self)Self::Item};type S struct{p &D};impl P for S{type Item=&D;func get(self Self)Self::Item{return self.p}};func mutate[T P[Item=&mut D]](x T){x.get().N=1};func f(s S){mutate(s)}`, "associated reference permissions do not satisfy"},
		{"projection-binding", `trait P{type Item;func get(self Self)Self::Item};func read[T P[Item=int]](x T)T::P[Item=string]::Item{return x.get()}`, "projection binding conflicts"},
		{"unknown-type-member", `enum E{A};func read(x E::Missing)int{return 0}`, "unknown associated type or member path"},
		{"unknown-expression-member", `enum E{A};func read()int{return E::Missing()}`, "unknown associated type or member path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, res := parse(t, "package p;"+tc.source)
			_, ds := check.Check(files, res)
			for _, d := range ds {
				if strings.Contains(d.Message, tc.want) {
					return
				}
			}
			t.Fatalf("wanted %s, got %v", tc.want, ds)
		})
	}
}

func TestTraitEvidenceDoesNotRequireTargetCarrier(t *testing.T) {
	for _, source := range []string{
		`type D struct{}; type Target *D`,
		`type Target interface{}`,
	} {
		files, res := parse(t, "package p;"+source+`;trait P{func get(self Self)int};impl P for Target{func get(self Self)int{return 1}};func read[T P](x T)int{return x.get()};func f(x Target)int{return read(x)}`)
		info, diagnostics := check.Check(files, res)
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		if len(info.FunctionInstances) != 1 {
			t.Fatalf("missing inferred call evidence: %v", info.FunctionInstances)
		}
		for _, instance := range info.FunctionInstances {
			if len(instance.Evidence) != 1 || instance.Evidence[0].Impl == nil {
				t.Fatalf("missing implementation: %+v", instance)
			}
		}
	}
}

func TestTraitForwardAssociatedBindings(t *testing.T) {
	files, res := parse(t, `package p;trait P{type A;type B;func get(self Self)Self::A};type S struct{};impl P for S{type A=Self::B;type B=int;func get(self Self)Self::A{return 1}};func f(s S)int{return s.get()}`)
	info, ds := check.Check(files, res)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, impl := range info.Impls {
		for _, name := range []string{"A", "B"} {
			if !types.Identical(impl.Associated[name], types.Typ[types.Int]) {
				t.Fatalf("%s=%s", name, impl.Associated[name])
			}
		}
	}
}

func TestTraitMacroProjectionHygiene(t *testing.T) {
	for _, qualified := range []bool{false, true} {
		sources := map[string]string{"fixture/main.gox": `package p;trait Source{type Item;func get(self Self)Self::Item};macro_rules! generate{()=>{func Read[T Source](x T)T::Source::Item{return x.get()}};};generate!()`}
		wantFile := "fixture/main.gox"
		if qualified {
			sources = map[string]string{
				"fixture/defs/defs.gox":   `package defs;pub trait Source{type Item;func get(self Self)Self::Item}`,
				"fixture/other/other.gox": `package other;pub trait Source{type Item;func get(self Self)Self::Item}`,
				"fixture/p/macros.gox":    `package p;use fixture::defs as protocol;macro_rules! generate{()=>{func Read[T protocol::Source](x T)T::protocol::Source::Item{return x.get()}};}`,
				"fixture/p/main.gox":      `package p;use fixture::other as protocol;generate!()`,
			}
			wantFile = "fixture/defs/defs.gox"
		}
		files := map[string]*ast.File{}
		var trees []*ast.File
		for name, source := range sources {
			tree, ds := parser.ParseFile(name, []byte(source))
			if len(ds) != 0 {
				t.Fatal(ds)
			}
			files[name] = tree
			trees = append(trees, tree)
		}
		expanded, err := expand.Expand(trees, nil)
		if err != nil {
			t.Fatal(err)
		}
		res := resolve.ResolveExpanded(files, expanded, nil)
		if len(res.Diagnostics) != 0 {
			t.Fatal(res.Diagnostics)
		}
		info, ds := check.Check(files, res)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		if len(info.GenericBounds) != 1 {
			t.Fatalf("bounds=%d", len(info.GenericBounds))
		}
		for fn, bounds := range info.GenericBounds {
			if len(bounds) != 1 || bounds[0].Trait.File != wantFile {
				t.Fatalf("projection resolved outside macro definition: %+v", bounds)
			}
			if got := info.TypeOverrides[fn.Results[0].Type]; !types.Identical(got, bounds[0].Associated["Item"]) {
				t.Fatalf("macro result projection = %v, want associated Item", got)
			}
		}
		for _, selected := range info.Members {
			if selected.Bound != nil {
				if !types.Identical(selected.Signature.Results().At(0).Type(), selected.Bound.Associated["Item"]) {
					t.Fatal("macro projection lost associated identity")
				}
			}
		}
	}
}
