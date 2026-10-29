package check_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestSymbolicCoherence(t *testing.T) {
	for _, test := range []struct {
		name, source string
		conflict     bool
	}{
		{"underlying partition", `trait P {}; type Box[T any] struct { value T }
impl[T interface{~int}] P for Box[T] {}
impl[T interface{~string}] P for Box[T] {}`, false},
		{"intersecting union", `trait P {}; type Box[T any] struct { value T }
impl[T interface{~int|~string}] P for Box[T] {}
impl[T interface{~string|~bool}] P for Box[T] {}`, true},
		{"shared prerequisites", `trait A {}; trait B {}; trait P {}; type Box[T any] struct { value T }
impl A for int {}; impl B for string {}
impl[T interface{int|string}] P for Box[T] where T:A {}
impl[T interface{int|string}] P for Box[T] where T:B {}`, false},
		{"shared prerequisites overlap", `trait A {}; trait B {}; trait P {}; type Box[T any] struct { value T }
impl A for int {}; impl B for int {}
impl[T interface{int|string}] P for Box[T] where T:A {}
impl[T interface{int|string}] P for Box[T] where T:B {}`, true},
		{"duplicate marker with prerequisite", `trait P {}; trait Q where Self:P {}; type T struct{}
impl P for T {}; impl Q for T {}; impl Q for T {}; impl Q for T {}`, true},
		{"duplicate methods with prerequisite", `trait P {}; trait Q where Self:P {func value(self Self)int}; type T struct{}
impl P for T {}
impl Q for T {func value(self Self)int{return 1}}
impl Q for T {func value(self Self)int{return 2}}`, true},
		{"shared prerequisite reached twice", `trait P {}; trait A where Self:P {}; trait B where Self:P {}; trait Q {}; type T struct{}
impl P for T {}; impl A for T {}; impl B for T {}
impl Q for T where T:A {}; impl Q for T where T:B {}`, true},
		{"future downstream", `trait A {}; trait B {}; trait P {}; type Box[T any] struct { value T }
impl A for int {}; impl B for string {}
impl[T any] P for Box[T] where T:A {}
impl[T any] P for Box[T] where T:B {}`, true},
		{"independent output uniqueness", `trait Q { type Item }; trait P {}; type Box[T any] struct { value T }
impl[T any] P for Box[T] where T:Q[Item=int] {}
impl[T any] P for Box[T] where T:Q[Item=string] {}`, false},
		{"outputs do not disambiguate declarations", `trait P { type Item }; type Box[T any] struct { value T }
impl[T any] P for Box[T] { type Item=int }
impl[T any] P for Box[T] { type Item=string }`, true},
		{"symbolic array length", `trait A {}; trait B {}; trait P {}
impl[T any] A for [2]T {}; impl[T any] B for [3]T {}
impl[T any,const N int] P for [N]T where [N]T:A {}
impl[T any,const N int] P for [N]T where [N]T:B {}`, false},
		{"symbolic equal array length", `trait A {}; trait B {}; trait P {}
impl[T any] A for [2]T {}; impl[T any] B for [2]T {}
impl[T any,const N int] P for [N]T where [N]T:A {}
impl[T any,const N int] P for [N]T where [N]T:B {}`, true},
		{"native signature intersection", `trait P {}; type Box[T any] struct { value T }
impl[T interface{ Get() int }] P for Box[T] {}
impl[T interface{ Get() string }] P for Box[T] {}`, false},
		{"comparable intersection", `trait P {}; type Box[T any] struct { value T }
impl[T comparable] P for Box[T] {}
impl[T interface{~[]int}] P for Box[T] {}`, false},
		{"nominal occurs check", `trait From[S any] {}; type Box[T any] struct { value T }
impl[T any] From[T] for T {}
impl[T any] From[T] for Box[T] {}`, false},
		{"same container identity witness", `trait From[S any] {}
impl[T any] From[T] for T {}
impl[S any,T any] From[[]S] for []T where T:From[S] {}`, true},
		{"source reference constructors", `trait Q[S any] {}; type Box[T any] struct { value T }
impl[T any] Q[&T] for Box[T] {}
impl[T any] Q[&mut T] for Box[T] {}
impl[T any] Q[*T] for Box[T] {}`, false},
		{"closed inductive cycle", `trait A {}; trait B {}; trait P {}; type X struct {}
impl A for X where X:B {}; impl B for X where X:A {}
impl P for int where X:A {}; impl P for int {}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+test.source)
			_, diagnostics := check.Check(files, resolved)
			var conflicts []string
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Message, "overlapping applications") || strings.Contains(diagnostic.Message, "cannot prove trait implementations disjoint") {
					conflicts = append(conflicts, diagnostic.Message)
				} else if test.name != "closed inductive cycle" || !strings.Contains(diagnostic.Message, "prerequisites are not satisfied") {
					t.Errorf("unexpected diagnostic: %v", diagnostic)
				}
			}
			if (len(conflicts) > 0) != test.conflict {
				t.Fatalf("conflicts = %v; want conflict %v", conflicts, test.conflict)
			}
		})
	}
}

func TestSourcePremiseCompleteEvidencePartition(t *testing.T) {
	for _, test := range []struct {
		name, source string
		branches     int
	}{
		{"closed owner premise", `trait Q {}; trait R {func r(self Self)string}; type Box[T any] struct{value T}
impl[T interface{~int}] Q for Box[T] {}; impl[T interface{~string}] Q for Box[T] {}
impl[T interface{~int}] R for Box[T] {func r(self Self)string{return "int"}}
impl[T interface{~string}] R for Box[T] {func r(self Self)string{return "string"}}
func unused[T any](value Box[T])string where Box[T]:Q {return value.r()}`, 2},
		{"complete input application", `trait Q[S any] {}; trait R {func r(self Self)string}; type Box[T any] struct{value T}
impl[T interface{~int}] Q[int] for Box[T] {}; impl[T interface{~string}] Q[int] for Box[T] {}
impl[T interface{~bool}] Q[string] for Box[T] {}
impl[T interface{~int}] R for Box[T] {func r(self Self)string{return "int"}}
impl[T interface{~string}] R for Box[T] {func r(self Self)string{return "string"}}
func unused[T any](value Box[T])string where Box[T]:Q[int] {return value.r()}`, 2},
		{"uncovered source branch", `trait Q {}; trait R {func r(self Self)string}; type Box[T any] struct{value T}
impl[T interface{~int}] Q for Box[T] {}; impl[T interface{~string}] Q for Box[T] {}
impl[T interface{~int}] R for Box[T] {func r(self Self)string{return "int"}}
func unused[T any](value Box[T])string where Box[T]:Q {return value.r()}`, 0},
		{"future downstream premise", `trait Q {}; trait R {func r(self Self)string}; type Box[T any] struct{value T}
impl[T interface{~int}] Q for T {}; impl[T interface{~string}] Q for T {}
impl[T interface{~int}] R for Box[T] {func r(self Self)string{return "int"}}
impl[T interface{~string}] R for Box[T] {func r(self Self)string{return "string"}}
func unused[T any](value Box[T])string where T:Q {return value.r()}`, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+test.source)
			info, diagnostics := check.Check(files, resolved)
			if test.branches == 0 {
				if len(diagnostics) == 0 {
					t.Fatal("incomplete source premise partition was accepted")
				}
				return
			}
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			for declaration, branches := range info.BodyCases {
				if declaration.Name.Name == "unused" && len(branches) == test.branches {
					return
				}
			}
			t.Fatalf("missing %d fully checked source premise branches", test.branches)
		})
	}
}

func TestOpenWorldGenericConversionCoherence(t *testing.T) {
	protocols := `package protocols
pub trait From[S any] {}
pub trait Into[T any] {}
impl[T any] From[T] for T {}
impl[S any,T any] Into[T] for S where T:From[S] {}
`
	for _, extra := range []string{"", "impl[T any] protocols::From[Wrap[T]] for []T {}"} {
		t.Run(map[bool]string{true: "new actual intersection", false: "open generic proof"}[extra != ""], func(t *testing.T) {
			sources := map[string]string{
				"protocols/protocols.gox": protocols,
				"model/model.gox": `package model; use fixture::protocols;
pub type Wrap[T any] struct { value T }
impl[T any] protocols::Into[[]T] for Wrap[T] {}
` + extra,
				"consumer/consumer.gox": `package consumer; use fixture::model; use fixture::protocols;
pub func identity[T any](value T) T { return value }`,
			}
			_, diagnostics := checkSourcePackages(t, sources)
			if extra == "" && len(diagnostics) != 0 {
				t.Fatalf("generic independent Into was rejected: %v", diagnostics)
			}
			if extra != "" {
				found := false
				for _, diagnostic := range diagnostics {
					found = found || strings.Contains(diagnostic.Message, "overlapping applications")
				}
				if !found {
					t.Fatalf("new From did not invalidate independent Into: %v", diagnostics)
				}
			}
		})
	}
}

func TestOrderedTraitInputOwner(t *testing.T) {
	for _, test := range []struct {
		name, declaration string
		allowed           bool
	}{
		{"source owner", `impl api::From[Local[int]] for foreign::Value {}`, true},
		{"pointer source owner", `impl api::From[**Local[int]] for foreign::Value {}`, true},
		{"pointer target owner", `impl api::From[foreign::Value] for **Local[int] {}`, true},
		{"covered preceding parameter", `impl[P any] api::From[Local[P]] for []P {}`, true},
		{"uncovered preceding parameter", `impl[P any] api::From[Local[P]] for P {}`, false},
		{"pointer does not cover parameter", `impl[P any] api::From[Local[P]] for *P {}`, false},
		{"slice does not transfer owner", `impl api::From[foreign::Value] for []Local[int] {}`, false},
		{"nested source owner", `impl api::From[[]Local[int]] for foreign::Value {}`, false},
		{"readonly is not pointer owner", `impl api::From[foreign::Value] for &Local[int] {}`, false},
		{"alias is not owner", `impl api::From[int] for Alias {}`, false},
		{"defined reference is owner", `impl api::From[int] for Reference {}`, true},
		{"local root before uncovered input", `impl[P any] api::From[P] for Local[P] {}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostics := checkSourcePackages(t, map[string]string{
				"api/api.gox":         `package api; pub trait From[S any] {}`,
				"foreign/foreign.gox": `package foreign; pub type Value struct {}`,
				"model/model.gox": `package model; use fixture::api; use fixture::foreign;
type Local[P any] struct { value P }
type Alias = foreign::Value
type Reference &foreign::Value
` + test.declaration,
			})
			if test.allowed {
				if len(diagnostics) != 0 {
					t.Fatal(diagnostics)
				}
			} else {
				found := false
				for _, diagnostic := range diagnostics {
					found = found || strings.Contains(diagnostic.Message, "allowed local type in its ordered head")
				}
				if !found {
					t.Fatalf("missing owner diagnostic: %v", diagnostics)
				}
			}
		})
	}
}

func TestNativeConstraintFutureMethods(t *testing.T) {
	for _, test := range []struct {
		name, native, target string
		unknown, overlap     bool
	}{
		{"missing external method", `pub type Row struct {}`, "native::Row", true, false},
		{"external method added", `pub type Row struct {}; func(self Row)Get()int{return 1}`, "native::Row", false, true},
		{"stable signature contradiction", `pub type Row struct {}; func(self Row)Get()string{return ""}`, "native::Row", false, false},
		{"unnamed external embedding", `pub type Row struct {}`, "struct{native::Row}", true, false},
		{"named external embedding", `pub type Row struct {}`, "Holder", true, false},
		{"promoted external signature", `pub type Row struct {}; func(self Row)Get()string{return ""}`, "Holder", true, false},
		{"promoted method added", `pub type Row struct {}; func(self Row)Get()int{return 1}`, "Holder", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostics := checkSourcePackages(t, map[string]string{
				"api/api.gox":       `package api; pub trait P {}`,
				"native/native.gox": "package native;" + test.native,
				"model/model.gox": `package model; use fixture::api; use fixture::native;
type Box[T any] struct { value T }
type Holder struct {native::Row}
impl[T interface{Get()int}] api::P for Box[T] {}
impl api::P for Box[` + test.target + `] {}`,
			})
			unknown, overlap := false, false
			for _, diagnostic := range diagnostics {
				unknown = unknown || strings.Contains(diagnostic.Message, "cannot prove trait implementations disjoint")
				overlap = overlap || strings.Contains(diagnostic.Message, "overlapping applications")
			}
			if unknown != test.unknown || overlap != test.overlap || !unknown && !overlap && len(diagnostics) != 0 {
				t.Fatalf("unknown=%v overlap=%v; diagnostics=%v", unknown, overlap, diagnostics)
			}
		})
	}
}

func TestCoherenceUnknownDoesNotBecomeDisjoint(t *testing.T) {
	for _, test := range []struct{ name, source, reason string }{
		{"growing prerequisite", `trait Q {}; trait P {}; type Box[T any] struct{value T}
impl[T any] Q for Box[T] where Box[[]T]:Q {}
impl[T any] P for Box[T] where Box[T]:Q {}
impl[T any] P for Box[T] {}`, "budget exhausted"},
		{"circular output certification", `trait Q {type Item}; trait R {type Item}; type Box[T any] struct{value T}
impl[T any] Q for Box[T] where T:R[Item=int] {type Item=int}
impl[T any] Q for Box[T] where T:R[Item=string] {type Item=string}
impl[T any] R for Box[T] where T:Q[Item=int] {type Item=int}
impl[T any] R for Box[T] where T:Q[Item=string] {type Item=string}`, "CircularCoherenceDependency"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+test.source)
			_, diagnostics := check.Check(files, resolved)
			found := false
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Message, "cannot prove trait implementations disjoint") && strings.Contains(diagnostic.Message, test.reason) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing bounded Unknown %q: %v", test.reason, diagnostics)
			}
		})
	}
}

func checkSourcePackages(t *testing.T, sources map[string]string) (*check.Info, []check.Diagnostic) {
	t.Helper()
	files := map[string]*ast.File{}
	root := t.TempDir()
	for relative, source := range sources {
		name := filepath.Join(root, relative)
		file, diagnostics := parser.ParseFile(name, []byte(source))
		if len(diagnostics) != 0 {
			t.Fatalf("%s: %v", relative, diagnostics)
		}
		files[name] = file
	}
	resolved := resolve.Resolve(files, nil)
	if len(resolved.Diagnostics) != 0 {
		t.Fatal(resolved.Diagnostics)
	}
	return check.Check(files, resolved)
}
