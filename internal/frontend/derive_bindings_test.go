package frontend

import (
	"reflect"
	"testing"
)

func TestDeriveDeclarationBindingsRespectPackageScope(t *testing.T) {
	sources := map[string][]byte{
		"native.go": []byte(`package p
import importOnly "strings"
type Native struct{Field int}
func (Native) Method(){}
var ( NativeValue int; Pair, Other int )
var enum int
func macro_rules(){}
const ( NativeConstant = 1; NextConstant )
`),
		"model.gox": []byte("package p\nuse serde;\n" + "const Text = `type Fake struct{}; func FakeFunc(){}`\n" + `
// type Comment struct{}
#[derive(serde::Serialize)]
struct Item { Field int }
pub enum State { Empty, Value(int), }
#[derive(Clone)] type SameLine struct{}
#[derive(Clone)] #[derive(Debug)] pub enum InlineState{Empty,}
type (
 Alias = Item
 Generic[T interface{~int}] struct{Hidden int}
)
var First, Second = 1, 2
func Function[T any](v T)T{type Local struct{}; local:=1;_=local;return v}
macro_rules! helper {()=>{func(){var LocalMacro int;_=LocalMacro}()};}
`),
	}
	got := map[string]string{}
	for name, b := range localDeclarationBindings("example.org/p", sources) {
		if b.OwnerPath != "example.org/p" || b.Name != name {
			t.Fatalf("incorrect lexical identity: %+v", b)
		}
		got[name] = b.Kind
	}
	want := map[string]string{
		"Native": "type", "NativeValue": "variable", "Pair": "variable", "Other": "variable", "NativeConstant": "constant", "NextConstant": "constant",
		"enum": "variable", "macro_rules": "function",
		"Text": "constant", "Item": "type", "State": "type", "Alias": "type", "Generic": "type", "First": "variable", "Second": "variable", "Function": "function", "helper": "macro",
		"SameLine": "type", "InlineState": "type",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("package bindings: %#v; want %#v", got, want)
	}
}
