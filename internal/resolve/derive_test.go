package resolve_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestConcreteProtocolImplRejectsTypeArguments(t *testing.T) {
	for _, header := range []string{"impl Default[int] for T", "impl Default for T[int]"} {
		source := "package p\ntype T struct{}\n" + header + " { func default() T { return T{} } }"
		files := parseFiles(t, map[string]string{"p.gox": source})
		result := resolve.Resolve(files, nil)
		start := strings.Index(source, header)
		d := diagnosticAt(t, result, "p.gox", ast.Span{Start: uint32(start), End: uint32(len(source))})
		if d.Code != "derive-manual" || len(result.Derives) != 0 {
			t.Fatalf("invalid protocol registered: %#v, %v", result.Derives, result.Diagnostics)
		}
	}
}

type deriveImports struct{ target []string }

func (deriveImports) Lookup([]string, resolve.Key) []resolve.External { return nil }

func (i deriveImports) CanonicalPath(parts []string) ([]string, bool) {
	return i.target, strings.Join(parts, "/") == "routing/Serialize"
}

func TestDeriveNamespaceResolvesCanonicalOwner(t *testing.T) {
	for _, source := range []string{
		`use routing as wire;#[derive(wire::Serialize)]type Record struct{Value int}`,
		`use routing::Serialize as Save;#[derive(Save)]type Record struct{Value int}`,
	} {
		for _, target := range [][]string{{"github.com", "goxide-lang", "serde", "Serialize"}, {"example.org", "pretend", "Serialize"}} {
			files := parseFiles(t, map[string]string{"fixture/main.gox": "package main;" + source})
			result := resolve.Resolve(files, deriveImports{target})
			if target[0] == "github.com" {
				noDiagnostics(t, result)
				if len(result.Derives) != 1 || result.Derives[0].Owner != "github.com/goxide-lang/serde" || result.Derives[0].Name != "Serialize" {
					t.Fatalf("canonical derive owner lost: %#v", result.Derives)
				}
			} else {
				found := false
				for _, diagnostic := range result.Diagnostics {
					found = found || diagnostic.Code == "derive-owner"
				}
				if !found || len(result.Derives) != 0 {
					t.Fatalf("foreign protocol accepted as serde: derives=%#v diagnostics=%v", result.Derives, result.Diagnostics)
				}
			}
		}
	}
}
