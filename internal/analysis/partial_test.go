package analysis_test

import (
	"context"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
)

func TestIncompleteBodyKeepsCurrentSignatureAndSiblingQueries(t *testing.T) {
	for _, broken := range []string{"value.", "println(", "Missing::", "if true {"} {
		t.Run(broken, func(t *testing.T) {
			text := "package fixture\nfunc Broken(value int)int{ " + broken + "\n}\nfunc Healthy(input string)string{return input}\n"
			result, err := analysis.Analyze(context.Background(), analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture", Options: analysis.Options{Editor: true}})
			if err != nil || result == nil || result.Err == nil || len(result.Diagnostics) == 0 {
				t.Fatalf("analysis = %#v, %v", result, err)
			}
			queries := analysis.NewQueries(result)
			hover, err := queries.Hover("fixture/main.gox", uint32(strings.Index(text, "Broken(")))
			if err != nil || hover == nil || !strings.Contains(hover.Text, "func Broken(value int) int") {
				t.Fatalf("signature = %#v, %v; diagnostics %v", hover, err, result.Diagnostics)
			}
			hover, err = queries.Hover("fixture/main.gox", uint32(strings.Index(text, "Healthy(")))
			if err != nil || hover == nil || !strings.Contains(hover.Text, "func Healthy(input string) string") {
				t.Fatalf("sibling = %#v, %v; diagnostics %v", hover, err, result.Diagnostics)
			}
			if result.Projection == nil || !result.Projection.ProjectionOnly {
				t.Fatalf("missing editor projection: %v", result.Diagnostics)
			}
			if output, err := lower.Generate(context.Background(), result, lower.GenerationOptions{}); err == nil || output != nil {
				t.Fatal("invalid original input generated executable output")
			}
			if _, err := lower.Generate(context.Background(), result.Projection, lower.GenerationOptions{}); err != nil {
				t.Fatalf("projection lowering: %v", err)
			}
		})
	}
}

func TestUnknownSignatureDoesNotReusePreviousFacts(t *testing.T) {
	input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte("package fixture\nfunc F(value int)int{return value}\nfunc Healthy()string{return \"ok\"}\n")}, PackagePath: "fixture", Options: analysis.Options{Editor: true}}
	previous, err := analysis.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	text := "package fixture\nfunc F(value [])int{return value}\nfunc Healthy()string{return \"ok\"}\n"
	input.Sources["main.gox"], input.Options.Previous = []byte(text), previous
	result, err := analysis.Analyze(context.Background(), input)
	if err != nil || result.Err == nil {
		t.Fatalf("analysis = %#v, %v", result, err)
	}
	queries := analysis.NewQueries(result)
	if hover, err := queries.Hover("fixture/main.gox", uint32(strings.Index(text, "F(value"))); err == nil || hover != nil {
		t.Fatalf("unknown signature = %#v, %v", hover, err)
	}
	if hover, err := queries.Hover("fixture/main.gox", uint32(strings.Index(text, "Healthy()"))); err != nil || hover == nil {
		t.Fatalf("healthy signature = %#v, %v; %v", hover, err, result.Diagnostics)
	}
	if result.Projection != nil {
		t.Fatal("unknown signature produced a complete projection")
	}
}

func TestConflictingImplBlocksSelectionAndProjection(t *testing.T) {
	text := "package fixture\ntrait P{func get(self Self)int}\ntype S struct{}\nimpl P for S{func get(self Self)int{return 1}}\nimpl P for S{func get(self Self)int{return 2}}\nfunc Healthy()int{return 3}\nfunc F(s S)int{return s.get()}\n"
	result, err := analysis.Analyze(context.Background(), analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture", Options: analysis.Options{Editor: true}})
	if err != nil || result.Err == nil {
		t.Fatalf("analysis = %#v, %v", result, err)
	}
	queries := analysis.NewQueries(result)
	if definitions, err := queries.Definition("fixture/main.gox", uint32(strings.Index(text, "s.get()")+2)); err == nil || len(definitions) != 0 {
		t.Fatalf("conflicting selection = %#v, %v", definitions, err)
	}
	if hover, err := queries.Hover("fixture/main.gox", uint32(strings.Index(text, "Healthy()"))); err != nil || hover == nil {
		t.Fatalf("healthy signature = %#v, %v", hover, err)
	}
	if result.Projection != nil {
		t.Fatal("conflicting impl produced a projection")
	}
}

func TestNativeBodyRecoveryKeepsSourceSignature(t *testing.T) {
	native := "package fixture\nfunc Native(value int)int{value.}\nfunc Good()string{return \"good\"}\n"
	text := "package fixture\nfunc F()int{return Native(1)}\n"
	result, err := analysis.Analyze(context.Background(), analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text), "native.go": []byte(native)}, PackagePath: "fixture", Options: analysis.Options{Editor: true}})
	if err != nil || result == nil || result.Err == nil {
		t.Fatalf("analysis = %#v, %v", result, err)
	}
	hover, err := analysis.NewQueries(result).Hover("fixture/main.gox", uint32(strings.Index(text, "Native(1)")))
	if err != nil || hover == nil || !strings.Contains(hover.Text, "func Native(value int) int") {
		t.Fatalf("native signature = %#v, %v; %v", hover, err, result.Diagnostics)
	}
	if result.Projection != nil {
		t.Fatal("invalid native source claimed a fully checked projection")
	}
}

func TestFailedMacroDoesNotDiscardIndependentPackage(t *testing.T) {
	dependency := "package dep\npub func Healthy(value string)string{return value}\n"
	result, err := analysis.Analyze(context.Background(), analysis.Input{Sources: map[string][]byte{"main.gox": []byte("package fixture\nmissing!()\nfunc F()int{return 1}\n")}, PackagePath: "fixture", Options: analysis.Options{Editor: true, SourcePackages: []analysis.SourcePackage{{PackagePath: "dep", ModulePath: "dep", Target: config.HostTarget(), Sources: map[string][]byte{"lib.gox": []byte(dependency)}}}}})
	if err != nil || result == nil || result.Err == nil {
		t.Fatalf("analysis = %#v, %v", result, err)
	}
	hover, err := analysis.NewQueries(result).Hover("dep/lib.gox", uint32(strings.Index(dependency, "Healthy(")))
	if err != nil || hover == nil || !strings.Contains(hover.Text, "func Healthy(value string) string") {
		t.Fatalf("independent package = %#v, %v; %v", hover, err, result.Diagnostics)
	}
	if result.Projection != nil {
		t.Fatal("failed macro generated a reliable projection")
	}
}
