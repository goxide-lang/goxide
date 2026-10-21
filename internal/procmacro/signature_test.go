package procmacro_test

import (
	"fmt"
	goast "go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/procmacro"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

type sdkImporter map[string]*types.Package

func (i sdkImporter) Import(path string) (*types.Package, error) {
	if pkg := i[path]; pkg != nil {
		return pkg, nil
	}
	return nil, fmt.Errorf("package %s unavailable", path)
}

func goPackage(t *testing.T, path, source string, importer types.Importer) *types.Package {
	t.Helper()
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "input.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (&types.Config{Importer: importer}).Check(path, set, []*goast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func sdk(t *testing.T) sdkImporter {
	t.Helper()
	return sdkImporter{
		procmacro.TokenPackage: goPackage(t, procmacro.TokenPackage, "package token; type TokenStream struct { tokens []int }", nil),
		procmacro.SDKPackage:   goPackage(t, procmacro.SDKPackage, "package proc_macro; type Context struct { scope int }; type Output struct { tokens []int }", nil),
	}
}

func functionSignature(t *testing.T, body string, importer types.Importer) *types.Signature {
	t.Helper()
	pkg := goPackage(t, "example.org/macros", `package macros
import pm "github.com/goxide-lang/syntax/proc_macro"
import "github.com/goxide-lang/syntax/token"
var _ pm.Context
var _ token.TokenStream
`+body, importer)
	return pkg.Scope().Lookup("entry").Type().(*types.Signature)
}

func TestValidateSignatureUsesCanonicalSDKTypes(t *testing.T) {
	importer := sdk(t)
	for _, test := range []struct {
		name, body string
		kind       pm.Kind
		want       string
	}{
		{"function", `func entry(pm.Context, token.TokenStream) pm.Output { return pm.Output{} }`, pm.Function, ""},
		{"derive", `func entry(pm.Context, token.TokenStream) pm.Output { return pm.Output{} }`, pm.Derive, ""},
		{"attribute", `func entry(pm.Context, token.TokenStream, token.TokenStream) pm.Output { return pm.Output{} }`, pm.Attribute, ""},
		{"aliases", `type C = pm.Context; type T = token.TokenStream; type O = pm.Output
func entry(C, T) O { return O{} }`, pm.Function, ""},
		{"new context type", `type C pm.Context; func entry(C, token.TokenStream) pm.Output { return pm.Output{} }`, pm.Function, "parameter 1"},
		{"pointer context", `func entry(*pm.Context, token.TokenStream) pm.Output { return pm.Output{} }`, pm.Function, "parameter 1"},
		{"new stream type", `type T token.TokenStream; func entry(pm.Context, T) pm.Output { return pm.Output{} }`, pm.Function, "parameter 2"},
		{"attribute item", `func entry(pm.Context, token.TokenStream, string) pm.Output { return pm.Output{} }`, pm.Attribute, "parameter 3"},
		{"new output type", `type O pm.Output; func entry(pm.Context, token.TokenStream) O { return O{} }`, pm.Function, "result"},
		{"wrong arity", `func entry(token.TokenStream) pm.Output { return pm.Output{} }`, pm.Function, "requires func"},
		{"attribute requires args", `func entry(pm.Context, token.TokenStream) pm.Output { return pm.Output{} }`, pm.Attribute, "requires func"},
		{"two results", `func entry(pm.Context, token.TokenStream) (pm.Output, error) { return pm.Output{}, nil }`, pm.Function, "requires func"},
		{"no result", `func entry(pm.Context, token.TokenStream) {}`, pm.Function, "requires func"},
		{"variadic", `func entry(pm.Context, ...token.TokenStream) pm.Output { return pm.Output{} }`, pm.Function, "requires func"},
		{"generic", `func entry[T any](pm.Context, token.TokenStream) pm.Output { return pm.Output{} }`, pm.Function, "requires func"},
	} {
		t.Run(test.name, func(t *testing.T) {
			signature := functionSignature(t, test.body, importer)
			err := procmacro.ValidateSignature(&procmacro.Entry{Name: "entry", Kind: test.kind}, signature, importer)
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateSignatureDoesNotTrustTypeSpelling(t *testing.T) {
	importer := sdk(t)
	other := sdk(t)
	signature := functionSignature(t, `func entry(pm.Context, token.TokenStream) pm.Output { return pm.Output{} }`, other)
	if err := procmacro.ValidateSignature(&procmacro.Entry{Name: "entry", Kind: pm.Function}, signature, importer); err == nil || !strings.Contains(err.Error(), "canonical SDK type") {
		t.Fatalf("different SDK type identity with identical spelling accepted: %v", err)
	}
}

func TestValidateSignatureRequiresSelectedSDK(t *testing.T) {
	importer := sdk(t)
	entry := &procmacro.Entry{Name: "entry", Kind: pm.Function}
	signature := functionSignature(t, `func entry(pm.Context, token.TokenStream) pm.Output { return pm.Output{} }`, importer)
	for _, test := range []struct {
		name string
		imp  types.Importer
		want string
	}{
		{"no importer", nil, "requires the selected SDK"},
		{"missing package", sdkImporter{}, "unavailable"},
		{"wrong package", sdkImporter{procmacro.SDKPackage: goPackage(t, "other.org/sdk", "package sdk; type Context struct{}", nil)}, "identity mismatch"},
		{"missing type", sdkImporter{procmacro.SDKPackage: goPackage(t, procmacro.SDKPackage, "package sdk", nil)}, "missing type Context"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := procmacro.ValidateSignature(entry, signature, test.imp); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
