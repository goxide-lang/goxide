package acceptance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/hxsf/goxide/internal/rootconfig"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
)

// Primary token provenance and a proven expansion chain are independent. These
// public-CLI cases keep synthetic points unmapped without losing their frames.
func TestMacroSyntheticOriginCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, goTool, true, nil, "build", "-o", cli, "./cmd/goxide")
	outside := t.TempDir()

	for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
		for _, tc := range []struct{ name, mode, body, point string }{
			{"single-nested-enum", "single", "new($x?)", ""},
			{"directory-same-file", "directory", "new($x?)", ""},
			{"same-package-multifile", "multifile", "new($x?)", ""},
			{"cross-package-same-basename", "cross", "new($x?)", ""},
			{"make-synthetic-type-slot", "single", "make($x?,1)", ""},
			{"exact-token-after-question", "single", "func()Option[int]{_=$x?;_=new(7);return Option[int]::None}()", "7"},
			{"cross-package-exact-token", "cross", "new(7)", "7"},
		} {
			t.Run(ending.name+"/"+tc.name, func(t *testing.T) {
				source, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				writeMacroFixtureRoots(t, source, nil)
				const earlier = "func value()Option[int]{return Option[int]::Some(1)}\nfunc before()Option[int]{a:=value()?;b:=value()?;return Option[int]::Some(a+b)}\n"
				const enumeration = "enum event { A, B(int), }\n"
				definitions := "macro_rules! inner {($x:expr)=>{" + tc.body + "};}\nmacro_rules! outer {($x:expr)=>{inner!($x)};}\n"
				call := "outer!(value())"
				caller := "func Run()Option[int]{_=" + call + "?;return Option[int]::None}\n"
				files := map[string]string{}
				definitionFile, callerFile := "main.gox", "main.gox"
				switch tc.mode {
				case "single", "directory":
					files["main.gox"] = "package main\n" + earlier + enumeration + definitions + caller
				case "multifile":
					definitionFile = "definitions.gox"
					files[definitionFile] = "package main\n" + earlier + definitions
					files[callerFile] = "package main\n" + enumeration + caller
				case "cross":
					definitionFile = "defs/main.gox"
					definitions = strings.Replace(definitions, "macro_rules! outer", "pub macro_rules! outer", 1)
					files[definitionFile] = "package defs\n" + earlier + definitions
					call = "m::outer!(value())"
					// The other package deliberately repeats the basename, macro
					// name and literal. None is evidence for the dependency token.
					files[callerFile] = "package main\nuse defs as m;\n" + earlier + enumeration + "macro_rules! inner {()=>{new(7)};}\n" + strings.Replace(caller, "outer!(value())", call, 1)
				}
				for file, text := range files {
					files[file] = strings.ReplaceAll(text, "\n", ending.text)
					packagesWrite(t, source, file, files[file])
				}
				input := filepath.Join(source, "main.gox")
				args := []string{"transpile", input}
				if tc.mode != "single" {
					packagesWrite(t, source, "go.mod", "module origin.example/app\ngo 1.27\n")
					writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
						"defs": {Module: "origin.example/app", Package: "origin.example/app/defs"},
					})
					args = []string{"transpile", "-o", filepath.Join(t.TempDir(), "generated"), source}
				}
				output := deliveryCommand(t, outside, cli, false, nil, args...)
				if !strings.Contains(output, "fixed predeclared type syntax") {
					t.Fatalf("wrong rejection: %s", output)
				}
				if tc.point == "" {
					if !strings.Contains(output, "generated/unmapped") || strings.Contains(output, "source span [") {
						t.Fatalf("synthetic token acquired a source span: %s", output)
					}
				} else {
					text := files[definitionFile]
					start := strings.Index(text, "new(7)") + len("new(")
					want := syntheticOriginPosition(filepath.Join(source, definitionFile), text, start) + ":"
					if !strings.HasPrefix(output, want) || !strings.Contains(output, fmt.Sprintf("source span [%d,%d)", start, start+1)) || strings.Contains(output, "generated/unmapped") {
						t.Fatalf("wrong original literal provenance, want %s: %s", want, output)
					}
				}
				loc := func(file, token string) string {
					return syntheticOriginPosition(filepath.Join(source, file), files[file], strings.Index(files[file], token))
				}
				wantFrames := []string{
					"  macro outer defined at " + loc(definitionFile, "macro_rules! outer") + "; invoked at " + loc(callerFile, call),
					"  macro inner defined at " + loc(definitionFile, "macro_rules! inner") + "; invoked at " + loc(definitionFile, "inner!($x)"),
				}
				syntheticOriginFrames(t, output, wantFrames)
			})
		}

		for _, synthetic := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/native-error-synthetic-%v", ending.name, synthetic), func(t *testing.T) {
				source, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				writeMacroFixtureRoots(t, source, nil)
				pattern, body, call := "$n:expr", "make([]int,$n)", `allocate!("bad")`
				if !synthetic {
					pattern = ""
					body = `make([]int,"bad")`
					call = "allocate!()"
				}
				text := strings.ReplaceAll("package main\nmacro_rules! allocate {("+pattern+")=>{"+body+"};}\nfunc main(){_="+call+"}\n", "\n", ending.text)
				packagesWrite(t, source, "main.gox", text)
				input := filepath.Join(source, "main.gox")
				generated := filepath.Join(t.TempDir(), "main.go")
				mapFile := generated + ".map.json"
				deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, "--source-map", mapFile, input)
				output := deliveryCommand(t, outside, cli, false, nil, "build", "--go", goTool, "--runtime", root, "-o", filepath.Join(t.TempDir(), "rejected"), input)
				if !strings.Contains(output, "./main.go:") || !strings.Contains(output, "cannot convert") || !strings.Contains(output, "generated Go build") {
					t.Fatalf("native stderr was lost: %s", output)
				}
				wantFrames := []string{"  macro allocate defined at " + syntheticOriginPosition(input, text, strings.Index(text, "macro_rules! allocate")) + "; invoked at " + syntheticOriginPosition(input, text, strings.Index(text, call))}
				syntheticOriginFrames(t, output, wantFrames)
				goBytes, err := os.ReadFile(generated)
				if err != nil {
					t.Fatal(err)
				}
				mapBytes, err := os.ReadFile(mapFile)
				if err != nil {
					t.Fatal(err)
				}
				var exported struct {
					Diagnostics sourcemap.Map `json:"diagnostics"`
				}
				if err = json.Unmarshal(mapBytes, &exported); err != nil {
					t.Fatal(err)
				}
				match := regexp.MustCompile(`(?m)^\./main\.go:(\d+):(\d+):`).FindStringSubmatch(output)
				if match == nil {
					t.Fatal(output)
				}
				line, _ := strconv.Atoi(match[1])
				column, _ := strconv.Atoi(match[2])
				offset := 0
				for n := 1; n < line; n++ {
					next := bytes.IndexByte(goBytes[offset:], '\n')
					if next < 0 {
						t.Fatal("bad native line")
					}
					offset += next + 1
				}
				offset += column - 1
				segment := exported.Diagnostics.Lookup(offset)
				if segment == nil || len(segment.Stack) != 1 {
					t.Fatalf("JSON map lost native error's proven frame: %#v", segment)
				}
				frame := segment.Stack[0]
				if "  macro "+frame.Macro+" defined at "+fmt.Sprintf("%s:%d:%d", frame.Definition.File, frame.Definition.Line, frame.Definition.Column)+"; invoked at "+fmt.Sprintf("%s:%d:%d", frame.Callsite.File, frame.Callsite.Line, frame.Callsite.Column) != wantFrames[0] {
					t.Fatalf("text/map stack disagree: %#v", frame)
				}
				if synthetic {
					if segment.Origin != nil || segment.Kind != "generated/unmapped" || !strings.Contains(output, "generated/unmapped") {
						t.Fatalf("invented synthetic origin: %#v\n%s", segment, output)
					}
				} else {
					start := strings.Index(text, `"bad"`)
					if segment.Origin == nil || segment.Origin.File != input || segment.Origin.Start != start || segment.Origin.End != start+len(`"bad"`) || strings.Contains(output, "generated/unmapped") {
						t.Fatalf("lost exact template literal: %#v\n%s", segment, output)
					}
				}
			})
		}
	}

	t.Run("legal-output-unchanged", func(t *testing.T) {
		source := t.TempDir()
		writeMacroFixtureRoots(t, source, nil)
		packagesWrite(t, source, "main.gox", `package main
use fmt;
macro_rules! buffer {($n:expr)=>{make([]int,$n)};}
macro_rules! pointer {()=>{new(int)};}
func main(){fmt::Println(len(buffer!(2)),*pointer!())}
`)
		input := filepath.Join(source, "main.gox")
		first := filepath.Join(t.TempDir(), "main.go")
		second := filepath.Join(t.TempDir(), "main.go")
		for _, output := range []string{first, second} {
			deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", output, "--source-map", output+".map.json", input)
		}
		for _, suffix := range []string{"", ".map.json"} {
			a, err := os.ReadFile(first + suffix)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(second + suffix)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, b) {
				t.Fatalf("repeated output differs: %s", suffix)
			}
		}
		packagesWrite(t, filepath.Dir(first), "go.mod", "module origin.example/legal\ngo 1.27\n")
		if output := deliveryCommand(t, filepath.Dir(first), goTool, true, nil, "run", "-race", "."); output != "2 0\n" {
			t.Fatal(output)
		}
	})
}

func syntheticOriginPosition(file, text string, offset int) string {
	if offset < 0 {
		panic("missing expected fixture token")
	}
	line := strings.Count(text[:offset], "\n") + 1
	column := offset - strings.LastIndex(text[:offset], "\n")
	return fmt.Sprintf("%s:%d:%d", file, line, column)
}

func syntheticOriginFrames(t *testing.T, output string, want []string) {
	t.Helper()
	var got []string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "  macro ") {
			got = append(got, line)
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("wrong complete expansion chain\nwant:\n%s\ngot:\n%s\n%s", strings.Join(want, "\n"), strings.Join(got, "\n"), output)
	}
}
