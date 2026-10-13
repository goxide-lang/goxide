package check_test

import (
	"testing"

	"github.com/hxsf/goxide/internal/check"
)

func TestReferenceGenericFunctionCapabilities(t *testing.T) {
	for _, test := range []struct {
		name, source string
		reject       bool
	}{
		{"identity", `func id[T any](value T) T{return value};func f(r &int){copy:=id(r);_=*copy}`, false},
		{"typed-local-copy", `func keep[T any](value T) T{var copied T=value;return copied};func f(r &int){_=keep(r)}`, false},
		{"identity-write", `func id[T any](value T) T{return value};func f(r &int){copy:=id(r);*copy=2}`, true},
		{"interface-erasure", `func hide[T any](value T) any{return value};func f(r &int){_=hide(r)}`, true},
		{"explicit-interface-erasure", `func hide[T any](value T) any{return value};func f(r &int){_=hide[&int](r)}`, true},
		{"instantiated-function-value", `func hide[T any](value T) any{return value};func f(r &int){call:=hide[&int];_=call(r)}`, true},
		{"escaped-function-value", `func hide[T any](value T) any{return value};func f()func(&int)any{return hide[&int]}`, true},
		{"ordinary-pointer", `func hide[T any](value T) any{return value};func f(r *int){_=hide(r)}`, false},
		{"generic-function-field", `type Box[T any] struct{Call func(T)};func f(r &int){b:=Box[&int]{Call:func(p *int){*p=9}};b.Call(r)}`, true},
		{"generic-method-result", `type Box[T any] struct{Value T};func(b Box[T]) Get()T{return b.Value};func f(r &int){b:=Box[&int]{Value:r};got:=b.Get();*got=9}`, true},
		{"generic-method-erasure", `type Box[T any] struct{Value T};func(b Box[T]) Hide()any{return b.Value};func f(r &int){_=Box[&int]{Value:r}.Hide()}`, true},
		{"generic-method-typed-copy", `type Box[T any] struct{Value T};func(b Box[T]) Get()T{var copy T=b.Value;return copy};func f(r &int){_=Box[&int]{Value:r}.Get()}`, false},
		{"generic-enum", `enum Box[T any]{Item(T)};func id[T any](value T) T{return value};func f(r &int){box:=Box[&int]::Item(r);copy:=id(box);match copy{Box::Item(value)=>{_=*value}}}`, false},
		{"generic-enum-write", `enum Box[T any]{Item(T)};func id[T any](value T) T{return value};func f(r &int){box:=Box[&int]::Item(r);copy:=id(box);match copy{Box::Item(value)=>{*value=2}}}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+test.source)
			_, diagnostics := check.Check(files, resolved)
			found := false
			for _, diagnostic := range diagnostics {
				if diagnostic.Code != check.ReferencePermission {
					t.Fatalf("unexpected diagnostic: %v", diagnostic)
				}
				found = true
			}
			if found != test.reject {
				t.Fatalf("permission rejection = %v, want %v: %v", found, test.reject, diagnostics)
			}
		})
	}
}

func TestReferenceClosureUsesCapturedSlots(t *testing.T) {
	for _, test := range []struct {
		name, source string
		reject       bool
	}{
		{"capture-read", `func f(r &[]int){items:=*r;read:=func()int{return items[0]};_=read()}`, false},
		{"late-readonly-write", `func f(r &[]int){items:=[]int{1};write:=func(){items[0]++};items=*r;write()}`, true},
		{"writeback", `func f(r &[]int){items:=[]int{1};install:=func(){items=*r};install();items[0]++}`, true},
		{"writeback-then-replacement", `func f(r &[]int){items:=[]int{1};install:=func(){items=*r};install();items=[]int{2};items[0]++}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+test.source)
			_, diagnostics := check.Check(files, resolved)
			if (len(diagnostics) > 0) != test.reject {
				t.Fatalf("permission rejection = %v, want %v: %v", len(diagnostics) > 0, test.reject, diagnostics)
			}
		})
	}
}

func TestReferenceCopiedContainerElementsKeepPermissions(t *testing.T) {
	for _, test := range []struct {
		name, body string
		reject     bool
	}{
		{"copy-pointer-elements", `out:=make([]*int,1);copy(out,view.Pointers);*out[0]=9`, true},
		{"append-pointer-elements", `out:=append([]*int{},view.Pointers...);*out[0]=9`, true},
		{"copy-value-elements", `out:=make([]int,1);copy(out,view.Values);out[0]=9`, false},
		{"append-value-elements", `out:=append([]int{},view.Values...);out[0]=9`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;type Record struct{Pointers []*int;Values []int};func f(view &Record){"+test.body+"}")
			_, diagnostics := check.Check(files, resolved)
			if (len(diagnostics) > 0) != test.reject {
				t.Fatalf("permission rejection = %v, want %v: %v", len(diagnostics) > 0, test.reject, diagnostics)
			}
		})
	}
}

func TestReferenceJoinedAddressesTrackSlots(t *testing.T) {
	for _, test := range []struct {
		name, tail string
		reject     bool
	}{
		{"later-readonly-values", `left=*r;right=*r;(*pointer)[0]=9`, true},
		{"reborrow-readonly-values", `again:=&*pointer;left=*r;right=*r;(*again)[0]=9`, true},
		{"reborrow-fresh-replacements", `again:=&*pointer;left=*r;right=*r;left=[]int{3};right=[]int{4};(*again)[0]=9`, false},
		{"fresh-replacements", `left=*r;right=*r;left=[]int{3};right=[]int{4};(*pointer)[0]=9`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;func f(r &[]int,b bool){left:=[]int{1};right:=[]int{2};var pointer *[]int;if b {pointer=&left}else{pointer=&right};"+test.tail+"}")
			_, diagnostics := check.Check(files, resolved)
			if (len(diagnostics) > 0) != test.reject {
				t.Fatalf("permission rejection = %v, want %v: %v", len(diagnostics) > 0, test.reject, diagnostics)
			}
		})
	}
}

func TestReferenceJoinedAddressFieldReborrow(t *testing.T) {
	files, resolved := parse(t, `package p;type Record struct{Values []int};func f(view &[]int,b bool){left:=Record{Values:[]int{1}};right:=Record{Values:[]int{2}};var pointer *Record;if b {pointer=&left}else{pointer=&right};again:=&pointer.Values;left.Values=*view;right.Values=*view;(*again)[0]=9}`)
	_, diagnostics := check.Check(files, resolved)
	if len(diagnostics) == 0 {
		t.Fatal("field reborrow erased a readonly slot assignment")
	}
}

func TestReferenceForwardGlobalCopy(t *testing.T) {
	for _, test := range []struct {
		name, source string
		reject       bool
	}{
		{"readonly", `var value int;var copied=view;var view &int=&value;func f(){*copied=9}`, true},
		{"native-pointer", `var value int;var copied=view;var view *int=&value;func f(){*copied=9}`, false},
		{"global-shared-write", `var items []int;func install(view &[]int){items=*view};func write(){items[0]=9}`, true},
		{"global-fresh-write", `var items []int;func install(){items=[]int{1}};func write(){items[0]=9}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+test.source)
			_, diagnostics := check.Check(files, resolved)
			if (len(diagnostics) > 0) != test.reject {
				t.Fatalf("permission rejection = %v, want %v: %v", len(diagnostics) > 0, test.reject, diagnostics)
			}
		})
	}
}
