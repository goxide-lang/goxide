package lower_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
)

func TestTraitCasesValidateUnusedBranches(t *testing.T) {
	for _, test := range []struct{ name, declaration string }{
		{"function", `func unused[T interface{~int|~string}](value Box[T]) { _ = value.value()-1 }`},
		{"const function", `func unused[T interface{~int|~string},const N int](value Box[T]) { _ = value.value()-1 }`},
		{"impl", `trait Consume{func consume(self Self)};impl[T interface{~int|~string}]Consume for Box[T]{func consume(self Self){_=self.value()-1}}`},
		{"const impl", `trait Consume{func consume(self Self)};impl[T interface{~int|~string},const N int]Consume for [N]Box[T]{func consume(self Self){_=self[0].value()-1}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := `package p
trait Value {type Item;func value(self Self)Self::Item}
type Box[T any] struct {item T}
impl[T interface{~int}] Value for Box[T] {type Item=int;func value(self Self)int{return 1}}
impl[T interface{~string}] Value for Box[T] {type Item=string;func value(self Self)string{return "text"}}
` + test.declaration
			_, err := lowerDeriveSources(map[string]string{"main.gox": source})
			var diagnostic *analysis.Diagnostic
			if !errors.As(err, &diagnostic) || diagnostic.File != "main.gox" || diagnostic.Span.Start == 0 || !strings.Contains(diagnostic.Message, "mismatched types string") {
				t.Fatalf("unused branch did not report source string subtraction: %v", err)
			}
		})
	}
}

func TestConstDefinitionValidationKeepsSymbolicArrayOperations(t *testing.T) {
	source := `package p
func Forward[T any,const N int](value [N]T) [N]T { return value }
func Unused[T comparable,const N int](value [N]T) bool {
 var zero [N]T
 _ = value[200]
 _ = len(value)+cap(zero)+N
 _ = Forward[T,N](value)
 return zero==value
}
func Literal[const N int](value [N]int) int {
 empty := [N]int{}
 values := [N]int{1,2}
 _ = empty[200]
 return values[200]+value[200]
}
func Nested[T any,const M,N int](value [M][N]T) [M][N]T { return value }
`
	output, err := lowerDeriveSources(map[string]string{"main.gox": source})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output["main.gox"]), "goxideValidation") || strings.Contains(string(output["main.gox"]), "Unused") {
		t.Fatalf("definition validation leaked into Go output: %s", output["main.gox"])
	}
}

func TestConstDefinitionValidationRejectsUnusedBodyErrors(t *testing.T) {
	for _, declaration := range []string{
		`func Unused[const N int](value [N]int) { _ = "text"-N }`,
		`func Unused[const N int](value [N]int) { var result string=N; _=result }`,
		`func Unused[const N int](value [N]int) { _ = [N]int{"text"} }`,
		`func Unused[T any,const N int](value [N]T) { _ = value==value }`,
		`func Unused[const N int](value [N]int) { _ = value["text"] }`,
		`func Unused[const N int](value [N]int) { _ = [N]int{1:2,1:3} }`,
		`func Unused[const N int](value [N]int, index int) { _ = [N]int{index:2} }`,
	} {
		t.Run(declaration, func(t *testing.T) {
			_, err := lowerDeriveSources(map[string]string{"unused.gox": "package p\n" + declaration})
			if err == nil {
				t.Fatal("unused const body was not checked")
			}
			if !strings.Contains(err.Error(), "unused.gox") || strings.Contains(err.Error(), "goxideValidation") {
				t.Fatalf("invalid source diagnostic: %v", err)
			}
		})
	}
}

func TestConstDefinitionValidationIncludesNativeDeclarations(t *testing.T) {
	files := map[string][]byte{
		"native.go": []byte(`package fixture
type Native struct { Value int }
func Read(value Native) int { return value.Value }
func Identity[T any](value T) T { return value }
`),
		"source.gox": []byte(`package fixture
func Unused[const N int](value [N]Native) int { return Read(Identity(value[0])) }
`),
	}
	output, err := lower.CompilePackage(files, "fixture", nil, nil, lower.PackageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 1 || output["source.gox"] == nil {
		t.Fatalf("validation changed emitted source selection: %v", output)
	}
}

func TestConstDefinitionValidationDoesNotReserveSourceNames(t *testing.T) {
	source := `package p
const goxideValidationLength1 = 3
type goxideValidationArray1 struct { Value int }
func Unused[const N int](value [N]goxideValidationArray1) int {
 goxideValidationLength2 := goxideValidationLength1
 var goxideValidationArray2 [N]int
 return value[200].Value+goxideValidationArray2[200]+N+goxideValidationLength2
}
`
	if _, err := lowerDeriveSources(map[string]string{"main.gox": source}); err != nil {
		t.Fatal(err)
	}
}

func TestGenericDefinitionValidationRejectsGrowingInstantiation(t *testing.T) {
	source := `package p
trait From[S any] {func from(value S)Self}
impl[T any] From[T] for T {func from(value T)Self{return value}}
func Grow[T any](value T)T where T:From[T] {
 _=Grow[[]T]([]T{value})
 return T::from(value)
}
`
	_, err := lowerDeriveSources(map[string]string{"growing.gox": source})
	if err == nil || !strings.Contains(err.Error(), "instantiation cycle") || !strings.Contains(err.Error(), "growing.gox:") {
		t.Fatalf("generic definition with growing type arguments: %v", err)
	}
}
