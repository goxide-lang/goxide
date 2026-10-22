package check_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
)

func checkCanonicalTry(t *testing.T, source string) (*check.Info, []check.Diagnostic) {
	t.Helper()
	const standard = `package convert
pub trait From[S any]{func from(value S)Self}
pub trait Into[T any]{func into(self Self)T}
impl[T any]From[T]for T{func from(value T)Self{return value}}
impl[S any,T any]Into[T]for S where T:From[S]{func into(self Self)T{return T::from::<From[S]>(self)}}`
	imports := canonicalImports{"conversion": {
		{Name: resolve.Key{Name: "From"}, Kind: resolve.Trait, GoName: "From", Visibility: resolve.Public, Target: []string{"github.com", "goxide-lang", "std", "convert", "From"}},
		{Name: resolve.Key{Name: "Into"}, Kind: resolve.Trait, GoName: "Into", Visibility: resolve.Public, Target: []string{"github.com", "goxide-lang", "std", "convert", "Into"}},
	}}
	return checkPackageSources(t, map[string]string{
		"github.com/goxide-lang/std/convert/convert.gox": standard,
		"fixture/main.gox": source,
	}, imports)
}

func TestTryCanonicalFromExactApplications(t *testing.T) {
	info, diagnostics := checkCanonicalTry(t, `package p
use conversion::From as ConvertFrom;
type First struct{}
type Second struct{}
type Target struct{}
impl ConvertFrom[First]for Target{func from(value First)Self{return Target{}}}
impl ConvertFrom[Second]for Target{func from(value Second)Self{return Target{}}}
func first(r Result[int,First])Result[int,Target]{return Result[int,Target]::Ok(r?)}
func second(r Result[int,Second])Result[int,Target]{return Result[int,Target]::Ok(r?)}`)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	applications := map[string]bool{}
	for _, trial := range info.Tries {
		if trial.Conversion == nil || trial.Conversion.Impl == nil || len(trial.Conversion.Impl.Inputs) != 1 {
			t.Fatalf("missing frozen conversion: %+v", trial)
		}
		applications[trial.Conversion.Impl.ApplicationKey()] = true
	}
	if len(applications) != 2 {
		t.Fatalf("precise error sources merged: %v", applications)
	}
}

func TestTryAssignmentPrecedesFrom(t *testing.T) {
	info, diagnostics := checkCanonicalTry(t, `package p
use conversion::From;
type Source struct{}
impl From[Source]for any{func from(value Source)Self{return "converted"}}
func run(r Result[int,Source])Result[int,any]{return Result[int,any]::Ok(r?)}`)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for _, trial := range info.Tries {
		if trial.Conversion != nil {
			t.Fatal("assignable error selected a From implementation")
		}
	}
}

func TestTryKeepsDefinitionFromEvidence(t *testing.T) {
	info, diagnostics := checkCanonicalTry(t, `package p
use conversion::From;
func convert[E any,F any](r Result[int,E])Result[int,F] where F:From[E]{return Result[int,F]::Ok(r?)}
func run(r Result[int,int])Result[int,int]{return convert[int,int](r)}`)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for _, trial := range info.Tries {
		if trial.Conversion == nil || trial.Conversion.Evidence.Bound == nil {
			t.Fatalf("generic definition discarded its From obligation: %+v", trial)
		}
	}
}

func TestTryPreservesSourceReferenceApplications(t *testing.T) {
	info, diagnostics := checkCanonicalTry(t, `package p
use conversion::From;
type Target struct{number int}
impl From[&int]for Target{func from(value &int)Self{return Target{number:*value}}}
func convert(r Result[int,&int])Result[int,Target]{return Result[int,Target]::Ok(r?)}
func read(r Result[int,&mut int])Result[int,&int]{return Result[int,&int]::Ok(r?)}`)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	assigned, converted := 0, 0
	for _, trial := range info.Tries {
		if trial.Conversion == nil {
			assigned++
		} else if trial.Conversion.Impl != nil && strings.Contains(trial.Conversion.Impl.ApplicationKey(), "ref") {
			converted++
		}
	}
	if assigned != 1 || converted != 1 {
		t.Fatalf("reference choice: assigned=%d, converted=%d", assigned, converted)
	}
}

func TestTryRejectsNonCanonicalAndIndirectConversions(t *testing.T) {
	for _, test := range []struct{ name, source, message string }{
		{"same spelling", `package p;trait From[S any]{func from(value S)Self};type Source struct{};type Target struct{};impl From[Source]for Target{func from(value Source)Self{return Target{}}};func f(r Result[int,Source])Result[int,Target]{return Result[int,Target]::Ok(r?)}`, "requires"},
		{"Into only", `package p;use conversion::Into;type Source struct{};type Target struct{};impl Into[Target]for Source{func into(self Self)Target{return Target{}}};func f(r Result[int,Source])Result[int,Target]{return Result[int,Target]::Ok(r?)}`, "requires"},
		{"conversion chain", `package p;use conversion::From;type Source struct{};type Middle struct{};type Target struct{};impl From[Source]for Middle{func from(value Source)Self{return Middle{}}};impl From[Middle]for Target{func from(value Middle)Self{return Target{}}};func f(r Result[int,Source])Result[int,Target]{return Result[int,Target]::Ok(r?)}`, "requires"},
		{"interface boxing cannot change source", `package p;use conversion::From;type Source struct{};func(self *Source)Error()string{return "source"};type Target struct{};impl From[error]for Target{func from(value error)Self{return Target{}}};func f(r Result[int,*Source])Result[int,Target]{return Result[int,Target]::Ok(r?)}`, "requires"},
		{"dynamic error cannot change source", `package p;use conversion::From;type Source struct{};func(self *Source)Error()string{return "source"};type Target struct{};impl From[*Source]for Target{func from(value *Source)Self{return Target{}}};func f(r Result[int,error])Result[int,Target]{return Result[int,Target]::Ok(r?)}`, "requires"},
		{"read-only cannot become native pointer", `package p;func f(r Result[int,&int])Result[int,*int]{return Result[int,*int]::Ok(r?)}`, "requires"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostics := checkCanonicalTry(t, test.source)
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == check.TryReturnType && strings.Contains(diagnostic.Message, test.message) {
					return
				}
			}
			t.Fatalf("conversion was accepted or failed for another reason: %v", diagnostics)
		})
	}
}
