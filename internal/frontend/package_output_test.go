package frontend

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
	"github.com/hxsf/goxide/internal/testdeps"
	"github.com/hxsf/goxide/internal/typecheck"
)

func TestPackageGoFilesPreservesBuildSelectionAndOriginal(t *testing.T) {
	const name = "model_linux_amd64.gox"
	imp := typecheck.NewImporter("playground.gox")
	result, err := CompilePackage("playground", map[string][]byte{name: []byte(`//go:build feature

// Package p contains a selected model.
package p
#[derive(Clone)]
type Item struct{ N int }
`)}, imp)
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), result.Files[name].Go...)
	originalMap := append([]byte(nil), result.Files[name].SourceMap...)
	files, err := PackageGoFiles("playground", result, imp)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("missing helper: %v", files)
	}
	if !bytes.Equal(original, result.Files[name].Go) || !bytes.Equal(originalMap, result.Files[name].SourceMap) {
		t.Fatal("emission changed frontend source or map")
	}
	dir := t.TempDir()
	for path, r := range files {
		if !strings.Contains(string(r.Go), "//go:build feature") || !strings.Contains(string(r.Go), "// Package p contains a selected model.") {
			t.Fatalf("lost leading comments in %s:\n%s", path, r.Go)
		}
		path = strings.TrimSuffix(path, ".gox")
		if !strings.HasSuffix(path, ".go") {
			path += ".go"
		}
		if !strings.HasSuffix(path, "_linux_amd64.go") {
			t.Fatalf("lost platform suffix: %s", path)
		}
		if err := os.WriteFile(filepath.Join(dir, path), r.Go, 0600); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			os, arch string
			tags     []string
			want     bool
		}{
			{"linux", "amd64", []string{"feature"}, true},
			{"linux", "amd64", nil, false},
			{"windows", "amd64", []string{"feature"}, false},
			{"linux", "arm64", []string{"feature"}, false},
		} {
			ctx := build.Default
			ctx.GOOS, ctx.GOARCH, ctx.BuildTags = tc.os, tc.arch, tc.tags
			got, err := ctx.MatchFile(dir, path)
			if err != nil || got != tc.want {
				t.Fatalf("%s %+v: %v, %v", path, tc, got, err)
			}
		}
	}
}

func TestPackageGoFilesImportBindingsAndCollision(t *testing.T) {
	imp := typecheck.NewImporter("playground.gox")
	// Two imports of the same path have distinct file bindings. A helper split
	// must not preserve the unused alias simply because the path is still used.
	r := PackageResult{Files: map[string]Result{"main.gox": {
		Go: []byte(`package p
import first "strings"
import second "strings"
func Main()string{return first.ToUpper("main")}
func CloneItem()string{return second.ToLower("HELPER")}
`), generatedHelpers: map[string]bool{"CloneItem": true},
	}}}
	files, err := PackageGoFiles("playground", r, imp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["main.gox"].Go), "import second") || strings.Contains(string(files["goxide_helpers_main.go"].Go), "import first") {
		t.Fatal("file import bindings were merged")
	}
	r.Files["goxide_helpers_main.go"] = Result{Go: []byte("package p\n")}
	if _, err := PackageGoFiles("playground", r, imp); err == nil || !strings.Contains(err.Error(), "filename conflicts") {
		t.Fatalf("helper collision: %v", err)
	}
}

func TestPackageHelperRejectsUnselectedNativeCollision(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":                  "module playground\ngo 1.27\n",
		"model.gox":               "package p\n#[derive(Clone)]\ntype Item struct{N int}\n",
		"goxide_helpers_model.go": "//go:build never_selected\n\npackage p\nconst NativeSentinel = 42\n",
	} {
		if err := testdeps.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(t.TempDir(), "out")
	if _, err := CompileModule(dir, dest); err == nil || !strings.Contains(err.Error(), "generated helper filename conflicts with existing output") {
		t.Fatalf("unselected native source overwritten: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("failed output was retained")
	}
	data, err := os.ReadFile(filepath.Join(dir, "goxide_helpers_model.go"))
	if err != nil || !strings.Contains(string(data), "NativeSentinel") {
		t.Fatalf("source changed: %v", err)
	}
}

// Helper extraction is a structural rewrite: identical repeated macro calls
// must keep their own origins/stacks, and generated helpers cannot borrow them.
func TestPackageGoFilesPreservesMacroProvenance(t *testing.T) {
	for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
		for _, nested := range []bool{false, true} {
			name := "direct"
			if nested {
				name = "nested"
			}
			t.Run(ending.name+"/"+name, func(t *testing.T) {
				definitions := "macro_rules! zero {()=>{int(0)};}\n"
				macro, frames := "zero", 1
				if nested {
					definitions += "macro_rules! outer {()=>{zero!()};}\n"
					macro, frames = "outer", 2
				}
				sources := map[string][]byte{
					"definitions.gox": []byte("package p\n" + definitions),
					"caller.gox":      []byte("package p\n#[derive(PartialEq,Eq)]\ntype Item struct{N int}\nfunc Run()int{return " + macro + "!()+" + macro + "!()}\nfunc Native()int{return int(0)}\nfunc Compare(a,b Item)bool{return a.equals(b)}\n"),
				}
				for name, source := range sources {
					sources[name] = bytes.ReplaceAll(source, []byte("\n"), []byte(ending.text))
				}
				imp := typecheck.NewImporter("playground.gox")
				result, err := CompilePackage("example/provenance", sources, imp)
				if err != nil {
					t.Fatal(err)
				}
				original := result.Files["caller.gox"]
				originalMap, originalSource := append([]byte(nil), original.SourceMap...), append([]byte(nil), original.Diagnostics.Source...)
				calls := func(r Result) map[string][]sourcemap.Segment {
					fs := token.NewFileSet()
					f, err := parser.ParseFile(fs, "caller.go", r.Go, 0)
					if err != nil {
						t.Fatal(err)
					}
					found := map[string][]sourcemap.Segment{}
					for _, d := range f.Decls {
						fn, ok := d.(*ast.FuncDecl)
						if !ok || (fn.Name.Name != "Run" && fn.Name.Name != "Native") {
							continue
						}
						ast.Inspect(fn.Body, func(n ast.Node) bool {
							if call, ok := n.(*ast.CallExpr); ok {
								for _, node := range []ast.Node{call.Fun, call.Args[0]} {
									segment := r.Diagnostics.Lookup(fs.Position(node.Pos()).Offset)
									if segment == nil {
										t.Fatalf("missing token evidence in %s", fn.Name.Name)
									}
									copy := *segment
									copy.Start, copy.End = 0, 0 // Output offsets intentionally change.
									found[fn.Name.Name] = append(found[fn.Name.Name], copy)
								}
							}
							return true
						})
					}
					return found
				}
				before := calls(original)
				if len(before["Run"]) != 4 || len(before["Native"]) != 2 {
					t.Fatal("missing probe calls")
				}
				for name, tokens := range before {
					for index, token := range tokens {
						want := 0
						if name == "Run" {
							want = frames
						}
						// Primitive alias normalization may retain only the macro
						// context on its rewritten callee. Arguments/native tokens
						// still have point origins; never invent one for the callee.
						wantOrigin := name == "Native" || index%2 == 1
						if (wantOrigin && token.Origin == nil) || len(token.Stack) != want {
							t.Fatalf("invalid initial %s evidence: %+v", name, token)
						}
					}
				}
				if reflect.DeepEqual(before["Run"][0].Stack, before["Run"][2].Stack) {
					t.Fatal("repeated calls lost distinct callsites")
				}
				emitted, err := PackageGoFiles("example/provenance", result, imp)
				if err != nil {
					t.Fatal(err)
				}
				if after := calls(emitted["caller.gox"]); !reflect.DeepEqual(before, after) {
					t.Fatalf("helper extraction changed origins/stacks:\nbefore=%+v\nafter=%+v", before, after)
				}
				helper, ok := emitted["goxide_helpers_caller.go"]
				if !ok || !strings.Contains(string(helper.Go), "func EqualsItem") {
					t.Fatal("missing equality helper")
				}
				fs := token.NewFileSet()
				f, err := parser.ParseFile(fs, "helper.go", helper.Go, 0)
				if err != nil {
					t.Fatal(err)
				}
				for _, d := range f.Decls {
					fn, ok := d.(*ast.FuncDecl)
					if !ok || fn.Name.Name != "EqualsItem" {
						continue
					}
					start, end := fs.Position(fn.Pos()).Offset, fs.Position(fn.End()).Offset
					for _, s := range helper.Diagnostics.Segments {
						if s.Start >= start && s.End <= end && (s.Origin != nil || len(s.Stack) != 0) {
							t.Fatalf("generated helper borrowed source: %+v", s)
						}
					}
				}
				if !bytes.Equal(originalMap, result.Files["caller.gox"].SourceMap) || !bytes.Equal(originalSource, result.Files["caller.gox"].Diagnostics.Source) {
					t.Fatal("emission mutated original provenance")
				}
			})
		}
	}
}
