package check_test

import (
	goast "go/ast"
	"go/importer"
	goparser "go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

type callbackImporter struct {
	types.Importer
	native *types.Package
}

func (i callbackImporter) Import(path string) (*types.Package, error) {
	if path == "native" {
		return i.native, nil
	}
	return i.Importer.Import(path)
}

func checkCallbackSources(t *testing.T, source string) []check.Diagnostic {
	t.Helper()
	files := map[string]*ast.File{}
	for name, text := range map[string]string{
		"main.gox": source,
		"codec/lib.gox": `package codec;pub trait Clone{func clone(self &Self)Self};pub trait Ready{func read()(Self,error)};
pub func Read[T Ready]()(T,error){return T::read()};
pub func Identity[T any](value T)T{return value}`,
	} {
		file, diagnostics := parser.ParseFile(name, []byte(text))
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		files[name] = file
	}
	resolved := resolve.Resolve(files, nil)
	if len(resolved.Diagnostics) != 0 {
		t.Fatal(resolved.Diagnostics)
	}
	fset := token.NewFileSet()
	native, err := goparser.ParseFile(fset, "native.go", `package native
func Decode[T any](read func()(T,error))(T,error){return read()}
func Apply[T any](f func(T)T,value T)T{return f(value)}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	config := types.Config{}
	pkg, err := config.Check("native", fset, []*goast.File{native}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, diagnostics := check.CheckWithImporter(files, resolved, callbackImporter{importer.Default(), pkg}, nil)
	return diagnostics
}

func TestTraitCallbackFunctionInstances(t *testing.T) {
	for name, body := range map[string]string{
		"local":     `ticket,_:=native::Decode(Read[Ticket]);_=codec::Clone::clone(&ticket)`,
		"qualified": `ticket,_:=native::Decode(codec::Read[Ticket]);_=codec::Clone::clone(&ticket)`,
		"saved":     `read:=codec::Read[Ticket];ticket,_:=native::Decode(read);_=codec::Clone::clone(&ticket)`,
	} {
		t.Run(name, func(t *testing.T) {
			diagnostics := checkCallbackSources(t, `package p;use codec;use native;
struct Ticket{Value int};
impl codec::Clone for Ticket{func clone(self &Self)Self{return *self}};
impl codec::Ready for Ticket{func read()(Self,error){return Ticket{Value:7},nil}};
func Read[T codec::Ready]()(T,error){return T::read()};
func useCallback(){`+body+`}`)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
		})
	}
}

func TestTraitCallbackFunctionInstancesPreserveReadonly(t *testing.T) {
	for name, callback := range map[string]string{"direct": "codec::Identity[&int]", "saved": "identity"} {
		t.Run(name, func(t *testing.T) {
			diagnostics := checkCallbackSources(t, `package p;use codec;use native;
func useCallback(value &int){identity:=codec::Identity[&int];result:=native::Apply(`+callback+`,value);*result=9}`)
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Message, "read-only") || strings.Contains(diagnostic.Message, "cannot write through") {
					return
				}
			}
			t.Fatalf("callback discarded readonly capability: %v", diagnostics)
		})
	}
}
