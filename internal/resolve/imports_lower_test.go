package resolve_test

import (
	"reflect"
	"testing"

	"github.com/hxsf/goxide/internal/resolve"
)

func TestExplicitImportedItemsKeepTargetGoNames(t *testing.T) {
	importer := testImporter{"fixture::lib": {
		{Name: resolve.Key{Name: "answer"}, Kind: resolve.Function, GoName: "Answer", Visibility: resolve.Public},
		{Name: resolve.Key{Name: "item"}, Kind: resolve.Type, GoName: "Item", Visibility: resolve.Public},
	}}
	for _, tc := range []struct {
		name, use, body, marker, ident, target, goName string
		kind                                           resolve.Kind
	}{
		{"function", "fixture::lib::answer", "_ = answer()", "answer()", "answer", "answer", "Answer", resolve.Function},
		{"function_alias", "fixture::lib::answer as call", "_ = call()", "call()", "call", "answer", "Answer", resolve.Function},
		{"type", "fixture::lib::item", "var value item; _ = value", "value item", "item", "item", "Item", resolve.Type},
		{"type_alias", "fixture::lib::item as alias", "var value alias; _ = value", "value alias", "alias", "item", "Item", resolve.Type},
		{"package_alias", "fixture::lib as lib", "_ = lib::answer()", "lib::answer", "answer", "answer", "Answer", resolve.Function},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package p\nuse " + tc.use + ";\nfunc f() { " + tc.body + " }\n"
			files := parseFiles(t, map[string]string{"p.gox": source})
			result := resolve.Resolve(files, importer)
			noDiagnostics(t, result)
			id := named(t, files["p.gox"], source, tc.marker, tc.ident)
			ref := result.Files["p.gox"].References[id.ID()]
			if ref.Symbol == nil || ref.Symbol.Kind != tc.kind || ref.Symbol.GoName != tc.goName || ref.Deferred {
				t.Fatalf("imported target = %#v, want kind %v and Go name %s", ref, tc.kind, tc.goName)
			}
			if want := []string{"fixture", "lib", tc.target}; !reflect.DeepEqual(ref.Symbol.Target, want) {
				t.Fatalf("target = %v, want %v", ref.Symbol.Target, want)
			}
		})
	}
}

func TestExplicitImportedItemsKeepAmbiguity(t *testing.T) {
	const source = "package p\nuse fixture::lib::item;\nfunc f() { _ = item }\n"
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, testImporter{"fixture::lib": {
		{Name: resolve.Key{Name: "item"}, Kind: resolve.Type, GoName: "Item", Visibility: resolve.Public},
		{Name: resolve.Key{Name: "item"}, Kind: resolve.Function, GoName: "Item", Visibility: resolve.Public},
	}})
	id := named(t, files["p.gox"], source, "_ = item", "item")
	ref := result.Files["p.gox"].References[id.ID()]
	if ref.Symbol != nil || len(ref.Candidates) != 2 {
		t.Fatalf("import ambiguity was lost: %#v", ref)
	}
	if d := diagnosticAt(t, result, "p.gox", id.Span()); d.Code != "ambiguous" {
		t.Fatalf("diagnostic = %v, want ambiguous", d)
	}
}
