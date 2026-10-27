package analysis_test

import (
	"bytes"
	"context"
	"fmt"
	goast "go/ast"
	"go/importer"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
)

func TestPreservedSyntaxAndIncrementalParseReuse(t *testing.T) {
	sources := map[string][]byte{
		"main.gox":  []byte("package fixture\nuse {};\n#[cfg(any())]\nfunc Disabled() Missing {return missing}\nfunc Value()int{return Native()}\n"),
		"native.go": []byte("package fixture\nfunc Native()int{return 2}\n"),
	}
	input := analysis.Input{Sources: sources, PackagePath: "fixture", Options: analysis.Options{PreserveSyntax: true}}
	first, err := analysis.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	raw := first.Sources["fixture/main.gox"].Syntax
	if raw == nil || len(raw.Items) != 3 {
		t.Fatalf("lost raw declarations: %#v", raw)
	}
	if first.Files["fixture/main.gox"] == raw || len(first.Files["fixture/main.gox"].Items) != 1 {
		t.Fatal("configuration did not use a separate working tree")
	}
	if use, ok := raw.Items[0].(*syntax.UseDecl); !ok || use.Tree.Children == nil {
		t.Fatal("raw grouped use was normalized")
	}
	original := printer.Source(raw)
	native := first.Sources["fixture/native.go"].GoSyntax
	if native == nil || !strings.Contains(string(first.Sources["fixture/native.go"].Content), "return 2") {
		t.Fatal("lost native source")
	}
	function, ok := native.Decls[0].(*goast.FuncDecl)
	if !ok || function.Body == nil || len(function.Body.List) != 1 {
		t.Fatal("native declaration projection removed the original Go body")
	}
	input.Options.Previous = first
	second, err := analysis.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if second.Sources["fixture/main.gox"].Syntax != raw || second.Sources["fixture/native.go"].GoSyntax != native {
		t.Fatal("unchanged syntax was reparsed")
	}
	if second.Files["fixture/main.gox"] == first.Files["fixture/main.gox"] {
		t.Fatal("analyses share mutable working trees")
	}
	input.Sources = map[string][]byte{"main.gox": sources["main.gox"], "native.go": []byte("package fixture\nfunc Native()int{return 3}\n")}
	input.Options.Previous = second
	third, err := analysis.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if third.Sources["fixture/main.gox"].Syntax != raw || third.Sources["fixture/native.go"].GoSyntax == native {
		t.Fatal("parse reuse did not follow changed bytes")
	}
	firstGo, err := lower.Generate(context.Background(), first, lower.GenerationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	again, err := lower.Generate(context.Background(), first, lower.GenerationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstGo["fixture/main.gox"], again["fixture/main.gox"]) || printer.Source(raw) != original {
		t.Fatal("generation changed the retained analysis")
	}
	sources["main.gox"][0] = 'X'
	if first.Sources["fixture/main.gox"].Content[0] == 'X' {
		t.Fatal("published source bytes alias caller input")
	}
}

func TestLoadInputFreezesCrateAndNativeSources(t *testing.T) {
	directory := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		filename := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module fixture\n\ngo 1.27\n")
	write("lib.gox", "package fixture\nmod child;\npub func Value()int{return child::Value()+Native()}\n")
	write("child/value.gox", "package child\npub func Value()int{return 1}\n")
	write("native.go", "package fixture\nfunc Native()int{return 2}\n")
	environment, err := config.NewEnvironment(config.HostTarget(), os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	input, err := analysis.LoadInput(context.Background(), analysis.Input{Modules: []analysis.Module{{Path: "fixture", Dir: directory, StableGo: true, Roots: paths.Roots{}, Config: config.Config{Environment: environment}, NativePackage: &analysis.NativePackage{Importer: importer.Default()}, Crates: []analysis.Crate{{Name: "fixture", Root: "fixture", SourceDir: "fixture", Library: true}}}}, Options: analysis.Options{PreserveSyntax: true}})
	if err != nil {
		t.Fatal(err)
	}
	write("native.go", "this file changed after the snapshot")
	if err := os.Remove(filepath.Join(directory, "child/value.gox")); err != nil {
		t.Fatal(err)
	}
	result, err := analysis.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	output, err := lower.Generate(context.Background(), result, lower.GenerationOptions{StableGo: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "lib_gox.go")); !os.IsNotExist(err) {
		t.Fatal("analysis or generation wrote output")
	}
	target := t.TempDir()
	for name, data := range output {
		filename := filepath.Join(target, strings.TrimSuffix(strings.TrimPrefix(name, "fixture/"), ".gox")+".go")
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string][]byte{"go.mod": []byte("module fixture\n\ngo 1.27\n"), "native.go": result.Sources["fixture/native.go"].Content, "value_test.go": []byte("package fixture\nimport \"testing\"\nfunc TestValue(t *testing.T){if Value()!=3 {t.Fatal(Value())}}\n")} {
		if err := os.WriteFile(filepath.Join(target, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(environment.GoTool, "test", "-mod=readonly", "./...")
	command.Dir = target
	command.Env = append(environment.ExecEnv(), "GOTOOLCHAIN=local", "GOWORK=off")
	if log, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated fixed snapshot: %v\n%s", err, log)
	}
	input.Options.Previous = result
	input.Overlays = map[string][]byte{"fixture/child/value.gox": []byte("package child\npub func Value()int{return 4}\n")}
	updated, err := analysis.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Sources["fixture/lib.gox"].Syntax != result.Sources["fixture/lib.gox"].Syntax || updated.Sources["fixture/child/value.gox"].Syntax == result.Sources["fixture/child/value.gox"].Syntax {
		t.Fatal("overlay did not invalidate only its parsed source")
	}
}

func TestFailedAnalysisCannotGenerate(t *testing.T) {
	result, err := analysis.Analyze(context.Background(), analysis.Input{Sources: map[string][]byte{"main.gox": []byte("package p\nfunc F(value Missing){}\n")}, PackagePath: "p"})
	if err == nil || result == nil || result.Err == nil || len(result.Diagnostics) == 0 {
		t.Fatal("failed analysis lost its diagnostics")
	}
	if output, err := lower.Generate(context.Background(), result, lower.GenerationOptions{}); err == nil || output != nil {
		t.Fatal("partial analysis generated Go")
	}
}

func TestDiscoveryRetryRetainsTargetSelection(t *testing.T) {
	environment, err := config.NewEnvironment(config.HostTarget(), os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	input := analysis.Input{Files: map[string][]byte{
		"fixture/main.gox": []byte(fmt.Sprintf(`package fixture
macro_rules! modules {()=>{pub mod child;};}
modules!()
#[cfg(target_os(%q))]
pub func Value()int{return crate::child::Value()}
`, environment.Target.OS)),
		"fixture/child/value.gox": []byte("package child\npub func Value()int{return 42}\n"),
	}, Modules: []analysis.Module{{Path: "fixture", Dir: t.TempDir(), Roots: paths.Roots{}, Config: config.Config{Environment: environment}, NativePackage: &analysis.NativePackage{Importer: importer.Default()}, Crates: []analysis.Crate{{Root: "fixture", SourceDir: "fixture"}}}}}
	result, err := analysis.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	output, err := lower.Generate(context.Background(), result, lower.GenerationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(output["fixture/main.gox"]), "//go:build "+environment.Target.OS) {
		t.Fatalf("discovery retry lost target constraint:\n%s", output["fixture/main.gox"])
	}
}
