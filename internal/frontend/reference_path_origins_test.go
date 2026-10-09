package frontend

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/sourcemap"
	"github.com/hxsf/goxide/internal/typecheck"
)

func TestStaticUseImportOriginsSurviveFormatting(t *testing.T) {
	source := []byte("package p\nuse fmt as a;\nfunc Run(){f:=a::Sprint;_=f}\n")
	result, err := preparePackageContext("example/p", map[string][]byte{"use.gox": source}, nil, typecheck.NewImporter("use.gox"), paths.Roots{"fmt": {PackagePath: "fmt"}})
	if err != nil {
		t.Fatal(err)
	}
	r := result.Files["use.gox"]
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "use.gox", r.Go, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Imports) != 1 || f.Imports[0].Name == nil {
		t.Fatalf("expected one named namespace import: %s", r.Go)
	}
	for _, pos := range []token.Pos{f.Imports[0].Name.Pos(), f.Imports[0].Path.Pos()} {
		segment := r.Diagnostics.Lookup(fs.Position(pos).Offset)
		if segment == nil || segment.Origin == nil || segment.Kind != "use import" || segment.Origin.File != "use.gox" || segment.Origin.Line != 2 || segment.Origin.Column != 1 {
			t.Fatalf("use cause lost after package formatting: %#v", segment)
		}
	}
}

func TestCausalImportDoesNotAuthorizeCopiedToken(t *testing.T) {
	original := []byte("package p\nuse fmt;\n")
	prepared := []byte("package p\nimport generatedAlias \"fmt\"\n")
	aliasStart, aliasEnd := 17, 31
	origin := sourcemap.Position("use.gox", original, 10, 13)
	span := sourcemap.Position("use.gox", prepared, aliasStart, aliasEnd)
	mapping := &sourcemap.Map{Source: prepared}
	mapping.Attach(aliasStart, aliasEnd, &origin, nil, "use import")
	mapper := macroSourceMapper{filename: "use.gox", original: original, prepared: mapping}
	if got := mapper.causalImport(span, false); got == nil || *got != origin {
		t.Fatalf("lost exact import cause: %v", got)
	}
	if mapper.point(span, false) != nil || mapper.causalImport(span, true) != nil {
		t.Fatal("causal import was promoted to copied/original token evidence")
	}
	partial := sourcemap.Position("use.gox", prepared, aliasStart, aliasEnd-1)
	if mapper.causalImport(partial, false) != nil {
		t.Fatal("partial token borrowed an import cause")
	}
	mapping.Segments[0].Kind = "namespace-path"
	if mapper.causalImport(span, false) != nil {
		t.Fatal("namespace envelope borrowed an import cause")
	}
}
