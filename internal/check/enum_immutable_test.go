package check_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestEnumPayloadSlotsAreImmutable(t *testing.T) {
	for name, source := range map[string]string{
		"explicit":       `enum E { Value(int) }; func f(){e:=E::Value(1);match e{E::Value(ref mut value)=>{*value=2}}}`,
		"default":        `enum E { Value(int) }; func f(){e:=E::Value(1);match &mut e{E::Value(value)=>{_=*value}}}`,
		"discard":        `enum E { Value(int) }; func f(){e:=E::Value(1);match &mut e{_=>{}}}`,
		"named-field":    `enum E { Value{item int} }; func f(){e:=E::Value{item:1};match e{E::Value{item:ref mut value}=>{*value=2}}}`,
		"container-slot": `enum E { Value([]int) }; func f(){e:=E::Value([]int{1});match e{E::Value(ref mut value)=>{*value=[]int{2}}}}`,
		"option":         `func f(){e:=Option[int]::Some(1);match &mut e{Option::Some(value)=>{*value=2},Option::None=>{}}}`,
		"result":         `func f(){e:=Result[int,string]::Ok(1);match e{Result::Ok(ref mut value)=>{*value=2},Result::Err(_)=>{}}}`,
		"generic":        `enum E[T any] { Value(T) }; func f[T any](e E[T]){match e{E::Value(ref mut value)=>{_=*value}}}`,
		"nested":         `enum E { Value(int) }; enum Outer { Inside(E) }; func f(e Outer){match e{Outer::Inside(E::Value(ref mut value))=>{_=*value}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+source)
			_, diagnostics := check.Check(files, resolved)
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == check.ReferencePermission && strings.Contains(diagnostic.Message, "enum payload slots are immutable; use a value pattern") {
					return
				}
			}
			t.Fatalf("missing immutable enum payload diagnostic: %v", diagnostics)
		})
	}
}

func TestEnumInherentWritableReceiverIsRejected(t *testing.T) {
	for _, source := range []string{
		`enum E{A,B};impl E{func replace(self &mut Self){*self=E::B}}`,
		`enum E[T any]{A(T),B};impl[T any] E[T]{func replace(self &mut Self){*self=E[T]::B}}`,
	} {
		files, resolved := parse(t, "package p;"+source)
		_, diagnostics := check.Check(files, resolved)
		found := false
		for _, diagnostic := range diagnostics {
			found = found || diagnostic.Code == check.TraitInvalid && strings.Contains(diagnostic.Message, "enum inherent methods cannot use self &mut Self")
		}
		if !found {
			t.Fatalf("missing immutable enum receiver diagnostic: %v", diagnostics)
		}
	}
}

func TestEnumRebindingAndPayloadContainersKeepTheirPermissions(t *testing.T) {
	for name, source := range map[string]string{
		"variable-rebinding":    `enum E{A(int),B};func replace(e &mut E){*e=E::B};func f(){e:=E::A(1);before:=e;replace(&mut e);_=before}`,
		"payload-value-copy":    `enum E{A(int)};func f(){e:=E::A(1);match e{E::A(value)=>{value=2;_=value}}}`,
		"slice-map-pointer":     `type Data struct{Items []int;Index map[string]int;Pointer *int};enum E{A(Data)};func f(e E){match e{E::A(value)=>{value.Items[0]=2;value.Index["x"]=3;*value.Pointer=4}}}`,
		"nested-value":          `enum Inner{A([]int)};enum Outer{Inside(Inner)};func f(e Outer){match e{Outer::Inside(Inner::A(value))=>{value[0]=2}}}`,
		"option-result":         `func f(o Option[[]int],r Result[map[string]int,string]){match o{Option::Some(value)=>{value[0]=2},Option::None=>{}};match r{Result::Ok(value)=>{value["x"]=3},Result::Err(_)=>{}}}`,
		"readonly-projection":   `enum E{A(int)};func f(e E)int{return match &e{E::A(value)=>*value}}`,
		"trait-owner-rebinding": `enum E{A,B};trait Replace{func replace(self &mut Self)};impl Replace for E{func replace(self &mut Self){*self=E::B}};func f(){e:=E::A;Replace::replace(&mut e)}`,
	} {
		t.Run(name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+source)
			_, diagnostics := check.Check(files, resolved)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
		})
	}
}
