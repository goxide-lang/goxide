package compile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/project"
)

// Unlike TestParameterizedConversionModulesAndDependencyRecheck, this fixture
// only reexports From/Into from the compiler's pinned, published standard module.
func TestStandardConversionAndPropagationModules(t *testing.T) {
	compilerSDKForTest(t)
	run := testWASI(t, nil)
	standard := standardConversionManifest(t)
	root := t.TempDir()
	protocol, model := filepath.Join(root, "protocol"), filepath.Join(root, "model")
	business, consumer := filepath.Join(root, "business"), filepath.Join(root, "consumer")
	readFixture := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", "conversion_protocol", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	writeProjectFile(t, protocol, project.ManifestName, `[go_mod]
module="example.org/conversion-protocol"
go="1.27"
[lib]
name="bridge"
path="."
`+standard)
	// Elements and Convert are ordinary author-owned APIs. In particular, this
	// bridge does not redeclare the standard traits or their blanket bodies.
	writeProjectFile(t, protocol, "lib.gox", `package bridge
pub mod facade;
pub use standard::convert::From;
pub use standard::convert::Into;
pub trait Elements { type Item; func elements(self Self) []Self::Item }
pub func Convert[T any,S any](value S) T where T:From[S] {
    return T::from::<From[S]>(value)
}
`)
	writeProjectFile(t, protocol, "facade/lib.gox", "package facade\npub use crate::From;\npub use crate::Into;\n")
	writeProjectFile(t, root, "native/go.mod", "module example.org/conversion-native\n\ngo 1.27\n")
	writeProjectFile(t, root, "native/native.go", readFixture("native.go"))
	writeProjectFile(t, root, "native/propagation.go", readFixture("propagation_native.go"))
	for _, module := range []struct {
		directory, name, dependencies string
	}{
		{model, "model", "[dependencies.protocol]\npath=\"../protocol\"\n[dependencies.native]\npath=\"../native\"\n"},
		{business, "business", "[dependencies.protocol]\npath=\"../protocol\"\n[dependencies.model]\npath=\"../model\"\n"},
		{consumer, "consumer", "[dependencies.model]\npath=\"../model\"\n[dependencies.business]\npath=\"../business\"\n[dependencies.native]\npath=\"../native\"\n"},
	} {
		kind := "[lib]"
		if module.name == "consumer" {
			kind = "[[bin]]"
		}
		writeProjectFile(t, module.directory, project.ManifestName, fmt.Sprintf("[go_mod]\nmodule=\"example.org/conversion-%s\"\ngo=\"1.27\"\n%s\nname=%q\npath=\".\"\n", module.name, kind, module.name)+module.dependencies+standard)
	}
	writeProjectFile(t, model, "lib.gox", readFixture("model.gox"))
	writeProjectFile(t, model, "propagation.gox", readFixture("propagation.gox"))
	writeProjectFile(t, business, "lib.gox", readFixture("business.gox"))
	writeProjectFile(t, business, "propagation.gox", readFixture("propagation_business.gox"))
	writeProjectFile(t, consumer, "main.go", strings.Replace(readFixture("consumer.go"), "func verify() {", "func verify() {\n\tverifyPropagation()", 1))
	writeProjectFile(t, consumer, "propagation.go", readFixture("propagation_consumer.go"))
	writeProjectFile(t, consumer, "main_test.go", readFixture("consumer_test.go"))
	library, err := PrepareProject(model, Options{Lib: true, RunProcMacro: run})
	if err != nil {
		t.Fatalf("standard conversion generic library: %v", err)
	}
	configuredGo(t, library, "build", "-mod=readonly", library.Entry)
	var symbols []string
	for _, alias := range []string{"standard", "foundation"} {
		t.Run(alias, func(t *testing.T) {
			if alias != "standard" {
				manifest, err := os.ReadFile(filepath.Join(protocol, project.ManifestName))
				if err != nil {
					t.Fatal(err)
				}
				writeProjectFile(t, protocol, project.ManifestName, strings.ReplaceAll(string(manifest), "dependencies.standard", "dependencies.foundation"))
				source, err := os.ReadFile(filepath.Join(protocol, "lib.gox"))
				if err != nil {
					t.Fatal(err)
				}
				writeProjectFile(t, protocol, "lib.gox", strings.ReplaceAll(string(source), "standard::", "foundation::"))
				writeProjectFile(t, protocol, "facade/lib.gox", "package facade\npub use crate::Into;\npub use crate::From;\n")
			}
			first, err := PrepareProject(consumer, Options{RunProcMacro: run})
			if err != nil {
				t.Fatal(err)
			}
			configuredGo(t, first, "build", "-mod=readonly", "-o", filepath.Join(root, "consumer-bin"), first.Entry)
			if output := configuredGo(t, first, "run", "-mod=readonly", first.Entry); output != "parameterized conversions, private types, views and call frames\n" {
				t.Fatalf("standard conversion consumer output = %q", output)
			}
			configuredGo(t, first, "test", "-mod=readonly", "-count=1", first.Entry)
			before := projectSnapshot(t, filepath.Dir(first.Root))
			current := conversionDeclarationNames(t, before)
			if symbols == nil {
				symbols = current
			} else if !reflect.DeepEqual(symbols, current) {
				t.Fatalf("standard alias/reexport changed canonical identities:\n%v\n%v", symbols, current)
			}
			lock := unchangedConfigLock(t, consumer, nil)
			second, err := PrepareProject(consumer, Options{Offline: true, Locked: true, RunProcMacro: run})
			if err != nil {
				t.Fatal(err)
			}
			if first.Root != second.Root || !reflect.DeepEqual(before, projectSnapshot(t, filepath.Dir(second.Root))) {
				t.Fatal("locked replay changed canonical conversion output")
			}
			unchangedConfigLock(t, consumer, lock)
		})
	}
}

func TestStandardPropagationRejectsNoncanonicalOrIndirectEvidence(t *testing.T) {
	compilerSDKForTest(t)
	run := testWASI(t, nil)
	standard := standardConversionManifest(t)
	for _, fixture := range []struct {
		name, declarations string
	}{
		{"two-hops", `use standard::convert::From;
struct A {}; struct B {}; struct C {}
impl From[A] for B {func from(value A)Self{return Self{}}}
impl From[B] for C {func from(value B)Self{return Self{}}}
func f(value Result[int,A])Result[int,C]{n:=value?;return Result[int,C]::Ok(n)}`},
		{"no-dynamic-error-downcast", `use standard::convert::From;
struct Fault {};func(value *Fault)Error()string{return "fault"};struct Target{}
impl From[*Fault] for Target {func from(value *Fault)Self{return Self{}}}
func f(value Result[int,error])Result[int,Target]{n:=value?;return Result[int,Target]::Ok(n)}`},
		{"no-source-boxing", `use standard::convert::From;
struct Fault {};func(value *Fault)Error()string{return "fault"};struct Target{}
impl From[error] for Target {func from(value error)Self{return Self{}}}
func f(value Result[int,*Fault])Result[int,Target]{n:=value?;return Result[int,Target]::Ok(n)}`},
		{"into-is-not-from", `use standard::convert::Into;
func f[S any,T any](value Result[int,S])Result[int,T] where S:Into[T] {n:=value?;return Result[int,T]::Ok(n)}`},
		{"independent-into-is-not-from", `use standard::convert::Into;
struct Source{};struct Target{}
impl Into[Target] for Source {func into(self Self)Target{return Target{}}}
func f(value Result[int,Source])Result[int,Target]{n:=value?;return Result[int,Target]::Ok(n)}`},
		{"same-spelling-trait-is-not-standard", `trait From[S any]{func from(value S)Self}
struct Source{};struct Target{}
impl From[Source] for Target {func from(value Source)Self{return Self{}}}
func f(value Result[int,Source])Result[int,Target]{n:=value?;return Result[int,Target]::Ok(n)}`},
		{"inherent-from-is-not-standard", `struct Source{};struct Target{}
impl Target {func from(value Source)Self{return Self{}}}
func f(value Result[int,Source])Result[int,Target]{n:=value?;return Result[int,Target]::Ok(n)}`},
		{"no-auto-dereference", `use standard::convert::From;
struct Source{};struct Target{}
impl From[Source] for Target {func from(value Source)Self{return Self{}}}
func f(value Result[int,*Source])Result[int,Target]{n:=value?;return Result[int,Target]::Ok(n)}`},
		{"readonly-is-not-mutable", `use standard::convert::From;
struct Source{};struct Target{}
impl From[&mut Source] for Target {func from(value &mut Source)Self{return Self{}}}
func f(value Result[int,&Source])Result[int,Target]{n:=value?;return Result[int,Target]::Ok(n)}`},
		{"option-is-not-result", `use standard::convert::From;
func f(value Option[int])Result[int,int]{n:=value?;return Result[int,int]::Ok(n)}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			writeProjectFile(t, root, project.ManifestName, "[go_mod]\nmodule=\"example.org/standard-negative\"\ngo=\"1.27\"\n[lib]\nname=\"negative\"\npath=\".\"\n"+standard)
			writeProjectFile(t, root, "lib.gox", "package negative\n"+fixture.declarations+"\n")
			result, err := PrepareProject(root, Options{Lib: true, RunProcMacro: run})
			var diagnostic check.Diagnostic
			if result != nil || !errors.As(err, &diagnostic) || diagnostic.Code != check.TryReturnType {
				t.Fatalf("invalid propagation was accepted or failed before propagation checking: %v, %v", result, err)
			}
		})
	}
}

func TestStandardConversionRejectsConflictsAndSignatures(t *testing.T) {
	compilerSDKForTest(t)
	run := testWASI(t, nil)
	standard := standardConversionManifest(t)
	for _, fixture := range []struct {
		name, declarations string
		overlap            bool
	}{
		{"identity-cannot-be-overridden", `struct Value{}
impl From[Value] for Value {func from(value Value)Self{return value}}`, true},
		{"independent-into-conflicts-with-from", `struct Source{};struct Target{}
impl From[Source] for Target {func from(value Source)Self{return Self{}}}
impl Into[Target] for Source {func into(self Self)Target{return Target{}}}`, true},
		{"alias-does-not-create-a-second-source", `struct Source{};struct Target{};type Alias=Source
impl From[Source] for Target {func from(value Source)Self{return Self{}}}
impl From[Alias] for Target {func from(value Alias)Self{return Self{}}}`, true},
		{"conversion-cannot-return-result", `struct Source{};struct Target{}
impl From[Source] for Target {func from(value Source)Result[Self,error]{return Result[Self,error]::Ok(Self{})}}`, false},
		{"conversion-cannot-return-error-pair", `struct Source{};struct Target{}
impl From[Source] for Target {func from(value Source)(Self,error){return Self{},nil}}`, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			writeProjectFile(t, root, project.ManifestName, "[go_mod]\nmodule=\"example.org/standard-negative\"\ngo=\"1.27\"\n[lib]\nname=\"negative\"\npath=\".\"\n"+standard)
			writeProjectFile(t, root, "lib.gox", "package negative\nuse standard::convert::From;\nuse standard::convert::Into;\n"+fixture.declarations+"\n")
			result, err := PrepareProject(root, Options{Lib: true, RunProcMacro: run})
			var diagnostic check.Diagnostic
			if result != nil || !errors.As(err, &diagnostic) || diagnostic.Code != check.TraitInvalid || fixture.overlap && !strings.Contains(diagnostic.Message, "overlap") {
				t.Fatalf("invalid standard conversion declaration was accepted or misdiagnosed: %v, %v", result, err)
			}
		})
	}
}

func standardConversionManifest(t *testing.T) string {
	t.Helper()
	return pinnedStandardManifest(t)
}
