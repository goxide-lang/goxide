package typecheck

import (
	"context"
	"go/importer"
	"go/types"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/resolve"
)

func TestGeneratedGraphKeepsExternalTestsSeparate(t *testing.T) {
	result := &analysis.Result{Resolved: &resolve.Result{Packages: []*resolve.Package{
		{Dir: "fixture", Name: "fixture", Files: []*resolve.File{{Name: "fixture/value.gox"}}},
		{Dir: "fixture", Name: "fixture_test", Files: []*resolve.File{{Name: "fixture/value_test.gox"}}},
	}}}
	generated := map[string][]byte{
		"fixture/value.gox":      []byte("package fixture\nfunc Value() int { return 1 }\n"),
		"fixture/value_test.gox": []byte("package fixture_test\nimport \"fixture\"\nfunc testValue() int { return fixture.Value() }\n"),
	}
	packages := GeneratedSources(result, generated)
	if len(packages) != 2 {
		t.Fatalf("test package merged into importable package: %v", packages)
	}
	if err := CheckGeneratedGraph(t.Context(), packages, importer.Default(), types.SizesFor("gc", runtime.GOARCH)); err != nil {
		t.Fatal(err)
	}
	packages["unreferenced"] = map[string][]byte{"unreferenced/error.gox": []byte("package unreferenced\nfunc Broken() int {}\n")}
	if err := CheckGeneratedGraph(t.Context(), packages, importer.Default(), types.SizesFor("gc", runtime.GOARCH)); err == nil || !strings.Contains(err.Error(), "missing return") {
		t.Fatalf("unchecked independent package: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := CheckGeneratedGraph(ctx, packages, importer.Default(), types.SizesFor("gc", runtime.GOARCH)); err != context.Canceled {
		t.Fatalf("canceled graph: %v", err)
	}
}
