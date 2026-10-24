package compile

import (
	"errors"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/project"
)

func TestStandardConversionRejectsUnboundOrAmbiguousApplications(t *testing.T) {
	compilerSDKForTest(t)
	run := testWASI(t, nil)
	standard := standardConversionManifest(t)
	for _, fixture := range []struct {
		name, declarations, want string
	}{
		{"different-package-is-not-owner", `pub mod foreign;
impl From[foreign::Source] for foreign::Target {func from(value foreign::Source)Self{return Self{}}}`, "allowed local type in its ordered head"},
		{"uncovered-target-parameter", `struct Local[P any]{Value P}
impl[P any] From[Local[P]] for P {func from(value Local[P])Self{return value.Value}}`, "allowed local type in its ordered head"},
		{"parameter-only-in-where", `trait Marker{};struct Source{};struct Target{}
impl[P any] From[Source] for Target where P:Marker {func from(value Source)Self{return Self{}}}`, "impl type parameter must occur in its head: P"},
		{"parameter-only-in-associated-output", `trait Output[S any]{type Item};struct Source{};struct Target{}
impl[P any] Output[Source] for Target {type Item=P}`, "impl type parameter must occur in its head: P"},
		{"parameter-only-in-body", `struct Source{};struct Target{}
impl[P any] From[Source] for Target {func from(value Source)Self{var hidden P;_=hidden;return Self{}}}`, "impl type parameter must occur in its head: P"},
		{"same-member-different-traits", `trait Alternate[S any]{func from(value S)Self};struct Target{}
impl From[int] for Target {func from(value int)Self{return Self{}}}
impl Alternate[int] for Target {func from(value int)Self{return Self{}}}
func f()Target{return Target::from(1)}`, "ambiguous"},
		{"implementation-list-does-not-infer-target", `struct Source{};struct Target{}
impl From[Source] for Target {func from(value Source)Self{return Self{}}}
func f(){_=Source{}.into()}`, "cannot infer complete trait inputs"},
		{"ordinary-call-does-not-search-boxed-source", `struct Source{};func(value *Source)Error()string{return "source"};struct Target{}
impl From[error] for Target {func from(value error)Self{return Self{}}}
func f(value *Source)Target{return Target::from(value)}`, "no explicit implementation for selected trait application"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			writeProjectFile(t, root, project.ManifestName, "[go_mod]\nmodule=\"example.org/standard-boundary\"\ngo=\"1.27\"\n[lib]\nname=\"boundary\"\npath=\".\"\n"+standard)
			writeProjectFile(t, root, "lib.gox", "package boundary\nuse standard::convert::From;\nuse standard::convert::Into;\n"+fixture.declarations+"\n")
			writeProjectFile(t, root, "foreign/lib.gox", "package foreign\npub struct Source{}\npub struct Target{}\n")
			result, err := PrepareProject(root, Options{Lib: true, RunProcMacro: run})
			var diagnostic check.Diagnostic
			if result != nil || !errors.As(err, &diagnostic) || !strings.Contains(diagnostic.Message, fixture.want) {
				t.Fatalf("invalid application was accepted or misdiagnosed: %v, %v; want %q", result, err, fixture.want)
			}
		})
	}
}

func TestStandardConversionProofAndDemandTermination(t *testing.T) {
	compilerSDKForTest(t)
	run := testWASI(t, nil)
	standard := standardConversionManifest(t)
	for _, fixture := range []struct {
		name, declarations, want string
	}{
		{"closed-conversion-cycle", `trait Gate{};struct Box[T any]{Value T};struct Target{}
impl[T any] From[Box[T]] for Target where Box[T]:Gate {func from(value Box[T])Self{return Self{}}}
impl[T any] Gate for Box[T] where Target:From[Box[T]] {}
func f(value Result[int,Box[int]])Result[int,Target]{n:=value?;return Result[int,Target]::Ok(n)}`, "requires"},
		{"growing-conversion-premise", `struct Box[T any]{Value T};struct Target{}
impl[T any] From[Box[T]] for Target where Target:From[Box[[]T]] {func from(value Box[T])Self{return Self{}}}
func f(value Result[int,Box[int]])Result[int,Target]{n:=value?;return Result[int,Target]::Ok(n)}`, "requires"},
		{"growing-conversion-demand", `func Grow[T any](value T)T where T:From[T] {
_=Grow[[]T]([]T{value});return T::from(value)
}
func f()int{return Grow(1)}`, "instantiation cycle"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			writeProjectFile(t, root, project.ManifestName, "[go_mod]\nmodule=\"example.org/standard-termination\"\ngo=\"1.27\"\n[lib]\nname=\"termination\"\npath=\".\"\n"+standard)
			writeProjectFile(t, root, "lib.gox", "package termination\nuse standard::convert::From;\n"+fixture.declarations+"\n")
			var first string
			for range 2 {
				result, err := PrepareProject(root, Options{Lib: true, RunProcMacro: run})
				if result != nil {
					configuredGo(t, result, "build", "-mod=readonly", result.Entry)
				}
				var diagnostic check.Diagnostic
				var definitionDiagnostic *lower.Diagnostic
				validDiagnostic := errors.As(err, &diagnostic) && strings.Contains(diagnostic.Message, fixture.want)
				if fixture.name == "growing-conversion-demand" {
					validDiagnostic = errors.As(err, &definitionDiagnostic) && strings.Contains(err.Error(), fixture.want) && strings.Contains(err.Error(), "lib.gox:")
				}
				if result != nil || !validDiagnostic {
					t.Fatalf("unbounded application was accepted or misdiagnosed: %v, %v; want %q", result, err, fixture.want)
				}
				if first != "" && first != err.Error() {
					t.Fatalf("same proof changed its diagnostic:\n%s\n%s", first, err)
				}
				first = err.Error()
			}
		})
	}
}
