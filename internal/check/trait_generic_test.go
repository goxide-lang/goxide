package check_test

import (
	"go/types"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestGenericTraitImplInstantiatedEvidence(t *testing.T) {
	files, resolved := parse(t, `package p
trait P { func score(self Self) int }
trait Source { type Item; func get(self Self) Self::Item }
type Box[T any] struct { value T }
impl P for int { func score(self Self) int { return int(self) } }
impl[T any] P for Box[T] where T: P {
 func score(self Self) int { return self.value.score() }
}
impl[T any] Source for Box[T] where T: P {
 type Item = T
 func get(self Self) Self::Item { return self.value }
}
func Score[T P](value T) int { return value.score() }
func Read[T Source](value T) T::Source::Item { return value.get() }
func run() int { b := Box[int]{value: 7}; return Score[Box[int]](b) + Read[Box[int]](b) }
`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	seen := map[string]bool{}
	for _, instance := range info.FunctionInstances {
		if len(instance.TypeArgs) != 1 || len(instance.Evidence) != 1 {
			t.Fatalf("function instance lacks concrete argument or evidence: %+v", instance)
		}
		impl := instance.Evidence[0].Impl
		if impl == nil || impl.Template == nil || !types.Identical(impl.Target, instance.TypeArgs[0]) {
			t.Fatalf("function evidence does not select its instantiated target: %+v", instance)
		}
		seen[impl.Trait.Symbol.Name.Name] = true
	}
	if !seen["P"] || !seen["Source"] {
		t.Fatalf("missing function evidence: %v", seen)
	}
	seen = map[string]bool{}
	for _, instance := range info.ImplInstances {
		named, ok := types.Unalias(instance.Target).(*types.Named)
		if !ok || named.Obj().Name() != "Box" {
			continue
		}
		if named.TypeArgs().Len() != 1 || !types.Identical(named.TypeArgs().At(0), types.Typ[types.Int]) || len(instance.TypeArgs) != 1 || !types.Identical(instance.TypeArgs[0], types.Typ[types.Int]) {
			t.Fatalf("impl target or arguments remain generic: %+v", instance)
		}
		if instance.Template == nil || types.Identical(instance.Template.Target, instance.Target) {
			t.Fatal("impl instance overwrote or retained its generic target")
		}
		if len(instance.Requirements) != 1 || !types.Identical(instance.Requirements[0].Target, types.Typ[types.Int]) || len(instance.RequirementEvidence) != 1 {
			t.Fatalf("impl requirements remain generic: %+v", instance)
		}
		witness := instance.RequirementEvidence[0].Impl
		if witness == nil || witness.Trait.Symbol.Name.Name != "P" || !types.Identical(witness.Target, types.Typ[types.Int]) {
			t.Fatalf("wrong prerequisite evidence: %+v", instance.RequirementEvidence)
		}
		name := instance.Trait.Symbol.Name.Name
		seen[name] = true
		if name == "Source" && (!types.Identical(instance.Associated["Item"], types.Typ[types.Int]) || len(instance.Methods) != 1 || !types.Identical(instance.Methods[0].Signature.Results().At(0).Type(), types.Typ[types.Int])) {
			t.Fatalf("associated type or member signature remains generic: %+v", instance)
		}
	}
	if !seen["P"] || !seen["Source"] {
		t.Fatalf("missing concrete impl instances: %v", seen)
	}
}

func TestGenericTraitContainerTargets(t *testing.T) {
	files, resolved := parse(t, `package p
trait P { func score(self Self) int }
impl P for int { func score(self Self) int { return int(self) } }
impl[T any] P for []T where T: P { func score(self Self) int { return len(self) } }
impl[T any] P for *T where T: P { func score(self Self) int { return 1 } }
impl[K comparable, V any] P for map[K]V where V: P { func score(self Self) int { return len(self) } }
func Score[T P](value T) int { return value.score() }
func run(pointer *int) int {
 return Score[[]int]([]int{1}) + Score[*int](pointer) + Score[map[string]int](map[string]int{"x": 1})
}
`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	seen := map[string]bool{}
	for _, instance := range info.FunctionInstances {
		if len(instance.Evidence) != 1 || instance.Evidence[0].Impl == nil {
			t.Fatalf("container call has no selected impl: %+v", instance)
		}
		impl := instance.Evidence[0].Impl
		if len(impl.RequirementEvidence) != 1 || impl.RequirementEvidence[0].Impl == nil || !types.Identical(impl.RequirementEvidence[0].Impl.Target, types.Typ[types.Int]) {
			t.Fatalf("container prerequisite is not int: %+v", impl)
		}
		seen[types.TypeString(impl.Target, nil)] = true
		if _, ok := impl.Target.(*types.Map); ok && (len(impl.TypeArgs) != 2 || !types.Identical(impl.TypeArgs[0], types.Typ[types.String]) || !types.Identical(impl.TypeArgs[1], types.Typ[types.Int])) {
			t.Fatalf("map arguments = %v, want string, int", impl.TypeArgs)
		}
	}
	for _, target := range []string{"[]int", "*int", "map[string]int"} {
		if !seen[target] {
			t.Errorf("missing container evidence for %s: %v", target, seen)
		}
	}
}

func TestGenericTraitPremises(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"condition leaves type construction unconstrained", `
trait P { func score(self Self) int }
type Box[T any] struct { value T }
impl[T any] P for Box[T] where T: P { func score(self Self) int { return self.value.score() } }
func construct() Box[string] { return Box[string]{value: "unconstrained"} }
`},
		{"qualified multi bound", `
trait P { func score(self Self) int }; trait Q { func score(self Self) int }
impl P for int { func score(self Self) int { return 1 } }
impl Q for int { func score(self Self) int { return 2 } }
func Score[T any](value T) int where T: P + Q { return value.score::<P>() + value.score::<Q>() }
func run() int { return Score[int](1) }
`},
		{"distinct associated bindings", `
trait Number { type Item; func get(self Self) Self::Item }
trait Text { type Item; func get(self Self) Self::Item }
impl Number for int { type Item=int; func get(self Self) Self::Item { return int(self) } }
impl Text for int { type Item=string; func get(self Self) Self::Item { return "value" } }
func Read[T any](value T) (T::Number::Item, T::Text::Item) where T: Number[Item=int] + Text[Item=string] {
 return value.get::<Number>(), value.get::<Text>()
}
func run() (int, string) { return Read[int](7) }
`},
		{"concrete where target", `
trait P { func score(self Self) int }; type Box[T any] struct { value T }
impl P for Box[int] { func score(self Self) int { return self.value } }
func Score[T any](value T) int where Box[int]: P { return Box[int]{value: 3}.score() }
func run() int { return Score[string]("unused") }
`},
		{"composite target projection in signature", `
trait Source { type Item; func get(self Self) Self::Item }
type Box[T any] struct { value T }
impl Source for Box[int] { type Item=int; func get(self Self) Self::Item { return self.value } }
func Read[T any](value Box[T]) Box[T]::Source::Item where Box[T]:Source { return value.get() }
func run() int { return Read[int](Box[int]{value: 3}) }
`},
		{"impl Self premise", `
trait P { func score(self Self) int }; trait Q { func extra(self Self) int }
type Box[T any] struct { value T }
impl P for int { func score(self Self) int { return int(self) } }
impl[T any] P for Box[T] where T: P { func score(self Self) int { return self.value.score() } }
impl[T any] Q for Box[T] where Self: P { func extra(self Self) int { return self.score() } }
func Extra[T Q](value T) int { return value.extra() }
func run() int { return Extra[Box[int]](Box[int]{value: 3}) }
`},
		{"trait Self premise inherited", `
trait Base { func base(self Self) int }
trait Derived where Self: Base { func derived(self Self) int }
impl Base for int { func base(self Self) int { return 1 } }
impl Derived for int { func derived(self Self) int { return 2 } }
func Read[T Derived](value T) int { return value.base() + value.derived() }
func run() int { return Read[int](3) }
`},
		{"concrete native declare", `
use time;
pub trait Stamp { func Unix(self Self) int64 }
declare impl Stamp for time::Time;
func Unix[T Stamp](value T) int64 { return value.Unix() }
func run(value time::Time) int64 { return Unix[time::Time](value) }
`},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p\n"+test.source)
			info, diagnostics := check.Check(files, resolved)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			if test.name == "trait Self premise inherited" {
				for function, bounds := range info.GenericBounds {
					if function.Name.Name == "Read" && len(bounds) != 2 {
						t.Fatalf("Derived did not provide its Base premise: %+v", bounds)
					}
				}
			}
			if test.name == "concrete native declare" {
				if len(info.FunctionInstances) == 0 {
					t.Fatal("native declare has no function instance evidence")
				}
				for _, instance := range info.FunctionInstances {
					if len(instance.Evidence) != 1 || instance.Evidence[0].Impl == nil || !instance.Evidence[0].Impl.Declare {
						t.Fatalf("native declare evidence lost: %+v", instance)
					}
				}
			}
			if test.name == "distinct associated bindings" {
				seen := map[string]bool{}
				for _, bounds := range info.GenericBounds {
					for _, bound := range bounds {
						name := bound.Trait.Symbol.Name.Name
						want := map[string]types.Type{"Number": types.Typ[types.Int], "Text": types.Typ[types.String]}[name]
						if want == nil || !types.Identical(bound.Associated["Item"], want) {
							t.Fatalf("%s Item = %v, want %v", name, bound.Associated["Item"], want)
						}
						seen[name] = true
					}
				}
				if len(seen) != 2 {
					t.Fatalf("distinct Item bindings lost: %v", seen)
				}
			}
		})
	}
}

func TestGenericTraitAssociatedReferencePermissions(t *testing.T) {
	const source = `package p
type D struct { N int }
trait Source { type Item; func get(self Self) Self::Item }
type Box[T any] struct { value &T }
impl[T any] Source for Box[T] {
 type Item = &T
 func get(self Self) Self::Item { return self.value }
}
`
	for _, test := range []struct{ name, body, code, message string }{
		{"instance read", `func read(value Box[D]) int { return value.get().N }`, "", ""},
		{"instance write", `func write(value Box[D]) { value.get().N=1 }`, check.ReferencePermission, "cannot write through"},
		{"instance projection", `func write(value Box[D]) { var x Box[D]::Source::Item=value.get(); x.N=1 }`, check.ReferencePermission, "cannot write through"},
		{"bound projection read", `func Read[T Source[Item=&D]](value T) int { var x T::Source::Item=value.get(); return x.N };func run(value Box[D]) int { return Read[Box[D]](value) }`, "", ""},
		{"bound projection write", `func Write[T Source[Item=&D]](value T) { var x T::Source::Item=value.get(); x.N=1 };func run(value Box[D]) { Write[Box[D]](value) }`, check.ReferencePermission, "cannot write through"},
		{"bound cannot gain write", `func Write[T Source[Item=&mut D]](value T) {};func run(value Box[D]) { Write[Box[D]](value) }`, check.GenericConstraint, "associated reference permissions do not satisfy"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, source+test.body)
			_, diagnostics := check.Check(files, resolved)
			if test.code == "" {
				if len(diagnostics) != 0 {
					t.Fatal(diagnostics)
				}
				return
			}
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == test.code && strings.Contains(diagnostic.Message, test.message) {
					return
				}
			}
			t.Fatalf("want %s containing %q, got %v", test.code, test.message, diagnostics)
		})
	}
}

func TestGenericTraitRejections(t *testing.T) {
	for _, test := range []struct{ name, source, code, message string }{
		{"missing bound", `trait P {}; type Box[T any] struct {}; impl[T any] P for Box[T] where T: P {}; func Accept[T P](x T) {}; func run(){Accept[Box[string]](Box[string]{})}`, check.GenericConstraint, "does not explicitly implement"},
		{"bare type parameter", `trait P {}; impl[T any] P for T {}`, check.TraitInvalid, ""},
		{"unused parameter", `trait P {}; type Box[T any] struct {}; impl[T any, U any] P for Box[T] {}`, check.TraitInvalid, ""},
		{"ambiguous multi bound", `trait P {func score(self Self)int}; trait Q {func score(self Self)int}; func Score[T any](x T)int where T:P+Q{return x.score()}`, check.TraitInvalid, "ambiguous"},
		{"concrete where missing", `trait P {}; type Box[T any] struct {}; func Accept[T any](x T) where Box[int]:P {}; func run(){Accept[int](1)}`, check.GenericConstraint, "does not explicitly implement"},
		{"missing trait prerequisite", `trait Base {}; trait Derived where Self:Base {}; impl Derived for int {}; func Accept[T Derived](x T){}; func run(){Accept[int](1)}`, check.GenericConstraint, "does not explicitly implement"},
		{"trait premise cycle", `trait A where Self:B {}; trait B where Self:A {}`, check.TraitInvalid, "cyclic"},
		{"recursive conditional proof", `trait P {}; trait Q {}; type Box[T any] struct {}; impl[T any] P for Box[T] where Box[T]:Q {}; impl[T any] Q for Box[T] where Box[T]:P {}; func Accept[T P](x T){}; func run(){Accept[Box[int]](Box[int]{})}`, check.GenericConstraint, "does not explicitly implement"},
		{"impl prerequisite cannot gain write", `type D struct{N int};trait Source{type Item};trait P{};type ReadOnly struct{};impl Source for ReadOnly{type Item=&D};type Box[T any] struct{};impl[T any] P for Box[T] where T:Source[Item=&mut D]{};func Accept[T P](x T){};func run(){Accept[Box[ReadOnly]](Box[ReadOnly]{})}`, check.GenericConstraint, "does not explicitly implement"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+test.source)
			_, diagnostics := check.Check(files, resolved)
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == test.code && strings.Contains(diagnostic.Message, test.message) {
					return
				}
			}
			t.Fatalf("want %s containing %q, got %v", test.code, test.message, diagnostics)
		})
	}
}

func TestGenericTraitRepeatedAndDiamondPremises(t *testing.T) {
	const source = `package p
trait Source { type Item; func get(self Self) Self::Item }
impl Source for int { type Item=int; func get(self Self) Self::Item { return int(self) } }
`
	for _, test := range []struct {
		name, declarations string
		boundCount         int
	}{
		{"repeated", `
func First[T Source](x T) T::Source::Item where T:Source { return x.get() }
func Second[T Source](x T) T::Source::Item where T:Source { return x.get() }
`, 1},
		{"diamond", `
trait A where Self:Source {};trait B where Self:Source {}
impl A for int {};impl B for int {}
func First[T any](x T) T::Source::Item where T:A+B { return x.get() }
func Second[T any](x T) T::Source::Item where T:A+B { return x.get() }
`, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, source+test.declarations+`func run() int { return First[int](1)+Second[int](2) }`)
			info, diagnostics := check.Check(files, resolved)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			outputs := map[string]types.Type{}
			for function, bounds := range info.GenericBounds {
				if len(bounds) != test.boundCount {
					t.Fatalf("%s has %d bounds, want %d", function.Name.Name, len(bounds), test.boundCount)
				}
				for _, bound := range bounds {
					if bound.Trait.Symbol.Name.Name == "Source" {
						if outputs[function.Name.Name] != nil || len(bound.Projected) != 1 {
							t.Fatal("repeated Source premise created more than one associated output")
						}
						outputs[function.Name.Name] = bound.Associated["Item"]
					}
				}
			}
			if len(outputs) != 2 || outputs["First"] == outputs["Second"] {
				t.Fatalf("functions share associated output parameters: %v", outputs)
			}
		})
	}
	for _, declarations := range []string{
		`func Bad[T Source[Item=int]](x T) where T:Source[Item=string] {}`,
		`trait A where Self:Source[Item=int] {};trait B where Self:Source[Item=string] {};func Bad[T any](x T) where T:A+B {}`,
	} {
		t.Run("conflict "+declarations, func(t *testing.T) {
			files, resolved := parse(t, source+declarations)
			_, diagnostics := check.Check(files, resolved)
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == check.TraitInvalid && strings.Contains(diagnostic.Message, "conflicting associated") {
					return
				}
			}
			t.Fatalf("different explicit Item bindings were accepted: %v", diagnostics)
		})
	}
}

func TestGenericTraitForwardedAssociatedPermissions(t *testing.T) {
	const source = `package p
type D struct { N int }
trait Source { type Item; func get(self Self) Self::Item }
type Inner struct { value &D }
impl Source for Inner { type Item=&D; func get(self Self) Self::Item { return self.value } }
type Box[T any] struct { value T }
impl[T any] Source for Box[T] where T:Source {
 type Item=T::Source::Item
 func get(self Self) Self::Item { return self.value.get() }
}
`
	for _, test := range []struct {
		name, body string
		write      bool
	}{
		{"read", `func read(value Box[Inner]) int { return value.get().N }`, false},
		{"write", `func write(value Box[Inner]) { value.get().N=1 }`, true},
		{"explicit function result", `func Read[T Source](value T) T::Source::Item { return value.get() };func write(value Inner) { result:=Read[Inner](value);result.N=1 }`, true},
		{"inferred function result", `func Read[T Source](value T) T::Source::Item { return value.get() };func write(value Inner) { result:=Read(value);result.N=1 }`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, source+test.body)
			_, diagnostics := check.Check(files, resolved)
			if !test.write {
				if len(diagnostics) != 0 {
					t.Fatal(diagnostics)
				}
				return
			}
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == check.ReferencePermission && strings.Contains(diagnostic.Message, "cannot write through") {
					return
				}
			}
			t.Fatalf("forwarded associated type gained write permission: %v", diagnostics)
		})
	}
}

func TestGenericTraitOverlapIgnoresWhereAndOrder(t *testing.T) {
	const head = `package p; trait P {}; trait A {}; trait B {}; type Box[T any] struct {};`
	for _, pair := range [][2]string{
		{`impl[T any] P for Box[T] where T:A {}`, `impl[U any] P for Box[U] where U:B {}`},
		{`impl[T any] P for Box[T] {}`, `impl P for Box[int] {}`},
	} {
		for _, reverse := range []bool{false, true} {
			left, right := pair[0], pair[1]
			if reverse {
				left, right = right, left
			}
			t.Run(left+" then "+right, func(t *testing.T) {
				files, resolved := parse(t, head+left+";"+right)
				_, diagnostics := check.Check(files, resolved)
				for _, diagnostic := range diagnostics {
					if diagnostic.Code == check.TraitInvalid && strings.Contains(diagnostic.Message, "overlap") {
						return
					}
				}
				t.Fatalf("overlapping impls were accepted or diagnosed for another reason: %v", diagnostics)
			})
		}
	}
}
