package macros

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
)

const fragmentCategoryMessage = "macro type position does not support captured syntax"

func fragmentCategorySpan(t *testing.T, file, source, text string) sourcemap.Span {
	t.Helper()
	start := strings.Index(source, text)
	if start < 0 || strings.Count(source, text) != 1 {
		t.Fatalf("fixture must contain exactly one %q in %s", text, source)
	}
	return sourcemap.Position(file, []byte(source), start, start+len(text))
}

func fragmentCategoryDiagnostic(t *testing.T, err error, file, source, use string, original bool, stack []sourcemap.Expansion) *Diagnostic {
	t.Helper()
	var d *Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("want structured category diagnostic, got %v", err)
	}
	where := fragmentCategorySpan(t, file, source, use)
	where.Start += strings.Index(use, "$")
	where = sourcemap.Position(file, []byte(source), where.Start, where.Start+1)
	if d.Message != fragmentCategoryMessage || d.Position.Filename != file || d.Position.Offset != where.Start || d.End != where.End || d.Position.Line != where.Line || d.Position.Column != where.Column || d.Original != original {
		t.Fatalf("want category error on template $ at %+v (original=%v), got %+v; error: %v", where, original, d, err)
	}
	if !reflect.DeepEqual(d.Stack, stack) {
		t.Fatalf("wrong expansion stack:\n got %+v\nwant %+v", d.Stack, stack)
	}
	return d
}

func TestFragmentCategoryDiagnosticTokenAndStack(t *testing.T) {
	for _, kind := range []string{"expr", "ident", "tt"} {
		for _, newline := range []string{"\n", "\r\n"} {
			t.Run(fmt.Sprintf("%s/%q", kind, newline), func(t *testing.T) {
				definition := "macro_rules! reject {($T:" + kind + ")=>{func()int{\n _=$T\n var value $T\n _=value\n return 0\n}()};}"
				definition = strings.ReplaceAll(definition, "\n", newline)
				source := "package p" + newline + definition + newline + "func Run(){_=reject!(int)}" + newline
				_, err := ExpandWithMap("category.gox", []byte(source))
				stack := []sourcemap.Expansion{{Macro: "reject", Definition: fragmentCategorySpan(t, "category.gox", source, definition), Callsite: fragmentCategorySpan(t, "category.gox", source, "reject!(int)")}}
				fragmentCategoryDiagnostic(t, err, "category.gox", source, "var value $T", false, stack)
			})
		}
	}
}

func TestFragmentCategoryNestedDiagnosticTokenAndStack(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("%q", newline), func(t *testing.T) {
			inner := "macro_rules! inner {($T:expr)=>{func()int{" + newline + "var value $T;_=value;return 0" + newline + "}()};}"
			outer := "macro_rules! outer {($U:expr)=>{inner!($U)};}"
			source := "package p" + newline + inner + newline + outer + newline + "func Run(){_=outer!(int)}" + newline
			stack := []sourcemap.Expansion{
				{Macro: "outer", Definition: fragmentCategorySpan(t, "nested.gox", source, outer), Callsite: fragmentCategorySpan(t, "nested.gox", source, "outer!(int)")},
				{Macro: "inner", Definition: fragmentCategorySpan(t, "nested.gox", source, inner), Callsite: fragmentCategorySpan(t, "nested.gox", source, "inner!($U)")},
			}
			_, err := ExpandWithMap("nested.gox", []byte(source))
			first := fragmentCategoryDiagnostic(t, err, "nested.gox", source, "var value $T", false, stack)
			_, err = ExpandWithMap("nested.gox", []byte(source))
			second := fragmentCategoryDiagnostic(t, err, "nested.gox", source, "var value $T", false, stack)
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("diagnostic changed across identical expansions: %+v / %+v", first, second)
			}
		})
	}
}

func TestFragmentCategoryRepeatedDiagnosticTokenAndStack(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("%q", newline), func(t *testing.T) {
			definition := "macro_rules! reject_many {($($T:expr),*)=>{$(func()int{" + newline + "var value $T;_=value;return 0" + newline + "}());*};}"
			source := "package p" + newline + definition + newline + "func Run(){reject_many!(int,string)}" + newline
			_, err := ExpandWithMap("repeated.gox", []byte(source))
			stack := []sourcemap.Expansion{{Macro: "reject_many", Definition: fragmentCategorySpan(t, "repeated.gox", source, definition), Callsite: fragmentCategorySpan(t, "repeated.gox", source, "reject_many!(int,string)")}}
			fragmentCategoryDiagnostic(t, err, "repeated.gox", source, "var value $T", false, stack)
		})
	}
}

func TestFragmentCategoryPackageDiagnosticTokenAndStack(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("%q", newline), func(t *testing.T) {
			inner := "macro_rules! reject {($T:expr)=>{func()int{" + newline + "var value $T;_=value;return 0" + newline + "}()};}"
			coreSource := "package core" + newline + "pub " + inner + newline
			core := registry(t, "example/core", map[string][]byte{"core.gox": []byte(coreSource)}, nil)
			outer := "macro_rules! outer {($U:expr)=>{c.reject!($U)};}"
			middleSource := "package middle" + newline + "import c \"example/core\"" + newline + "pub " + outer + newline
			middle := registry(t, "example/middle", map[string][]byte{"middle.gox": []byte(middleSource)}, map[string]*Package{"example/core": core})
			callerSource := "package caller" + newline + "import m \"example/middle\"" + newline + "func Run(){type Local int;_=m.outer!(Local)}" + newline
			caller := registry(t, "example/caller", map[string][]byte{"caller.gox": []byte(callerSource)}, map[string]*Package{"example/middle": middle})
			_, err := ExpandPackage("caller.gox", []byte(callerSource), nil, caller, nil)
			stack := []sourcemap.Expansion{
				{Macro: "outer", Definition: fragmentCategorySpan(t, "middle.gox", middleSource, outer), Callsite: fragmentCategorySpan(t, "caller.gox", callerSource, "m.outer!(Local)"), DefinitionOriginal: true},
				{Macro: "reject", Definition: fragmentCategorySpan(t, "core.gox", coreSource, inner), Callsite: fragmentCategorySpan(t, "middle.gox", middleSource, "c.reject!($U)"), DefinitionOriginal: true, CallsiteOriginal: true},
			}
			fragmentCategoryDiagnostic(t, err, "core.gox", coreSource, "var value $T", true, stack)
		})
	}
}

func TestFragmentCategoryOpaqueCapturesExecute(t *testing.T) {
	source := `package main
import "fmt"
macro_rules! identity {($x:expr)=>{$x};}
macro_rules! forward {($x:expr)=>{identity!($x)};}
macro_rules! array {($n:expr)=>{new([$n]int)};}
macro_rules! each {($($x:expr),*)=>{$(Record($x));*};}
macro_rules! empty {($($T:expr),*)=>{func()int{$(var value $T;_=value;)*return 7}()};}
type Local int
var seen []string
func Record(value any){seen=append(seen,fmt.Sprintf("%T",value))}
func main(){
 values:=forward!(make([]Local,2))
 value:=forward!(func(value Local)Local{return value}(Local(3)))
 storage:=array!(len([2]Local{}))
 make:=func(value int)Local{return Local(value+7)}
 callerBound:=identity!(make(2))
 each!(func(value Local)Local{return value}(Local(4)),[]Local{5})
 fmt.Println(len(values),values[0],value,len(*storage),callerBound,seen,empty!())
}
`
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("%q", newline), func(t *testing.T) {
			run(t, strings.ReplaceAll(source, "\n", newline), "2 0 3 2 9 [main.Local []main.Local] 7\n")
		})
	}
}

func TestFragmentCategoryPackageOpaqueCaptureOrigins(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("%q", newline), func(t *testing.T) {
			coreSource := "package core" + newline + "pub macro_rules! identity {($x:expr)=>{$x};}" + newline
			core := registry(t, "example/core", map[string][]byte{"core.gox": []byte(coreSource)}, nil)
			middleSource := "package middle" + newline + "import c \"example/core\"" + newline + "pub macro_rules! forward {($x:expr)=>{c.identity!($x)};}" + newline
			middle := registry(t, "example/middle", map[string][]byte{"middle.gox": []byte(middleSource)}, map[string]*Package{"example/core": core})
			callerSource := "package caller" + newline + "import m \"example/middle\"" + newline + "type Local int" + newline + "func Run()Local{return m.forward!(func(value Local)Local{return value}(Local(3)))}" + newline
			caller := registry(t, "example/caller", map[string][]byte{"caller.gox": []byte(callerSource)}, map[string]*Package{"example/middle": middle})
			first, err := ExpandPackage("caller.gox", []byte(callerSource), nil, caller, nil)
			if err != nil {
				t.Fatal(err)
			}
			second, err := ExpandPackage("caller.gox", []byte(callerSource), nil, caller, nil)
			if err != nil || !bytes.Equal(first.Source, second.Source) || !reflect.DeepEqual(first.Origins, second.Origins) {
				t.Fatalf("opaque capture is not deterministic: %v", err)
			}
			found := 0
			for _, origin := range first.Origins {
				if string(first.Source[origin.OutputStart:origin.OutputEnd]) != "Local" || len(origin.Stack) == 0 {
					continue
				}
				found++
				if origin.File != "caller.gox" || origin.SourceStart < 0 || origin.SourceEnd > len(callerSource) || callerSource[origin.SourceStart:origin.SourceEnd] != "Local" || len(origin.Stack) != 2 || origin.Stack[0].Macro != "forward" || origin.Stack[1].Macro != "identity" {
					t.Fatalf("captured type token lost caller ownership or nested stack: %+v", origin)
				}
			}
			if found != 3 {
				t.Fatalf("want three opaque caller Local tokens, got %d in %s", found, first.Source)
			}
		})
	}
}
