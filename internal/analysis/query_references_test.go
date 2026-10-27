package analysis_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
)

func applyQueryEdits(t *testing.T, result *analysis.Result, edits []analysis.SourceEdit) map[string][]byte {
	t.Helper()
	sources := map[string][]byte{}
	for key, source := range result.Sources {
		sources[strings.TrimPrefix(key, "fixture/")] = append([]byte(nil), source.Content...)
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].Span.Start > edits[j].Span.Start })
	for _, edit := range edits {
		name := strings.TrimPrefix(edit.File, "fixture/")
		text := sources[name]
		sources[name] = []byte(string(text[:edit.Span.Start]) + edit.NewText + string(text[edit.Span.End:]))
	}
	return sources
}

func TestRenameTraitMethodKeepsAllImplementationsConsistent(t *testing.T) {
	text := "package fixture\ntrait P{func value(self Self)int}\ntype A struct{}\ntype B struct{}\nimpl P for A{func value(self Self)int{return 20}}\nimpl P for B{func value(self Self)int{return 22}}\nfunc F(a A,b B)int{return a.value()+b.value()}\n"
	result, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text)})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	edits, err := queries.Rename("fixture/main.gox", uint32(strings.LastIndex(text, "value()")), "answer")
	if err != nil || len(edits) != 5 {
		t.Fatalf("protocol family rename: %+v, %v", edits, err)
	}
	after, _ := analyzeQueries(t, applyQueryEdits(t, result, edits))
	if after.Err != nil {
		t.Fatalf("renamed protocol implementations: %v", after.Err)
	}
}

func TestRenameCrossPackageImportAndExplicitAlias(t *testing.T) {
	text := "package fixture\nuse api::add as local;\nfunc F()int{return local()}\n"
	dependency := "package dep\npub func add()int{return 42}\n"
	result, err := analysis.Analyze(context.Background(), analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture", Roots: paths.Roots{"api": {PackagePath: "dep", ModulePath: "dep"}}, Options: analysis.Options{Editor: true, SourcePackages: []analysis.SourcePackage{{PackagePath: "dep", ModulePath: "dep", RootPackage: "dep", Target: config.HostTarget(), Sources: map[string][]byte{"lib.gox": []byte(dependency)}}}}})
	if err != nil || result.Err != nil {
		t.Fatalf("analysis: %v / %v", err, result.Err)
	}
	queries := analysis.NewQueries(result)
	edits, err := queries.Rename("fixture/main.gox", uint32(strings.LastIndex(text, "local()")), "renamed")
	if err != nil || len(edits) != 2 {
		t.Fatalf("local import alias rename: %+v, %v", edits, err)
	}
	for _, edit := range edits {
		if edit.File != "fixture/main.gox" || text[edit.Span.Start:edit.Span.End] != "local" {
			t.Fatalf("alias rename changed the imported declaration: %+v", edit)
		}
	}
	edits, err = queries.Rename("dep/lib.gox", uint32(strings.Index(dependency, "add()")), "answer")
	if err != nil || len(edits) != 2 {
		t.Fatalf("imported declaration rename: %+v, %v", edits, err)
	}
	var definition, importedPath bool
	for _, edit := range edits {
		definition = definition || edit.File == "dep/lib.gox" && dependency[edit.Span.Start:edit.Span.End] == "add"
		importedPath = importedPath || edit.File == "fixture/main.gox" && text[edit.Span.Start:edit.Span.End] == "add"
	}
	if !definition || !importedPath {
		t.Fatalf("imported path or definition omitted: %+v", edits)
	}
}

func TestReferencesAndRenameRespectLocalShadowing(t *testing.T) {
	text := "package fixture\nfunc F(value int)int { total:=value; {total:=9; _=total}; return total }\n"
	result, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text)})
	offset := uint32(strings.LastIndex(text, "total"))
	refs, err := queries.FindReferences("fixture/main.gox", offset, false)
	if err != nil || refs == nil || len(refs.Locations) != 1 || int(refs.Locations[0].Span.Start) != int(offset) {
		t.Fatalf("references included a different binding: %+v, %v", refs, err)
	}
	edits, err := queries.Rename("fixture/main.gox", offset, "answer")
	if err != nil || len(edits) != 2 {
		t.Fatalf("rename: %+v, %v", edits, err)
	}
	sources := applyQueryEdits(t, result, edits)
	if got := string(sources["main.gox"]); !strings.Contains(got, "answer:=value") || !strings.Contains(got, "return answer") || !strings.Contains(got, "{total:=9; _=total}") {
		t.Fatalf("wrong source after rename: %s", got)
	}
	after, _ := analyzeQueries(t, sources)
	if after.Err != nil {
		t.Fatalf("renamed program: %v", after.Err)
	}
}

func TestRenameSourceFunctionAcrossFiles(t *testing.T) {
	declaration := "package fixture\nfunc value() int {return 42}\n"
	consumer := "package fixture\nfunc F() int {return value()}\n"
	result, queries := analyzeQueries(t, map[string][]byte{"value.gox": []byte(declaration), "main.gox": []byte(consumer)})
	edits, err := queries.Rename("fixture/main.gox", uint32(strings.Index(consumer, "value()")), "answer")
	if err != nil || len(edits) != 2 || edits[0].File == edits[1].File {
		t.Fatalf("cross-file edits: %+v, %v", edits, err)
	}
	after, _ := analyzeQueries(t, applyQueryEdits(t, result, edits))
	if after.Err != nil {
		t.Fatalf("renamed program: %v", after.Err)
	}
}

func TestRenameRejectsBindingCollisionsAndMacroOrigins(t *testing.T) {
	for _, test := range []struct{ source, token, rename string }{
		{"package fixture\nfunc F(value int)int {other:=2; return value+other}\n", "value+", "other"},
		{"package fixture\nfunc outer()int{return 2}\nfunc F(value int)int {return outer()+value}\n", "value}", "outer"},
		{"package fixture\nmacro_rules! make{()=>{func Generated()int{return 1}};}\nmake!()\nfunc F()int{return Generated()}\n", "Generated()}", "Renamed"},
		{"package fixture\nfunc F(value int)int {return value}\n", "value}", "match"},
	} {
		_, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(test.source)})
		if edits, err := queries.Rename("fixture/main.gox", uint32(strings.Index(test.source, test.token)), test.rename); err == nil {
			t.Fatalf("unsafe rename %s -> %s returned %+v", test.token, test.rename, edits)
		}
	}
}
