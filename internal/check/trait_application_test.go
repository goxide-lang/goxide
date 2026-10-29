package check_test

import (
	"go/types"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestTraitApplicationInputs(t *testing.T) {
	const source = `package p
trait From[S any]{func from(value S)Self}
trait Into[T any]{func into(self Self)T}
trait Source[T any]{type Item;func get(self Self)Self::Item}
type Value struct{n int}
impl From[int] for Value{func from(value int)Self{return Value{n:value}}}
impl From[string] for Value{func from(value string)Self{return Value{n:len(value)}}}
impl Into[int] for Value{func into(self Self)int{return self.n}}
impl Into[string] for Value{func into(self Self)string{return "value"}}
impl Source[int] for Value{type Item=int;func get(self Self)int{return self.n}}
impl Source[string] for Value{type Item=string;func get(self Self)string{return "value"}}
func Known[T Into[int]](value T)int{return value.into()}
func Both[T any](value T)(T::Source[int]::Item,T::Source[string]::Item) where T:Source[int]+Source[string] {
 return value.get::<Source[int]>(),value.get::<Source[string]>()
}
func run(){
 a:=Value::from(3)
 b:=Value::from::<From[string]>("four")
 var integer int=a.into()
 var text string=b.into()
 f:=Value::from::<From[int]>;
 var g func(int)Value=Value::from
 _=f(integer);_=g(len(text));_=Known(a)
 _,_=Both(a)
 var view Into[int]=a
 _=view.into()
}`
	files, resolved := parse(t, source)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	keys := map[string]bool{}
	for _, impl := range info.Impls {
		key := impl.ApplicationKey()
		if keys[key] {
			t.Fatalf("applications share a key: %s", key)
		}
		keys[key] = true
	}
	for _, bounds := range info.GenericBounds {
		if len(bounds) == 2 && bounds[0].Trait == bounds[1].Trait && bounds[0].ApplicationKey() == bounds[1].ApplicationKey() {
			t.Fatal("different inputs merged in ParamEnv")
		}
	}
}

func TestTraitApplicationRequiresKnownInputs(t *testing.T) {
	const source = `package p
trait Into[T any]{func into(self Self)T}
type Value struct{}
impl Into[int] for Value{func into(self Self)int{return 1}}
func run(value Value){_=value.into()}`
	files, resolved := parse(t, source)
	_, diagnostics := check.Check(files, resolved)
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "cannot infer complete trait inputs") {
			return
		}
	}
	t.Fatalf("implementation inventory inferred an unknown input: %v", diagnostics)
}

func TestTraitApplicationInfersTrailingFunctionInputs(t *testing.T) {
	files, resolved := parse(t, `package p
trait From[S any]{func from(value S)Self}
type hidden struct{number int}
impl From[hidden] for int{func from(value hidden)Self{return value.number}}
func Convert[T any,S any](value S)T where T:From[S]{return T::from(value)}
func run()int{return Convert[int](hidden{number:3})}`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for node, instance := range info.FunctionInstances {
		if _, call := node.(*ast.CallExpr); call && len(instance.TypeArgs) == 2 && types.Identical(instance.TypeArgs[0], types.Typ[types.Int]) && len(instance.Evidence) == 1 {
			return
		}
	}
	t.Fatal("partial explicit call did not retain both type arguments and trait evidence")
}

func TestTraitApplicationOwnerIdentityBlanket(t *testing.T) {
	const source = `package p;trait From[S any]{func from(value S)Self};impl[T any] From[T] for T{func from(value T)Self{return value}};func run()int{return int::from::<From[int]>(3)}`
	files, resolved := parse(t, source)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for _, selection := range info.Members {
		if selection.Impl != nil && types.Identical(selection.Impl.Target, types.Typ[types.Int]) && len(selection.Impl.Inputs) == 1 {
			return
		}
	}
	t.Fatal("identity blanket did not bind both head positions")
}

func TestTraitApplicationChecksEveryDefinitionCase(t *testing.T) {
	const source = `package p
trait Value{func value(self Self)int}
type Box[T any] struct{item T}
impl[T interface{~int}] Value for Box[T]{func value(self Self)int{return 1}}
impl[T interface{~string}] Value for Box[T]{func value(self Self)int{return 2}}
func unused[T interface{~int|~string}](value Box[T])int{return value.value()}`
	files, resolved := parse(t, source)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for declaration, branches := range info.BodyCases {
		if declaration.Name.Name != "unused" {
			continue
		}
		if len(branches) != 2 {
			t.Fatalf("got %d definition branches, want 2", len(branches))
		}
		for _, branch := range branches {
			if branch.Info == nil || len(branch.TypeParams) != 1 || len(branch.GoConstraints) == 0 {
				t.Fatalf("incomplete branch: %#v", branch)
			}
			ast.Walk(declaration, func(node ast.Node) bool {
				if expr, ok := node.(ast.Expr); ok {
					if selection := branch.Info.Members[expr]; selection != nil && len(selection.Evidence.Cases) != 0 {
						t.Fatal("leaf retained an unresolved evidence choice")
					}
				}
				return true
			})
		}
		return
	}
	t.Fatal("unused generic body was not checked for every evidence leaf")
}

func TestTraitApplicationPropagatesDefinitionCases(t *testing.T) {
	files, resolved := parse(t, `package p
trait Value{func value(self Self)int}
trait Action{func first(self Self)int;func second(self Self)int}
type Box[T any]struct{item T}
type Envelope[T interface{~int|~string}]struct{item Box[T]}
impl[T interface{~int}]Value for Box[T]{func value(self Self)int{return 1}}
impl[T interface{~string}]Value for Box[T]{func value(self Self)int{return 2}}
impl[T interface{~int|~string}]Action for Envelope[T]{func first(self Self)int{return self.item.value()};func second(self Self)int{return self.item.value()}}
func Outer[T interface{~int|~string}](box Box[T])int{return Read(box)}
func Read[T interface{~int|~string}](box Box[T])int{return box.value()}
func View[T interface{~int|~string}](value Envelope[T])Action{return value}`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	wanted := map[string]bool{"Outer": false, "Read": false, "View": false}
	for declaration, cases := range info.BodyCases {
		if _, ok := wanted[declaration.Name.Name]; ok {
			if len(cases) != 2 {
				t.Fatalf("%s has %d cases, want 2", declaration.Name.Name, len(cases))
			}
			wanted[declaration.Name.Name] = true
		}
	}
	for name, checked := range wanted {
		if !checked {
			t.Errorf("%s did not propagate the callee or runtime-view evidence cases", name)
		}
	}
}

func TestTraitApplicationPreservesCalleeDiagnostics(t *testing.T) {
	files, resolved := parse(t, `package p
trait Value{func value(self Self)int}
trait Into[U any]{func into(self Self)U}
type Item struct{}
impl Into[int]for Item{func into(self Self)int{return 0}}
type Box[T any]struct{item T}
impl[T interface{~int}]Value for Box[T]{func value(self Self)int{return 1}}
impl[T interface{~string}]Value for Box[T]{func value(self Self)int{return 2}}
func Outer[T interface{~int|~string}](box Box[T])int{_=Bad(box);return box.value()}
func Bad[T any](value T)int{_=Item{}.into();return 0}`)
	_, diagnostics := check.Check(files, resolved)
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "cannot infer complete trait inputs") {
			return
		}
	}
	t.Fatalf("partition discovery lost a checked callee's diagnostic: %v", diagnostics)
}

func TestTraitApplicationCaseConditionsPreserveSourcePermissions(t *testing.T) {
	files, resolved := parse(t, `package p
trait Value{func value(self Self)int}
trait Into[U any]{func into(self Self)U}
type Box[T any]struct{item T}
impl[T interface{~int}]Value for Box[T]{func value(self Self)int{return 1}}
impl[T interface{~string}]Value for Box[T]{func value(self Self)int{return 2}}
impl Into[int]for &int{func into(self Self)int{return *self}}
func Read[T interface{~int|~string},S any](box Box[T],source S)int where S:Into[int]{_=source.into();return box.value()}
func run(read &int)int{return Read(Box[int]{},read)}`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for _, instance := range info.FunctionInstances {
		if instance.Case != nil && instance.Case.Decl.Name.Name == "Read" {
			return
		}
	}
	t.Fatal("concrete reference call did not select its checked definition branch")
}

func TestTraitApplicationInheritedInputsPreserveSourcePermissions(t *testing.T) {
	files, resolved := parse(t, `package p
trait Into[U any]{func into(self Self)U}
trait Read[S any] where Self:Into[S]{}
type Value struct{pointer &int}
impl Into[&int]for Value{func into(self Self)&int{return self.pointer}}
impl Read[&int]for Value{}
func read[T Read[&int]](value T)&int{return value.into::<Into[&int]>()}
func run(pointer &int)&int{return read(Value{pointer:pointer})}`)
	_, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
}

func TestTraitApplicationProjectionInTypeDeclaration(t *testing.T) {
	files, resolved := parse(t, `package p
trait Source[S any]{type Item;func get(self Self)Self::Item}
type Value struct{}
impl Source[int]for Value{type Item=int;func get(self Self)int{return 1}}
type Number=Value::Source[int]::Item
type Alias=Number
type Envelope struct{number Alias}
enum Item{Value(Alias)}
func run()Alias{return Envelope{number:1}.number}`)
	_, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
}

func TestTraitApplicationProjectionPreservesRecursiveNominalIdentity(t *testing.T) {
	files, resolved := parse(t, `package p
trait Source[S any]{type Item}
type Value struct{}
type Holder struct{next Value::Source[int]::Item}
impl Source[int]for Value{type Item=*Holder}
func makeHolder()*Holder{return &Holder{}}`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for symbol, typ := range info.Symbols {
		if symbol.Name.Name != "Holder" {
			continue
		}
		named, ok := typ.(*types.Named)
		if !ok {
			continue
		}
		structure, ok := named.Underlying().(*types.Struct)
		if !ok || structure.NumFields() != 1 {
			t.Fatalf("Holder incomplete: %v", typ)
		}
		pointer, ok := structure.Field(0).Type().(*types.Pointer)
		if !ok || !types.Identical(pointer.Elem(), named) {
			t.Fatalf("recursive field lost nominal identity: %v", structure)
		}
		return
	}
	t.Fatal("Holder type missing")
}

func TestTraitApplicationDeclarationProjectionsUseLocalEvidence(t *testing.T) {
	for _, source := range []string{
		`trait Source[S any]{type Item;func get(self Self)Self::Source[S]::Item}
type Value struct{}
impl Source[int]for Value{type Item=int;func get(self Self)Self::Source[int]::Item{return 1}}`,
		`trait Source{type Item};type Box[T any]struct{value T}
impl[T any]Source for Box[T] where Box[T]:Source{type Item=Box[T]::Source::Item}`,
		`trait Source{type Item};type Box[T any]struct{value T}
trait Read[S any]where Box[S]:Source{func read(self Self)Box[S]::Source::Item}`,
		`trait Source{type Item};type Value struct{}
impl Source for Value{type Item=interface{Get()int}}
trait Use[T Value::Source::Item]{}
type Box[T Value::Source::Item]struct{value T}
enum Choice[T Value::Source::Item]{Some(T),None}`,
	} {
		files, resolved := parse(t, "package p\n"+source)
		_, diagnostics := check.Check(files, resolved)
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
	}
}

func TestTraitApplicationNativePointerReceiverCandidates(t *testing.T) {
	const base = `package p
trait Count[S any]{func count(self &Self)S}
type Box[T any]struct{value T}
impl[T any]Count[int]for Box[T]{func count(self &Self)int{return 1}}
`
	for _, fixture := range []struct {
		name, declarations string
		reject             bool
	}{
		{"native pointer adapts", `func f(value *Box[int])int{return value.count::<Count[int]>()}`, false},
		{"qualified explicit pointer adapts", `func f()int{value:=Box[int]{};return Count[int]::count(&value)}`, false},
		{"native pointer ambiguity", `impl[T any]Count[int]for *Box[T]{func count(self &Self)int{return 2}};func f(value *Box[int])int{return value.count::<Count[int]>()}`, true},
		{"source borrow selects value", `impl[T any]Count[int]for *Box[T]{func count(self &Self)int{return 2}};func f(value &Box[int])int{return value.count::<Count[int]>()}`, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			files, resolved := parse(t, base+fixture.declarations)
			_, diagnostics := check.Check(files, resolved)
			if (len(diagnostics) != 0) != fixture.reject {
				t.Fatalf("diagnostics=%v, reject=%v", diagnostics, fixture.reject)
			}
			if fixture.reject && !strings.Contains(diagnostics[0].Message, "ambiguous trait method") {
				t.Fatal(diagnostics)
			}
		})
	}
}

func TestTraitApplicationStoredTupleResultsKeepSourceTypes(t *testing.T) {
	const declarations = `package p
trait Mark{}
impl Mark for int{}
impl Mark for &int{}
impl Mark for &mut int{}
func keep[T Mark](value T)T{return value}
func values(read &int,write &mut int)(int,&int,&mut int){return 1,read,write}
`
	for _, binding := range []string{"number,read,write:=values(r,w)", "var number,read,write=values(r,w)"} {
		for _, mutateRead := range []bool{false, true} {
			body := `func run(r &int,w &mut int){` + binding + `;_=keep(number);reader:=keep(read);writer:=keep(write);*writer=2;_=*reader`
			if mutateRead {
				body += `;*reader=3`
			}
			files, resolved := parse(t, declarations+body+`}`)
			_, diagnostics := check.Check(files, resolved)
			if (len(diagnostics) != 0) != mutateRead {
				t.Fatalf("binding=%s mutate=%v diagnostics=%v", binding, mutateRead, diagnostics)
			}
			for _, diagnostic := range diagnostics {
				if diagnostic.Code != check.ReferencePermission {
					t.Fatal(diagnostics)
				}
			}
		}
	}
}

func TestTraitApplicationGenericFieldCompositeBounds(t *testing.T) {
	files, resolved := parse(t, `package p
trait Serialize{func serialize(self &Self)int}
func Write[T Serialize](value &T)int{return Serialize::serialize(value)}
type Packet[T any]struct{Values []T;Pair [2]T}
impl[T any]Serialize for Packet[T] where []T:Serialize,[2]T:Serialize{
func serialize(self &Self)int{
 first:=func()int{return Write(&self.Values)}
 second:=func()int{return Write(&self.Pair)}
 return first()+second()
}}
`)
	_, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
}

func TestTraitApplicationExpectedSourcePermissions(t *testing.T) {
	for _, test := range []struct {
		name   string
		body   string
		reject bool
	}{
		{"read method value", `impl From[&int] for Value{func from(value &int)Self{return Value{}}};func run(){var f func(&int)Value=Value::from;_=f}`, false},
		{"mutable method value", `impl From[&mut int] for Value{func from(value &mut int)Self{return Value{}}};func run(){var f func(&mut int)Value=Value::from;_=f}`, false},
		{"native cannot satisfy read method value", `impl From[*int] for Value{func from(value *int)Self{return Value{}}};func run(){var f func(&int)Value=Value::from;_=f}`, true},
		{"native cannot satisfy mutable method value", `impl From[*int] for Value{func from(value *int)Self{return Value{}}};func run(){var f func(&mut int)Value=Value::from;_=f}`, true},
		{"read result", `impl Into[&int] for Value{func into(self Self)&int{return &self.number}};func run(value Value)&int{return value.into()}`, false},
		{"mutable result", `impl Into[&mut int] for Value{func into(self Self)&mut int{return &mut self.number}};func run(value Value)&mut int{return value.into()}`, false},
		{"native cannot satisfy read result", `impl Into[*int] for Value{func into(self Self)*int{return &self.number}};func run(value Value)&int{return value.into()}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, `package p;trait From[S any]{func from(value S)Self};trait Into[T any]{func into(self Self)T};type Value struct{number int};`+test.body)
			_, diagnostics := check.Check(files, resolved)
			if (len(diagnostics) != 0) != test.reject {
				t.Fatalf("diagnostics=%v, want rejection=%v", diagnostics, test.reject)
			}
		})
	}
}

func TestTraitApplicationInfersOrdinaryCallShapes(t *testing.T) {
	files, resolved := parse(t, `package p
trait Collect[S any]{func collect(values ...S)Self}
trait Pair[A any,B any]{func pair(first A,second B)Self}
type Value struct{}
impl Collect[int] for Value{func collect(values ...int)Self{return Value{}}}
impl Pair[&int,&mut int] for Value{func pair(first &int,second &mut int)Self{return Value{}}}
func two(read &int,write &mut int)(&int,&mut int){return read,write}
func run(read &int,write &mut int){_=Value::collect(1,2);_=Value::collect([]int{1,2}...);_=Value::pair(two(read,write))}`)
	_, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
}

func TestTraitApplicationDefinitionSignaturesFollowCases(t *testing.T) {
	files, resolved := parse(t, `package p
trait Source{type Item;func get(self Self)Self::Item}
type Box[T any]struct{item T}
impl[T interface{~int}]Source for Box[T]{type Item=int;func get(self Self)int{return 1}}
impl[T interface{~string}]Source for Box[T]{type Item=string;func get(self Self)string{return "one"}}
func Read[T interface{~int|~string}](box Box[T])Box[T]::Source::Item{return box.get()}
func run(){var number int=Read(Box[int]{});var text string=Read(Box[string]{});_=number;_=text}`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	callResults := map[string]bool{}
	for node, instance := range info.FunctionInstances {
		if instance.Case == nil {
			continue
		}
		if call, ok := node.(*ast.CallExpr); ok {
			callResults[info.Types[call].String()] = true
		}
	}
	if !callResults["int"] || !callResults["string"] {
		t.Fatalf("concrete calls retained provisional result types: %v", callResults)
	}
	for declaration, cases := range info.BodyCases {
		if declaration.Name.Name != "Read" {
			continue
		}
		if len(cases) != 2 {
			t.Fatalf("got %d signature cases, want 2", len(cases))
		}
		results := map[string]bool{}
		for _, branch := range cases {
			for symbol, typ := range branch.Info.Symbols {
				if symbol.Name.Name == "Read" {
					results[typ.(*types.Signature).Results().At(0).Type().String()] = true
				}
			}
		}
		if !results["int"] || !results["string"] {
			t.Fatalf("branch signatures retained provisional associated results: %v", results)
		}
		return
	}
	t.Fatal("signature projection did not partition the entire definition")
}
