package check_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestDeclarationEvidenceDoesNotKeepIncompleteNegativeCertificates(t *testing.T) {
	const foundation = `trait Source{type Item};type Seed struct{};impl Source for Seed{type Item=int}
type Alias=Seed::Source::Item
`
	for _, fixture := range []struct{ name, declarations string }{
		{"delayed-impl-head", `trait P{};impl P for int{};impl P for Alias{}`},
		{"delayed-prerequisite-implementation", `trait Q{};trait P{}
impl Q for Alias{}
impl P for int where int:Q{}
impl P for int{}`},
		{"delayed-input-against-identity", `trait From[S any]{}
impl[T any] From[T] for T{}
impl From[Alias] for int{}`},
		{"delayed-native-method", `trait P{};type Row struct{};type Box[T any]struct{Value T}
func(value Row)Get()Alias{return 1}
impl[T interface{Get()int}]P for Box[T]{}
impl P for Box[Row]{}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, source := range []string{foundation + fixture.declarations, fixture.declarations + "\n" + foundation} {
				files, resolved := parse(t, "package p\n"+source)
				_, diagnostics := check.Check(files, resolved)
				found := false
				for _, diagnostic := range diagnostics {
					found = found || diagnostic.Code == check.TraitInvalid && strings.Contains(diagnostic.Message, "overlapping applications")
				}
				if !found {
					t.Fatalf("projection-dependent declaration did not invalidate the incomplete negative proof: %v", diagnostics)
				}
			}
		})
	}
}

func TestDeclarationEvidenceResolvesProjectedSignaturesBeforeBodies(t *testing.T) {
	const foundation = `trait Source{type Item};type Seed struct{};impl Source for Seed{type Item=string}
type Alias=Seed::Source::Item
`
	const declarations = `trait Read{func read(self Self,value Alias)Alias}
type Row struct{}
impl Read for Row{func read(self Self,value Alias)Alias{return value}}
func(value Row)Get()Alias{return "text"}
type Box[T any]struct{Value T};trait P{}
impl[T interface{Get()int}]P for Box[T]{}
impl P for Box[Row]{}
func f(value Row)Alias{return value.read(value.Get())}
`
	for _, source := range []string{foundation + declarations, declarations + foundation} {
		files, resolved := parse(t, "package p\n"+source)
		_, diagnostics := check.Check(files, resolved)
		if len(diagnostics) != 0 {
			t.Fatalf("independently certified projection was not available to signatures: %v", diagnostics)
		}
	}
}

func TestDeclarationEvidenceCannotCertifyItsOwnNativeMethod(t *testing.T) {
	const source = `package p
trait Q{type Item}
type Row struct{}
func(value Row)Get()Row::Q::Item{return 1}
impl Q for Row{type Item=int}
impl[T interface{Get()int}]Q for T{type Item=string}
`
	var first string
	for range 2 {
		files, resolved := parse(t, source)
		_, diagnostics := check.Check(files, resolved)
		if len(diagnostics) == 0 {
			t.Fatal("Q was certified by treating its own deferred Get signature as an absent method")
		}
		var messages []string
		for _, diagnostic := range diagnostics {
			messages = append(messages, diagnostic.Message)
		}
		current := strings.Join(messages, "\n")
		if !strings.Contains(current, "cannot prove") && !strings.Contains(current, "CircularCoherenceDependency") {
			t.Fatalf("recursive declaration evidence lost the uncertified obligation: %v", diagnostics)
		}
		if first != "" && first != current {
			t.Fatalf("recursive declaration evidence changed its diagnostic:\n%s\n%s", first, current)
		}
		first = current
	}
}

func TestDeclarationEvidenceSeparatesParamEnvApplications(t *testing.T) {
	files, resolved := parse(t, `package p
trait Source[S any]{type Item}
type Seed struct{}
impl Source[int]for Seed{type Item=int}
impl Source[string]for Seed{type Item=string}
trait Read{func read(self Self)string}
impl Read for Seed where Seed:Source[int]{
func read(self Self)Seed::Source[string]::Item{return "text"}
}
func f(value Seed)string{return value.read()}
`)
	_, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatalf("a different ParamEnv input was treated as sufficient declaration evidence: %v", diagnostics)
	}
}

func TestDeclarationEvidencePreservesAliasesInLocalApplications(t *testing.T) {
	for _, fixture := range []struct{ name, source string }{
		{"trait-input", `type Identity[T any]=T
trait Source[S any]{type Item;func get(self Self)Self::Source[Identity[S]]::Item}
type Seed struct{}
impl Source[int]for Seed{type Item=int;func get(self Self)int{return 1}}`},
		{"impl-input", `type Identity[T any]=T
trait Source[S any]{type Item;func get(self Self)Self::Item}
type Seed struct{}
impl Source[int]for Seed{type Item=int;func get(self Self)Self::Source[Identity[int]]::Item{return 1}}`},
		{"nested-trait-input", `type Identity[T any]=T
trait Source[S any]{type Item;func get(self Self)Self::Source[Identity[Identity[S]]]::Item}
type Seed struct{}
impl Source[int]for Seed{type Item=int;func get(self Self)int{return 1}}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			files, resolved := parse(t, "package p\n"+fixture.source)
			info, diagnostics := check.Check(files, resolved)
			if len(diagnostics) != 0 {
				for _, trait := range info.Traits {
					for _, member := range trait.Methods {
						t.Logf("trait %s: %s", member.Name, member.Signature)
					}
				}
				for _, impl := range info.Impls {
					for _, member := range impl.Methods {
						t.Logf("impl %s: %s", member.Name, member.Signature)
					}
				}
				t.Fatalf("an alias changed the application supplied by the declaration: %v", diagnostics)
			}
		})
	}
}
